package middleware

import (
	"gin-template/internal/common"
	"gin-template/pkg/trace"

	"github.com/gin-gonic/gin"
)

var (
	IsMiddleWireRegisterFinished = false
	AuthMiddleWire               func(optional bool) gin.HandlerFunc
)

// AddMetaData 为每个请求补充元数据（客户端 IP、UA、TraceID 等）。
//
// TraceID 直接取自链路上下文（OTel 埋点中间件已注入），因此这里不生成任何 ID：
// 全链路只有一个 ID —— TraceID。
func AddMetaData() gin.HandlerFunc {
	return func(c *gin.Context) {
		meta := &common.RequestMetadata{
			UserID: 0, // 接入认证中间件后由鉴权结果填充
			// Request 携带链路上下文，业务层可直接取 TraceID / span
			Request:   c.Request,
			ClientIP:  c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
			TraceID:   trace.TraceID(c.Request.Context()),
		}
		common.SetRequestMetadata(c, meta)
		c.Next()
	}
}
