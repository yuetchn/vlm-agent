package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// validConfigYAML 合法的完整配置内容（grpc 模式）
const validConfigYAML = `
vlm:
  backend: grpc
  grpc_model_path: "models/test.gguf"
  http_endpoint: "http://localhost:11434/v1"
  http_model_name: "llava:latest"
llm:
  endpoint: "https://api.openai.com/v1"
  model: "gpt-4o"
game:
  mode: ranked
  max_sessions: 0
`

// TestLoadValidConfig 合法配置加载，所有字段正确反序列化
func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(validConfigYAML), 0600); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.VLM.Backend != "grpc" {
		t.Errorf("vlm.backend = %q, want grpc", cfg.VLM.Backend)
	}
	if cfg.VLM.GRPCModelPath != "models/test.gguf" {
		t.Errorf("vlm.grpc_model_path = %q", cfg.VLM.GRPCModelPath)
	}
	if cfg.LLM.Endpoint != "https://api.openai.com/v1" {
		t.Errorf("llm.endpoint = %q", cfg.LLM.Endpoint)
	}
	if cfg.LLM.Model != "gpt-4o" {
		t.Errorf("llm.model = %q", cfg.LLM.Model)
	}
	if cfg.Game.Mode != "ranked" {
		t.Errorf("game.mode = %q", cfg.Game.Mode)
	}
	if cfg.Game.MaxSessions != 0 {
		t.Errorf("game.max_sessions = %d", cfg.Game.MaxSessions)
	}
}

// TestLoadMissingRequiredField 缺少必填字段 → 含字段名的 ErrConfigInvalid
func TestLoadMissingRequiredField(t *testing.T) {
	const yamlStr = `
vlm:
  backend: grpc
  grpc_model_path: ""
llm:
  endpoint: "https://api.openai.com/v1"
  model: "gpt-4o"
game:
  mode: ranked
  max_sessions: 0
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing grpc_model_path, got nil")
	}
	const want = "vlm.grpc_model_path"
	if !containsStr(err.Error(), want) {
		t.Errorf("error %q does not contain field name %q", err.Error(), want)
	}
}

// TestLoadWrongFieldType vlm.backend 为非法值 → 返回含 vlm.backend 的错误
func TestLoadWrongFieldType(t *testing.T) {
	const yamlStr = `
vlm:
  backend: "invalid_backend"
  grpc_model_path: "x"
llm:
  endpoint: "https://api.openai.com/v1"
  model: "gpt-4o"
game:
  mode: ranked
  max_sessions: 0
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid backend")
	}
	if !containsStr(err.Error(), "vlm.backend") {
		t.Errorf("error %q does not mention vlm.backend", err.Error())
	}
}

// TestEncryptDecryptRoundtrip 加密 → 解密 → 原文相同
func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	plaintext := "hello world 你好世界"
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("roundtrip: got %q, want %q", got, plaintext)
	}
}

// TestEncryptEmptySecrets 空 SecretsData 整体往返，字段均为空字符串
func TestEncryptEmptySecrets(t *testing.T) {
	key := make([]byte, 32)
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")

	empty := SecretsData{}
	if err := SaveSecrets(path, key, empty); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSecrets(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.LLMAPIKey != "" || got.SecondaryPassword != "" {
		t.Errorf("expected empty secrets, got %+v", got)
	}
}

// TestDecryptWrongKey 不同密钥解密 → 返回 ErrDecryptFailed
func TestDecryptWrongKey(t *testing.T) {
	key := make([]byte, 32)
	ct, err := Encrypt(key, "secret")
	if err != nil {
		t.Fatal(err)
	}

	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xFF
	_, err = Decrypt(wrongKey, ct)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

// TestDecryptShortData 传入 < 12 字节数据 → 返回 ErrDecryptFailed（不 panic）
func TestDecryptShortData(t *testing.T) {
	key := make([]byte, 32)
	_, err := Decrypt(key, []byte{0x01, 0x02})
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

// TestSecretsRoundtrip SaveSecrets → LoadSecrets → SecretsData 完整恢复
func TestSecretsRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")

	original := SecretsData{LLMAPIKey: "my-api-key-12345", SecondaryPassword: ""}
	if err := SaveSecrets(path, key, original); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSecrets(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.LLMAPIKey != original.LLMAPIKey {
		t.Errorf("LLMAPIKey: got %q, want %q", got.LLMAPIKey, original.LLMAPIKey)
	}
	if got.SecondaryPassword != original.SecondaryPassword {
		t.Errorf("SecondaryPassword: got %q, want %q", got.SecondaryPassword, original.SecondaryPassword)
	}
}

// TestMachineFingerprintNotEmpty MachineFingerprint() 返回非空字符串
// Windows 上应含冒号（hostname:serial）；非 Windows 跳过。
func TestMachineFingerprintNotEmpty(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only: full fingerprint test requires disk serial number")
	}
	fp, err := MachineFingerprint()
	if err != nil {
		t.Fatalf("MachineFingerprint() error: %v", err)
	}
	if fp == "" {
		t.Error("fingerprint must not be empty")
	}
	if !containsStr(fp, ":") {
		t.Errorf("windows fingerprint should contain ':', got %q", fp)
	}
	t.Logf("fingerprint: %s", fp)
}

// TestLoadConfigNotFound 配置文件不存在时返回 ErrConfigNotFound（全平台可测）
func TestLoadConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	nonExistent := filepath.Join(dir, "does-not-exist.yaml")
	_, err := Load(nonExistent, "secrets.enc")
	if !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}

// TestLoadSecretsNotFound 配置合法但 secrets.enc 不存在时返回 ErrSecretsNotFound
// 需要 Windows 以获取机器指纹；非 Windows 跳过（MachineFingerprint 返回 ErrUnsupportedOS）。
func TestLoadSecretsNotFound(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Load() 集成路径需要 Windows 机器指纹")
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(validConfigYAML), 0600); err != nil {
		t.Fatal(err)
	}
	// secretsPath 故意不创建，触发 ErrSecretsNotFound
	secretsPath := filepath.Join(dir, "secrets.enc")
	_, err := Load(cfgPath, secretsPath)
	if !errors.Is(err, ErrSecretsNotFound) {
		t.Errorf("expected ErrSecretsNotFound, got %v", err)
	}
}

// containsStr 检查 s 是否包含 substr
func containsStr(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
