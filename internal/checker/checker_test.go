package checker_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zerfx/new_jzd/internal/checker"
	"github.com/zerfx/new_jzd/internal/config"
)

// makeGRPCConfig 构造 grpc 模式配置（Validate 能通过）
func makeGRPCConfig(modelPath string) *config.Config {
	return &config.Config{
		VLM: config.VLMConfig{
			Backend:       "grpc",
			GRPCModelPath: modelPath,
		},
		LLM:  config.LLMConfig{Endpoint: "http://localhost:8080", Model: "gpt-4"},
		Game: config.GameConfig{Mode: "casual", MaxSessions: 0},
	}
}

// makeHTTPConfig 构造 http 模式配置
func makeHTTPConfig(endpoint string) *config.Config {
	return &config.Config{
		VLM: config.VLMConfig{
			Backend:       "http",
			HTTPEndpoint:  endpoint,
			HTTPModelName: "test-model",
		},
		LLM:  config.LLMConfig{Endpoint: "http://localhost:8080", Model: "gpt-4"},
		Game: config.GameConfig{Mode: "casual", MaxSessions: 0},
	}
}

// ───────────── RunAll ─────────────

func TestRunAll_AllFail_ReturnsAllResults(t *testing.T) {
	// 使用 grpc 模式 + 不存在的模型路径，确保部分检查失败
	cfg := makeGRPCConfig("/nonexistent/model.gguf")
	results, allPassed := checker.RunAll(cfg)

	// 应返回 5 项结果（无论单项是否失败）
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
	// 模型路径不存在，allPassed 应为 false
	if allPassed {
		t.Error("expected allPassed=false when model path does not exist")
	}
}

func TestRunAll_ContinuesAfterSingleFailure(t *testing.T) {
	cfg := makeGRPCConfig("/nonexistent/model.gguf")
	results, _ := checker.RunAll(cfg)

	// 即使有失败项，仍应返回 5 个结果（不中断）
	if len(results) != 5 {
		t.Errorf("RunAll should always return 5 results, got %d", len(results))
	}
}

// ───────────── CheckModel ─────────────

func TestCheckModel_GRPC_PathExists(t *testing.T) {
	// 创建临时文件模拟模型文件
	tmpDir := t.TempDir()
	modelPath := filepath.Join(tmpDir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := makeGRPCConfig(modelPath)
	r := checker.CheckModel(cfg)

	if !r.OK {
		t.Errorf("expected OK=true, got false. Message: %s", r.Message)
	}
	if r.Name != "模型文件" {
		t.Errorf("expected Name='模型文件', got %q", r.Name)
	}
}

func TestCheckModel_GRPC_PathNotExist(t *testing.T) {
	cfg := makeGRPCConfig("/nonexistent/path/model.gguf")
	r := checker.CheckModel(cfg)

	if r.OK {
		t.Error("expected OK=false when model path does not exist")
	}
	if r.Name != "模型文件" {
		t.Errorf("expected Name='模型文件', got %q", r.Name)
	}
}

func TestCheckModel_HTTP_Skipped(t *testing.T) {
	cfg := makeHTTPConfig("http://localhost:11434")
	r := checker.CheckModel(cfg)

	if !r.OK {
		t.Errorf("expected OK=true (skip) for http mode, got false. Message: %s", r.Message)
	}
	if r.Name != "模型文件" {
		t.Errorf("expected Name='模型文件', got %q", r.Name)
	}
}

// ───────────── CheckVLM HTTP 模式 ─────────────

func TestCheckVLM_HTTP_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	cfg := makeHTTPConfig(srv.URL)
	r := checker.CheckVLM(cfg)

	if !r.OK {
		t.Errorf("expected OK=true for HTTP 200, got false. Message: %s", r.Message)
	}
	if r.Name != "VLM HTTP 端点" {
		t.Errorf("expected Name='VLM HTTP 端点', got %q", r.Name)
	}
}

func TestCheckVLM_HTTP_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := makeHTTPConfig(srv.URL)
	r := checker.CheckVLM(cfg)

	if r.OK {
		t.Error("expected OK=false for HTTP 500")
	}
	if r.Name != "VLM HTTP 端点" {
		t.Errorf("expected Name='VLM HTTP 端点', got %q", r.Name)
	}
}

func TestCheckVLM_HTTP_ConnectionFailed(t *testing.T) {
	// 使用不可达地址
	cfg := makeHTTPConfig("http://127.0.0.1:19999")
	r := checker.CheckVLM(cfg)

	if r.OK {
		t.Error("expected OK=false when connection fails")
	}
	if r.Name != "VLM HTTP 端点" {
		t.Errorf("expected Name='VLM HTTP 端点', got %q", r.Name)
	}
}

// ───────────── CheckConfig ─────────────

// mapEntry 用于构造临时 map JSON 文件
type mapEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MapRegion       string `json:"map_region"`
	MapPositionHint string `json:"map_position_hint"`
	Priority        int    `json:"priority"`
	DropPoints      []any  `json:"drop_points"`
}

// TestCheckConfig_EmptyMaps 测试 maps 目录为空时返回落点配置错误
// 注意：CheckConfig 使用 os.Executable() 定位目录，测试中通过调整工作目录来绕过
// 实际上 CheckConfig 无法直接在测试中测试 exe 路径，这里只测试逻辑验证部分
// 通过创建内存测试配置验证 Validate() 失败分支
func TestCheckConfig_InvalidConfig_ReturnsError(t *testing.T) {
	// 故意构造 Validate() 失败的 Config（backend 为空）
	cfg := &config.Config{
		VLM:  config.VLMConfig{Backend: "invalid"},
		LLM:  config.LLMConfig{Endpoint: "http://localhost", Model: "gpt"},
		Game: config.GameConfig{Mode: "casual"},
	}
	r := checker.CheckConfig(cfg)

	if r.OK {
		t.Error("expected OK=false for invalid config")
	}
	if r.Name != "配置文件" {
		t.Errorf("expected Name='配置文件', got %q", r.Name)
	}
}

// TestCheckConfig_WithTempMapsDir 通过文件系统辅助测试落点配置校验逻辑
// 因为 CheckConfig 内部调用 os.Executable() 定位 maps 目录，此测试仅验证
// MapJSON 结构正确解析（通过外部可测逻辑）
func TestMapJSON_MissingDropPoints(t *testing.T) {
	tmpDir := t.TempDir()
	mapsDir := filepath.Join(tmpDir, "maps")
	if err := os.MkdirAll(mapsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 写入空 drop_points 的 JSON 文件
	entry := mapEntry{
		ID:              "erangel",
		Name:            "Erangel",
		MapRegion:       "asia",
		MapPositionHint: "north",
		Priority:        1,
		DropPoints:      []any{},
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(filepath.Join(mapsDir, "erangel.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// 验证 MapJSON 结构本身能被正确反序列化
	var m checker.MapJSON
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("expected valid JSON unmarshal, got error: %v", err)
	}
	if len(m.DropPoints) != 0 {
		t.Errorf("expected 0 drop points, got %d", len(m.DropPoints))
	}
}

// ───────────── PrintResults ─────────────

func TestPrintResults_DoesNotPanic(t *testing.T) {
	results := []checker.CheckResult{
		{Name: "测试A", OK: true, Message: "通过"},
		{Name: "测试B", OK: false, Message: "失败"},
	}
	// 只验证不 panic（stdout 输出不做断言）
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PrintResults panicked: %v", r)
		}
	}()
	checker.PrintResults(results)
}
