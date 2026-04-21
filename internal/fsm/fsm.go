package fsm

import (
	"github.com/looplab/fsm"
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
}

// NewController 构造 Controller，要求注入全部四个 port 接口。
func NewController(
	vlm ports.VLMInferrer,
	input ports.InputController,
	capture ports.ScreenCapturer,
	skill ports.SkillQuerier,
) *Controller {
	c := &Controller{
		vlm:     vlm,
		input:   input,
		capture: capture,
		skill:   skill,
	}

	c.machine = fsm.NewFSM(
		string(StateLobby),
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
		// state_transition 日志和 stdout 输出由 loop.go 在 machine.Event() 成功后写入，
		// 以便访问当前循环的 skill/confidence/action 字段。
		fsm.Callbacks{},
	)

	return c
}
