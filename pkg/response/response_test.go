package response

import (
	"context"
	"encoding/json"
	"errors"
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

// serve 用一个一次性 handler 跑一次请求，返回响应记录。
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
	if body.Data == nil {
		t.Error("成功响应应带 data")
	}
}

func TestFailEnvelope(t *testing.T) {
	rec := serve(t, func(c *gin.Context) { Fail(c, errs.NotFound("记录不存在")) })

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", rec.Code)
	}
	body := decode(t, rec)
	if body.Code != errs.CodeNotFound {
		t.Errorf("业务码 = %d, want %d", body.Code, errs.CodeNotFound)
	}
	if body.Message != "记录不存在" {
		t.Errorf("消息 = %q, want 记录不存在", body.Message)
	}
	if body.Data != nil {
		t.Error("失败响应不应带 data")
	}
}

// TestFailHidesInternalDetails 未归一化错误必须走 500 + 固定文案，绝不透出内部信息。
func TestFailHidesInternalDetails(t *testing.T) {
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
}

func TestOKPageSerializesEmptyListAsArray(t *testing.T) {
	type item struct {
		ID int `json:"id"`
	}
	rec := serve(t, func(c *gin.Context) { OKPage[item](c, nil, 0, 1, 10) })

	if !strings.Contains(rec.Body.String(), `"list":[]`) {
		t.Errorf("空列表应序列化为 []，实际：%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"list":null`) {
		t.Errorf("空列表不应序列化为 null，实际：%s", rec.Body.String())
	}
}

// TestTraceIDComesFromSpanContext 响应体里的 trace_id 必须与链路 TraceID 同源
// （请求 ID 就是 TraceID，不再单独生成）。
func TestTraceIDComesFromSpanContext(t *testing.T) {
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

	body := decode(t, rec)
	if body.TraceID != traceID.String() {
		t.Errorf("trace_id = %q, want %q（应与链路 TraceID 一致）", body.TraceID, traceID.String())
	}
}

// TestTraceIDEmptyWithoutSpan 没有 span 时不应伪造 ID（宁可为空，也不给假标识）。
func TestTraceIDEmptyWithoutSpan(t *testing.T) {
	rec := serve(t, func(c *gin.Context) { OK(c, gin.H{"ok": true}) })
	if body := decode(t, rec); body.TraceID != "" {
		t.Errorf("无 span 时 trace_id 应为空，实际 %q", body.TraceID)
	}
}
