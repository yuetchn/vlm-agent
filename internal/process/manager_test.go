package process

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerfx/new_jzd/internal/config"
)

// TestFreePort 验证两次调用均返回非零且互不相同的端口。
func TestFreePort(t *testing.T) {
	m := &Manager{}
	port1, err := m.freePort()
	if err != nil {
		t.Fatalf("freePort() 第一次调用失败: %v", err)
	}
	port2, err := m.freePort()
	if err != nil {
		t.Fatalf("freePort() 第二次调用失败: %v", err)
	}
	if port1 == 0 {
		t.Error("freePort() 第一次返回端口为 0")
	}
	if port2 == 0 {
		t.Error("freePort() 第二次返回端口为 0")
	}
	if port1 == port2 {
		t.Errorf("freePort() 两次返回相同端口 %d，预期互不相同", port1)
	}
}

// TestStartHTTP_Success 验证 HTTP 200 返回 nil。
func TestStartHTTP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		VLM: config.VLMConfig{
			Backend:      "http",
			HTTPEndpoint: srv.URL,
		},
	}
	if err := StartHTTP(cfg); err != nil {
		t.Errorf("StartHTTP() 预期 nil，实际返回: %v", err)
	}
}

// TestStartHTTP_Failure 验证 HTTP 500 返回 error。
func TestStartHTTP_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{
		VLM: config.VLMConfig{
			Backend:      "http",
			HTTPEndpoint: srv.URL,
		},
	}
	err := StartHTTP(cfg)
	if err == nil {
		t.Error("StartHTTP() 预期返回 error，实际返回 nil")
	}
}

// TestStartHTTP_TrailingSlash 验证尾部斜杠被正确去除，URL 构造无双斜杠。
func TestStartHTTP_TrailingSlash(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/models" {
			t.Errorf("URL 路径错误：预期 /v1/models，实际 %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{
		VLM: config.VLMConfig{
			Backend:      "http",
			HTTPEndpoint: srv.URL + "/", // 尾部斜杠
		},
	}
	if err := StartHTTP(cfg); err != nil {
		t.Errorf("StartHTTP() 预期 nil，实际返回: %v", err)
	}
	if !called {
		t.Error("请求未发出")
	}
}

// TestStartHTTP_Unreachable 验证无法连接时返回包含端点信息的 error。
func TestStartHTTP_Unreachable(t *testing.T) {
	cfg := &config.Config{
		VLM: config.VLMConfig{
			Backend:      "http",
			HTTPEndpoint: "http://127.0.0.1:1", // 不可达端口
		},
	}
	err := StartHTTP(cfg)
	if err == nil {
		t.Fatal("预期返回 error，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "HTTP VLM 端点不可达") {
		t.Errorf("error 消息格式错误：%v", err)
	}
}

// TestHealthMonitor_RestartCounter 验证重启计数器递增，第 4 次触发 OnMaxRestartsExceeded。
func TestHealthMonitor_RestartCounter(t *testing.T) {
	mgr := &Manager{}
	hm := NewHealthMonitor(mgr)

	// 注入 mock：始终认为进程已退出
	hm.IsAlive = func(pid uint32) bool { return false }

	maxExceeded := false
	hm.OnMaxRestartsExceeded = func() { maxExceeded = true }

	// 覆盖 manager.Start，避免实际启动子进程
	// 通过直接调用 onProcessDied 模拟进程死亡事件
	ctx := context.Background()

	// 第 1、2、3 次：应重启，不触发上限
	for i := 1; i <= 3; i++ {
		hm.onProcessDied(ctx)
		if hm.restartCount != i {
			t.Errorf("第 %d 次后 restartCount 预期 %d，实际 %d", i, i, hm.restartCount)
		}
		if maxExceeded {
			t.Errorf("第 %d 次不应触发 OnMaxRestartsExceeded", i)
		}
	}

	// 第 4 次：restartCount > 3，触发上限回调
	hm.onProcessDied(ctx)
	if hm.restartCount != 4 {
		t.Errorf("第 4 次后 restartCount 预期 4，实际 %d", hm.restartCount)
	}
	if !maxExceeded {
		t.Error("第 4 次应触发 OnMaxRestartsExceeded")
	}
}

// TestHealthMonitor_TimeWindowReset 验证时间窗口过期后重启计数器归零。
func TestHealthMonitor_TimeWindowReset(t *testing.T) {
	mgr := &Manager{}
	hm := NewHealthMonitor(mgr)
	hm.IsAlive = func(pid uint32) bool { return false }
	hm.OnMaxRestartsExceeded = func() {}

	// 手动设置：已处于旧时间窗口（2小时前），restartCount=3
	hm.firstRestartTime = time.Now().Add(-2 * time.Hour)
	hm.restartCount = 3

	ctx := context.Background()
	hm.onProcessDied(ctx)

	// 时间窗口过期，应重置为 1（不是 4）
	if hm.restartCount != 1 {
		t.Errorf("时间窗口重置后 restartCount 预期 1，实际 %d", hm.restartCount)
	}
}

// TestWaitReady_Timeout 验证 waitReady 超时路径返回正确错误（无需等待 60s）。
func TestWaitReady_Timeout(t *testing.T) {
	m := &Manager{port: 19999} // 不会有服务监听的端口

	ctx := context.Background()
	// 使用 1ms 超时，立即触发超时路径
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // 确保已超时

	err := m.waitReady(timeoutCtx, 1*time.Millisecond)
	if err == nil {
		t.Fatal("预期返回超时 error，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "VLM 服务启动超时") {
		t.Errorf("超时错误消息格式错误：%v", err)
	}
}
