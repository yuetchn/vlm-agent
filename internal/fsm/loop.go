package fsm

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/logger"
)

// Run 启动主游戏循环（阻塞，单线程顺序执行）。
// ctx 取消时退出并返回 ctx.Err()。
func (c *Controller) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		currentState := c.machine.Current()

		// Step 1: 截图
		screenshot, err := c.capture.Capture(ctx)
		if err != nil {
			log.Logger.Error().Str("event", string(logger.VLMInfer)).Err(err).Msg("截图失败")
			continue
		}

		// Step 2: 查询当前状态对应 Skill
		skillResult, err := c.skill.QueryByState(currentState)
		if err != nil {
			log.Logger.Error().Str("event", string(logger.Anomaly)).
				Str("from_state", currentState).
				Err(err).
				Msg("Skill 查询失败，触发异常")
			_ = c.machine.Event(string(EventAnomalyDetected))
			c.lowConfCount = 0
			continue
		}

		// Step 3: VLM 推理（阻塞，强制 100ms 子超时，由 inferCtx 控制）
		inferCtx, inferCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		result, err := c.vlm.Infer(inferCtx, screenshot, skillResult.Context, currentState)
		inferCancel()
		if err != nil {
			log.Logger.Error().Str("event", string(logger.VLMInfer)).Err(err).Msg("VLM 推理失败")
			continue
		}
		log.Logger.Info().
			Str("event", string(logger.VLMInfer)).
			Str("from_state", currentState).
			Str("skill", skillResult.Name).
			Float32("confidence", result.Confidence).
			Bool("stale_result", false).
			Msg("VLM 推理")

		// Step 4: 置信度检查
		if result.Confidence < skillResult.ConfidenceThreshold {
			c.lowConfCount++
			if c.lowConfCount >= 3 {
				log.Logger.Warn().
					Str("event", string(logger.Anomaly)).
					Str("from_state", currentState).
					Str("skill", skillResult.Name).
					Float32("confidence", result.Confidence).
					Str("reason", "low_confidence_3_retries").
					Msg("低置信度异常")
				_ = c.machine.Event(string(EventAnomalyDetected))
				c.lowConfCount = 0
			}
			continue
		}
		c.lowConfCount = 0

		// Step 5: 状态漂移二次确认
		confirmShot, err := c.capture.Capture(ctx)
		if err != nil {
			log.Logger.Warn().Str("event", string(logger.VLMInfer)).Err(err).Msg("确认截图失败，丢弃此次结果")
			continue
		}
		confirmResult, err := c.vlm.Infer(ctx, confirmShot, skillResult.Context, currentState)
		if err != nil {
			log.Logger.Warn().Str("event", string(logger.VLMInfer)).Err(err).Msg("确认推理失败，丢弃此次结果")
			continue
		}
		if confirmResult.State != result.State {
			log.Logger.Warn().
				Str("event", string(logger.VLMInfer)).
				Str("from_state", currentState).
				Str("skill", skillResult.Name).
				Bool("stale_result", true).
				Msg("状态漂移，丢弃推理结果")
			continue
		}

		// Step 6: FSM 状态转换（若状态发生变化）
		event := resolveEvent(currentState, result.State)
		if event != "" {
			if fsmErr := c.machine.Event(string(event)); fsmErr != nil {
				log.Logger.Error().
					Str("event", string(logger.StateTransition)).
					Str("from_state", currentState).
					Str("to_state", result.State).
					Err(fsmErr).
					Msg("FSM 事件触发失败")
			} else {
				newState := c.machine.Current()
				if newState != currentState {
					fmt.Printf("[%s] %s → %s | 动作: %s\n",
						time.Now().Format("2006-01-02 15:04"),
						currentState, newState, result.Action,
					)
					log.Logger.Info().
						Str("event", string(logger.StateTransition)).
						Str("from_state", currentState).
						Str("to_state", newState).
						Str("skill", skillResult.Name).
						Float32("confidence", result.Confidence).
						Str("action", result.Action).
						Msg("状态转换")
				}
			}
		}

		// Step 7: 执行动作（占位实现）
		c.executeAction(ctx, result.Action)
	}
}

// resolveEvent 根据 (当前状态, VLM识别状态) 映射返回应触发的 FSM 事件。
// 返回空字符串表示无需触发事件。
func resolveEvent(fromState, toState string) Event {
	switch {
	case fromState == string(StateLobby) && toState == string(StateMatching):
		return EventStartMatch
	case fromState == string(StateMatching) && toState == string(StateInFlight):
		return EventMatchFound
	case fromState == string(StateInFlight) && toState == string(StateInFlight):
		// 自转换：待跳伞，执行动作，下一循环再触发 jumped
		return EventJumpReady
	case fromState == string(StateInFlight) && toState == string(StateParachuting):
		return EventJumped
	case fromState == string(StateParachuting) && toState == string(StateLooting):
		return EventLanded
	case fromState == string(StateLooting) && toState == string(StateRunningZone):
		return EventZoneClosing
	case fromState == string(StateRunningZone) && toState == string(StateLooting):
		return EventInSafeZone
	case (fromState == string(StateLooting) || fromState == string(StateRunningZone) || fromState == string(StateParachuting)) &&
		toState == string(StateHealing):
		return EventHealthLow
	case fromState == string(StateHealing) && toState != string(StateHealing):
		return EventHealthOk
	case toState == string(StateGameOver):
		return EventGameEnded
	case fromState == string(StateGameOver) && toState == string(StateLobby):
		return EventStartMatch
	case toState == string(StateAnomaly):
		return EventAnomalyDetected
	case fromState == string(StateRecovering) && toState == string(StateLobby):
		return EventRecoveryDone
	default:
		return ""
	}
}

// executeAction 执行 VLM 决策动作（Story 3.1 占位实现）。
// Story 3.5 实现真实动作分发（含坐标、按键映射）时替换此方法。
func (c *Controller) executeAction(ctx context.Context, action string) {
	log.Logger.Info().
		Str("event", string(logger.InputAction)).
		Str("action", action).
		Msg("执行动作（占位）")
	_ = c.input.Click(ctx, 0, 0)
}
