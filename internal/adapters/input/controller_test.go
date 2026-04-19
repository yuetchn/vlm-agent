//go:build !windows

package input

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// 非 Windows 存根测试：验证所有方法返回 not-supported 错误

func TestController_Stub_Click(t *testing.T) {
	c := NewController(zerolog.Nop())
	err := c.Click(context.Background(), 100, 200)
	if err == nil {
		t.Fatal("expected error on non-windows, got nil")
	}
	if !strings.Contains(err.Error(), "not supported on non-windows") {
		t.Errorf("expected 'not supported on non-windows', got: %v", err)
	}
}

func TestController_Stub_KeyPress(t *testing.T) {
	c := NewController(zerolog.Nop())
	err := c.KeyPress(context.Background(), 0x41) // 'A' key
	if err == nil {
		t.Fatal("expected error on non-windows, got nil")
	}
	if !strings.Contains(err.Error(), "not supported on non-windows") {
		t.Errorf("expected 'not supported on non-windows', got: %v", err)
	}
}

func TestController_Stub_MouseMove(t *testing.T) {
	c := NewController(zerolog.Nop())
	err := c.MouseMove(context.Background(), 50, 75)
	if err == nil {
		t.Fatal("expected error on non-windows, got nil")
	}
	if !strings.Contains(err.Error(), "not supported on non-windows") {
		t.Errorf("expected 'not supported on non-windows', got: %v", err)
	}
}
