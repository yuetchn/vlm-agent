package process

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/logger"
)

// HealthMonitor 持续监控 VLM 子进程，检测到意外退出时自动重启。
// IsAlive 字段支持依赖注入，便于单元测试 mock。
type HealthMonitor struct {
	manager              *Manager
	restartCount         int
	firstRestartTime     time.Time
	IsAlive              func(pid uint32) bool // 依赖注入：生产传 isAliveWindows，测试传 fake
	OnMaxRestartsExceeded func()               // 回调：通知调用方（FSM anomaly 在 Story 3.x 实现）
	mu                   sync.Mutex
}

// NewHealthMonitor 创建 HealthMonitor，IsAlive 默认使用平台专属实现。
func NewHealthMonitor(m *Manager) *HealthMonitor {
	return &HealthMonitor{
		manager: m,
		IsAlive: isAliveWindows, // 两平台均可编译；非 Windows stub 返回 false
	}
}

// Run 启动监控循环，每 2s 检查一次子进程存活状态。
// ctx 取消时退出循环。
func (hm *HealthMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pid := hm.manager.Pid()
			if pid == 0 {
				continue
			}
			if !hm.IsAlive(pid) {
				hm.onProcessDied(ctx)
				// 若已触发 OnMaxRestartsExceeded，等待 ctx 取消
			}
		}
	}
}

// onProcessDied 处理进程退出事件：维护重启计数器时间窗口，决定是否重启或触发上限回调。
// hm.mu 仅保护计数器状态，在调用 m.Start()（最长阻塞 60s）前释放，避免长时持锁。
func (hm *HealthMonitor) onProcessDied(ctx context.Context) {
	hm.mu.Lock()

	// 时间窗口重置：首次重启或时间窗口已过期（> 1 小时）
	if hm.restartCount == 0 || time.Since(hm.firstRestartTime) > time.Hour {
		hm.restartCount = 0
		hm.firstRestartTime = time.Now()
	}
	hm.restartCount++
	count := hm.restartCount

	hm.mu.Unlock() // 释放锁后再执行耗时操作

	log.Logger.Info().
		Str("event", string(logger.Recovery)).
		Int("restart_count", count).
		Msg("VLM 子进程意外退出，尝试重启")

	if count > 3 {
		log.Logger.Error().
			Str("event", string(logger.Recovery)).
			Msg("VLM 子进程在1小时内重启次数超过3次，进入异常状态")
		if hm.OnMaxRestartsExceeded != nil {
			hm.OnMaxRestartsExceeded()
		}
		return
	}

	if err := hm.manager.Start(ctx); err != nil {
		log.Logger.Error().Err(err).Msg("VLM 子进程重启失败")
	}
}
