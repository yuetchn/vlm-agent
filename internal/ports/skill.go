package ports

import "errors"

// ErrSkillNotFound 表示当前状态无对应 Skill 定义。
var ErrSkillNotFound = errors.New("skill: no skill found for state")

// SkillResult 是 SkillQuerier 查询结果。
type SkillResult struct {
	// Name 是 Skill 文件名，如 "lobby.md"
	Name string
	// Context 是 Skill 正文，注入 VLM prompt
	Context string
	// ConfidenceThreshold 是该 Skill 要求的最低推理置信度
	ConfidenceThreshold float32
}

// SkillQuerier 是 Skill 查询的端口接口。
// FSM 通过此接口查询当前状态对应的 Skill，不直接 import internal/skill。
type SkillQuerier interface {
	// QueryByState 根据游戏状态名查询对应的 Skill。
	// 若无对应 Skill，返回 ErrSkillNotFound。
	QueryByState(state string) (SkillResult, error)
}
