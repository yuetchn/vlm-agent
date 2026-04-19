//go:build windows

package vlm

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/zerfx/new_jzd/internal/logger"
	"github.com/zerfx/new_jzd/internal/ports"
	vlmpb "github.com/zerfx/new_jzd/internal/vlmpb"
)

const maxScreenshotSize = 4 * 1024 * 1024

// GRPCClient 是 VLMInferrer 的 gRPC 实现。
// 注意：grpc.NewClient() 是惰性连接，构造函数不会立即建立 TCP 连接，
// 因此即使 Python VLM 服务未启动，NewGRPCClient 也不会返回 error。
// 连接错误只在第一次真正的 RPC 调用（Infer 或 HealthCheck）时才暴露。
type GRPCClient struct {
	conn                *grpc.ClientConn
	stub                vlmpb.VLMServiceClient
	confidenceThreshold float32 // D2: 置信度阈值，0 表示禁用；比较使用 epsilon=1e-6 处理 float32 精度
	logger              zerolog.Logger
}

// NewGRPCClient 创建 gRPC VLM 客户端。
// addr 为完整地址字符串，如 "127.0.0.1:50051"。
// confidenceThreshold 为 0 时跳过置信度门控。
func NewGRPCClient(addr string, log zerolog.Logger, confidenceThreshold float32) (*GRPCClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("vlm grpc: dial %s: %w", addr, err)
	}
	return &GRPCClient{
		conn:                conn,
		stub:                vlmpb.NewVLMServiceClient(conn),
		confidenceThreshold: confidenceThreshold,
		logger:              log,
	}, nil
}

// Infer 调用 VLM gRPC 服务进行推理，满足 ports.VLMInferrer 接口。
func (c *GRPCClient) Infer(ctx context.Context, screenshot []byte, skillContext, stateHint string) (ports.InferResult, error) {
	// 截图大小前置校验（gRPC 默认 4MB 消息上限）
	if len(screenshot) > maxScreenshotSize {
		return ports.InferResult{}, fmt.Errorf("vlm: screenshot %d bytes exceeds 4MB gRPC limit", len(screenshot))
	}

	resp, err := c.stub.Infer(ctx, &vlmpb.InferRequest{
		Screenshot:   screenshot,
		SkillContext: skillContext,
		StateHint:    stateHint,
	})
	if err != nil {
		return ports.InferResult{}, fmt.Errorf("vlm grpc: infer rpc: %w", err)
	}

	result, err := parseInferResult(resp.Result)
	if err != nil {
		return ports.InferResult{}, err
	}

	// D2: float32 精度处理——使用 epsilon 避免浮点边界误判（Story 1.2 deferred）
	if c.confidenceThreshold > 0 && result.Confidence < c.confidenceThreshold-1e-6 {
		return ports.InferResult{}, ports.ErrVLMLowConfidence
	}

	c.logger.Info().
		Str("event", string(logger.VLMInfer)).
		Str("backend", "grpc").
		Float32("confidence", result.Confidence).
		Str("state", result.State).
		Str("action", result.Action).
		Msg("vlm infer ok")

	return result, nil
}

// HealthCheck 检查 VLM 服务是否就绪。
func (c *GRPCClient) HealthCheck(ctx context.Context) (bool, error) {
	resp, err := c.stub.HealthCheck(ctx, &vlmpb.HealthCheckRequest{})
	if err != nil {
		return false, fmt.Errorf("vlm grpc: health check: %w", err)
	}
	return resp.Ready, nil
}

// Close 关闭 gRPC 连接。
func (c *GRPCClient) Close() error {
	return c.conn.Close()
}
