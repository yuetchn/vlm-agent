//go:build windows

package capture

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	user32 = windows.NewLazySystemDLL("user32.dll")

	procBitBlt                  = gdi32.NewProc("BitBlt")
	procCreateCompatibleDC      = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap  = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject            = gdi32.NewProc("SelectObject")
	procGetDIBits               = gdi32.NewProc("GetDIBits")
	procDeleteDC                = gdi32.NewProc("DeleteDC")
	procDeleteObject            = gdi32.NewProc("DeleteObject")

	procGetDesktopWindow = user32.NewProc("GetDesktopWindow")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	DIB_RGB_COLORS = 0
	SRCCOPY        = 0x00CC0020
	SM_CXSCREEN    = 0
	SM_CYSCREEN    = 1
)

// BITMAPINFOHEADER 对应 Windows SDK 的 BITMAPINFOHEADER 结构体
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// BITMAPINFO 包含 BITMAPINFOHEADER
type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// gdiCapture 使用 GDI BitBlt 执行全屏截图，返回 PNG 字节。
// 截图流程：GetDesktopWindow → GetDC → CreateCompatibleDC
// → CreateCompatibleBitmap(width, height) → SelectObject
// → BitBlt(SRCCOPY) → GetDIBits → 转 image.RGBA → png.Encode
func gdiCapture() ([]byte, error) {
	// 获取屏幕尺寸
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	width := int(screenW)
	height := int(screenH)

	if width == 0 || height == 0 {
		return nil, fmt.Errorf("capture gdi: invalid screen size %dx%d", width, height)
	}

	// 获取桌面窗口句柄
	hwnd, _, _ := procGetDesktopWindow.Call()

	// 获取屏幕 DC
	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		return nil, fmt.Errorf("capture gdi: GetDC failed")
	}
	defer procReleaseDC.Call(hwnd, hdc)

	// 创建内存 DC
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("capture gdi: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	// 创建兼容位图
	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if hBitmap == 0 {
		return nil, fmt.Errorf("capture gdi: CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hBitmap)

	// 将位图选入内存 DC（P8: 校验 SelectObject 返回值）
	oldObj, _, _ := procSelectObject.Call(memDC, hBitmap)
	if oldObj == 0 {
		return nil, fmt.Errorf("capture gdi: SelectObject failed")
	}
	defer procSelectObject.Call(memDC, oldObj)

	// BitBlt 拷贝屏幕内容到内存 DC
	ret, _, err := procBitBlt.Call(
		memDC, 0, 0, uintptr(width), uintptr(height),
		hdc, 0, 0,
		uintptr(SRCCOPY),
	)
	if ret == 0 {
		return nil, fmt.Errorf("capture gdi: BitBlt failed: %w", err)
	}

	// 准备 BITMAPINFO（32 位 BGRA，由 Windows 倒置 Y 轴）
	bi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:      uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:     int32(width),
			BiHeight:    -int32(height), // 负值表示从上到下（top-down DIB）
			BiPlanes:    1,
			BiBitCount:  32,
			BiCompression: 0, // BI_RGB
		},
	}

	// P4: 防止超大分辨率下 width*height*4 整数溢出
	const maxPixelBufSize = 256 * 1024 * 1024 // 256 MB 上限（覆盖 8K 分辨率）
	if int64(width)*int64(height)*4 > maxPixelBufSize {
		return nil, fmt.Errorf("capture gdi: screen resolution too large (%dx%d)", width, height)
	}

	// 分配像素缓冲区（每像素 4 字节 BGRA）
	pixelBuf := make([]byte, width*height*4)

	// P2: GetDIBits 须传 memDC（位图已选入其中），而非 hdc（屏幕 DC）
	ret, _, err = procGetDIBits.Call(
		memDC,
		hBitmap,
		0, uintptr(height),
		uintptr(unsafe.Pointer(&pixelBuf[0])),
		uintptr(unsafe.Pointer(&bi)),
		uintptr(DIB_RGB_COLORS),
	)
	// P5: 校验实际读取的扫描行数是否等于 height
	if ret == 0 {
		return nil, fmt.Errorf("capture gdi: GetDIBits failed: %w", err)
	}
	if ret != uintptr(height) {
		return nil, fmt.Errorf("capture gdi: GetDIBits incomplete: read %d of %d scanlines", ret, height)
	}

	// 将 BGRA 转换为 RGBA image.NRGBA
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := (y*width + x) * 4
			b := pixelBuf[offset+0]
			g := pixelBuf[offset+1]
			r := pixelBuf[offset+2]
			// offset+3 是 alpha，GDI 通常为 0，强制设为 255
			pixOffset := img.PixOffset(x, y)
			img.Pix[pixOffset+0] = r
			img.Pix[pixOffset+1] = g
			img.Pix[pixOffset+2] = b
			img.Pix[pixOffset+3] = 255
		}
	}

	// 编码为 PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("capture gdi: png encode: %w", err)
	}

	return buf.Bytes(), nil
}
