//go:build !windows

package checker

import "github.com/zerfx/new_jzd/internal/config"

// CheckGPU 在非 Windows 平台跳过 GPU 检查
func CheckGPU(cfg *config.Config) CheckResult {
	return CheckResult{Name: "GPU 可用性", OK: true, Message: "非 Windows 平台，跳过 GPU 检查"}
}
