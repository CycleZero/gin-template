package errs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantSt   int
		wantCode int
		wantMsg  string
	}{
		{"nil 视为成功", nil, http.StatusOK, CodeOK, "ok"},
		{"领域错误", NotFound("记录不存在"), http.StatusNotFound, CodeNotFound, "记录不存在"},
		{"包装后的领域错误仍可识别", fmt.Errorf("biz: %w", NotFound("记录不存在")), http.StatusNotFound, CodeNotFound, "记录不存在"},
		{"超时映射 504", fmt.Errorf("db: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, CodeTimeout, "请求超时"},
		{"取消映射 499", context.Canceled, 499, CodeCanceled, "请求已取消"},
		{
			"未归一化错误一律 500 且不泄漏细节",
			errors.New("sql: table demo not found (dsn=root:pw@tcp(127.0.0.1))"),
			http.StatusInternalServerError,
			CodeInternal,
			"服务器内部错误",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, code, message := Resolve(tc.err)
			if status != tc.wantSt || code != tc.wantCode || message != tc.wantMsg {
				t.Errorf("Resolve() = (%d, %d, %q), want (%d, %d, %q)",
					status, code, message, tc.wantSt, tc.wantCode, tc.wantMsg)
			}
		})
	}
}

// TestCauseStaysInternal 内部原因只进日志：对外消息不含细节，但日志（Error()）与 errors.Is 可取到。
func TestCauseStaysInternal(t *testing.T) {
	cause := errors.New("sql: dsn=root:pw@tcp(127.0.0.1)")
	err := Internal("创建失败").WithCause(cause)

	_, _, message := Resolve(err)
	if strings.Contains(message, "dsn") {
		t.Errorf("对外消息泄漏了内部细节：%q", message)
	}
	if !errors.Is(err, cause) {
		t.Error("cause 应可通过 errors.Is 找到，便于日志排查")
	}
	if !strings.Contains(err.Error(), "dsn") {
		t.Errorf("Error() 应包含内部原因供日志记录，实际：%q", err.Error())
	}
}

// TestWithCauseDoesNotMutateSentinel 包级错误变量可安全复用，不会被 WithCause 污染。
func TestWithCauseDoesNotMutateSentinel(t *testing.T) {
	sentinel := NotFound("记录不存在")

	wrapped := sentinel.WithCause(errors.New("boom"))
	if sentinel.Unwrap() != nil {
		t.Error("WithCause 不应修改原错误（包级错误变量会被并发复用）")
	}
	if wrapped.Unwrap() == nil {
		t.Error("副本应携带 cause")
	}
	// 同一个哨兵错误被包装后，errors.Is 仍应成立（按业务码匹配）
	if !errors.Is(wrapped, sentinel) {
		t.Error("相同业务码的错误应被 errors.Is 识别为同一类")
	}
	if !errors.Is(NotFound("别的消息"), sentinel) {
		t.Error("业务码相同、消息不同也应视为同一类错误")
	}
	if errors.Is(Conflict("冲突"), sentinel) {
		t.Error("不同业务码不应互相匹配")
	}
}

// TestResolveUsesDefaultStatusAndText 状态码/消息缺省时的兜底行为。
func TestResolveUsesDefaultStatusAndText(t *testing.T) {
	status, code, message := Resolve(&Error{Code: CodeForbidden})
	if status != http.StatusInternalServerError {
		t.Errorf("HTTPStatus 为 0 时应兜底 500，实际 %d", status)
	}
	if message != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("Message 为空时应兜底状态文案，实际 %q", message)
	}
	if code != CodeForbidden {
		t.Errorf("业务码应原样保留，实际 %d", code)
	}
}
