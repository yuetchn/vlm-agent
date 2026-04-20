//go:build windows

package checker

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/zerfx/new_jzd/internal/config"
	"golang.org/x/sys/windows"
)

const pubgProcessName = "TslGame.exe"

// CheckProcess 检查 PUBG 游戏进程是否运行（Windows 专属）
func CheckProcess(cfg *config.Config) CheckResult {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return CheckResult{Name: "游戏进程", OK: false, Message: "进程枚举失败: " + err.Error()}
	}
	defer windows.CloseHandle(snapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snapshot, &pe); err != nil {
		return CheckResult{Name: "游戏进程", OK: false, Message: "进程枚举失败（权限不足或系统异常）: " + err.Error()}
	}
	for {
		name := windows.UTF16ToString(pe.ExeFile[:])
		if strings.EqualFold(name, pubgProcessName) {
			return CheckResult{
				Name:    "游戏进程",
				OK:      true,
				Message: fmt.Sprintf("PUBG 进程已运行（PID: %d）", pe.ProcessID),
			}
		}
		if err := windows.Process32Next(snapshot, &pe); err != nil {
			break
		}
	}
	return CheckResult{Name: "游戏进程", OK: false, Message: "PUBG 进程未检测到，请先启动游戏"}
}
