package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/scrypt"
)

// ErrDecryptFailed 解密失败（机器不匹配或文件损坏）
var ErrDecryptFailed = errors.New("config: decrypt failed, wrong machine or corrupted file")

// fixedSalt 固定盐，16字节，保证密钥派生确定性。上线后禁止修改。
var fixedSalt = []byte("new_jzd-v1-salt\x00")

// DeriveKey 从机器指纹派生 AES-256 密钥（scrypt N=32768, r=8, p=1, keyLen=32）
// 密钥每次运行时实时派生，绝不写入磁盘（NFR11）。
func DeriveKey(fingerprint string) ([]byte, error) {
	key, err := scrypt.Key([]byte(fingerprint), fixedSalt, 32768, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("config: derive key: %w", err)
	}
	return key, nil
}

// Encrypt 使用 AES-256-GCM 加密明文，输出格式：nonce(12B) || ciphertext+tag
// nonce 必须来自 crypto/rand，绝不使用固定 nonce（防止密钥流泄露）。
func Encrypt(key []byte, plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("config: encrypt: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("config: encrypt: create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("config: generate nonce: %w", err)
	}
	// Seal 将 nonce 作为 dst 前缀，输出：nonce || ciphertext+tag
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

// Decrypt 解密 AES-256-GCM 密文（格式：nonce(12B) || ciphertext+tag）
// 先校验最小长度，防止 panic；解密失败返回 ErrDecryptFailed（不暴露底层错误）。
func Decrypt(key []byte, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrDecryptFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrDecryptFailed
	}
	minLen := gcm.NonceSize() + gcm.Overhead()
	if len(data) < minLen {
		return "", ErrDecryptFailed
	}
	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptFailed
	}
	return string(plaintext), nil
}

// SaveSecrets 将 SecretsData JSON 序列化后加密写入文件
func SaveSecrets(path string, key []byte, s SecretsData) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("config: save secrets: marshal: %w", err)
	}
	encrypted, err := Encrypt(key, string(raw))
	if err != nil {
		return fmt.Errorf("config: save secrets: %w", err)
	}
	if err := os.WriteFile(path, encrypted, 0600); err != nil {
		return fmt.Errorf("config: save secrets: write file: %w", err)
	}
	return nil
}

// LoadSecrets 从文件读取并解密 SecretsData
func LoadSecrets(path string, key []byte) (SecretsData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SecretsData{}, fmt.Errorf("config: load secrets: read file: %w", err)
	}
	plaintext, err := Decrypt(key, data)
	if err != nil {
		return SecretsData{}, err
	}
	var s SecretsData
	if err := json.Unmarshal([]byte(plaintext), &s); err != nil {
		return SecretsData{}, fmt.Errorf("config: load secrets: unmarshal: %w", err)
	}
	return s, nil
}
