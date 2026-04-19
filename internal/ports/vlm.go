package ports

import (
	"context"
	"errors"
)

// ErrVLMLowConfidence 表示 VLM 推理置信度低于阈值。
var ErrVLMLowConfidence = errors.New("vlm: confidence below threshold")

// ErrVLMServiceDown 表示任何 AI 服务不可用（VLM 或 LLM）。
// 此错误同时用于：
//  1. VLM gRPC/HTTP 适配器：服务连接失败或推理失败
//  2. LLM 适配器：超出最大重试次数（4 次尝试均失败）后返回
//
// 命名沿用架构文档定义，语义为"任何 AI 服务不可用"而非仅限 VLM。
var ErrVLMServiceDown = errors.New("vlm: service unavailable")

// InferResult 是 VLM 推理的结果。
type InferResult struct {
	State      string  // 识别到的游戏状态
	Action     string  // 建议执行的动作
	Confidence float32 // 置信度 [0,1]
	RawJSON    string  // 原始 JSON 响应
}

// VLMInferrer 是视觉语言模型推理的端口接口。
// FSM 包通过此接口调用 VLM 服务，不感知底层是 gRPC 还是 HTTP。
type VLMInferrer interface {
	Infer(ctx context.Context, screenshot []byte, skillContext, stateHint string) (InferResult, error)
}
