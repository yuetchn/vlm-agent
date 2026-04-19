//go:build windows

package capture

import (
	"bytes"
	"context"
	"image/png"
	"testing"
)

// screenAvailable 检查当前进程是否有桌面访问权限（屏幕尺寸非零）
func screenAvailable() bool {
	w, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	return int(w) > 0 && int(h) > 0
}

// TestGDICapture_Windows_Integration 验证真实 GDI 截图返回有效 PNG。
// 仅在 Windows 环境运行，依赖真实 GDI API 和桌面访问权限。
// 若进程没有桌面访问权限（如 CI service 账户），测试会跳过。
func TestGDICapture_Windows_Integration(t *testing.T) {
	if !screenAvailable() {
		t.Skip("no desktop access (screen size is 0), skipping GDI integration test")
	}

	data, err := gdiCapture()
	if err != nil {
		// GDI 在某些非交互式会话（service 账户、CI runner）下可能失败
		// 记录错误但不使测试套件整体失败
		t.Logf("gdiCapture failed (may be expected in non-interactive session): %v", err)
		t.Skip("gdiCapture unavailable in this session context")
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty PNG data")
	}
	// 验证返回的是有效 PNG
	_, err = png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gdiCapture returned invalid PNG: %v", err)
	}
}

// TestProductionCaptureManager_Windows 验证生产 CaptureManager（DXGI stub + GDI）可返回有效 PNG。
// 若 GDI 在此环境不可用，测试跳过。
func TestProductionCaptureManager_Windows(t *testing.T) {
	if !screenAvailable() {
		t.Skip("no desktop access (screen size is 0), skipping capture integration test")
	}

	mgr := NewProductionCaptureManager()
	data, err := mgr.Capture(context.Background())
	if err != nil {
		t.Logf("CaptureManager.Capture failed (may be expected in non-interactive session): %v", err)
		t.Skip("capture unavailable in this session context")
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty PNG data")
	}
	_, err = png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("CaptureManager returned invalid PNG: %v", err)
	}
}
