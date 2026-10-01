package internal

import (
	"log/slog"
	"net/http"

	"gin-template/internal/conf"
	"gin-template/internal/domain"
	"gin-template/internal/router"
	"gin-template/pkg/infra"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
)

// MainApp 应用主结构，封装 Gin Engine 和所有基础设施
type MainApp struct {
	Engine       *gin.Engine
	ServiceHub   *domain.ServiceHub
	addr         string
	data         *infra.Data
	RegisterFunc router.RegisterFunc
}

// NewMainApp 创建主应用实例（由 Wire 注入）
func NewMainApp(
	cfg *conf.Bootstrap,
	hub *domain.ServiceHub,
	registerFunc router.RegisterFunc,
	registeredMiddleWire router.RegisteredMiddleWire,
	data *infra.Data,
) *MainApp {
	gin.SetMode(gin.DebugMode)

	e := gin.New()

	// 基础中间件
	e.Use(gin.Logger())
	e.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		slog.Error("发生 Panic", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
	}))

	// 注册自定义中间件（必须在路由注册前完成）
	registeredMiddleWire.Register()

	// 注册 pprof（性能分析）
	pprof.Register(e)

	// 注册业务路由
	registerFunc(e, hub)

	app := &MainApp{
		Engine:       e,
		addr:         cfg.GetServer().GetHttp().Addr(),
		ServiceHub:   hub,
		RegisterFunc: registerFunc,
	}

	app.printRoutes()
	return app
}

func (a *MainApp) printRoutes() {
	routes := a.Engine.Routes()
	slog.Info("路由注册完成", "count", len(routes))
	for _, route := range routes {
		slog.Info("注册路由", "method", route.Method, "path", route.Path)
	}
}

// StartServer 启动 HTTP 服务
func (a *MainApp) StartServer() error {
	slog.Info("启动服务", "addr", a.addr)
	return a.Engine.Run(a.addr)
}

// Close 关闭应用，释放资源
func (a *MainApp) Close() error {
	return nil
}
