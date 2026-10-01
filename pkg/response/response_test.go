package response

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	oteltrace "go.opentelemetry.io/otel/trace"

	"gin-template/pkg/errs"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// captureLogs 把 slog 默认输出重定向到缓冲区（测试结束自动还原），
// 用于断言"哪类错误该记日志、哪类不该"。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// serve 用一次性 handler 跑一次请求。
func serve(t *testing.T, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.GET("/t", handler)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Body {
	t.Helper()
	var body Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应体失败：%v（原始内容 %s）", err, rec.Body.String())
	}
	return body
}

func TestOKEnvelope(t *testing.T) {
	rec := serve(t, func(c *gin.Context) { OK(c, gin.H{"id": 1}) })

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body.Code != 0 || body.Message != "ok" {
		t.Errorf("信封 = %+v, want code=0 message=ok", body)
	}
	// 业务数据必须整体位于 data 字段下
	if body.Data == nil {
		t.Error("成功响应应把业务数据放在 data 字段下")
	}
}

// TestFailRendersClientError 4xx 请求错误：按错误自身的业务码渲染，且**不写错误日志**（避免告警噪声）。
func TestFailRendersClientError(t *testing.T) {
	logs := captureLogs(t)

	rec := serve(t, func(c *gin.Context) { Fail(c, errs.NotFound("记录不存在")) })

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", rec.Code)
	}
	body := decode(t, rec)
	if body.Code != errs.CodeNotFound || body.Message != "记录不存在" {
		t.Errorf("信封 = %+v, want 40400/记录不存在", body)
	}
	if body.Data != nil {
		t.Error("失败响应不应带 data")
	}
	if logs.Len() != 0 {
		t.Errorf("4xx 请求错误不应写日志，实际输出：%s", logs.String())
	}
}

// TestFailWarnsWhenGivenServerError 用 Fail 渲染 5xx 属调用点选错函数：
// 仍按 5xx 渲染（不隐瞒真实状态），但要留下告警，且不泄漏内部细节。
func TestFailWarnsWhenGivenServerError(t *testing.T) {
	logs := captureLogs(t)

	rec := serve(t, func(c *gin.Context) {
		Fail(c, errors.New("sql: table demo not found (dsn=root:pw@tcp(127.0.0.1))"))
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500", rec.Code)
	}
	body := decode(t, rec)
	if body.Code != errs.CodeInternal || body.Message != "服务器内部错误" {
		t.Errorf("信封 = %+v, want 50000/服务器内部错误", body)
	}
	if strings.Contains(rec.Body.String(), "dsn") {
		t.Errorf("响应体泄漏了内部细节：%s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "应改用 response.Error") {
		t.Errorf("应告警提示改用 response.Error，实际日志：%s", logs.String())
	}
}

// TestErrorRendersServerErrorAndLogs 5xx 服务端错误：记录错误日志（含 cause），但对外只给稳定文案。
func TestErrorRendersServerErrorAndLogs(t *testing.T) {
	logs := captureLogs(t)

	rec := serve(t, func(c *gin.Context) {
		Error(c, errs.Internal("创建 Demo 失败").WithCause(errors.New("sql: dsn=root:pw@tcp(127.0.0.1)")))
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500", rec.Code)
	}
	body := decode(t, rec)
	if body.Code != errs.CodeInternal || body.Message != "创建 Demo 失败" {
		t.Errorf("信封 = %+v, want 50000/创建 Demo 失败", body)
	}
	if strings.Contains(rec.Body.String(), "dsn") {
		t.Errorf("响应体泄漏了内部细节：%s", rec.Body.String())
	}
	logText := logs.String()
	if !strings.Contains(logText, "服务端错误") {
		t.Errorf("应记录错误日志，实际：%s", logText)
	}
	if !strings.Contains(logText, "dsn") {
		t.Errorf("日志里应包含内部 cause 供排查，实际：%s", logText)
	}
}

// TestErrorWarnsWhenGivenClientError 用 Error 渲染 4xx 属选错函数：仍按 4xx 返回，并留下告警。
func TestErrorWarnsWhenGivenClientError(t *testing.T) {
	logs := captureLogs(t)

	rec := serve(t, func(c *gin.Context) { Error(c, errs.NotFound("记录不存在")) })

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", rec.Code)
	}
	logText := logs.String()
	if !strings.Contains(logText, "应改用 response.Fail") {
		t.Errorf("应告警提示改用 response.Fail，实际：%s", logText)
	}
	if strings.Contains(logText, "level=ERROR") {
		t.Errorf("4xx 不应记为 error 级别，实际：%s", logText)
	}
}

// TestTraceIDFromSpanContext 响应体里的 trace_id 与链路 TraceID 同源。
func TestTraceIDFromSpanContext(t *testing.T) {
	traceID, err := oteltrace.TraceIDFromHex("5f0af201e4905c0eeab1603c31ab1540")
	if err != nil {
		t.Fatalf("构造 TraceID 失败：%v", err)
	}
	spanID, err := oteltrace.SpanIDFromHex("c9a26a0dddcd8b12")
	if err != nil {
		t.Fatalf("构造 SpanID 失败：%v", err)
	}
	ctx := oteltrace.ContextWithSpanContext(context.Background(), oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.FlagsSampled,
	}))

	r := gin.New()
	r.GET("/t", func(c *gin.Context) { OK(c, gin.H{"ok": true}) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil).WithContext(ctx))

	if body := decode(t, rec); body.TraceID != traceID.String() {
		t.Errorf("trace_id = %q, want %q（应与链路 TraceID 一致）", body.TraceID, traceID.String())
	}
}

// TestTraceIDEmptyWithoutSpan 没有 span 时不应伪造 ID。
func TestTraceIDEmptyWithoutSpan(t *testing.T) {
	rec := serve(t, func(c *gin.Context) { OK(c, gin.H{"ok": true}) })
	if body := decode(t, rec); body.TraceID != "" {
		t.Errorf("无 span 时 trace_id 应为空，实际 %q", body.TraceID)
	}
}
