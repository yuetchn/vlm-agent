package checker

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zerfx/new_jzd/internal/config"
)

// CheckVLM 检查 VLM 服务可用性
// grpc 模式：检查 vlm_server.exe 文件存在性（不启动）
// http 模式：检查 HTTP 端点连通性
func CheckVLM(cfg *config.Config) CheckResult {
	if cfg.VLM.Backend == "grpc" {
		return checkVLMGRPC()
	}
	return checkVLMHTTP(cfg.VLM.HTTPEndpoint)
}

// checkVLMHTTP 通过 GET {endpoint}/v1/models 检查 HTTP 端点连通性（超时 5s）
func checkVLMHTTP(endpoint string) CheckResult {
	client := &http.Client{Timeout: 5 * time.Second}
	// 去除尾部斜杠，防止双斜杠 URL
	url := strings.TrimRight(endpoint, "/") + "/v1/models"
	resp, err := client.Get(url)
	if err != nil {
		return CheckResult{
			Name:    "VLM HTTP 端点",
			OK:      false,
			Message: fmt.Sprintf("%s 不可达（连接失败：%v）", endpoint, err),
		}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK {
		return CheckResult{Name: "VLM HTTP 端点", OK: true, Message: endpoint + " 可达"}
	}
	return CheckResult{
		Name:    "VLM HTTP 端点",
		OK:      false,
		Message: fmt.Sprintf("%s 不可达（HTTP %d）", endpoint, resp.StatusCode),
	}
}
