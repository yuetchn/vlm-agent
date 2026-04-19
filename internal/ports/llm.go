package ports

import "context"

// LLMDecider 是大语言模型决策的端口接口。
// FSM 包通过此接口调用 LLM 服务获取决策文本。
//
// 返回值为自由文本字符串，具体格式解析延迟到 Story 3.1 FSM 实现时定义。
// Story 1.6 不做任何格式约束。
type LLMDecider interface {
	Decide(ctx context.Context, systemPrompt, userContent string) (string, error)
}
