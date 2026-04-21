package fsm

// State 是游戏状态的强类型，防止拼写错误。
type State string

// Event 是 FSM 事件的强类型，防止拼写错误。
type Event string

// 11 个状态常量（值为 snake_case，与架构文档锁定枚举完全一致）
const (
	StateLobby       State = "lobby"
	StateMatching    State = "matching"
	StateInFlight    State = "in_flight"
	StateParachuting State = "parachuting"
	StateLooting     State = "looting"
	StateRunningZone State = "running_zone"
	StateHealing     State = "healing"
	StateGameOver    State = "game_over"
	StateAnomaly     State = "anomaly"
	StateRecovering  State = "recovering"
	StatePaused      State = "paused" // Epic 4 实现，此处仅占位
)

// 15 个事件常量（值为 snake_case，与架构文档锁定枚举完全一致）
const (
	EventStartMatch      Event = "start_match"
	EventMatchFound      Event = "match_found"
	EventJumpReady       Event = "jump_ready"
	EventJumped          Event = "jumped"
	EventLanded          Event = "landed"
	EventLootDone        Event = "loot_done"
	EventZoneClosing     Event = "zone_closing"
	EventInSafeZone      Event = "in_safe_zone"
	EventHealthLow       Event = "health_low"
	EventHealthOk        Event = "health_ok"
	EventGameEnded       Event = "game_ended"
	EventAnomalyDetected Event = "anomaly_detected"
	EventRecoveryDone    Event = "recovery_done"
	EventPause           Event = "pause"   // Epic 4 实现，此处仅占位
	EventResume          Event = "resume"  // Epic 4 实现，此处仅占位
)
