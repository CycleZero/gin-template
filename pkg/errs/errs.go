// Package errs 定义统一的应用错误体系。
//
// 解决的问题：让"业务失败"在 service/handler 之间只表达一次，且不泄漏内部细节。
//
//	type Error struct {
//	    Code       int    // 稳定业务码，前端据此分支（0 表示成功）
//	    HTTPStatus int    // 对应 HTTP 状态码
//	    Message    string // 可安全下发给客户端的消息
//	    cause      error  // 内部原因，只进日志，永不出网
//	}
//
// 约定：
//   - 业务码沿用 HTTP 状态码 × 100 的方案（40000/40400/50000 …），便于人眼识别与扩展
//   - 领域层（biz）返回 *Error；data 层把存储错误翻译成领域错误；
//     service 层不再逐个分支判断，直接交给 pkg/response 渲染
//   - 未归一化的 error 一律按 500 处理且消息固定，避免把 SQL/连接串等细节透给客户端
package errs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// 业务码：HTTP 状态码 × 100。
const (
	CodeOK              = 0
	CodeInvalidArgument = 40000
	CodeUnauthorized    = 40100
	CodeForbidden       = 40300
	CodeNotFound        = 40400
	CodeConflict        = 40900
	CodeTooManyRequests = 42900
	CodeCanceled        = 49900
	CodeInternal        = 50000
	CodeUnavailable     = 50300
	CodeTimeout         = 50400
)

// Error 统一应用错误。实现 error 接口，可被 errors.Is/As 与 errors.Unwrap 处理。
type Error struct {
	Code       int    `json:"code"`
	HTTPStatus int    `json:"-"`
	Message    string `json:"message"`

	// cause 记录内部原因（如 SQL 错误），仅用于日志，不参与序列化、不下发。
	cause error
}

// Error 实现 error；带内部原因时一并输出，方便日志排查。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause != nil {
		return fmt.Sprintf("%s (code=%d): %v", e.Message, e.Code, e.cause)
	}
	return fmt.Sprintf("%s (code=%d)", e.Message, e.Code)
}

// Unwrap 支持 errors.Is/As 穿透到内部原因。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// WithCause 返回附加了内部原因的错误副本（不修改原错误，便于复用包级错误变量）。
//
// 注意：cause 永远不会出现在响应体里，只会出现在日志中。
func (e *Error) WithCause(cause error) *Error {
	if e == nil {
		return nil
	}
	clone := *e
	clone.cause = cause
	return &clone
}

// Is 让同一个业务码的错误可被 errors.Is 识别，便于按码判断。
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Code == t.Code
}

// New 构造自定义错误（业务码 + HTTP 状态码 + 可对外消息）。
func New(code, httpStatus int, message string) *Error {
	return &Error{Code: code, HTTPStatus: httpStatus, Message: message}
}

// InvalidArgument 400：参数/请求体不合法。
func InvalidArgument(message string) *Error {
	return New(CodeInvalidArgument, http.StatusBadRequest, message)
}

// Unauthorized 401：未认证或凭证失效。
func Unauthorized(message string) *Error {
	return New(CodeUnauthorized, http.StatusUnauthorized, message)
}

// Forbidden 403：已认证但无权限。
func Forbidden(message string) *Error {
	return New(CodeForbidden, http.StatusForbidden, message)
}

// NotFound 404：资源不存在。
func NotFound(message string) *Error {
	return New(CodeNotFound, http.StatusNotFound, message)
}

// Conflict 409：状态冲突（重复创建、乐观锁失败等）。
func Conflict(message string) *Error {
	return New(CodeConflict, http.StatusConflict, message)
}

// TooManyRequests 429：触发限流。
func TooManyRequests(message string) *Error {
	return New(CodeTooManyRequests, http.StatusTooManyRequests, message)
}

// Internal 500：服务内部错误（细节只进日志）。
func Internal(message string) *Error {
	return New(CodeInternal, http.StatusInternalServerError, message)
}

// Unavailable 503：依赖不可用/未就绪。
func Unavailable(message string) *Error {
	return New(CodeUnavailable, http.StatusServiceUnavailable, message)
}

// Timeout 504：下游超时。
func Timeout(message string) *Error {
	return New(CodeTimeout, http.StatusGatewayTimeout, message)
}

// As 提取错误链中的 *Error。
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Resolve 把任意 error 归一化为 (HTTP 状态码, 业务码, 可对外消息)。
//
// 这是响应层唯一需要调用的函数：无论错误来自哪一层，客户端拿到的都是稳定契约。
//   - nil                     → 200 / 0 / "ok"
//   - *Error                  → 其自身携带的状态码、业务码与消息
//   - context.DeadlineExceeded → 504
//   - context.Canceled        → 499（沿用 nginx 约定，客户端已断开）
//   - 其它                    → 500 + 固定消息（绝不透出内部细节）
func Resolve(err error) (httpStatus, code int, message string) {
	switch {
	case err == nil:
		return http.StatusOK, CodeOK, "ok"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, CodeTimeout, "请求超时"
	case errors.Is(err, context.Canceled):
		return 499, CodeCanceled, "请求已取消"
	}

	if e, ok := As(err); ok {
		status := e.HTTPStatus
		if status == 0 {
			status = http.StatusInternalServerError
		}
		msg := e.Message
		if msg == "" {
			msg = http.StatusText(status)
		}
		return status, e.Code, msg
	}

	return http.StatusInternalServerError, CodeInternal, "服务器内部错误"
}
