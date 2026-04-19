//go:build !windows

package capture

import "errors"

// gdiCapture 在非 Windows 平台不可用。
func gdiCapture() ([]byte, error) {
	return nil, errors.New("capture: not supported on non-windows")
}

// dxgiCapture 在非 Windows 平台不可用。
func dxgiCapture() ([]byte, error) {
	return nil, errors.New("capture: not supported on non-windows")
}

// NewProductionCaptureManager 创建非 Windows 平台的 CaptureManager（两个后端均返回 not-supported）。
func NewProductionCaptureManager() *CaptureManager {
	return NewCaptureManager(dxgiCapture, gdiCapture)
}
