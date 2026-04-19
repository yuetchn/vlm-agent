//go:build !windows

package input

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
)

// Controller 是非 Windows 平台的存根实现，所有方法返回不支持错误。
// 此文件确保在 CI（非 Windows 环境）中项目可正常编译。
type Controller struct {
	logger zerolog.Logger
}

// NewController 创建非 Windows 存根控制器。
func NewController(log zerolog.Logger) *Controller {
	return &Controller{logger: log}
}

func (c *Controller) Click(_ context.Context, _, _ int) error {
	return errors.New("input: not supported on non-windows")
}

func (c *Controller) KeyPress(_ context.Context, _ uint16) error {
	return errors.New("input: not supported on non-windows")
}

func (c *Controller) MouseMove(_ context.Context, _, _ int) error {
	return errors.New("input: not supported on non-windows")
}
