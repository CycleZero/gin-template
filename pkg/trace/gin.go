package trace

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Middleware 返回 Gin 的 OTel 中间件：为每个 HTTP 请求创建 server span
// （span 名 = HTTP method，属性含 http.route / http.status_code 等），
// 并在 MeterProvider 就绪时自动记录 `http.server.*` 指标。
//
// 未初始化 Tracer/Meter 时（endpoint 为空）此中间件为无操作透传；
// 是否挂载由调用方决定（本项目在 otel.endpoint 非空时才挂）。
func Middleware(serviceName string) gin.HandlerFunc {
	return otelgin.Middleware(serviceName)
}
