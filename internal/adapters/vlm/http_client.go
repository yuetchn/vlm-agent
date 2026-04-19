package vlm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/zerfx/new_jzd/internal/logger"
	"github.com/zerfx/new_jzd/internal/ports"
)

// HTTPClient 是 VLMInferrer 的 HTTP 实现，兼容 OpenAI Vision API（Ollama / LM Studio / 云端）。
type HTTPClient struct {
	endpoint            string  // 完整路径，如 http://localhost:11434/v1/chat/completions
	modelName           string
	apiKey              string  // P6: 可选 API key；本地 Ollama/LM Studio 可传空字符串
	confidenceThreshold float32 // D2: 置信度阈值，0 表示禁用；比较使用 epsilon=1e-6 处理 float32 精度
	httpClient          *http.Client
	logger              zerolog.Logger
}

// NewHTTPClient 创建 HTTP VLM 客户端，超时 30s。
// apiKey 为空时不添加 Authorization header（适用于本地 Ollama/LM Studio）。
// confidenceThreshold 为 0 时跳过置信度门控。
func NewHTTPClient(endpoint, modelName, apiKey string, log zerolog.Logger, confidenceThreshold float32) *HTTPClient {
	return &HTTPClient{
		endpoint:            endpoint,
		modelName:           modelName,
		apiKey:              apiKey,
		confidenceThreshold: confidenceThreshold,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log,
	}
}

// openAIVisionRequest 是 OpenAI Vision API 兼容的请求体。
type openAIVisionRequest struct {
	Model    string              `json:"model"`
	Messages []openAIVisionMsg   `json:"messages"`
}

type openAIVisionMsg struct {
	Role    string              `json:"role"`
	Content []openAIContentPart `json:"content"`
}

type openAIContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *openAIImageURL   `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

// openAIResponse 是 OpenAI Chat Completion API 的响应体。
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Infer 调用 HTTP VLM 服务进行推理，满足 ports.VLMInferrer 接口。
func (c *HTTPClient) Infer(ctx context.Context, screenshot []byte, skillContext, stateHint string) (ports.InferResult, error) {
	// Base64 编码截图
	b64 := base64.StdEncoding.EncodeToString(screenshot)

	// 构造 OpenAI Vision 格式请求体
	reqBody := openAIVisionRequest{
		Model: c.modelName,
		Messages: []openAIVisionMsg{
			{
				Role: "user",
				Content: []openAIContentPart{
					{
						Type:     "image_url",
						ImageURL: &openAIImageURL{URL: "data:image/png;base64," + b64},
					},
					{
						Type: "text",
						Text: skillContext + "\nCurrent state: " + stateHint,
					},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm http: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm http: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// P6: 云端接口（OpenAI / 兼容 API）需要 Authorization header；本地 Ollama/LM Studio 传空 apiKey 跳过
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm http: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// P7: 使用 LimitReader 防止恶意/异常服务端流式返回超大 error body
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return ports.InferResult{}, fmt.Errorf("vlm http: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm http: decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return ports.InferResult{}, fmt.Errorf("vlm http: empty choices in response")
	}

	rawContent := apiResp.Choices[0].Message.Content
	result, err := parseInferResult(rawContent)
	if err != nil {
		return ports.InferResult{}, err
	}

	// D2: float32 精度处理——使用 epsilon 避免浮点边界误判（Story 1.2 deferred）
	if c.confidenceThreshold > 0 && result.Confidence < c.confidenceThreshold-1e-6 {
		return ports.InferResult{}, ports.ErrVLMLowConfidence
	}

	c.logger.Info().
		Str("event", string(logger.VLMInfer)).
		Str("backend", "http").
		Float32("confidence", result.Confidence).
		Str("state", result.State).
		Str("action", result.Action).
		Msg("vlm infer ok")

	return result, nil
}
