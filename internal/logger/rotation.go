package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxFileSize  = 50 * 1024 * 1024  // 50MB 单文件上限
	maxTotalSize = 500 * 1024 * 1024 // 500MB 总量上限
	maxAgeDays   = 30                // 最大保留天数
	maxSeqNum    = 10000             // openFile 序号上限，防止无限循环
)

// RotationWriter 实现 io.Writer，按天滚动，并限制单文件大小、总量及文件年龄。
type RotationWriter struct {
	logDir          string
	currentFile     *os.File
	currentFilePath string // 当前打开的文件路径，cleanup 时跳过以防误删
	currentDate     string
	currentSize     int64
	seqNum          int
	mu              sync.Mutex
}

// NewRotationWriter 创建一个新的 RotationWriter，logDir 为日志目录（必须已存在）。
func NewRotationWriter(logDir string) (*RotationWriter, error) {
	w := &RotationWriter{
		logDir: logDir,
	}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write 实现 io.Writer，将 p 写入当前日志文件，必要时自动滚动。
func (w *RotationWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.rotateIfNeeded(int64(len(p))); err != nil {
		return 0, err
	}
	if w.currentFile == nil {
		return 0, fmt.Errorf("logger: no log file open")
	}
	n, err := w.currentFile.Write(p)
	w.currentSize += int64(n)
	return n, err
}

// Close 关闭当前日志文件。
func (w *RotationWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.currentFile != nil {
		err := w.currentFile.Close()
		w.currentFile = nil
		return err
	}
	return nil
}

// rotateIfNeeded 检查是否需要滚动（日期变化或文件超限），如需要则关闭当前文件并打开新文件。
// 调用前必须持有 w.mu 锁。
func (w *RotationWriter) rotateIfNeeded(writeSize int64) error {
	today := time.Now().Format("2006-01-02")
	needDateRotation := w.currentDate != today
	needSizeRotation := !needDateRotation && (w.currentSize+writeSize) > maxFileSize

	if !needDateRotation && !needSizeRotation {
		return nil
	}

	if w.currentFile != nil {
		_ = w.currentFile.Close()
		w.currentFile = nil
	}

	if needDateRotation {
		// 日期变化：重置序列号
		w.seqNum = 0
	} else {
		// 大小超限：递增序列号，强制 openFile 创建新文件
		w.seqNum++
	}

	return w.openFile()
}

// openFile 打开（或创建）当前日志文件，并执行清理。
// 调用前必须持有 w.mu 锁（或在初始化时调用）。
// 最多尝试 maxSeqNum 次，防止磁盘满等极端情况下的无限循环。
func (w *RotationWriter) openFile() error {
	today := time.Now().Format("2006-01-02")

	// 先执行清理（跳过 currentFilePath，防止删除正在使用的文件）
	w.cleanup()

	for i := 0; i < maxSeqNum; i++ {
		var filePath string
		if w.seqNum == 0 {
			filePath = filepath.Join(w.logDir, fmt.Sprintf("app-%s.log", today))
		} else {
			filePath = filepath.Join(w.logDir, fmt.Sprintf("app-%s-%d.log", today, w.seqNum))
		}

		info, err := os.Stat(filePath)
		if err != nil {
			// 文件不存在，创建新文件
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("logger: create log file: %w", err)
			}
			w.currentFile = f
			w.currentFilePath = filePath
			w.currentDate = today
			w.currentSize = 0
			return nil
		}

		if info.Size() < maxFileSize {
			// 文件存在但未超限，继续写入
			f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("logger: open log file: %w", err)
			}
			w.currentFile = f
			w.currentFilePath = filePath
			w.currentDate = today
			w.currentSize = info.Size()
			return nil
		}

		// 文件已超限，尝试下一个序号
		w.seqNum++
	}

	return fmt.Errorf("logger: cannot find available log file in %s after %d attempts", w.logDir, maxSeqNum)
}

// logFileInfo 保存日志文件的路径和日期，用于排序和清理。
type logFileInfo struct {
	path     string
	fileDate time.Time // 从文件名解析的日期，比 mtime 更可靠
	size     int64
}

// parseLogFileDate 从文件名中解析日期。
// 支持格式：app-YYYY-MM-DD.log 和 app-YYYY-MM-DD-N.log。
func parseLogFileDate(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, "app-") || filepath.Ext(name) != ".log" {
		return time.Time{}, false
	}
	// 去掉 "app-" 前缀和 ".log" 后缀
	inner := name[4 : len(name)-4]
	// 取前三段作为日期（YYYY-MM-DD），忽略可能的序号后缀
	parts := strings.SplitN(inner, "-", 4)
	if len(parts) < 3 {
		return time.Time{}, false
	}
	dateStr := strings.Join(parts[:3], "-")
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// cleanup 删除超过 maxAgeDays 的旧文件，以及当总量超过 maxTotalSize 时最旧的文件（FIFO）。
// 使用文件名中的日期（而非 mtime）判断年龄，避免备份恢复等场景下的误判。
// 跳过 currentFilePath，防止删除正在写入的文件。
func (w *RotationWriter) cleanup() {
	entries, err := os.ReadDir(w.logDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	var files []logFileInfo
	var totalSize int64

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		fileDate, ok := parseLogFileDate(name)
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(w.logDir, name)

		// 跳过当前正在写入（或刚关闭等待轮转）的文件
		if fullPath == w.currentFilePath {
			continue
		}

		// 删除超龄文件（按文件名日期判断，而非 mtime）
		if fileDate.Before(cutoff) {
			_ = os.Remove(fullPath)
			continue
		}

		files = append(files, logFileInfo{
			path:     fullPath,
			fileDate: fileDate,
			size:     info.Size(),
		})
		totalSize += info.Size()
	}

	// 按文件日期升序排列（最旧的在前）
	sort.Slice(files, func(i, j int) bool {
		return files[i].fileDate.Before(files[j].fileDate)
	})

	// 删除最旧文件直到总量不超过 maxTotalSize
	for totalSize > maxTotalSize && len(files) > 0 {
		oldest := files[0]
		files = files[1:]
		totalSize -= oldest.size
		_ = os.Remove(oldest.path)
	}
}
