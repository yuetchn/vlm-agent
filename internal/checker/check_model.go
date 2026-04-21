package checker

import (
	"os"

	"github.com/zerfx/new_jzd/internal/config"
)

// CheckModel 检查 VLM 模型文件是否存在（仅 grpc 模式）
func CheckModel(cfg *config.Config) CheckResult {
	if cfg.VLM.Backend != "grpc" {
		return CheckResult{Name: "模型文件", OK: true, Message: "HTTP 模式无需本地模型，跳过"}
	}

	path := cfg.VLM.GRPCModelPath
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:    "模型文件",
				OK:      false,
				Message: "路径 " + path + " 不存在，请下载模型",
			}
		}
		return CheckResult{
			Name:    "模型文件",
			OK:      false,
			Message: "路径 " + path + " 检查失败: " + err.Error(),
		}
	}
	if info.IsDir() {
		return CheckResult{
			Name:    "模型文件",
			OK:      false,
			Message: "路径 " + path + " 为目录而非模型文件，请检查路径",
		}
	}
	return CheckResult{Name: "模型文件", OK: true, Message: path + " 存在"}
}
