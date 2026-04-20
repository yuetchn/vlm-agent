//go:build !windows

package process

// isAliveWindows 非 Windows 平台存根，始终返回 false。
func isAliveWindows(pid uint32) bool { return false }
