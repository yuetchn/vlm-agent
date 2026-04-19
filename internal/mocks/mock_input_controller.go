package mocks

import (
	"context"

	"github.com/zerfx/new_jzd/internal/ports"
)

// 编译期接口断言（防止接口变更后 Mock 静默失效）
var _ ports.InputController = (*MockInputController)(nil)

// ClickCall 记录单次 Click 调用的参数。
type ClickCall struct {
	X, Y int
}

// KeyPressCall 记录单次 KeyPress 调用的参数。
type KeyPressCall struct {
	KeyCode uint16
}

// MouseMoveCall 记录单次 MouseMove 调用的参数。
type MouseMoveCall struct {
	X, Y int
}

// MockInputController 是 ports.InputController 的 Mock 实现。
// 非线程安全，仅用于单线程 FSM 测试（架构约定：FSM 单线程顺序执行）
type MockInputController struct {
	// 可配置错误返回
	ClickErr     error
	KeyPressErr  error
	MouseMoveErr error

	// 调用记录，供测试断言
	ClickCalls     []ClickCall
	KeyPressCalls  []KeyPressCall
	MouseMoveCalls []MouseMoveCall
}

func (m *MockInputController) Click(_ context.Context, x, y int) error {
	m.ClickCalls = append(m.ClickCalls, ClickCall{X: x, Y: y})
	return m.ClickErr
}

func (m *MockInputController) KeyPress(_ context.Context, keyCode uint16) error {
	m.KeyPressCalls = append(m.KeyPressCalls, KeyPressCall{KeyCode: keyCode})
	return m.KeyPressErr
}

func (m *MockInputController) MouseMove(_ context.Context, x, y int) error {
	m.MouseMoveCalls = append(m.MouseMoveCalls, MouseMoveCall{X: x, Y: y})
	return m.MouseMoveErr
}
