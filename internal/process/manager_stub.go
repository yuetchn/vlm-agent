//go:build !windows

package process

// killResidual 非 Windows 平台存根，直接返回 nil。
func killResidual() error { return nil }
