package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// 具体错误变量
var (
	ErrConfigNotFound   = errors.New("config: config file not found")
	ErrConfigInvalid    = errors.New("config: invalid configuration")
	ErrSecretsNotFound  = errors.New("config: secrets file not found")
	ErrSecretsCorrupted = errors.New("config: secrets file corrupted or wrong machine")
)

// Load 从 configPath 加载配置并从 secretsPath 解密敏感字段。
//
// 流程：
//  1. 读取并 YAML 解析 configPath → Config struct
//  2. 调用 Validate() 做 schema 校验
//  3. MachineFingerprint() → DeriveKey() 派生密钥
//  4. 直接读取 secretsPath：不存在返回 ErrSecretsNotFound；存在则解密填充 cfg.Secrets
//
// 典型调用：config.Load("config.yaml", "secrets.enc")
func Load(configPath, secretsPath string) (*Config, error) {
	// 1. 读取并解析 YAML
	// P2: 仅在 ENOENT 时返回 ErrConfigNotFound，其他 I/O 错误单独报告
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w", ErrConfigNotFound)
		}
		return nil, fmt.Errorf("config: read config: %w", err)
	}

	var cfg Config
	// P4: 用 %w 保留内层错误链，支持 errors.Is/As 追溯
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigInvalid, fmt.Errorf("config: load yaml: %w", err))
	}

	// 2. Schema 校验
	// P5: Validate() 已返回 "config: field X: Y" 格式（NFR19），外层仅附加 sentinel
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigInvalid, err)
	}

	// 3. 派生密钥
	fp, err := MachineFingerprint()
	if err != nil {
		return nil, fmt.Errorf("config: machine fingerprint: %w", err)
	}
	// P3: DeriveKey 失败时添加上下文包装，不再返回裸 error
	key, err := DeriveKey(fp)
	if err != nil {
		return nil, fmt.Errorf("config: derive key: %w", err)
	}

	// 4. 加载 secrets.enc
	// P1: 移除 os.Stat 预检，直接 ReadFile 后判断 ENOENT，消除 TOCTOU 竞态
	secrets, err := LoadSecrets(secretsPath, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: 首次运行请先执行配置初始化以生成 secrets.enc", ErrSecretsNotFound)
		}
		// P4: 用 %w 保留 ErrDecryptFailed 等内层错误链
		return nil, fmt.Errorf("%w: %w", ErrSecretsCorrupted, err)
	}
	cfg.Secrets = secrets

	return &cfg, nil
}
