package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zerfx/new_jzd/internal/checker"
	"github.com/zerfx/new_jzd/internal/config"
)

func main() {
	checkMode := flag.Bool("check", false, "执行环境前置检查并退出")
	flag.Parse()

	// 使用可执行文件所在目录定位配置文件，避免 CWD 依赖
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
		os.Exit(1)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.yaml")
	secretsPath := filepath.Join(exeDir, "secrets.enc")

	if *checkMode {
		// --check 模式：只加载 YAML，不解密 secrets.enc
		// 原因：secrets.enc 可能不存在（首次安装），若此处报错退出则
		// check_config 的友好提示永远不会显示
		cfg, err := config.LoadYAMLOnly(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "配置文件加载失败: %v\n", err)
			os.Exit(1)
		}
		results, allPassed := checker.RunAll(cfg)
		checker.PrintResults(results)
		if allPassed {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// 正式启动模式：完整加载（含 secrets 解密）
	cfg, err := config.Load(configPath, secretsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}
	_ = cfg // 非 --check 模式：后续 Story 填充
}
