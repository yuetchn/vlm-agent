//go:build !windows

package checker

import "github.com/zerfx/new_jzd/internal/config"

// CheckProcess 在非 Windows 平台返回不支持提示
func CheckProcess(cfg *config.Config) CheckResult {
	return CheckResult{Name: "游戏进程", OK: false, Message: "不支持非 Windows 平台"}
}
