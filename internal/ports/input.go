package ports

import (
	"context"
	"errors"
)

// ErrWhitelistViolation 表示操作被白名单拦截（Story 4.1 白名单实现使用）。
var ErrWhitelistViolation = errors.New("whitelist: operation not permitted")

// InputController 是键鼠输入控制的端口接口。
// 所有坐标和按键通过接口参数传入，不硬编码。
type InputController interface {
	// Click 在屏幕坐标 (x, y) 执行鼠标左键点击。
	Click(ctx context.Context, x, y int) error
	// KeyPress 发送按键事件（按下并释放）。
	KeyPress(ctx context.Context, keyCode uint16) error
	// MouseMove 将鼠标移动到屏幕坐标 (x, y)。
	MouseMove(ctx context.Context, x, y int) error
}
