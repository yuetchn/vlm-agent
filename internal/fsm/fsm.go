package fsm

import (
	"github.com/looplab/fsm"
	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/logger"
	"github.com/zerfx/new_jzd/internal/ports"
)

// Controller 是游戏主状态机的控制器。
// 单线程顺序执行，不并发访问 machine。
type Controller struct {
	machine      *fsm.FSM
	vlm          ports.VLMInferrer
	input        ports.InputController
	capture      ports.ScreenCapturer
	skill        ports.SkillQuerier
	lowConfCount int // 低置信度连续计数（0-3）

	persist      *Persister // nil = 不持久化（测试时传 nil）
	persistData  StateData  // 缓存最后持久化值（含 LastPasswordInput）
	NeedsPassword bool      // AC4：last_password_input 超 24h 时置 true，供 Epic 5 recovery.go 读取
}

// NewController 构造 Controller，要求注入全部四个 port 接口。
// persist 为 nil 时跳过所有持久化操作（用于单元测试）。
func NewController(
	vlm ports.VLMInferrer,
	input ports.InputController,
	capture ports.ScreenCapturer,
	skill ports.SkillQuerier,
	persist *Persister,
) *Controller {
	// Step 1：先初始化 c（闭包将捕获此指针，必须在 fsm.NewFSM 之前完成）
	c := &Controller{
		vlm:     vlm,
		input:   input,
		capture: capture,
		skill:   skill,
		persist: persist,
	}

	// Step 2：从持久化状态推导 FSM 初始状态
	initialState := string(StateLobby)
	if persist != nil {
		data, err := persist.Load()
		if err != nil {
			log.Logger.Error().
				Str("event", string(logger.Recovery)).
				Err(err).
				Msg("state.json 读取失败，从 lobby 启动")
		} else {
			c.persistData = data
			switch data.CurrentState {
			case string(StateGameOver):
				initialState = data.CurrentState
				log.Logger.Info().
					Str("event", string(logger.Recovery)).
					Str("from_state", data.CurrentState).
					Msg("从稳定状态恢复，跳过崩溃时的中间状态")
			case string(StateLobby), "":
				// 默认从 lobby 启动，无需日志（首次运行或正常退出）
			default:
				// state.json 含非稳定状态值（数据损坏或手动编辑），安全降级到 lobby
				log.Logger.Warn().
					Str("event", string(logger.Recovery)).
					Str("invalid_state", data.CurrentState).
					Msg("state.json 含非稳定状态值，从 lobby 启动")
			}
		}
	}

	// Step 3：初始化 FSM（闭包捕获已完整赋值的 c）
	c.machine = fsm.NewFSM(
		initialState,
		fsm.Events{
			{Name: string(EventStartMatch), Src: []string{string(StateLobby), string(StateGameOver)}, Dst: string(StateMatching)},
			{Name: string(EventMatchFound), Src: []string{string(StateMatching)}, Dst: string(StateInFlight)},
			// 自转换：in_flight → in_flight（执行跳伞动作，下一循环触发 jumped）
			{Name: string(EventJumpReady), Src: []string{string(StateInFlight)}, Dst: string(StateInFlight)},
			{Name: string(EventJumped), Src: []string{string(StateInFlight)}, Dst: string(StateParachuting)},
			{Name: string(EventLanded), Src: []string{string(StateParachuting)}, Dst: string(StateLooting)},
			{Name: string(EventLootDone), Src: []string{string(StateLooting)}, Dst: string(StateRunningZone)},
			{Name: string(EventZoneClosing), Src: []string{string(StateLooting)}, Dst: string(StateRunningZone)},
			{Name: string(EventInSafeZone), Src: []string{string(StateRunningZone)}, Dst: string(StateLooting)},
			{Name: string(EventHealthLow), Src: []string{string(StateLooting), string(StateRunningZone), string(StateParachuting)}, Dst: string(StateHealing)},
			{Name: string(EventHealthOk), Src: []string{string(StateHealing)}, Dst: string(StateLooting)},
			{Name: string(EventGameEnded), Src: []string{
				string(StateLooting), string(StateRunningZone), string(StateHealing),
				string(StateAnomaly), string(StateParachuting), string(StateInFlight), string(StateMatching),
			}, Dst: string(StateGameOver)},
			{Name: string(EventAnomalyDetected), Src: []string{
				string(StateLobby), string(StateMatching), string(StateInFlight), string(StateParachuting),
				string(StateLooting), string(StateRunningZone), string(StateHealing),
				string(StateGameOver), string(StateRecovering),
			}, Dst: string(StateAnomaly)},
			{Name: string(EventRecoveryDone), Src: []string{string(StateRecovering)}, Dst: string(StateLobby)},
			{Name: string(EventPause), Src: []string{string(StateLobby)}, Dst: string(StatePaused)},
			{Name: string(EventResume), Src: []string{string(StatePaused)}, Dst: string(StateLobby)},
		},
		fsm.Callbacks{
			// 进入稳定状态时持久化（looplab/fsm v0.3.0 回调签名：func(*fsm.Event)，无 context）
			"enter_" + string(StateLobby): func(e *fsm.Event) {
				c.onEnterStableState(string(StateLobby))
			},
			"enter_" + string(StateGameOver): func(e *fsm.Event) {
				c.onEnterStableState(string(StateGameOver))
			},
		},
	)

	return c
}

// onEnterStableState 在 FSM 进入稳定状态（lobby / game_over）时调用：
//  1. 将当前状态原子写入 state.json
//  2. 进入 lobby 时检查二级密码周期，若超 24h 则置 NeedsPassword = true 并写日志
//
// 仅由 FSM 回调调用，始终在主 goroutine 内执行，无需加锁。
func (c *Controller) onEnterStableState(state string) {
	if c.persist == nil {
		return
	}

	c.persistData.CurrentState = state
	if err := c.persist.Save(c.persistData); err != nil {
		log.Logger.Error().
			Str("event", string(logger.Recovery)).
			Str("state", state).
			Err(err).
			Msg("state.json 写入失败")
		return
	}

	// 进入 lobby 时检查二级密码周期（AC4）
	if state == string(StateLobby) && c.persist.NeedsPasswordInput(c.persistData.LastPasswordInput) {
		c.NeedsPassword = true // 供 Epic 5 recovery.go 读取，无需 grep 日志
		log.Logger.Info().
			Str("event", string(logger.Recovery)).
			Str("state", state).
			Bool("needs_password", true).
			Msg("二级密码超过 24 小时，待 Epic 5 实现自动输入")
	}
}
