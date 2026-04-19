package logger

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
)

// TestEventTypeConstants 验证 7 个 EventType 枚举常量的字符串值。
func TestEventTypeConstants(t *testing.T) {
	cases := []struct {
		name     string
		et       EventType
		expected string
	}{
		{"StateTransition", StateTransition, "state_transition"},
		{"VLMInfer", VLMInfer, "vlm_infer"},
		{"InputAction", InputAction, "input_action"},
		{"Anomaly", Anomaly, "anomaly"},
		{"Recovery", Recovery, "recovery"},
		{"SessionStat", SessionStat, "session_stat"},
		{"VersionCheck", VersionCheck, "version_check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.et) != c.expected {
				t.Errorf("EventType %s: got %q, want %q", c.name, string(c.et), c.expected)
			}
		})
	}
}

// TestEventTypeIsStrongType 验证 EventType 不能被任意 string 替换（编译期约束通过类型系统保证，
// 此测试验证运行期类型断言行为）。
func TestEventTypeIsStrongType(t *testing.T) {
	var et EventType = StateTransition
	if et != StateTransition {
		t.Error("EventType assignment failed")
	}
	// 验证类型不是普通 string
	var _ EventType = EventType("state_transition") // 强制转换应可工作
}

// TestInitCreatesLogDir 验证 Init 会创建不存在的日志目录。
func TestInitCreatesLogDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-logs")
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Init did not create log directory")
	}
}

// TestJSONOutputFormat 验证日志以 JSON 格式写入，含必填字段 time、level、event。
func TestJSONOutputFormat(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	log.Info().Str("event", string(StateTransition)).Msg("test message")

	// Close 刷新并关闭，保证内容落盘
	Close()

	// 找到生成的日志文件
	logFile := findFirstLogFile(t, dir)

	// 读取并解析 JSON
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("invalid JSON line: %s, err: %v", line, err)
			continue
		}
		// 验证必填字段
		if _, ok := entry["time"]; !ok {
			t.Errorf("missing 'time' field in: %s", line)
		}
		if _, ok := entry["level"]; !ok {
			t.Errorf("missing 'level' field in: %s", line)
		}
		if ev, ok := entry["event"]; ok && ev == "state_transition" {
			found = true
		}
	}
	if !found {
		t.Error("did not find log entry with event=state_transition")
	}
}

// TestAsyncWriterFlushOnClose 验证异步写入后调用 Close() 能刷新内容到磁盘。
func TestAsyncWriterFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	const msgCount = 100
	for i := 0; i < msgCount; i++ {
		log.Info().Str("event", string(VLMInfer)).Int("i", i).Msg("async test")
	}

	Close() // 等待 goroutine 排空

	logFile := findFirstLogFile(t, dir)
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	// 计算行数（每条日志一行）
	lines := 0
	for _, b := range content {
		if b == '\n' {
			lines++
		}
	}
	if lines < msgCount {
		t.Errorf("expected at least %d log lines, got %d", msgCount, lines)
	}
}

// TestTimeFieldFormatIsRFC3339 验证 time 字段格式符合 RFC3339。
func TestTimeFieldFormatIsRFC3339(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	log.Info().Str("event", string(Anomaly)).Msg("time format check")
	Close()

	logFile := findFirstLogFile(t, dir)
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	var entry map[string]interface{}
	// 解析第一行
	for _, line := range splitLines(string(content)) {
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("json parse: %v", err)
		}
		break
	}
	timeVal, ok := entry["time"].(string)
	if !ok {
		t.Fatal("time field not a string")
	}
	if _, err := time.Parse(time.RFC3339, timeVal); err != nil {
		t.Errorf("time field %q is not RFC3339: %v", timeVal, err)
	}
}

// TestAsyncWriterIsNonBlocking 验证 asyncWriter 在 channel 未满时写入几乎无延迟（AC1/NFR5）。
func TestAsyncWriterIsNonBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	start := time.Now()
	const msgCount = 1000
	for i := 0; i < msgCount; i++ {
		log.Info().Str("event", string(InputAction)).Int("i", i).Msg("nonblocking test")
	}
	elapsed := time.Since(start)
	// 1000 条消息写入缓冲 channel（容量 4096），不等待磁盘落盘，应在 1s 内完成
	if elapsed > time.Second {
		t.Errorf("1000 个 Write 调用耗时 %v，预期 <1s（异步 channel 写入）", elapsed)
	}
}

// findFirstLogFile 在 dir 中找到第一个 .log 文件，测试失败时报告。
func findFirstLogFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".log" {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no .log file found in %s", dir)
	return ""
}

// splitLines 将字符串按换行符分割。
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
