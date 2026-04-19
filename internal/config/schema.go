package config

import "fmt"

// VLMConfig 视觉语言模型后端配置
type VLMConfig struct {
	Backend       string `yaml:"backend"`          // 必填：grpc | http
	GRPCModelPath string `yaml:"grpc_model_path"`  // grpc 模式必填
	HTTPEndpoint  string `yaml:"http_endpoint"`    // http 模式必填
	HTTPModelName string `yaml:"http_model_name"`  // http 模式必填
}

// LLMConfig 大语言模型决策接口配置
type LLMConfig struct {
	Endpoint string `yaml:"endpoint"` // 必填
	Model    string `yaml:"model"`    // 必填
}

// GameConfig 游戏模式设置
type GameConfig struct {
	Mode        string `yaml:"mode"`         // 必填：ranked | casual | arcade
	MaxSessions int    `yaml:"max_sessions"` // 0=无限制
}

// SecretsData 敏感数据，从 secrets.enc 解密填充，不从 config.yaml 读取
type SecretsData struct {
	LLMAPIKey         string `json:"llm_api_key"`
	SecondaryPassword string `json:"secondary_password"` // 预留，当前始终为空；写入时机由后续二级密码相关 story 处理
}

// Config 顶层配置 struct
type Config struct {
	VLM     VLMConfig   `yaml:"vlm"`
	LLM     LLMConfig   `yaml:"llm"`
	Game    GameConfig  `yaml:"game"`
	Secrets SecretsData `yaml:"-" json:"-"` // 不从 yaml/json 读取，从 secrets.enc 填充；json:"-" 防止 json.Marshal 明文泄露
}

// Validate 校验配置字段合法性，返回包含具体字段路径的错误（NFR19）
func (c *Config) Validate() error {
	// vlm.backend
	if c.VLM.Backend != "grpc" && c.VLM.Backend != "http" {
		return fmt.Errorf("config: field vlm.backend: must be 'grpc' or 'http', got %q", c.VLM.Backend)
	}
	// grpc 模式必填
	if c.VLM.Backend == "grpc" && c.VLM.GRPCModelPath == "" {
		return fmt.Errorf("config: field vlm.grpc_model_path: required when vlm.backend is 'grpc'")
	}
	// http 模式必填
	if c.VLM.Backend == "http" {
		if c.VLM.HTTPEndpoint == "" {
			return fmt.Errorf("config: field vlm.http_endpoint: required when vlm.backend is 'http'")
		}
		if c.VLM.HTTPModelName == "" {
			return fmt.Errorf("config: field vlm.http_model_name: required when vlm.backend is 'http'")
		}
	}
	// llm.endpoint
	if c.LLM.Endpoint == "" {
		return fmt.Errorf("config: field llm.endpoint: required, must not be empty")
	}
	// llm.model
	if c.LLM.Model == "" {
		return fmt.Errorf("config: field llm.model: required, must not be empty")
	}
	// game.mode
	switch c.Game.Mode {
	case "ranked", "casual", "arcade":
	default:
		return fmt.Errorf("config: field game.mode: must be 'ranked', 'casual', or 'arcade', got %q", c.Game.Mode)
	}
	// game.max_sessions
	if c.Game.MaxSessions < 0 {
		return fmt.Errorf("config: field game.max_sessions: must be >= 0")
	}
	return nil
}
