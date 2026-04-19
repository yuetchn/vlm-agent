//go:build !windows

package config

import "errors"

// ErrUnsupportedOS 在非 Windows 平台上调用 MachineFingerprint 时返回。
// secrets.enc 机器绑定仅在 Windows 生产环境中有效；非 Windows 不支持解密。
var ErrUnsupportedOS = errors.New("config: fingerprint: unsupported OS, machine binding requires Windows")

// MachineFingerprint 非 Windows 平台实现：直接返回 ErrUnsupportedOS。
// 生产环境须在 Windows 上运行；CI 环境中调用 Load() 时将收到此错误，
// 测试应使用不依赖 Load() 的单元测试路径（直接调用 Encrypt/Decrypt/Validate）。
func MachineFingerprint() (string, error) {
	return "", ErrUnsupportedOS
}
