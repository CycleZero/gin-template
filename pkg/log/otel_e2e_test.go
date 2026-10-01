package log

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"gin-template/pkg/otelx"

	oteltrace "go.opentelemetry.io/otel/trace"
	collog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

// TestOTLPCoreEndToEndHTTP 端到端验证：日志经 OTLP/HTTP 真正送达接收端，
// 且记录上带有链路上下文——这是"日志可跳转 trace"的最终证据。
//
// 用进程内 httptest 作为 OTLP 接收端，因此测试不依赖任何外部后端（SigNoz 等）。
func TestOTLPCoreEndToEndHTTP(t *testing.T) {
	const (
		traceIDHex = "5f0af201e4905c0eeab1603c31ab1540"
		spanIDHex  = "c9a26a0dddcd8b12"
	)

	var (
		mu       sync.Mutex
		received *collog.ExportLogsServiceRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 OTLP 请求体失败：%v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var req collog.ExportLogsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("解码 OTLP 请求失败：%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = &req
		mu.Unlock()

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("解析测试服务地址失败：%v", err)
	}

	core, shutdown, err := newOTLPCore(
		otelx.ServiceInfo{Name: "e2e-test", Version: "v9.9.9", Environment: "test"},
		otelx.ParseEndpoint(parsed.Host, true, "/v1/logs"),
		zapcore.DebugLevel,
	)
	if err != nil {
		t.Fatalf("创建 OTLP core 失败：%v", err)
	}
	if core == nil {
		t.Fatal("端点非空时 core 不应为 nil")
	}

	if err := core.Write(
		zapcore.Entry{Level: zapcore.InfoLevel, Message: "端到端日志", Time: time.Now()},
		[]zapcore.Field{
			zap.String(FieldTraceID, traceIDHex),
			zap.String(FieldSpanID, spanIDHex),
			zap.Int("user_id", 42),
		},
	); err != nil {
		t.Fatalf("Write 返回错误：%v", err)
	}

	// 刷出批量缓冲，确保记录真正发出（不 Shutdown 会一直留在缓冲里）。
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("关闭 OTLP 日志 Provider 失败：%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("接收端未收到任何 OTLP 导出请求")
	}

	resourceLogs := received.GetResourceLogs()
	if len(resourceLogs) == 0 ||
		len(resourceLogs[0].GetScopeLogs()) == 0 ||
		len(resourceLogs[0].GetScopeLogs()[0].GetLogRecords()) == 0 {
		t.Fatalf("OTLP 请求中缺少日志记录：%+v", received)
	}
	record := resourceLogs[0].GetScopeLogs()[0].GetLogRecords()[0]

	if got := hex.EncodeToString(record.GetTraceId()); got != traceIDHex {
		t.Errorf("OTLP 记录 TraceId = %s, 期望 %s", got, traceIDHex)
	}
	if got := hex.EncodeToString(record.GetSpanId()); got != spanIDHex {
		t.Errorf("OTLP 记录 SpanId = %s, 期望 %s", got, spanIDHex)
	}
	if got := record.GetBody().GetStringValue(); got != "端到端日志" {
		t.Errorf("OTLP 记录 Body = %q, 期望 %q", got, "端到端日志")
	}

	// 普通字段（非链路字段）应作为属性上报。
	attrs := make(map[string]int64)
	for _, kv := range record.GetAttributes() {
		attrs[kv.GetKey()] = kv.GetValue().GetIntValue()
	}
	if attrs["user_id"] != 42 {
		t.Errorf("OTLP 记录属性 user_id = %d, 期望 42", attrs["user_id"])
	}

	// Resource 属性应带上服务身份（service.name / service.version / deployment.environment）。
	resourceAttrs := make(map[string]string)
	for _, kv := range resourceLogs[0].GetResource().GetAttributes() {
		resourceAttrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	for k, want := range map[string]string{
		"service.name":           "e2e-test",
		"service.version":        "v9.9.9",
		"deployment.environment": "test",
	} {
		if resourceAttrs[k] != want {
			t.Errorf("Resource 属性 %s = %q, 期望 %q", k, resourceAttrs[k], want)
		}
	}
}

// TestNewOTLPCoreDisabled 端点为空时应降级为纯本地日志（返回 nil core，不建 Provider）。
func TestNewOTLPCoreDisabled(t *testing.T) {
	core, shutdown, err := newOTLPCore(otelx.ServiceInfo{Name: "x"}, otelx.Endpoint{}, zapcore.DebugLevel)
	if err != nil {
		t.Fatalf("端点为空不应报错：%v", err)
	}
	if core != nil {
		t.Error("端点为空时 core 应为 nil")
	}
	if shutdown != nil {
		t.Error("端点为空时不应返回关闭函数")
	}
}

// TestWithContextInjectsTraceFields 验证 ctx 中的 span 会被注入为 trace.id / span.id 字段，
// 而 OTLP core 正是靠这些字段还原链路上下文。
func TestWithContextInjectsTraceFields(t *testing.T) {
	traceID, err := oteltrace.TraceIDFromHex("5f0af201e4905c0eeab1603c31ab1540")
	if err != nil {
		t.Fatalf("构造 TraceID 失败：%v", err)
	}
	spanID, err := oteltrace.SpanIDFromHex("c9a26a0dddcd8b12")
	if err != nil {
		t.Fatalf("构造 SpanID 失败：%v", err)
	}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.FlagsSampled,
	})
	ctx := oteltrace.ContextWithSpanContext(context.Background(), sc)

	fields := correlationFields(ctx)
	if len(fields) != 4 {
		t.Fatalf("correlationFields 应产出 2 组键值，实际：%v", fields)
	}
	if fields[0] != FieldTraceID || fields[2] != FieldSpanID {
		t.Errorf("链路字段键名不符：%v", fields)
	}

	// 无 span 的 ctx 不应产出任何字段（避免 trace.id="" 噪声）。
	if got := correlationFields(context.Background()); len(got) != 0 {
		t.Errorf("无 span 的 ctx 不应产出字段，实际：%v", got)
	}
}
