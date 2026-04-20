//go:build windows

package checker

import (
	"os"
	"path/filepath"
)

// checkVLMGRPC 检查 vlm_server.exe 是否存在于可执行文件同级目录（Windows 专属）
func checkVLMGRPC() CheckResult {
	exePath, err := os.Executable()
	if err != nil {
		return CheckResult{Name: "VLM 服务", OK: false, Message: "获取可执行文件路径失败: " + err.Error()}
	}
	exeDir := filepath.Dir(exePath)
	vlmPath := filepath.Join(exeDir, "vlm_server.exe")

	info, err := os.Stat(vlmPath)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{Name: "VLM 服务", OK: false, Message: "vlm_server.exe 未找到，请检查安装目录"}
		}
		return CheckResult{Name: "VLM 服务", OK: false, Message: "vlm_server.exe 检查失败: " + err.Error()}
	}
	if info.IsDir() {
		return CheckResult{Name: "VLM 服务", OK: false, Message: "vlm_server.exe 路径为目录而非文件，请检查安装目录"}
	}
	if info.Size() == 0 {
		return CheckResult{Name: "VLM 服务", OK: false, Message: "vlm_server.exe 文件为空，请重新安装"}
	}
	return CheckResult{Name: "VLM 服务", OK: true, Message: "vlm_server.exe 存在"}
}
