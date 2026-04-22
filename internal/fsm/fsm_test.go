package fsm

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/mocks"
	"github.com/zerfx/new_jzd/internal/ports"
)

// newTestController 构造用于测试的 Controller，注入所有 Mock 依赖。
func newTestController(t *testing.T, vlmFn func(context.Context, []byte, string, string) (ports.InferResult, error)) (*Controller, *mocks.MockVLMInferrer, *mocks.MockInputController, *mocks.MockScreenCapturer) {
	t.Helper()
	mockVLM := &mocks.MockVLMInferrer{InferFunc: vlmFn}
	mockInput := &mocks.MockInputController{}
	mockCapture := &mocks.MockScreenCapturer{CaptureResult: []byte("fake_png")}
	mockSkill := &mocks.MockSkillQuerier{
		QueryResult: ports.SkillResult{
			Name:                "lobby.md",
			Context:             "test skill",
			ConfidenceThreshold: 0.85,
		},
	}
	ctrl := NewController(mockVLM, mockInput, mockCapture, mockSkill, nil)
	return ctrl, mockVLM, mockInput, mockCapture
}

// TestInitialState 验证状态机初始状态为 lobby。
func TestInitialState(t *testing.T) {
	ctrl, _, _, _ := newTestController(t, nil)
	if ctrl.machine.Current() != string(StateLobby) {
		t.Errorf("expected initial state %q, got %q", StateLobby, ctrl.machine.Current())
	}
}

// TestStateTransitions 验证正常状态转换路径（至少5个主要转换）。
func TestStateTransitions(t *testing.T) {
	ctx := context.Background()
	ctrl, _, _, _ := newTestController(t, nil)

	transitions := []struct {
		event    Event
		expected State
	}{
		{EventStartMatch, StateMatching},
		{EventMatchFound, StateInFlight},
		{EventJumped, StateParachuting},
		{EventLanded, StateLooting},
		{EventZoneClosing, StateRunningZone},
	}

	for _, tr := range transitions {
		if err := ctrl.machine.Event(string(tr.event)); err != nil {
			t.Fatalf("event %q failed from state %q: %v", tr.event, ctrl.machine.Current(), err)
		}
		if ctrl.machine.Current() != string(tr.expected) {
			t.Errorf("after event %q: expected state %q, got %q", tr.event, tr.expected, ctrl.machine.Current())
		}
		_ = ctx
	}
}

// TestLowConfidence_Single 验证单次低置信度不触发 anomaly，FSM 仍在 lobby。
func TestLowConfidence_Single(t *testing.T) {
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 第1次调用：返回低置信度 + 立即 cancel，使下一次循环退出
	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		cancel()
		return ports.InferResult{State: "lobby", Confidence: 0.5}, nil
	}

	_ = ctrl.Run(ctx)

	// FSM 应仍在 lobby（没有触发 anomaly）
	if ctrl.machine.Current() != string(StateLobby) {
		t.Errorf("expected state %q after single low confidence, got %q", StateLobby, ctrl.machine.Current())
	}
	// lowConfCount 应为 1（未累积到 3）
	if ctrl.lowConfCount != 1 {
		t.Errorf("expected lowConfCount == 1, got %d", ctrl.lowConfCount)
	}
}

// TestLowConfidence_ThreeConsecutive 验证连续3次低置信度触发 anomaly_detected，FSM 进入 anomaly 状态。
func TestLowConfidence_ThreeConsecutive(t *testing.T) {
	callCount := 0
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		// 每次主推理返回低置信度（确认推理在此情境下不会到达，因为置信度检查先触发）
		if callCount >= 4 {
			cancel()
		}
		return ports.InferResult{State: "lobby", Confidence: 0.5}, nil
	}

	_ = ctrl.Run(ctx)

	if ctrl.machine.Current() != string(StateAnomaly) {
		t.Errorf("expected state %q after 3 low confidence, got %q", StateAnomaly, ctrl.machine.Current())
	}
}

// TestLowConfidence_CounterReset 验证低置信度计数器重置后可正常触发状态转换。
func TestLowConfidence_CounterReset(t *testing.T) {
	callCount := 0
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// 先触发3次低置信度（进入 anomaly），然后在 anomaly 状态高置信度触发 game_ended
	phase := 0
	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		switch phase {
		case 0:
			// 3次低置信度 → anomaly
			if callCount >= 4 {
				phase = 1
				callCount = 0
			}
			return ports.InferResult{State: "lobby", Confidence: 0.5}, nil
		case 1:
			// anomaly 状态下：主推理和确认推理都返回 game_over（高置信度）
			if callCount >= 3 {
				cancel()
			}
			return ports.InferResult{State: "game_over", Confidence: 0.95}, nil
		}
		return ports.InferResult{}, nil
	}

	_ = ctrl.Run(ctx)

	// 触发 anomaly 后 lowConfCount 应归零
	if ctrl.lowConfCount != 0 {
		t.Errorf("expected lowConfCount == 0 after reset, got %d", ctrl.lowConfCount)
	}
}

