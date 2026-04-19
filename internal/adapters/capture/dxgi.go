//go:build windows

package capture

import "fmt"

// dxgiCapture 是 DXGI Output Duplication 截图的存根实现。
// 始终返回 error，由 CaptureManager 回退到 GDI。
//
// 完整 DXGI COM vtable 实现（IDXGIOutputDuplication）延迟到 Story 1.7 Spike 验证，
// 届时根据延迟需求决定是否完整实现。
// 当前策略：DXGI 存根触发回退，GDI 保证功能完整性。
func dxgiCapture() ([]byte, error) {
	return nil, fmt.Errorf("capture dxgi: not implemented (stub, will fallback to GDI)")
}

// NewProductionCaptureManager 创建生产环境的 CaptureManager（DXGI primary + GDI fallback）。
func NewProductionCaptureManager() *CaptureManager {
	return NewCaptureManager(dxgiCapture, gdiCapture)
}
