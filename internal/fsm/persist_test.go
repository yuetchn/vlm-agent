package fsm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test_Load_NoFile 验证首次运行（无 state.json）返回默认 lobby 状态。
func Test_Load_NoFile(t *testing.T) {
	dir := t.TempDir()
	p := NewPersister(filepath.Join(dir, "state.json"))

	data, err := p.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if data.CurrentState != string(StateLobby) {
		t.Errorf("expected CurrentState %q, got %q", StateLobby, data.CurrentState)
	}
	if !data.LastPasswordInput.IsZero() {
		t.Errorf("expected zero LastPasswordInput, got %v", data.LastPasswordInput)
	}
}

// Test_Load_ExistingFile 验证正常读取已有 state.json。
func Test_Load_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	ts := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	input := StateData{CurrentState: string(StateGameOver), LastPasswordInput: ts}
	b, _ := json.Marshal(input)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	p := NewPersister(path)
	data, err := p.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.CurrentState != string(StateGameOver) {
		t.Errorf("expected CurrentState %q, got %q", StateGameOver, data.CurrentState)
	}
	if !data.LastPasswordInput.Equal(ts) {
		t.Errorf("expected LastPasswordInput %v, got %v", ts, data.LastPasswordInput)
	}
}

// Test_Load_CleansTmp_WithStateFile 验证 .tmp 残留 + state.json 存在时：
// .tmp 被删除，state.json 被正确读取。
func Test_Load_CleansTmp_WithStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	tmpPath := path + ".tmp"

	// 写入正常 state.json
	good := StateData{CurrentState: string(StateLobby)}
	b, _ := json.Marshal(good)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("setup state.json: %v", err)
	}
	// 写入损坏的 .tmp
	if err := os.WriteFile(tmpPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("setup .tmp: %v", err)
	}

	p := NewPersister(path)
	data, err := p.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .tmp 应已被删除
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Error(".tmp file should have been removed")
	}
	if data.CurrentState != string(StateLobby) {
		t.Errorf("expected CurrentState %q, got %q", StateLobby, data.CurrentState)
	}
}

// Test_Load_CleansTmp_NoStateFile 验证 .tmp 残留 + 无 state.json 时：
// .tmp 被删除，返回默认 lobby 状态，无 error。
func Test_Load_CleansTmp_NoStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	tmpPath := path + ".tmp"

	// 只写 .tmp，不写 state.json
	if err := os.WriteFile(tmpPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("setup .tmp: %v", err)
	}

	p := NewPersister(path)
	data, err := p.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .tmp 应已被删除
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Error(".tmp file should have been removed")
	}
	if data.CurrentState != string(StateLobby) {
		t.Errorf("expected default CurrentState %q, got %q", StateLobby, data.CurrentState)
	}
}

// Test_Save_CreatesFile 验证 Save 创建 state.json，且 .tmp 被原子替换（不残留）。
func Test_Save_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	p := NewPersister(path)

	ts := time.Now().Truncate(time.Second).UTC()
	input := StateData{CurrentState: string(StateGameOver), LastPasswordInput: ts}
	if err := p.Save(input); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// state.json 应存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state.json should exist after Save")
	}
	// .tmp 应不存在（已被 Rename 替换）
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("state.json.tmp should not exist after successful Save")
	}

	// 验证内容
	b, _ := os.ReadFile(path)
	var got StateData
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("failed to parse saved file: %v", err)
	}
	if got.CurrentState != string(StateGameOver) {
		t.Errorf("expected CurrentState %q, got %q", StateGameOver, got.CurrentState)
	}
	if !got.LastPasswordInput.Equal(ts) {
		t.Errorf("expected LastPasswordInput %v, got %v", ts, got.LastPasswordInput)
	}
}

// Test_Save_RoundTrip 验证 Save → Load 往返一致性。
func Test_Save_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := NewPersister(filepath.Join(dir, "state.json"))

	original := StateData{
		CurrentState:      string(StateLobby),
		LastPasswordInput: time.Now().Truncate(time.Second).UTC(),
	}
	if err := p.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := p.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.CurrentState != original.CurrentState {
		t.Errorf("CurrentState: expected %q, got %q", original.CurrentState, loaded.CurrentState)
	}
	if !loaded.LastPasswordInput.Equal(original.LastPasswordInput) {
		t.Errorf("LastPasswordInput: expected %v, got %v", original.LastPasswordInput, loaded.LastPasswordInput)
	}
}

// Test_NeedsPasswordInput_ZeroTime 验证零值时间返回 true。
func Test_NeedsPasswordInput_ZeroTime(t *testing.T) {
	p := NewPersister(filepath.Join(t.TempDir(), "state.json"))
	if !p.NeedsPasswordInput(time.Time{}) {
		t.Error("expected NeedsPasswordInput=true for zero time")
	}
}

// Test_NeedsPasswordInput_Recent 验证 1 小时前返回 false。
func Test_NeedsPasswordInput_Recent(t *testing.T) {
	p := NewPersister(filepath.Join(t.TempDir(), "state.json"))
	if p.NeedsPasswordInput(time.Now().Add(-1 * time.Hour)) {
		t.Error("expected NeedsPasswordInput=false for 1h ago")
	}
}

// Test_NeedsPasswordInput_Expired 验证 25 小时前返回 true。
func Test_NeedsPasswordInput_Expired(t *testing.T) {
	p := NewPersister(filepath.Join(t.TempDir(), "state.json"))
	if !p.NeedsPasswordInput(time.Now().Add(-25 * time.Hour)) {
		t.Error("expected NeedsPasswordInput=true for 25h ago")
	}
}

// Test_NeedsPasswordInput_FutureTimestamp 验证未来时间戳返回 true（防止时钟回拨永久跳过密码检查）。
func Test_NeedsPasswordInput_FutureTimestamp(t *testing.T) {
	p := NewPersister(filepath.Join(t.TempDir(), "state.json"))
	if !p.NeedsPasswordInput(time.Now().Add(1 * time.Hour)) {
		t.Error("expected NeedsPasswordInput=true for future timestamp")
	}
}

// Test_Save_OverwritesExistingStateJSON 验证 Save 可覆写已有 state.json，内容以最新写入为准。
func Test_Save_OverwritesExistingStateJSON(t *testing.T) {
	dir := t.TempDir()
	p := NewPersister(filepath.Join(dir, "state.json"))

	// 第一次写入 game_over
	first := StateData{
		CurrentState:      string(StateGameOver),
		LastPasswordInput: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := p.Save(first); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}

	// 第二次覆写为 lobby
	second := StateData{
		CurrentState:      string(StateLobby),
		LastPasswordInput: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := p.Save(second); err != nil {
		t.Fatalf("second Save failed: %v", err)
	}

	// 验证内容以第二次写入为准
	got, err := p.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.CurrentState != string(StateLobby) {
		t.Errorf("expected CurrentState %q, got %q", StateLobby, got.CurrentState)
	}
	if !got.LastPasswordInput.Equal(second.LastPasswordInput) {
		t.Errorf("expected LastPasswordInput %v, got %v", second.LastPasswordInput, got.LastPasswordInput)
	}
}
