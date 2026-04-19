package ports

import (
	"context"
	"errors"
)

// ErrCaptureNotReady 表示截图后端尚未初始化。
var ErrCaptureNotReady = errors.New("capture: backend not initialized")

// ScreenCapturer 是屏幕截图的端口接口。
// 返回 PNG 格式字节。
type ScreenCapturer interface {
	Capture(ctx context.Context) ([]byte, error)
}
