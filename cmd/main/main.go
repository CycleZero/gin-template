package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"gin-template/internal/conf"
	"gin-template/pkg/log"
	"gin-template/pkg/metrics"
	"gin-template/pkg/trace"
)

// shutdownTimeout 进程退出时等待各 telemetry provider flush 的上限。
const shutdownTimeout = 5 * time.Second

func main() {
	confPath := flag.String("conf", conf.DefaultPath, "配置文件路径（文件或目录）")
	flag.Parse()

	// 显式加载配置：不设全局配置，加载结果随构造函数注入（见 initApp）
	cfg, cfgSource, err := conf.Load(*confPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}

	// 服务身份：trace / metrics / logs 三路共用同一个 Resource
	service := cfg.OtelServiceInfo(hostname())
	tracesEndpoint, metricsEndpoint, logsEndpoint := otelEndpoints(cfg.GetOtel())

	// 日志：控制台 + 文件 + OTLP（端点为空时自动降级为纯本地日志）
	logger, err := log.New(logConfig(cfg, service, logsEndpoint))
	if err != nil {
		_ = cfgSource.Close()
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
	// 注册为 slog 默认 Logger：使包级 slog.Info/Error（app.go、middleware）
	// 与显式注入的 logger 使用同一后端
	slog.SetDefault(logger.Logger)

	// trace / metrics：OTLP 主动推送；端点为空时 Init 返回 nil 表示"未启用"
	tracerProvider, err := trace.Init(traceConfig(cfg, service, tracesEndpoint))
	if err != nil {
		logger.Error("初始化 trace 上报失败，已降级为不上报", "error", err)
	}
	meterProvider, err := metrics.Init(metricsConfig(cfg, service, metricsEndpoint))
	if err != nil {
		logger.Error("初始化 metrics 上报失败，已降级为不上报", "error", err)
	}

	// 进程退出（含 panic）前逆序 flush：先刷 trace/metrics，最后关日志，
	// 保证收尾日志（含上面的关闭失败提示）也能落到文件与控制台。
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := trace.Shutdown(ctx, tracerProvider); err != nil {
			logger.Error("关闭 trace 上报失败", "error", err)
		}
		if err := metrics.Shutdown(ctx, meterProvider); err != nil {
			logger.Error("关闭 metrics 上报失败", "error", err)
		}
		if err := logger.Close(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "关闭日志失败:", err)
		}
		if err := cfgSource.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "关闭配置失败:", err)
		}
	}()

	app := initApp(cfg, logger.Logger)

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
