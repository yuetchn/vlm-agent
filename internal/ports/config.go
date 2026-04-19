package ports

import "github.com/zerfx/new_jzd/internal/config"

// ConfigStore 提供全局配置只读访问，FSM 和适配器通过此接口访问配置
type ConfigStore interface {
	GetConfig() *config.Config
}
