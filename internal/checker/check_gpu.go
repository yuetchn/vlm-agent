//go:build windows

package checker

import (
	"os/exec"
	"strings"

	"github.com/zerfx/new_jzd/internal/config"
)

// CheckGPU 检查 NVIDIA GPU 可用性（仅 grpc 模式）（Windows 专属）
func CheckGPU(cfg *config.Config) CheckResult {
	if cfg.VLM.Backend != "grpc" {
		return CheckResult{Name: "GPU 可用性", OK: true, Message: "HTTP 模式无需 GPU，跳过"}
	}

	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    "GPU 可用性",
			OK:      false,
			Message: "未检测到支持 CUDA 的 NVIDIA 驱动，gRPC 模式需要 GPU",
		}
	}

	// 多 GPU 时 nvidia-smi 每行一个名称，只取第一行
	gpuName := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if gpuName == "" {
		return CheckResult{
			Name:    "GPU 可用性",
			OK:      false,
			Message: "未检测到支持 CUDA 的 NVIDIA 驱动，gRPC 模式需要 GPU",
		}
	}
	return CheckResult{
		Name:    "GPU 可用性",
		OK:      true,
		Message: "检测到 NVIDIA GPU: " + gpuName,
	}
}
