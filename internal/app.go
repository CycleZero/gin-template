package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gin-template/internal/conf"
	"gin-template/internal/domain"
	"gin-template/internal/router"
	"gin-template/pkg/errs"
	"gin-template/pkg/infra"
	"gin-template/pkg/log"
	"gin-template/pkg/response"
	"gin-template/pkg/trace"

	"github.com/gin-gonic/gin"
)

// MainApp 应用主结构，封装 Gin Engine 与所有基础设施。
//
// 生命周期约定：
//   - Start()    阻塞运行：后台起可选的 pprof 调试服务，前台跑 HTTP 服务
//   - Shutdown() 优雅停机：排空在途请求 → 关调试服务 → 释放数据库/Redis
type MainApp struct {
	Engine       *gin.Engine
	ServiceHub   *domain.ServiceHub
	RegisterFunc router.RegisterFunc

	api   *Server
	debug *Server // pprof；未启用时为 nil
	data  *infra.Data
}

// NewMainApp 创建主应用实例（由 Wire 注入）
func NewMainApp(
	cfg *conf.Bootstrap,
	hub *domain.ServiceHub,
	registerFunc router.RegisterFunc,
	registeredMiddleWire router.RegisteredMiddleWire,
	data *infra.Data,
) *MainApp {
	// gin 运行模式跟随配置：dev_mode=false（如 prod）时用 ReleaseMode，
	// 否则 gin 会为每个请求打印调试日志，控制台彩色输出也会污染日志采集。
	if cfg.GetApp().GetDevMode() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	e := gin.New()

	// 访问日志与 panic 兜底（统一响应体，内部细节只进日志）
	e.Use(accessLogger())
	e.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		slog.ErrorContext(c.Request.Context(), "发生 Panic", "error", err)
		response.Fail(c, errs.Internal("服务器内部错误"))
	}))

	// OTel HTTP 埋点：**始终启用**。它负责为每个请求产生 span 并写入 TraceID，
	// 而本项目的"请求 ID"就是 TraceID，因此即使未配置 OTLP 端点也必须挂载
	// （未配置时 trace.Init 用的是 NeverSample + 无导出器，不产生导出开销）。
	e.Use(trace.Middleware(cfg.ServiceName()))

	// 注册自定义中间件（必须在路由注册前完成）
	registeredMiddleWire.Register()

	// 先注册业务路由：RegisterRouter 内部会 Use(TraceID/CORS/Metadata)，
	// gin 的中间件链在注册路由时固化，因此健康检查必须在这之后注册，
	// 才能同样带上响应头回写与跨域等全局中间件。
	registerFunc(e, hub)

	// 健康检查：K8s 探针语义，挂在根路径（不进 /api 前缀）
	router.RegisterHealth(e, data.Health)

	app := &MainApp{
		Engine:       e,
		ServiceHub:   hub,
		RegisterFunc: registerFunc,
		api:          NewServer("http", cfg.GetServer().GetHttp().Addr(), e),
		debug:        newDebugServer(cfg),
		data:         data,
	}

	app.printRoutes()
	return app
}

// accessLogger 结构化访问日志。
//
// 通过 log.WithContext 注入关联字段，因此日志里会带上 trace.id
// —— 与响应头 X-Request-ID、响应体 trace_id 同源，形成请求 ID 闭环。
func accessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		log.WithContext(slog.Default(), c.Request.Context()).Info("http 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"cost_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			"resp_size", c.Writer.Size(),
		)
	}
}

func (a *MainApp) printRoutes() {
	routes := a.Engine.Routes()
	slog.Info("路由注册完成", "count", len(routes))
	for _, route := range routes {
		slog.Info("注册路由", "method", route.Method, "path", route.Path)
	}
}

// Start 启动服务并阻塞，直到服务被关闭或监听失败（优雅停机返回 nil）。
func (a *MainApp) Start() error {
	if a.debug != nil {
		go func() {
			if err := a.debug.Start(); err != nil {
				slog.Error("pprof 调试服务异常退出", "error", err)
			}
		}()
	}

	slog.Info("启动 HTTP 服务", "addr", a.api.Addr())
	return a.api.Start()
}

// Shutdown 优雅停机。
//
//  1. HTTP 服务：停止接收新连接，等待在途请求完成（超时由 ctx 控制）
//  2. pprof 调试服务：同样优雅关闭
//  3. 数据库与 Redis：释放连接
//
// 任一环节失败都继续执行后续环节（errors.Join 汇总），否则一处卡住就会泄漏资源。
func (a *MainApp) Shutdown(ctx context.Context) error {
	var problems []error

	if err := a.api.Shutdown(ctx); err != nil {
		problems = append(problems, fmt.Errorf("关闭 HTTP 服务: %w", err))
	}
	if a.debug != nil {
		if err := a.debug.Shutdown(ctx); err != nil {
			problems = append(problems, fmt.Errorf("关闭 pprof 服务: %w", err))
		}
	}
	if err := a.data.Close(); err != nil {
		problems = append(problems, fmt.Errorf("释放数据连接: %w", err))
	}

	return errors.Join(problems...)
}
