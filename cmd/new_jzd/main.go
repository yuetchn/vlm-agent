package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zerfx/new_jzd/internal/checker"
	"github.com/zerfx/new_jzd/internal/config"
	"github.com/zerfx/new_jzd/internal/logger"
	"github.com/zerfx/new_jzd/internal/process"
	"github.com/zerfx/new_jzd/internal/version"
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

	// 打印当前版本号（AC#1）
	fmt.Printf("new_jzd %s\n", version.Version)

	// 1. 初始化日志
	if err := logger.Init(filepath.Join(exeDir, "logs")); err != nil {
		fmt.Fprintf(os.Stderr, "日志初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	// 后台版本检查 goroutine（AC#2 #3 #4）
	go func() {
		vctx, vcancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer vcancel()
		latest, err := version.Check(vctx, cfg.VersionCheckURL)
		if err != nil || latest == "" {
			return // 静默跳过
		}
		updateAvailable := version.IsNewer(latest, version.Version)
		if updateAvailable {
			fmt.Printf("[提示] 新版本 %s 可用，请访问 GitHub Releases 下载\n", latest)
		}
		log.Logger.Info().
			Str("event", string(logger.VersionCheck)).
			Str("current", version.Version).
			Str("latest", latest).
			Bool("update_available", updateAvailable).
			Msg("版本检查完成")
	}()

	// 2. 前置检查（复用 checker 包）
	// 注意：config.Load() 已解密 secrets.enc；check_config 内部会再独立解密一次做校验
	// 这是 Story 2-1 的设计 trade-off，非 bug，不必修改
	results, allPassed := checker.RunAll(cfg)
	checker.PrintResults(results)
	if !allPassed {
		os.Exit(1)
	}

	// 3. 信号感知 context（支持 Ctrl+C 退出 + anomaly 触发 cancel）
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 4. VLM 就绪
	var mgr *process.Manager
	switch cfg.VLM.Backend {
	case "grpc":
		mgr = process.NewManager(cfg)
		if err := mgr.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer mgr.Stop() // 确保退出时子进程被回收
		// 5. 健康监控（anomaly 触发 cancel，主循环 <-ctx.Done() 退出）
		hm := process.NewHealthMonitor(mgr)
		hm.OnMaxRestartsExceeded = func() {
			fmt.Fprintln(os.Stderr, "VLM 进程多次崩溃，系统进入异常状态")
			cancel() // 触发优雅退出；FSM anomaly 状态在 Story 3.x 实现
		}
		go hm.Run(ctx)
	case "http":
		if err := process.StartHTTP(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "未知 VLM backend：%s\n", cfg.VLM.Backend)
		os.Exit(1)
	}

	// 6. FSM 主循环占位（Story 3.1 填充）
	// 禁止用 select{}：无法响应 Ctrl+C 或 anomaly cancel
	<-ctx.Done()
}
