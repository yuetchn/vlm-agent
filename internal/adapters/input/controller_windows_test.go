//go:build windows

package input

import (
	"testing"

	"github.com/rs/zerolog"
)

// TestController_Windows_Construction 验证 Windows Controller 可正常构造。
// 注意：不调用真实的 SendInput，避免在 CI 中产生真实鼠标/键盘操作。
func TestController_Windows_Construction(t *testing.T) {
	c := NewController(zerolog.Nop())
	if c == nil {
		t.Fatal("expected non-nil Controller")
	}
}

// TestGetScreenSize_Windows 验证 GetSystemMetrics 调用返回合理的屏幕尺寸。
func TestGetScreenSize_Windows(t *testing.T) {
	w, h := getScreenSize()
	if w <= 0 || h <= 0 {
		t.Errorf("expected positive screen size, got %dx%d", w, h)
	}
}
