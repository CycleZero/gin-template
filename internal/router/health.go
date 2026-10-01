package router

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"gin-template/pkg/errs"
	"gin-template/pkg/response"
)

// readinessTimeout 就绪检查探测依赖的超时：探针本身必须快速失败，
// 不能让 K8s 的探测请求堆积。
const readinessTimeout = 3 * time.Second

// ReadinessChecker 就绪检查函数：返回 nil 表示所有依赖可用。
type ReadinessChecker func(ctx context.Context) error

// RegisterHealth 注册健康检查路由（K8s 探针语义）。
//
//   - GET /healthz —— 存活探针（liveness）：只要进程还能响应就返回 200，不检查依赖。
//     若在这里检查数据库，依赖抖动会让容器被反复重启，反而放大故障。
//   - GET /readyz  —— 就绪探针（readiness）：检查 MySQL/Redis，不可用时返回 503。
//     此时不应接流量（K8s 会把它从 Service Endpoints 摘除），但容器无需重启。
//
// 两个接口都返回统一响应体（pkg/response），因此响应里带 request_id，便于对齐排查。
func RegisterHealth(root gin.IRouter, readiness ReadinessChecker) {
	root.GET("/healthz", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok"})
	})

	root.GET("/readyz", func(c *gin.Context) {
		if readiness == nil {
			response.OK(c, gin.H{"status": "ready"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()

		if err := readiness(ctx); err != nil {
			// 详情只进日志（含具体依赖名）；响应给 503 + 稳定业务码。
			// 依赖不可用属服务端问题，因此走 response.Error（5xx 语义）。
			response.Error(c, errs.Unavailable("依赖未就绪").WithCause(err))
			return
		}

		response.OK(c, gin.H{"status": "ready"})
	})
}
