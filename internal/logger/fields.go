package logger

// EventType 是日志 event 字段的强类型枚举，防止拼写错误。
// 所有日志调用必须通过此类型的常量写入 event 字段。
type EventType string

const (
	// StateTransition 表示游戏状态机发生状态转换
	StateTransition EventType = "state_transition"
	// VLMInfer 表示 VLM 视觉推理调用
	VLMInfer EventType = "vlm_infer"
	// InputAction 表示鼠标/键盘输入操作
	InputAction EventType = "input_action"
	// Anomaly 表示检测到异常情况
	Anomaly EventType = "anomaly"
	// Recovery 表示从异常状态恢复
	Recovery EventType = "recovery"
	// SessionStat 表示会话统计数据记录
	SessionStat EventType = "session_stat"
	// VersionCheck 表示版本检查事件
	VersionCheck EventType = "version_check"
)
