package fsm

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/logger"
)

// StateData 是 state.json 的完整结构（架构文档锁定，不得擅自新增字段）。
// 扩展点：
//   - Story 4.4：新增 SessionRound int `json:"session_round"`
//   - Story 5.3：新增 GameFileMtime string `json:"game_file_mtime"`
type StateData struct {
	CurrentState      string    `json:"current_state"`
	LastPasswordInput time.Time `json:"last_password_input"`
}

// Persister 负责 state.json 的原子读写。
// 写入权限：FSM 主 goroutine 独占，禁止异步调用 Save。
type Persister struct {
	path    string // state.json 路径
	tmpPath string // state.json.tmp 路径（原子写入中转）
}

// NewPersister 创建 Persister，path 为 state.json 的完整文件路径。
func NewPersister(path string) *Persister {
	return &Persister{
		path:    path,
		tmpPath: path + ".tmp",
	}
}

// Load 在系统启动时调用，执行以下操作（顺序固定）：
//  1. 若 .tmp 残留文件存在 → 删除（上次原子写入未完成）
//  2. 若 state.json 不存在 → 返回默认值（首次运行，从 lobby 启动）
//  3. 读取并反序列化 state.json
func (p *Persister) Load() (StateData, error) {
	// Step 1：清理残留 .tmp
	if _, err := os.Stat(p.tmpPath); err == nil {
		if removeErr := os.Remove(p.tmpPath); removeErr != nil {
			log.Logger.Warn().
				Str("event", string(logger.Recovery)).
				Str("tmp_path", p.tmpPath).
				Err(removeErr).
				Msg("state.json.tmp 残留文件删除失败，将尝试继续读取 state.json")
		}
	}

	// Step 2：首次运行，state.json 不存在
	if _, err := os.Stat(p.path); errors.Is(err, os.ErrNotExist) {
		return StateData{CurrentState: string(StateLobby)}, nil
	}

	// Step 3：读取并反序列化
	data, err := os.ReadFile(p.path)
	if err != nil {
		return StateData{CurrentState: string(StateLobby)}, err
	}

	var sd StateData
	if err := json.Unmarshal(data, &sd); err != nil {
		return StateData{CurrentState: string(StateLobby)}, err
	}

	return sd, nil
}

// Save 将 StateData 原子写入 state.json。
// 流程：marshal → 写 .tmp → os.Rename（原子替换）。
// 失败时返回 error，主循环应 log 后继续（非关键路径）。
func (p *Persister) Save(data StateData) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// 追加换行符，提高可读性
	b = append(b, '\n')

	if err := os.WriteFile(p.tmpPath, b, 0600); err != nil {
		return err
	}

	return os.Rename(p.tmpPath, p.path)
}

// NeedsPasswordInput 返回 true 表示二级密码超过 24 小时未输入，
// 供 Epic 5 Story 5.2 的 recovery.go 决定是否触发密码输入流程。
func (p *Persister) NeedsPasswordInput(lastInput time.Time) bool {
	if lastInput.IsZero() {
		return true
	}
	now := time.Now()
	// 未来时间戳（时钟回拨或数据异常）视为需要输入，避免永久跳过密码检查
	if lastInput.After(now) {
		return true
	}
	return now.Sub(lastInput) > 24*time.Hour
}
