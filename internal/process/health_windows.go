//go:build windows

package process

import "golang.org/x/sys/windows"

// isAliveWindows 通过 WinAPI 检查指定 PID 的进程是否存活。
// WaitForSingleObject(handle, 0)：0ms 超时。
// WAIT_OBJECT_0 (0x0)   = 进程已退出 → 返回 false
// WAIT_TIMEOUT  (0x102) = 进程存活   → 返回 true
// WAIT_FAILED   (0xFFFFFFFF) = API 错误 → 视为已退出，返回 false
func isAliveWindows(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false // 进程不存在或无权限，视为已退出
	}
	defer windows.CloseHandle(h)
	s, _ := windows.WaitForSingleObject(h, 0)
	// WAIT_OBJECT_0 (0x0) = 进程已退出；WAIT_FAILED (0xFFFFFFFF) = API 错误
	// 两者均视为进程不存活
	return s != windows.WAIT_OBJECT_0 && s != windows.WAIT_FAILED
}
