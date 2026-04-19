package mocks

import (
	"context"

	"github.com/zerfx/new_jzd/internal/ports"
)

// 编译期接口断言（防止接口变更后 Mock 静默失效）
var _ ports.LLMDecider = (*MockLLMDecider)(nil)

// DecideCall 记录单次 Decide 调用的参数。
type DecideCall struct {
	SystemPrompt string
	UserContent  string
}

// MockLLMDecider 是 ports.LLMDecider 的 Mock 实现。
// 非线程安全，仅用于单线程 FSM 测试（架构约定：FSM 单线程顺序执行）
type MockLLMDecider struct {
	// DecideFunc 可覆写 Decide 行为；若为 nil，返回空字符串和 nil error。
	DecideFunc func(ctx context.Context, systemPrompt, userContent string) (string, error)
	// Calls 记录所有调用参数，供测试断言调用次数和参数。
	Calls []DecideCall
}

func (m *MockLLMDecider) Decide(ctx context.Context, systemPrompt, userContent string) (string, error) {
	m.Calls = append(m.Calls, DecideCall{
		SystemPrompt: systemPrompt,
		UserContent:  userContent,
	})
	if m.DecideFunc != nil {
		return m.DecideFunc(ctx, systemPrompt, userContent)
	}
	return "", nil
}
