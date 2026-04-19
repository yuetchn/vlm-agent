package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRotationWriterBasicWrite 验证基本写入功能。
func TestRotationWriterBasicWrite(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRotationWriter(dir)
	if err != nil {
		t.Fatalf("NewRotationWriter: %v", err)
	}
	defer rw.Close()

	data := []byte(`{"time":"2026-04-13T00:00:00Z","level":"info","event":"test"}` + "\n")
	n, err := rw.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
}

// TestRotationWriterFileSizeTriggersRotation 验证文件超过 50MB 时触发滚动。
func TestRotationWriterFileSizeTriggersRotation(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRotationWriter(dir)
	if err != nil {
		t.Fatalf("NewRotationWriter: %v", err)
	}

	// 预先将 currentSize 设置为接近 maxFileSize，模拟即将超限
	rw.mu.Lock()
	rw.currentSize = maxFileSize - 10
	rw.mu.Unlock()

	// 写入超过剩余容量的数据，应触发滚动
	overflowData := make([]byte, 20) // 10+20 > maxFileSize
	_, err = rw.Write(overflowData)
	if err != nil {
		t.Fatalf("Write after size limit: %v", err)
	}
	rw.Close()

	// 应该有 2 个日志文件（原文件 + 新滚动文件）
	logFiles := listLogFiles(t, dir)
	if len(logFiles) < 2 {
		t.Errorf("expected at least 2 log files after rotation, got %d: %v", len(logFiles), logFiles)
	}
}

// TestRotationWriterAgeCleanup 验证超过 30 天的文件被删除。
func TestRotationWriterAgeCleanup(t *testing.T) {
	dir := t.TempDir()

	// 创建一个超过 30 天的旧日志文件
	oldFile := filepath.Join(dir, "app-2025-01-01.log")
	if err := os.WriteFile(oldFile, []byte("old log\n"), 0644); err != nil {
		t.Fatalf("create old file: %v", err)
	}
	// 设置文件修改时间为 31 天前
	oldTime := time.Now().AddDate(0, 0, -31)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("set mtime: %v", err)
	}

	// 创建 RotationWriter 会触发 cleanup
	rw, err := NewRotationWriter(dir)
	if err != nil {
		t.Fatalf("NewRotationWriter: %v", err)
	}
	defer rw.Close()

	// 旧文件应已被删除
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old log file to be deleted, but it still exists")
	}
}

// TestRotationWriterTotalSizeLimit 验证总量超过 500MB 时删除最旧文件。
func TestRotationWriterTotalSizeLimit(t *testing.T) {
	dir := t.TempDir()

	// 创建多个日志文件，使总量超过 500MB
	// 每个文件 100MB，6 个文件 = 600MB > 500MB
	// 文件修改时间依次递增，最旧的是 file1
	base := time.Now().AddDate(0, 0, -10) // 都在 30 天内
	fileNames := make([]string, 6)
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("app-%s-%d.log", base.AddDate(0, 0, -5+i).Format("2006-01-02"), i)
		path := filepath.Join(dir, name)
		// 使用稀疏文件（sparse file）创建 100MB 占位，避免实际写入 600MB 数据
		f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			t.Fatalf("create fake log file: %v", ferr)
		}
		if terr := f.Truncate(100 * 1024 * 1024); terr != nil {
			f.Close()
			t.Fatalf("truncate fake log file: %v", terr)
		}
		f.Close()
		mtime := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("set mtime: %v", err)
		}
		fileNames[i] = path
	}

	// 创建 RotationWriter 触发 cleanup
	rw, err := NewRotationWriter(dir)
	if err != nil {
		t.Fatalf("NewRotationWriter: %v", err)
	}
	defer rw.Close()

	// 计算剩余总量，应 <= 500MB（加上新创建的空文件）
	var totalSize int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".log" {
			continue
		}
		info, _ := e.Info()
		totalSize += info.Size()
	}
	if totalSize > maxTotalSize {
		t.Errorf("total log size %d exceeds limit %d after cleanup", totalSize, maxTotalSize+maxFileSize)
	}

	// 最旧的文件（fileNames[0]）应已被删除
	if _, err := os.Stat(fileNames[0]); !os.IsNotExist(err) {
		t.Error("expected oldest file to be deleted, but it still exists")
	}
}

// TestRotationWriterDateRotation 验证跨日期时触发滚动。
func TestRotationWriterDateRotation(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRotationWriter(dir)
	if err != nil {
		t.Fatalf("NewRotationWriter: %v", err)
	}

	// 模拟日期变化：将 currentDate 设置为昨天
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rw.mu.Lock()
	rw.currentDate = yesterday
	rw.mu.Unlock()

	// 写入数据应触发日期滚动
	_, err = rw.Write([]byte("new day log\n"))
	if err != nil {
		t.Fatalf("Write on new day: %v", err)
	}
	rw.Close()

	// 应有今天日期的日志文件
	today := time.Now().Format("2006-01-02")
	logFiles := listLogFiles(t, dir)
	foundToday := false
	for _, f := range logFiles {
		if strings.Contains(f, today) {
			foundToday = true
			break
		}
	}
	if !foundToday {
		t.Errorf("expected log file with today's date %s, got: %v", today, logFiles)
	}
}

// listLogFiles 返回 dir 中所有 .log 文件名（不含路径）。
func listLogFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".log" {
			files = append(files, e.Name())
		}
	}
	return files
}
