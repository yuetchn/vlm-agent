package fsm

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/mocks"
	"github.com/zerfx/new_jzd/internal/ports"
)

// newTestControllerWithPersist 构造带 Persister 的测试 Controller。
func newTestControllerWithPersist(t *testing.T, p *Persister) *Controller {
	t.Helper()
	return NewController(
		&mocks.MockVLMInferrer{},
		&mocks.MockInputController{},
		&mocks.MockScreenCapturer{CaptureResult: []byte("fake_png")},
		&mocks.MockSkillQuerier{
			QueryResult: ports.SkillResult{
				Name:                "lobby.md",
				Context:             "test skill",
				ConfidenceThreshold: 0.5,
			},
		},
		p,
	)
}

// Test_OnEnterStableState_SavesStateJSON 验证 FSM 进入 game_over 时
// 回调触发 Save，state.json 被写入正确内容（AC1）。
func Test_OnEnterStableState_SavesStateJSON(t *testing.T) {
	dir := t.TempDir()
	p := NewPersister(filepath.Join(dir, "state.json"))
	ctrl := newTestControllerWithPersist(t, p)

	// 通过直接触发 FSM 事件驱动状态转换，无需启动 Run() 循环
	steps := []Event{
		EventStartMatch,  // lobby → matching
		EventMatchFound,  // matching → in_flight
		EventJumped,      // in_flight → parachuting
		EventLanded,      // parachuting → looting
		EventGameEnded,   // looting → game_over ← 触发 enter_game_over 回调 → Save
	}
	for _, ev := range steps {
		if err := ctrl.machine.Event(string(ev)); err != nil {
			t.Fatalf("event %q failed: %v", ev, err)
		}
	}

	if ctrl.machine.Current() != string(StateGameOver) {
		t.Fatalf("expected game_over, got %q", ctrl.machine.Current())
	}

	// 验证 state.json 已被写入
	data, err := p.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if data.CurrentState != string(StateGameOver) {
		t.Errorf("expected CurrentState %q in state.json, got %q", StateGameOver, data.CurrentState)
	}
}

// Test_NeedsPassword_SetOnLobbyEntry_WhenExpired 验证：
// 从超过 24h 的 state.json 启动后，onEnterStableState(lobby) 时
// NeedsPassword 被置 true，且日志含 needs_password:true（AC4）。
func Test_NeedsPassword_SetOnLobbyEntry_WhenExpired(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	// 预置 state.json：last_password_input 超过 24h
	p := NewPersister(statePath)
	if err := p.Save(StateData{
		CurrentState:      string(StateLobby),
		LastPasswordInput: time.Now().Add(-25 * time.Hour),
	}); err != nil {
		t.Fatalf("setup Save failed: %v", err)
	}

	// 捕获 zerolog 输出
	var buf bytes.Buffer
	origLogger := log.Logger
	log.Logger = zerolog.New(&buf)
	defer func() { log.Logger = origLogger }()

	ctrl := newTestControllerWithPersist(t, p)

	// 直接调用 onEnterStableState（同包可访问私有方法）
	ctrl.onEnterStableState(string(StateLobby))

	if !ctrl.NeedsPassword {
		t.Error("expected NeedsPassword=true when last_password_input > 24h")
	}
	logOutput := buf.String()
	if !bytes.Contains([]byte(logOutput), []byte(`"needs_password":true`)) {
		t.Errorf("expected log to contain needs_password:true, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte(`"event":"recovery"`)) {
		t.Errorf("expected log to contain event:recovery, got: %s", logOutput)
	}
}

// Test_NewController_RecoveryFromGameOver 验证 state.json 含 game_over 时，
// NewController 将 FSM 初始化为 game_over 状态（AC2）。
func Test_NewController_RecoveryFromGameOver(t *testing.T) {
	dir := t.TempDir()
	p := NewPersister(filepath.Join(dir, "state.json"))

	// 预置 state.json：game_over
	if err := p.Save(StateData{
		CurrentState:      string(StateGameOver),
		LastPasswordInput: time.Now().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("setup Save failed: %v", err)
	}

	ctrl := newTestControllerWithPersist(t, p)

	if ctrl.machine.Current() != string(StateGameOver) {
		t.Errorf("expected FSM initial state %q after recovery, got %q", StateGameOver, ctrl.machine.Current())
	}
}

// Test_NeedsPassword_NotSet_WhenRecent 验证 1 小时前密码输入时
// NeedsPassword 保持 false（AC4 反向验证）。
func Test_NeedsPassword_NotSet_WhenRecent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	p := NewPersister(statePath)
	if err := p.Save(StateData{
		CurrentState:      string(StateLobby),
		LastPasswordInput: time.Now().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("setup Save failed: %v", err)
	}

	ctrl := newTestControllerWithPersist(t, p)
	ctrl.onEnterStableState(string(StateLobby))

	if ctrl.NeedsPassword {
		t.Error("expected NeedsPassword=false when last_password_input < 24h")
	}
}
