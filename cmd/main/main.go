package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"gin-template/internal/conf"
	"gin-template/pkg/log"
)

func main() {
	confPath := flag.String("conf", conf.DefaultPath, "配置文件路径（文件或目录）")
	flag.Parse()

	// 显式加载配置：不设全局配置，加载结果随构造函数注入（见 initApp）
	cfg, cfgSource, err := conf.Load(*confPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}

	logger, err := log.NewLogger(cfg)
	if err != nil {
		_ = cfgSource.Close()
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}

	// 注册为 slog 默认 Logger：使包级 slog.Info/Error（app.go、middleware）
	// 与显式注入的 logger 使用同一后端
	slog.SetDefault(logger)

	// 进程退出（含 panic）前刷新异步日志缓冲、释放配置 source watcher
	defer func() {
		if err := log.Close(); err != nil {
			logger.Error("关闭日志失败", "error", err)
		}
		if err := cfgSource.Close(); err != nil {
			logger.Error("关闭配置失败", "error", err)
		}
	}()

	app := initApp(cfg, logger)

	done := make(chan os.Signal, 1)
	go func() {
		defer func() {
			done <- os.Interrupt
		}()
		logger.Info("服务已启动")
		if err := app.StartServer(); err != nil {
			logger.Error("服务崩溃", "error", err)
			return
		}
	}()

	signal.Notify(done, os.Interrupt)
	<-done
	logger.Info("服务退出")
}
