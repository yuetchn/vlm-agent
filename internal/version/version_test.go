package version

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ── Check() 测试 ──────────────────────────────────────────────────────────────

func TestCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.0.0"})
	}))
	defer srv.Close()

	got, err := Check(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Check() 期望无错误，得到 %v", err)
	}
	if got != "v2.0.0" {
		t.Errorf("Check() = %q，期望 %q", got, "v2.0.0")
	}
}

func TestCheck_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Check(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("Check() 期望返回错误（非 200），但得到 nil")
	}
}

func TestCheck_EmptyTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": ""})
	}))
	defer srv.Close()

	got, err := Check(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Check() 期望无错误，得到 %v", err)
	}
	if got != "" {
		t.Errorf("Check() = %q，期望空字符串", got)
	}
}

func TestCheck_EmptyURL(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
	}))
	defer srv.Close()

	got, err := Check(context.Background(), "")
	if err != nil {
		t.Fatalf("Check() url 为空时期望 nil 错误，得到 %v", err)
	}
	if got != "" {
		t.Errorf("Check() url 为空时期望空字符串，得到 %q", got)
	}
	if n := atomic.LoadInt32(&requestCount); n != 0 {
		t.Errorf("url 为空时不应发出网络请求，但 mock server 收到 %d 次请求", n)
	}
}

func TestCheck_Timeout(t *testing.T) {
	// 服务端 sleep 足够长，保证请求在 context 取消前不会完成
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.0.0"})
	}))
	defer srv.Close()

	// 使用已取消的 context，避免依赖 Windows 毫秒级定时精度
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消，确保 context 在 Check() 调用时已经失效

	_, err := Check(ctx, srv.URL)
	if err == nil {
		t.Fatal("Check() context 已取消时期望返回错误，但得到 nil")
	}
}

// ── IsNewer() 测试 ────────────────────────────────────────────────────────────

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v2.0.0", "v1.0.0", true},
		{"v1.2.0", "v1.1.9", true},
		{"v1.0.0", "v1.0.0", false}, // 相同版本
		{"v1.0.0", "v2.0.0", false}, // latest 旧于 current
		{"invalid", "v1.0.0", false}, // 解析失败不 panic
		{"v1.0.0", "dev", true},      // 本地开发版本：dev 归零，任何合法版本视为更新
	}
	for _, c := range cases {
		got := IsNewer(c.latest, c.current)
		if got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v，期望 %v", c.latest, c.current, got, c.want)
		}
	}
}
