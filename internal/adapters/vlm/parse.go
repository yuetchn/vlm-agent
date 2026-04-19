package vlm

import (
	"encoding/json"
	"fmt"

	"github.com/zerfx/new_jzd/internal/ports"
)

// parseInferResult 将 VLM 服务返回的原始 JSON 字符串解析为 InferResult。
// gRPC 和 HTTP 适配器共用此函数。
//
// 错误处理规范：
//   - JSON 解析失败：返回包含原始错误的 wrapped error
//   - state 字段为空：返回明确错误
//   - confidence 范围 [0,1] 自动 clamp（与 Python 侧 Story 1.5 P9 保持一致）
func parseInferResult(raw string) (ports.InferResult, error) {
	var j struct {
		State      string  `json:"state"`
		Action     string  `json:"action"`
		Confidence float32 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm: invalid infer result json: %w", err)
	}
	if j.State == "" {
		return ports.InferResult{}, fmt.Errorf("vlm: infer result missing state field")
	}
	// confidence clamp [0,1]（与 Python 侧 Story 1.5 P9 保持一致）
	if j.Confidence < 0 {
		j.Confidence = 0
	}
	if j.Confidence > 1 {
		j.Confidence = 1
	}
	return ports.InferResult{
		State:      j.State,
		Action:     j.Action,
		Confidence: j.Confidence,
		RawJSON:    raw,
	}, nil
}
