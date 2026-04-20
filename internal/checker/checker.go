package checker

import (
	"fmt"

	"github.com/zerfx/new_jzd/internal/config"
)

// CheckResult 单项检查结果
type CheckResult struct {
	Name    string
	OK      bool
	Message string
}

// CheckFunc 检查函数类型
type CheckFunc func(cfg *config.Config) CheckResult

// RunAll 串联执行全部 5 项检查，单项失败不阻止后续检查。
// 返回所有检查结果及总体是否全部通过。
func RunAll(cfg *config.Config) (results []CheckResult, allPassed bool) {
	checks := []CheckFunc{
		CheckProcess,
		CheckGPU,
		CheckConfig,
		CheckModel,
		CheckVLM,
	}

	allPassed = true
	for _, fn := range checks {
		r := fn(cfg)
		results = append(results, r)
		if !r.OK {
			allPassed = false
		}
	}
	return results, allPassed
}

// PrintResults 按格式输出检查结果到标准输出（不经 zerolog，--check 为人机交互模式）
func PrintResults(results []CheckResult) {
	for _, r := range results {
		symbol := "✓"
		if !r.OK {
			symbol = "✗"
		}
		fmt.Printf("[%s] %s — %s\n", symbol, r.Name, r.Message)
	}
}
