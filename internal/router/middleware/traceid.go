package middleware

import (
	"gin-template/pkg/trace"

	"github.com/gin-gonic/gin"
)

// TraceID 把链路 TraceID 回写到响应头 X-Request-ID，形成请求 ID 闭环。
//
// 本项目不再单独生成请求 ID：**请求 ID 就是 TraceID**，因此
//
//	响应头 X-Request-ID ≡ 响应体 trace_id ≡ 日志 trace.id ≡ 链路系统里的 TraceID
//
// 客户端只需拿响应头，就能在日志/链路平台里检索到整条请求，无需额外关联字段。
//
// 前提：需先注册 trace.Middleware（本项目的 OTel 埋点始终启用，见 internal/app.go），
// 否则取不到 TraceID，此时不写响应头（而不是造一个假 ID）。
// 上游若传了 traceparent，TraceID 由 OTel 传播器续接，跨服务链路不会断。
//
// 响应头在 c.Next() 之前写入：即使 handler 提前返回或 panic，响应也带着该 ID。
func TraceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id := trace.TraceID(c.Request.Context()); id != "" {
			c.Header(trace.TraceIDHeader, id)
		}
		c.Next()
	}
}
