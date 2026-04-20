package checker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zerfx/new_jzd/internal/config"
)

// MapJSON 落点配置 JSON 结构（用于 maps/*.json 校验）
type MapJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MapRegion       string `json:"map_region"`
	MapPositionHint string `json:"map_position_hint"`
	Priority        int    `json:"priority"`
	DropPoints      []any  `json:"drop_points"`
}

// CheckConfig 校验 config.yaml schema、maps/*.json 格式及 secrets.enc 解密
func CheckConfig(cfg *config.Config) CheckResult {
	// 1. config.yaml schema 校验（直接调用已有 cfg.Validate()）
	if err := cfg.Validate(); err != nil {
		return CheckResult{Name: "配置文件", OK: false, Message: err.Error()}
	}

	// 2. 定位可执行文件目录（maps/ 和 secrets.enc 与 exe 同级）
	exePath, err := os.Executable()
	if err != nil {
		return CheckResult{Name: "配置文件", OK: false, Message: fmt.Sprintf("获取可执行文件路径失败: %v", err)}
	}
	exeDir := filepath.Dir(exePath)

	// 3. maps/*.json 校验
	pattern := filepath.Join(exeDir, "maps", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return CheckResult{Name: "落点配置", OK: false, Message: fmt.Sprintf("枚举 maps 目录失败: %v", err)}
	}

	totalDropPoints := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return CheckResult{Name: "落点配置", OK: false, Message: fmt.Sprintf("读取 %s 失败: %v", filepath.Base(f), err)}
		}
		var m MapJSON
		if err := json.Unmarshal(data, &m); err != nil {
			return CheckResult{Name: "落点配置", OK: false, Message: fmt.Sprintf("%s 格式错误: %v", filepath.Base(f), err)}
		}
		// 校验必填字段（priority 为 int，0 是合法值，不做非零校验）
		if m.ID == "" || m.Name == "" || m.MapRegion == "" || m.MapPositionHint == "" {
			return CheckResult{Name: "落点配置", OK: false, Message: fmt.Sprintf("%s 缺少必填字段（id/name/map_region/map_position_hint）", filepath.Base(f))}
		}
		totalDropPoints += len(m.DropPoints)
	}

	if len(files) == 0 || totalDropPoints == 0 {
		return CheckResult{Name: "落点配置", OK: false, Message: "未找到任何有效落点，请在 maps/ 目录下配置至少一个落点"}
	}

	// 4. secrets.enc 解密校验（独立调用，不依赖调用方传入的 cfg.Secrets）
	fp, err := config.MachineFingerprint()
	if err != nil {
		return CheckResult{Name: "配置加密", OK: false, Message: fmt.Sprintf("获取机器指纹失败: %v", err)}
	}
	key, err := config.DeriveKey(fp)
	if err != nil {
		return CheckResult{Name: "配置加密", OK: false, Message: fmt.Sprintf("密钥派生失败: %v", err)}
	}

	secretsPath := filepath.Join(exeDir, "secrets.enc")
	_, err = config.LoadSecrets(secretsPath, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CheckResult{Name: "配置加密", OK: false, Message: "secrets.enc 不存在，请运行初始化命令生成"}
		}
		return CheckResult{Name: "配置加密", OK: false, Message: "secrets.enc 解密失败，请检查文件或重新初始化"}
	}

	return CheckResult{
		Name:    "配置文件",
		OK:      true,
		Message: "config.yaml 校验通过，maps 落点已加载，secrets.enc 解密正常",
	}
}
