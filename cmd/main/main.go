package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gin-template/internal/conf"
	"gin-template/pkg/log"
	"gin-template/pkg/metrics"
	"gin-template/pkg/trace"
)

// shutdownTimeout 优雅停机的总预算：排空在途请求 + flush 遥测数据。
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
	// 注册为 slog 默认 Logger：使包级 slog.Info/Error（app.go、中间件）共用同一后端
	slog.SetDefault(logger.Logger)

	// trace：始终安装 Provider —— 请求 ID 就是 TraceID，未接 OTLP 时也要能取到 ID
	tracerProvider, err := trace.Init(traceConfig(cfg, service, tracesEndpoint))
	if err != nil {
		logger.Error("初始化 trace 失败，已降级为不上报 span", "error", err)
	}
	// metrics：端点为空时 Init 返回 nil，表示未启用（无开销）
	meterProvider, err := metrics.Init(metricsConfig(cfg, service, metricsEndpoint))
	if err != nil {
		logger.Error("初始化 metrics 上报失败，已降级为不上报", "error", err)
	}

	// 装配应用（Wire 生成）。此处的 DB 连接失败仍是 fatal（见 pkg/infra.NewData）
	app := initApp(cfg, logger.Logger)

	// 监听 SIGINT 与 SIGTERM：K8s 滚动更新/容器停止发的是 SIGTERM，
	// 只监听 os.Interrupt 会导致进程被强杀、在途请求丢失。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// HTTP 服务在后台运行；主 goroutine 等待「退出信号」或「服务异常退出」
	serveErr := make(chan error, 1)
	go func() { serveErr <- app.Start() }()

	select {
	case err := <-serveErr:
		if err != nil {
			logger.Error("HTTP 服务异常退出", "error", err)
		} else {
			logger.Info("HTTP 服务已停止")
		}
	case <-ctx.Done():
		logger.Info("收到退出信号，开始优雅停机")
	}

	// 优雅停机（顺序即依赖顺序）：
	//   1. 排空在途请求、释放数据库/Redis
	//   2. flush trace / metrics
	//   3. 关闭日志与配置来源
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := app.Shutdown(shutdownCtx); err != nil {
		logger.Error("优雅停机未完全成功", "error", err)
	}
	logger.Info("HTTP 与依赖已停止，开始 flush 遥测数据")

	if err := trace.Shutdown(shutdownCtx, tracerProvider); err != nil {
		logger.Error("关闭 trace 上报失败", "error", err)
	}
	if err := metrics.Shutdown(shutdownCtx, meterProvider); err != nil {
		logger.Error("关闭 metrics 上报失败", "error", err)
	}
	// 日志必须最后关闭：上面的收尾日志同样要能落盘
	if err := logger.Close(shutdownCtx); err != nil {
		fmt.Fprintln(os.Stderr, "关闭日志失败:", err)
	}
	if err := cfgSource.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "关闭配置失败:", err)
	}
}
