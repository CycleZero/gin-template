// Package response 统一 HTTP 响应封装。
//
// 所有接口（成功与失败）都返回同一结构，前端只需一套解析逻辑：
//
//	{
//	  "code": 0,                     // 0 成功；非 0 见 pkg/errs 的业务码
//	  "message": "ok",
//	  "data": { ... },               // 业务数据，失败时省略
//	  "trace_id": "3f2a..."          // 链路 TraceID，与响应头 X-Request-ID 同值
//	}
//
// service 层只做两件事：成功用 response.OK(c, data)，失败用 response.Fail(c, err)。
// 状态码与业务码的映射集中在 pkg/errs.Resolve，handler 里不再出现分支判断。
package response

import (
	"github.com/gin-gonic/gin"

	"gin-template/pkg/errs"
	"gin-template/pkg/trace"
)

// Body 统一响应体。
type Body struct {
	Code    int    `json:"code"`               // 0 表示成功
	Message string `json:"message"`            // 成功固定为 ok
	Data    any    `json:"data,omitempty"`     // 业务数据（失败时省略）
	TraceID string `json:"trace_id,omitempty"` // 链路 TraceID，与响应头 X-Request-ID 一致
}

// Page 分页载荷：配合 response.OK 使用。
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// OK 返回 200 与业务数据。
func OK(c *gin.Context, data any) {
	c.JSON(200, Body{
		Code:    errs.CodeOK,
		Message: "ok",
		Data:    data,
		TraceID: traceIDOf(c),
	})
}

// OKPage 返回 200 与分页数据（空列表序列化为 []，而不是 null）。
func OKPage[T any](c *gin.Context, list []T, total int64, page, pageSize int) {
	if list == nil {
		list = make([]T, 0)
	}
	OK(c, Page[T]{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// Created 返回 201 与新建资源。
func Created(c *gin.Context, data any) {
	c.JSON(201, Body{
		Code:    errs.CodeOK,
		Message: "ok",
		Data:    data,
		TraceID: traceIDOf(c),
	})
}

// Fail 把任意 error 渲染为标准错误响应。
//
// 内部细节（errs.Error 的 cause、未归一化错误的原始内容）不会出现在响应里，
// 只会由产生错误的层写入日志。
func Fail(c *gin.Context, err error) {
	status, code, message := errs.Resolve(err)
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
