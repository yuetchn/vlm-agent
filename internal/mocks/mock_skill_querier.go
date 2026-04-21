package mocks

import "github.com/zerfx/new_jzd/internal/ports"

// 编译期接口断言（防止接口变更后 Mock 静默失效）
var _ ports.SkillQuerier = (*MockSkillQuerier)(nil)

// MockSkillQuerier 是 ports.SkillQuerier 的 Mock 实现。
// 非线程安全，仅用于单线程 FSM 测试（架构约定：FSM 单线程顺序执行）
type MockSkillQuerier struct {
	// QueryResult 是 QueryByState 方法返回的 SkillResult。
	QueryResult ports.SkillResult
	// QueryErr 是 QueryByState 方法返回的错误；为 nil 时无错误。
	QueryErr error
	// Calls 记录所有被查询的 state 字符串。
	Calls []string
}

func (m *MockSkillQuerier) QueryByState(state string) (ports.SkillResult, error) {
	m.Calls = append(m.Calls, state)
	return m.QueryResult, m.QueryErr
}
