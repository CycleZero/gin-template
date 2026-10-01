// Package response 统一 HTTP 响应封装。
//
// 成功响应：**业务数据全部放在 data 字段下**，外层只有固定几个字段：
//
//	{ "code": 0, "message": "ok", "data": { ... }, "trace_id": "..." }
//
// 失败响应：不返回 data，只给稳定业务码与可对外消息：
//
//	{ "code": 40400, "message": "记录不存在", "trace_id": "..." }
//
// 三个出口，语义严格区分：
//
//	OK(c, data)    成功（200）
//	Fail(c, err)   请求方错误（4xx）：参数不合法、未认证、无权限、不存在、冲突、限流
//	Error(c, err)  服务端错误（5xx）：依赖故障、超时、未归一化的内部错误
//
// 状态码与业务码的映射集中在 pkg/errs.Resolve，handler 里不再出现分支判断。
package response

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"gin-template/pkg/errs"
	"gin-template/pkg/trace"
)

// Body 统一响应体。
type Body struct {
	Code    int    `json:"code"`               // 0 表示成功
	Message string `json:"message"`            // 成功固定为 ok
	Data    any    `json:"data,omitempty"`     // 业务数据（失败时不返回）
	TraceID string `json:"trace_id,omitempty"` // 链路 TraceID，与响应头 X-Request-ID 同源
}

// OK 返回 200。
//
// 业务数据**统一放在 data 字段下**：分页、列表等结构由各 service 的 DTO 定义
// （例如 demo 的 ListDemoResponse），pkg/response 不规定业务结构。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{
		Code:    errs.CodeOK,
		Message: "ok",
		Data:    data,
		TraceID: traceIDOf(c),
	})
}

// Fail 渲染**请求方错误（4xx）**。
//
// 约定传入 4xx 语义的错误：errs.InvalidArgument / Unauthorized / Forbidden /
// NotFound / Conflict / TooManyRequests，或 biz 定义的同类领域错误
// （如 biz.ErrDemoNotFound）。这类错误由请求方造成，因此**不写错误日志**，避免告警噪声。
//
// 若传入的错误归一化后是 5xx（说明调用点选错了函数），仍按 5xx 渲染——不隐瞒真实状态，
// 但会记一条告警提示改用 response.Error。
func Fail(c *gin.Context, err error) {
	status, code, message := errs.Resolve(err)
	if status >= http.StatusInternalServerError {
		slog.WarnContext(requestContext(c),
			"response.Fail 收到了 5xx 错误，应改用 response.Error",
			"status", status, "code", code, "error", err)
	}
	abort(c, status, code, message)
}

// Error 渲染**服务端错误（5xx）**。
//
// 约定传入 5xx 语义的错误：errs.Internal / Unavailable / Timeout，或任何未归一化的
// error（Resolve 会兜底成 500 + 固定文案）。这类错误必须被看见：一律以 error 级别记录
// （含内部 cause、method/path 与 trace_id），但对外的消息保持稳定，
// **绝不泄漏 SQL/DSN 等内部细节**。
//
// 若传入的错误归一化后是 4xx（选错了函数），仍按 4xx 渲染并记一条告警。
func Error(c *gin.Context, err error) {
	status, code, message := errs.Resolve(err)
	logCtx := requestContext(c)

	if status >= http.StatusInternalServerError {
		fields := []any{"status", status, "code", code, "error", err}
		if c != nil && c.Request != nil {
			fields = append(fields,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
			)
		}
		slog.ErrorContext(logCtx, "服务端错误", fields...)
	} else {
		slog.WarnContext(logCtx, "response.Error 收到了 4xx 错误，应改用 response.Fail",
			"status", status, "code", code, "error", err)
	}

	abort(c, status, code, message)
}

// abort 终止后续 handler 并输出统一响应体。
func abort(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, Body{
		Code:    code,
		Message: message,
		TraceID: traceIDOf(c),
	})
}

// traceIDOf 取当前请求的链路 TraceID（由 OTel 埋点中间件写入 context）。
func traceIDOf(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return trace.TraceID(c.Request.Context())
}

// requestContext 取请求 context（供日志关联 trace.id）；缺失时退化为 Background。
func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}
