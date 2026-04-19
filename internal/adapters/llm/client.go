package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog"

	"github.com/zerfx/new_jzd/internal/logger"
	"github.com/zerfx/new_jzd/internal/ports"
)

// retryDelays 定义指数退避重试间隔（共尝试 4 次 = 首次 + 重试 3 次）。
var retryDelays = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
}

// LLMClient 是 LLMDecider 的 HTTP 实现，兼容 OpenAI Chat Completion API。
type LLMClient struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
	logger     zerolog.Logger
	// sleepFn 可注入，默认为 time.Sleep，测试时注入 no-op 避免 CI 等待 35s
	sleepFn func(time.Duration)
}

// NewLLMClient 创建 LLM 客户端。sleepFn 默认为 time.Sleep。
func NewLLMClient(endpoint, model, apiKey string, log zerolog.Logger) *LLMClient {
	return &LLMClient{
		endpoint:   endpoint,
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     log,
		sleepFn:    time.Sleep,
	}
}

// llmRequest 是 OpenAI 兼容请求体。
type llmRequest struct {
	Model    string       `json:"model"`
	Messages []llmMessage `json:"messages"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmResponse 是 OpenAI Chat Completion API 响应体。
type llmResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Decide 调用 LLM 服务获取决策文本，满足 ports.LLMDecider 接口。
// 仅对网络错误进行指数退避重试（共 4 次尝试，重试 3 次）。
// HTTP 4xx/5xx 不重试，直接返回错误。
func (c *LLMClient) Decide(ctx context.Context, systemPrompt, userContent string) (string, error) {
	reqBody := llmRequest{
		Model: c.model,
		Messages: []llmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		result, err := c.doRequest(ctx, bodyBytes)
		if err == nil {
			return result, nil
		}

		// D3: context.Canceled 表示调用方主动取消，立即返回，不触发重试睡眠。
		// context.DeadlineExceeded 可能是单次请求超时（而非外层 ctx 到期），保留重试。
		if errors.Is(err, context.Canceled) {
			return "", err
		}

		// 判断是否为网络错误（使用 errors.As，不能用 errors.Is 判断 *url.Error）
		var urlErr *url.Error
		isNetworkErr := errors.As(err, &urlErr) ||
			errors.Is(err, context.DeadlineExceeded)

		if !isNetworkErr {
			// HTTP 4xx/5xx 或其他非网络错误，直接返回，不重试
			return "", err
		}

		lastErr = err

		if attempt < len(retryDelays) {
			delay := retryDelays[attempt]
			c.logger.Warn().
				Str("event", string(logger.Anomaly)).
				Int("retry_count", attempt+1).
				Dur("retry_after", delay).
				Err(err).
				Msg("llm request failed, retrying with backoff")
			c.sleepFn(delay)
		}
	}

	// 4 次均失败，返回 ErrVLMServiceDown（语义：任何 AI 服务不可用）
	return "", fmt.Errorf("%w: %v", ports.ErrVLMServiceDown, lastErr)
}

// doRequest 执行单次 HTTP 请求，返回 choices[0].message.content。
// HTTP 状态码 >= 400 时返回错误（不重试）。
func (c *LLMClient) doRequest(ctx context.Context, bodyBytes []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("llm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err // 网络错误，由 Decide() 判断是否重试
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// P7: 使用 LimitReader 防止恶意/异常服务端流式返回超大 error body
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return "", fmt.Errorf("llm: http %d: %s", resp.StatusCode, string(body))
	}

	var apiResp llmResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices in response")
	}

	return apiResp.Choices[0].Message.Content, nil
}