// TestStateDrift 验证状态漂移时：FSM 状态未变化、无动作执行、截图调用2次。
func TestStateDrift(t *testing.T) {
	var buf bytes.Buffer
	origLogger := log.Logger
	log.Logger = zerolog.New(&buf)
	defer func() { log.Logger = origLogger }()

	callCount := 0
	ctrl, mockVLM, mockInput, mockCapture := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, stateHint string) (ports.InferResult, error) {
		callCount++
		if callCount == 1 {
			// 主推理：识别到 matching
			return ports.InferResult{State: "matching", Confidence: 0.95}, nil
		}
		// 确认推理：识别到 lobby（漂移）
		cancel()
		return ports.InferResult{State: "lobby", Confidence: 0.95}, nil
	}

	_ = ctrl.Run(ctx)

	// FSM 状态未变化（仍在 lobby）
	if ctrl.machine.Current() != string(StateLobby) {
		t.Errorf("expected state %q after drift, got %q", StateLobby, ctrl.machine.Current())
	}

	// 无动作执行
	if len(mockInput.ClickCalls) != 0 {
		t.Errorf("expected no click calls on drift, got %d", len(mockInput.ClickCalls))
	}

	// 截图调用2次（主 + 确认）
	if mockCapture.CallCount != 2 {
		t.Errorf("expected 2 capture calls, got %d", mockCapture.CallCount)
	}

	// VLM 调用2次（主推理 + 确认推理）
	if len(mockVLM.Calls) != 2 {
		t.Errorf("expected 2 VLM calls, got %d", len(mockVLM.Calls))
	}

	// 日志包含 stale_result:true
	if !bytes.Contains(buf.Bytes(), []byte(`"stale_result":true`)) {
		t.Errorf("expected stale_result:true in log, got: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"event":"vlm_infer"`)) {
		t.Errorf("expected event:vlm_infer in log, got: %s", buf.String())
	}
}

// TestPauseResume 验证 pause/resume 事件可触发（直接调用 machine.Event）。
func TestPauseResume(t *testing.T) {
	ctrl, _, _, _ := newTestController(t, nil)
	ctx := context.Background()
	_ = ctx

	if err := ctrl.machine.Event(string(EventPause)); err != nil {
		t.Fatalf("pause event failed: %v", err)
	}
	if ctrl.machine.Current() != string(StatePaused) {
		t.Errorf("expected state %q after pause, got %q", StatePaused, ctrl.machine.Current())
	}

	if err := ctrl.machine.Event(string(EventResume)); err != nil {
		t.Fatalf("resume event failed: %v", err)
	}
	if ctrl.machine.Current() != string(StateLobby) {
		t.Errorf("expected state %q after resume, got %q", StateLobby, ctrl.machine.Current())
	}
}

// TestRun_HealthLow 验证从 looting 状态经 Run() 触发 health_low 进入 healing。
func TestRun_HealthLow(t *testing.T) {
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// 手动将 FSM 推进到 looting 状态
	_ = ctrl.machine.Event(string(EventStartMatch)) // lobby → matching
	_ = ctrl.machine.Event(string(EventMatchFound)) // matching → in_flight
	_ = ctrl.machine.Event(string(EventJumped))     // in_flight → parachuting
	_ = ctrl.machine.Event(string(EventLanded))     // parachuting → looting

	callCount := 0
	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		if callCount >= 2 {
			cancel()
		}
		return ports.InferResult{State: "healing", Confidence: 0.95}, nil
	}

	_ = ctrl.Run(ctx)

	if ctrl.machine.Current() != string(StateHealing) {
		t.Errorf("expected state %q after health_low via Run, got %q", StateHealing, ctrl.machine.Current())
	}
}

// TestRun_HealthOk 验证从 healing 状态经 Run() 触发 health_ok 返回 looting。
func TestRun_HealthOk(t *testing.T) {
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// 手动将 FSM 推进到 healing 状态
	_ = ctrl.machine.Event(string(EventStartMatch))
	_ = ctrl.machine.Event(string(EventMatchFound))
	_ = ctrl.machine.Event(string(EventJumped))
	_ = ctrl.machine.Event(string(EventLanded))
	_ = ctrl.machine.Event(string(EventHealthLow)) // looting → healing

	callCount := 0
	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		if callCount >= 2 {
			cancel()
		}
		return ports.InferResult{State: "looting", Confidence: 0.95}, nil
	}

	_ = ctrl.Run(ctx)

	if ctrl.machine.Current() != string(StateLooting) {
		t.Errorf("expected state %q after health_ok via Run, got %q", StateLooting, ctrl.machine.Current())
	}
}

// TestRun_GameEnded 验证从 looting 状态经 Run() 触发 game_ended 进入 game_over。
func TestRun_GameEnded(t *testing.T) {
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// 手动将 FSM 推进到 looting 状态
	_ = ctrl.machine.Event(string(EventStartMatch))
	_ = ctrl.machine.Event(string(EventMatchFound))
	_ = ctrl.machine.Event(string(EventJumped))
	_ = ctrl.machine.Event(string(EventLanded))

	callCount := 0
	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		if callCount >= 2 {
			cancel()
		}
		return ports.InferResult{State: "game_over", Confidence: 0.95}, nil
	}

	_ = ctrl.Run(ctx)

	if ctrl.machine.Current() != string(StateGameOver) {
		t.Errorf("expected state %q after game_ended via Run, got %q", StateGameOver, ctrl.machine.Current())
	}
}

// TestRun_CtxCancel 验证 ctx.Done() 触发时 Run() 正常退出，返回 context.Canceled。
func TestRun_CtxCancel(t *testing.T) {
	callCount := 0
	ctrl, mockVLM, _, _ := newTestController(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	mockVLM.InferFunc = func(ctx context.Context, _ []byte, _, _ string) (ports.InferResult, error) {
		callCount++
		if callCount >= 2 {
			cancel()
		}
		return ports.InferResult{State: "lobby", Confidence: 0.9}, nil
	}

	err := ctrl.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
