package mocks

import (
	"context"

	"github.com/zerfx/new_jzd/internal/ports"
)

// 编译期接口断言（防止接口变更后 Mock 静默失效）
var _ ports.VLMInferrer = (*MockVLMInferrer)(nil)

// InferCall 记录单次 Infer 调用的参数。
type InferCall struct {
	Screenshot   []byte
	SkillContext string
	StateHint    string
}

// MockVLMInferrer 是 ports.VLMInferrer 的 Mock 实现。
// 非线程安全，仅用于单线程 FSM 测试（架构约定：FSM 单线程顺序执行）
type MockVLMInferrer struct {
	// InferFunc 可覆写 Infer 行为；若为 nil，返回零值 InferResult 和 nil error。
	InferFunc func(ctx context.Context, screenshot []byte, skillContext, stateHint string) (ports.InferResult, error)
	// Calls 记录所有调用参数，供测试断言调用次数和参数。
	Calls []InferCall
}

func (m *MockVLMInferrer) Infer(ctx context.Context, screenshot []byte, skillContext, stateHint string) (ports.InferResult, error) {
	m.Calls = append(m.Calls, InferCall{
		Screenshot:   screenshot,
		SkillContext: skillContext,
		StateHint:    stateHint,
	})
	if m.InferFunc != nil {
		return m.InferFunc(ctx, screenshot, skillContext, stateHint)
	}
	return ports.InferResult{}, nil
}
