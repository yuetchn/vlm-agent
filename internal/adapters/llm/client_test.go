package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/zerfx/new_jzd/internal/ports"
)

// nopSleep 是测试用的 no-op sleep 函数，避免 CI 等待重试间隔
func nopSleep(d time.Duration) {}

func makeTestClient(endpoint string) *LLMClient {
	c := NewLLMClient(endpoint, "test-model", "test-key", zerolog.Nop())
	c.sleepFn = nopSleep // 注入 no-op sleep
	return c
}

func TestLLMClient_Decide_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "decided action"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := makeTestClient(server.URL)
	result, err := client.Decide(context.Background(), "system prompt", "user content")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "decided action" {
		t.Errorf("expected 'decided action', got '%s'", result)
	}
}

func TestLLMClient_Decide_NetworkRetry_ThenSuccess(t *testing.T) {
	// 模拟前 3 次网络错误，第 4 次成功
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 3 {
			// 关闭连接模拟网络错误
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		// 第 4 次成功
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "success after retry"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := makeTestClient(server.URL)
	result, err := client.Decide(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if result != "success after retry" {
		t.Errorf("expected 'success after retry', got '%s'", result)
	}
	if callCount.Load() != 4 {
		t.Errorf("expected 4 calls (1 initial + 3 retries), got %d", callCount.Load())
	}
}

func TestLLMClient_Decide_AllFail_ReturnsErrVLMServiceDown(t *testing.T) {
	// 模拟所有 4 次均网络失败
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer server.Close()

	client := makeTestClient(server.URL)
	_, err := client.Decide(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrVLMServiceDown) {
		t.Errorf("expected ErrVLMServiceDown, got: %v", err)
	}
}

func TestLLMClient_Decide_HTTPError_NoRetry(t *testing.T) {
	// 模拟 HTTP 4xx 错误，不应重试
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := makeTestClient(server.URL)
	_, err := client.Decide(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
	// HTTP 4xx 不重试，只调用 1 次
	if callCount.Load() != 1 {
		t.Errorf("expected exactly 1 call for HTTP error (no retry), got %d", callCount.Load())
	}
	// 不应该是 ErrVLMServiceDown
	if errors.Is(err, ports.ErrVLMServiceDown) {
		t.Errorf("HTTP 400 should not return ErrVLMServiceDown, got: %v", err)
	}
}
