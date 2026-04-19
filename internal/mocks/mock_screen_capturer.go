package mocks

import (
	"context"

	"github.com/zerfx/new_jzd/internal/ports"
)

// 编译期接口断言（防止接口变更后 Mock 静默失效）
var _ ports.ScreenCapturer = (*MockScreenCapturer)(nil)

// MockScreenCapturer 是 ports.ScreenCapturer 的 Mock 实现。
// 非线程安全，仅用于单线程 FSM 测试（架构约定：FSM 单线程顺序执行）
type MockScreenCapturer struct {
	// CaptureResult 是 Capture 方法返回的 PNG 字节；为 nil 时返回空字节。
	CaptureResult []byte
	// CaptureErr 是 Capture 方法返回的错误；为 nil 时无错误。
	CaptureErr error
	// CallCount 记录 Capture 被调用的次数。
	CallCount int
}

func (m *MockScreenCapturer) Capture(_ context.Context) ([]byte, error) {
	m.CallCount++
	return m.CaptureResult, m.CaptureErr
}
