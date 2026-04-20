package process

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zerfx/new_jzd/internal/config"
	"github.com/zerfx/new_jzd/internal/vlmpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Manager 管理 VLM 子进程的生命周期（grpc 模式）。
type Manager struct {
	port int
	cmd  *exec.Cmd
	mu   sync.Mutex
	cfg  *config.Config
}

// NewManager 创建新的 Manager 实例。
func NewManager(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// freePort 通过监听 127.0.0.1:0 获取随机空闲端口。
// 关闭 listener 后端口号会短暂空闲，存在极短竞态窗口（127.0.0.1 本地可接受）。
func (m *Manager) freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("process: freePort: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// Start 执行 grpc 模式完整启动序列：清理残留进程 → 随机端口 → 启动子进程 → 等待就绪。
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 回收上一个子进程，避免产生僵尸进程
	if m.cmd != nil {
		_ = m.cmd.Wait()
		m.cmd = nil
	}

	if err := killResidual(); err != nil {
		return fmt.Errorf("process: killResidual: %w", err)
	}

	port, err := m.freePort()
	if err != nil {
		return err
	}
	m.port = port

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("process: get executable path: %w", err)
	}
	vlmServerPath := filepath.Join(filepath.Dir(exePath), "vlm_server.exe")

	cmd := exec.CommandContext(ctx, vlmServerPath, "--port", strconv.Itoa(m.port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("process: start vlm_server: %w", err)
	}
	m.cmd = cmd

	fmt.Println("VLM 加载中...")

	return m.waitReady(ctx, 60*time.Second)
}

// waitReady 轮询 gRPC HealthCheck 直至就绪或超时。
// timeout 参数化，便于测试注入短超时（生产调用传 60*time.Second）。
func (m *Manager) waitReady(parentCtx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", m.port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("process: gRPC dial failed: %w", err)
	}
	defer conn.Close()

	client := vlmpb.NewVLMServiceClient(conn)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("VLM 服务启动超时，请检查模型路径或 GPU 可用性")
		case <-ticker.C:
			resp, err := client.HealthCheck(ctx, &vlmpb.HealthCheckRequest{})
			// gRPC 错误在子进程启动初期属正常（服务未就绪），继续轮询不提前返回
			if err == nil && resp.GetReady() {
				fmt.Println("VLM 就绪，可开始挂机")
				return nil
			}
		}
	}
}

// StartHTTP 验证 http 模式下外部 VLM 端点可达性。
func StartHTTP(cfg *config.Config) error {
	endpoint := strings.TrimRight(cfg.VLM.HTTPEndpoint, "/")
	if endpoint == "" {
		return fmt.Errorf("HTTP VLM 端点未配置（http_endpoint 为空）")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint + "/v1/models")
	if err != nil {
		return fmt.Errorf("HTTP VLM 端点不可达：%s", endpoint)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP VLM 端点不可达：%s", endpoint)
	}
	return nil
}

// Stop 向子进程发送 kill 信号并清理。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
}

// Pid 返回子进程 PID（供 HealthMonitor 使用）。
func (m *Manager) Pid() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		return uint32(m.cmd.Process.Pid)
	}
	return 0
}
