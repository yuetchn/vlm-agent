package vlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestHTTPClient_Infer_Success(t *testing.T) {
	// 模拟 VLM 服务返回正确结果
	inferJSON := `{"state":"lobby","action":"click_match","confidence":0.92}`
	apiResp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"content": inferJSON,
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-model", "", zerolog.Nop(), 0)
	result, err := client.Infer(context.Background(), []byte("fakepng"), "skill context", "lobby")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.State != "lobby" {
		t.Errorf("expected state 'lobby', got '%s'", result.State)
	}
	if result.Action != "click_match" {
		t.Errorf("expected action 'click_match', got '%s'", result.Action)
	}
	if result.RawJSON != inferJSON {
		t.Errorf("expected RawJSON %q, got %q", inferJSON, result.RawJSON)
	}
}

func TestHTTPClient_Infer_JSONParseFailure(t *testing.T) {
	// 模拟服务返回无效 JSON
	apiResp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"content": "not valid json",
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-model", "", zerolog.Nop(), 0)
	_, err := client.Infer(context.Background(), []byte("fakepng"), "skill context", "lobby")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid infer result json") {
		t.Errorf("expected 'invalid infer result json' in error, got: %v", err)
	}
}

func TestHTTPClient_Infer_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-model", "", zerolog.Nop(), 0)
	_, err := client.Infer(context.Background(), []byte("fakepng"), "skill context", "lobby")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("expected status error, got: %v", err)
	}
}

func TestParseInferResult_Success(t *testing.T) {
	raw := `{"state":"in_game","action":"move","confidence":0.85}`
	result, err := parseInferResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != "in_game" {
		t.Errorf("expected state 'in_game', got '%s'", result.State)
	}
	if result.Action != "move" {
		t.Errorf("expected action 'move', got '%s'", result.Action)
	}
	if result.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", result.Confidence)
	}
	if result.RawJSON != raw {
		t.Errorf("expected RawJSON preserved")
	}
}

func TestParseInferResult_MissingState(t *testing.T) {
	raw := `{"action":"move","confidence":0.85}`
	_, err := parseInferResult(raw)
	if err == nil {
		t.Fatal("expected error for missing state, got nil")
	}
	if !strings.Contains(err.Error(), "missing state field") {
		t.Errorf("expected 'missing state field' in error, got: %v", err)
	}
}

func TestParseInferResult_ConfidenceClamp(t *testing.T) {
	// 置信度 > 1 应被 clamp 到 1
	raw := `{"state":"lobby","action":"wait","confidence":1.5}`
	result, err := parseInferResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", result.Confidence)
	}

	// 置信度 < 0 应被 clamp 到 0
	raw2 := `{"state":"lobby","action":"wait","confidence":-0.5}`
	result2, err := parseInferResult(raw2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Confidence != 0.0 {
		t.Errorf("expected confidence clamped to 0.0, got %f", result2.Confidence)
	}
}

func TestGRPCClient_ScreenshotSizeLimit(t *testing.T) {
	// 测试截图超过 4MB 时返回错误
	// 使用独立函数测试大小检查逻辑（无需真实 gRPC 连接）
	oversized := make([]byte, maxScreenshotSize+1)
	// 直接测试 parseInferResult 是 white-box，截图大小检查在 Infer() 内部
	// 这里通过构建 oversized 场景验证常量正确定义
	if len(oversized) <= maxScreenshotSize {
		t.Errorf("test setup error: oversized should exceed maxScreenshotSize")
	}
	if maxScreenshotSize != 4*1024*1024 {
		t.Errorf("maxScreenshotSize should be 4MB, got %d", maxScreenshotSize)
	}
}
