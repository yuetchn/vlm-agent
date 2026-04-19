//go:build windows

package input

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/rs/zerolog"
	"golang.org/x/sys/windows"

	"github.com/zerfx/new_jzd/internal/logger"
)

// Windows INPUT 类型常量
const (
	INPUT_MOUSE    = 0
	INPUT_KEYBOARD = 1

	MOUSEEVENTF_MOVE      = 0x0001
	MOUSEEVENTF_LEFTDOWN  = 0x0002
	MOUSEEVENTF_LEFTUP    = 0x0004
	MOUSEEVENTF_ABSOLUTE  = 0x8000

	KEYEVENTF_KEYUP        = 0x0002
	KEYEVENTF_EXTENDEDKEY  = 0x0001

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procSendInput    = user32.NewProc("SendInput")
	procGetSysMetrics = user32.NewProc("GetSystemMetrics")
)

// MOUSEINPUT 对应 Windows SDK 的 MOUSEINPUT 结构体
type MOUSEINPUT struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// KEYBDINPUT 对应 Windows SDK 的 KEYBDINPUT 结构体
type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// INPUT 联合体（Windows SDK INPUT struct）
// Go 没有原生 union，使用固定大小数组覆盖所有字段（64-bit：type=4字节 + 最大 union=32字节 = 40字节）
type INPUT struct {
	Type uint32
	// 联合体内容：使用 [32]byte 覆盖鼠标/键盘数据
	// 通过 unsafe.Pointer 转换为具体结构体
	data [32]byte
}

// P9: 编译期 ABI 布局断言——MOUSEINPUT 和 KEYBDINPUT 必须能放入 INPUT.data [32]byte。
// 若结构体定义与平台 ABI 不符，此 init 会在进程启动时立即 panic，而非静默产生错误输入。
func init() {
	const dataSize = uintptr(32)
	if s := unsafe.Sizeof(MOUSEINPUT{}); s > dataSize {
		panic(fmt.Sprintf("input: MOUSEINPUT (%d bytes) exceeds INPUT.data buffer (%d bytes)", s, dataSize))
	}
	if s := unsafe.Sizeof(KEYBDINPUT{}); s > dataSize {
		panic(fmt.Sprintf("input: KEYBDINPUT (%d bytes) exceeds INPUT.data buffer (%d bytes)", s, dataSize))
	}
}

func (i *INPUT) setMouse(mi MOUSEINPUT) {
	*(*MOUSEINPUT)(unsafe.Pointer(&i.data[0])) = mi
}

func (i *INPUT) setKeyboard(ki KEYBDINPUT) {
	*(*KEYBDINPUT)(unsafe.Pointer(&i.data[0])) = ki
}

// Controller 通过 Windows user32.dll SendInput 实现 InputController 接口。
type Controller struct {
	logger zerolog.Logger
}

// NewController 创建 Windows 输入控制器。
func NewController(log zerolog.Logger) *Controller {
	return &Controller{logger: log}
}

// getScreenSize 获取主屏幕分辨率（用于绝对坐标转换）
func getScreenSize() (width, height int) {
	w, _, _ := procGetSysMetrics.Call(uintptr(SM_CXSCREEN))
	h, _, _ := procGetSysMetrics.Call(uintptr(SM_CYSCREEN))
	return int(w), int(h)
}

// sendInput 调用 Windows SendInput API
func sendInput(inputs []INPUT) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(INPUT{})),
	)
	if n == 0 {
		return fmt.Errorf("input: SendInput failed: %w", err)
	}
	return nil
}

// Click 在屏幕坐标 (x, y) 执行鼠标左键点击（移动 + 按下 + 释放）。
func (c *Controller) Click(ctx context.Context, x, y int) error {
	screenW, screenH := getScreenSize()
	// P1: 防除零崩溃
	if screenW == 0 || screenH == 0 {
		return fmt.Errorf("input: invalid screen size %dx%d", screenW, screenH)
	}
	// P3: 坐标范围校验
	if x < 0 || y < 0 || x >= screenW || y >= screenH {
		return fmt.Errorf("input: coordinates (%d,%d) out of screen bounds [0..%d, 0..%d]", x, y, screenW-1, screenH-1)
	}
	// 绝对坐标转换公式：absX = x * 65535 / screenWidth
	absX := x * 65535 / screenW
	absY := y * 65535 / screenH

	inputs := make([]INPUT, 3)

	// MOUSEMOVE（绝对坐标）
	inputs[0].Type = INPUT_MOUSE
	inputs[0].setMouse(MOUSEINPUT{
		Dx:      int32(absX),
		Dy:      int32(absY),
		DwFlags: MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE,
	})

	// LEFTDOWN
	inputs[1].Type = INPUT_MOUSE
	inputs[1].setMouse(MOUSEINPUT{
		Dx:      int32(absX),
		Dy:      int32(absY),
		DwFlags: MOUSEEVENTF_LEFTDOWN | MOUSEEVENTF_ABSOLUTE,
	})

	// LEFTUP
	inputs[2].Type = INPUT_MOUSE
	inputs[2].setMouse(MOUSEINPUT{
		Dx:      int32(absX),
		Dy:      int32(absY),
		DwFlags: MOUSEEVENTF_LEFTUP | MOUSEEVENTF_ABSOLUTE,
	})

	if err := sendInput(inputs); err != nil {
		return err
	}

	c.logger.Info().
		Str("event", string(logger.InputAction)).
		Str("action", "click").
		Int("x", x).
		Int("y", y).
		Msg("mouse click")

	return nil
}

// KeyPress 发送按键事件（按下并释放）。
func (c *Controller) KeyPress(ctx context.Context, keyCode uint16) error {
	inputs := make([]INPUT, 2)

	// KEYDOWN
	inputs[0].Type = INPUT_KEYBOARD
	inputs[0].setKeyboard(KEYBDINPUT{
		WVk:     keyCode,
		DwFlags: 0,
	})

	// KEYUP
	inputs[1].Type = INPUT_KEYBOARD
	inputs[1].setKeyboard(KEYBDINPUT{
		WVk:     keyCode,
		DwFlags: KEYEVENTF_KEYUP,
	})

	if err := sendInput(inputs); err != nil {
		return err
	}

	c.logger.Info().
		Str("event", string(logger.InputAction)).
		Str("action", "keypress").
		Uint16("key_code", keyCode).
		Msg("key press")

	return nil
}

// MouseMove 将鼠标移动到屏幕坐标 (x, y)。
func (c *Controller) MouseMove(ctx context.Context, x, y int) error {
	screenW, screenH := getScreenSize()
	// P1: 防除零崩溃；P3: 坐标范围校验
	if screenW == 0 || screenH == 0 {
		return fmt.Errorf("input: invalid screen size %dx%d", screenW, screenH)
	}
	if x < 0 || y < 0 || x >= screenW || y >= screenH {
		return fmt.Errorf("input: coordinates (%d,%d) out of screen bounds [0..%d, 0..%d]", x, y, screenW-1, screenH-1)
	}
	absX := x * 65535 / screenW
	absY := y * 65535 / screenH

	inputs := make([]INPUT, 1)
	inputs[0].Type = INPUT_MOUSE
	inputs[0].setMouse(MOUSEINPUT{
		Dx:      int32(absX),
		Dy:      int32(absY),
		DwFlags: MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE,
	})

	if err := sendInput(inputs); err != nil {
		return err
	}

	c.logger.Info().
		Str("event", string(logger.InputAction)).
		Str("action", "mousemove").
		Int("x", x).
		Int("y", y).
		Msg("mouse move")

	return nil
}
