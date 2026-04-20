//go:build windows

package process

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const vlmProcessName = "vlm_server.exe"

// killResidual 枚举进程快照，终止所有名为 vlm_server.exe 的进程。
// 找不到残留进程时静默返回 nil。
func killResidual() error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return fmt.Errorf("process: snapshot failed: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snapshot, &pe); err != nil {
		return nil // 无进程，静默返回
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), vlmProcessName) {
			h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pe.ProcessID)
			if err == nil {
				_ = windows.TerminateProcess(h, 0)
				windows.CloseHandle(h)
			}
		}
		if err := windows.Process32Next(snapshot, &pe); err != nil {
			break
		}
	}
	return nil
}
