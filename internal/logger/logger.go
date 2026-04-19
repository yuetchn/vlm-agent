package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// 在包加载时统一设置时间格式，避免 Init 并发调用时的全局变量竞争。
	zerolog.TimeFieldFormat = time.RFC3339
}

// asyncWriter 基于 channel 的异步 io.Writer，写入不阻塞调用方。
type asyncWriter struct {
	ch        chan []byte
	dest      io.Writer
	done      chan struct{}
	closeOnce sync.Once
}

// newAsyncWriter 创建 asyncWriter，bufSize 为 channel 缓冲大小（建议 4096）。
func newAsyncWriter(dest io.Writer, bufSize int) *asyncWriter {
	w := &asyncWriter{
		ch:   make(chan []byte, bufSize),
		dest: dest,
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Write 实现 io.Writer，将 p 的副本发送到 channel，立即返回。
// channel 满时此处会短暂阻塞；bufSize=4096 可保证游戏循环近似无等待。
// 若 close() 已调用，Write 通过 recover 捕获 panic 并静默丢弃。
func (w *asyncWriter) Write(p []byte) (n int, err error) {
	defer func() {
		if recover() != nil {
			n, err = len(p), nil // channel 已关闭，静默丢弃
		}
	}()
	buf := make([]byte, len(p))
	copy(buf, p)
	w.ch <- buf
	return len(p), nil
}

// close 关闭 channel 并等待 goroutine 将剩余数据全部写入。
// 使用 sync.Once 保证多次调用安全，避免重复 close 导致 panic。
func (w *asyncWriter) close() {
	w.closeOnce.Do(func() {
		close(w.ch)
		<-w.done
	})
}

// run 是后台 goroutine，将 channel 中的数据依次写入目标 writer。
func (w *asyncWriter) run() {
	defer close(w.done)
	for buf := range w.ch {
		if _, err := w.dest.Write(buf); err != nil {
			fmt.Fprintf(os.Stderr, "logger: async write error: %v\n", err)
		}
	}
}

// 全局实例及其保护锁。
var (
	globalAsync    *asyncWriter
	globalRotation *RotationWriter
	globalMu       sync.Mutex
)

// Init 初始化全局 zerolog logger，设置 JSON 格式 + 异步写入 + 日志滚动。
// logDir 为日志目录（如 "logs"），不存在时自动创建。
// 应在 main() 最开始调用。重复调用时会先关闭前一个实例，不泄露资源。
func Init(logDir string) error {
	// 先取出旧实例（锁外关闭，避免持锁期间阻塞）
	globalMu.Lock()
	oldAsync := globalAsync
	oldRotation := globalRotation
	globalAsync = nil
	globalRotation = nil
	globalMu.Unlock()

	if oldAsync != nil {
		oldAsync.close()
	}
	if oldRotation != nil {
		_ = oldRotation.Close()
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("logger: create log dir: %w", err)
	}

	rw, err := NewRotationWriter(logDir)
	if err != nil {
		return err
	}

	aw := newAsyncWriter(rw, 4096)
	log.Logger = zerolog.New(aw).With().Timestamp().Logger()

	globalMu.Lock()
	globalAsync = aw
	globalRotation = rw
	globalMu.Unlock()

	return nil
}

// Close 优雅关闭异步 writer，等待 channel 排空后返回，然后关闭底层文件。
// 应在 main() 退出前（如 defer Close()）调用。
func Close() {
	globalMu.Lock()
	async := globalAsync
	rotation := globalRotation
	globalAsync = nil
	globalRotation = nil
	globalMu.Unlock()

	if async != nil {
		async.close()
	}
	if rotation != nil {
		if err := rotation.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "logger: close rotation writer: %v\n", err)
		}
	}
}
