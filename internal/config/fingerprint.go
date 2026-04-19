//go:build windows

package config

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// MachineFingerprint 返回机器唯一指纹：hostname + ":" + C盘序列号（8位十六进制）
// 用于密钥派生，保证 secrets.enc 机器绑定。
func MachineFingerprint() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("config: fingerprint: get hostname: %w", err)
	}

	serial, err := getDiskSerial()
	if err != nil {
		return "", err
	}

	return hostname + ":" + serial, nil
}

// getDiskSerial 获取 C: 盘的卷序列号，格式化为 8 位十六进制字符串
func getDiskSerial() (string, error) {
	var serial uint32
	err := windows.GetVolumeInformation(
		windows.StringToUTF16Ptr(`C:\`), nil, 0, &serial, nil, nil, nil, 0,
	)
	if err != nil {
		return "", fmt.Errorf("config: get disk serial: %w", err)
	}
	return fmt.Sprintf("%08X", serial), nil
}
