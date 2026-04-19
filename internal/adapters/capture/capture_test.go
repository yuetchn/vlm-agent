package capture

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
)

// makePNG 生成最小有效 PNG 字节用于测试。
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to create test PNG: %v", err)
	}
	return buf.Bytes()
}

// 测试1：primary 返回 error → fallback 被调用并返回有效 PNG bytes
func TestCaptureManager_Fallback_WhenPrimaryFails(t *testing.T) {
	validPNG := makePNG(t)
	fallbackCalled := false

	mgr := NewCaptureManager(
		func() ([]byte, error) {
			return nil, errors.New("primary failed")
		},
		func() ([]byte, error) {
			fallbackCalled = true
			return validPNG, nil
		},
	)

	result, err := mgr.Capture(context.Background())
	if err != nil {
		t.Fatalf("expected no error after fallback, got: %v", err)
	}
	if !fallbackCalled {
		t.Error("expected fallback to be called, but it was not")
	}
	if !bytes.Equal(result, validPNG) {
		t.Error("expected fallback PNG result")
	}
}

// 测试2：primary 成功 → fallback 不被调用
func TestCaptureManager_NoFallback_WhenPrimarySucceeds(t *testing.T) {
	validPNG := makePNG(t)
	fallbackCalled := false

	mgr := NewCaptureManager(
		func() ([]byte, error) {
			return validPNG, nil
		},
		func() ([]byte, error) {
			fallbackCalled = true
			return nil, errors.New("fallback should not be called")
		},
	)

	result, err := mgr.Capture(context.Background())
	if err != nil {
		t.Fatalf("expected no error when primary succeeds, got: %v", err)
	}
	if fallbackCalled {
		t.Error("fallback should NOT be called when primary succeeds")
	}
	if !bytes.Equal(result, validPNG) {
		t.Error("expected primary PNG result")
	}
}

// 测试3：primary 和 fallback 均失败 → 返回组合错误
func TestCaptureManager_BothFail_ReturnsCombinedError(t *testing.T) {
	mgr := NewCaptureManager(
		func() ([]byte, error) {
			return nil, errors.New("primary error")
		},
		func() ([]byte, error) {
			return nil, errors.New("fallback error")
		},
	)

	_, err := mgr.Capture(context.Background())
	if err == nil {
		t.Fatal("expected error when both primary and fallback fail")
	}
	// 错误消息应包含两个失败信息
	if !strings.Contains(err.Error(), "primary failed") {
		t.Errorf("expected error to mention primary failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fallback also failed") {
		t.Errorf("expected error to mention fallback failure, got: %v", err)
	}
}
