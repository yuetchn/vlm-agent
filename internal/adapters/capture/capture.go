package capture

import (
	"context"
	"fmt"
)

// CaptureManager 实现 ports.ScreenCapturer，先尝试主截图后端，失败时回退到备用后端。
// 结构体设计使用函数注入，便于测试时注入 mock 函数，生产代码注入真实的 dxgiCapture/gdiCapture。
type CaptureManager struct {
	primaryFn  func() ([]byte, error)
	fallbackFn func() ([]byte, error)
}

// NewCaptureManager 创建 CaptureManager。
// primary 优先尝试（预期为 DXGI），fallback 在 primary 失败时使用（预期为 GDI）。
func NewCaptureManager(primary, fallback func() ([]byte, error)) *CaptureManager {
	return &CaptureManager{
		primaryFn:  primary,
		fallbackFn: fallback,
	}
}

// Capture 执行屏幕截图，返回 PNG 格式字节。
// 先尝试 primary（DXGI），失败时回退到 fallback（GDI）。
// 两者均失败时返回组合错误。
func (m *CaptureManager) Capture(_ context.Context) ([]byte, error) {
	data, primaryErr := m.primaryFn()
	if primaryErr == nil {
		return data, nil
	}

	// primary 失败，回退到 fallback
	data, fallbackErr := m.fallbackFn()
	if fallbackErr == nil {
		return data, nil
	}

	return nil, fmt.Errorf("capture: primary failed (%v), fallback also failed: %w", primaryErr, fallbackErr)
}
