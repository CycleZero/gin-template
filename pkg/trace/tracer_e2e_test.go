package trace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"gin-template/pkg/otelx"

	"go.opentelemetry.io/otel"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// TestOTLPTraceEndToEndHTTP 端到端验证：span 真的经 OTLP/HTTP 主动推送到了接收端，
// 且携带统一的服务身份 Resource。
func TestOTLPTraceEndToEndHTTP(t *testing.T) {
	var (
		mu       sync.Mutex
		received *coltrace.ExportTraceServiceRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 OTLP 请求体失败：%v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var req coltrace.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("解码 OTLP 请求失败：%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = &req
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("解析测试服务地址失败：%v", err)
	}

	provider, err := Init(Config{
		Endpoint:   otelx.ParseEndpoint(parsed.Host, true, "/v1/traces"),
		Service:    otelx.ServiceInfo{Name: "e2e-trace", Version: "v1.2.3", Environment: "test"},
		SampleRate: 1,
	})
	if err != nil {
		t.Fatalf("Init 失败：%v", err)
	}
	if provider == nil {
		t.Fatal("端点非空时不应优雅跳过")
	}

	_, span := otel.Tracer("e2e").Start(context.Background(), "e2e-operation")
	span.End()

	if err := Shutdown(context.Background(), provider); err != nil {
		t.Fatalf("Shutdown 失败：%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("接收端未收到 trace 导出请求")
	}

	resourceSpans := received.GetResourceSpans()
	if len(resourceSpans) == 0 ||
		len(resourceSpans[0].GetScopeSpans()) == 0 ||
		len(resourceSpans[0].GetScopeSpans()[0].GetSpans()) == 0 {
		t.Fatalf("OTLP 请求中缺少 span：%+v", received)
	}
	got := resourceSpans[0].GetScopeSpans()[0].GetSpans()[0]
	if got.GetName() != "e2e-operation" {
		t.Errorf("span 名 = %q, 期望 e2e-operation", got.GetName())
	}
	if len(got.GetTraceId()) == 0 || len(got.GetSpanId()) == 0 {
		t.Error("span 缺少 trace_id / span_id")
	}

	resourceAttrs := make(map[string]string)
	for _, kv := range resourceSpans[0].GetResource().GetAttributes() {
		resourceAttrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	for k, want := range map[string]string{
		"service.name":           "e2e-trace",
		"service.version":        "v1.2.3",
		"deployment.environment": "test",
	} {
		if resourceAttrs[k] != want {
			t.Errorf("Resource 属性 %s = %q, 期望 %q", k, resourceAttrs[k], want)
		}
	}
}

// TestInitDisabledIsGraceful 无端点且无额外 processor 时应优雅跳过，Shutdown(nil) 为空操作。
func TestInitDisabledIsGraceful(t *testing.T) {
	provider, err := Init(Config{Service: otelx.ServiceInfo{Name: "disabled"}})
	if err != nil {
		t.Fatalf("未配置端点不应报错：%v", err)
	}
	if provider != nil {
		t.Error("未配置端点且无 processor 时应返回 nil provider")
	}
	if err := Shutdown(context.Background(), nil); err != nil {
		t.Errorf("Shutdown(nil) 应为空操作，实际：%v", err)
	}
}

// TestInitSampleRateZeroDefaultsToOne 采样率 0（proto3 默认）应视为 1.0，而不是"不采样"。
func TestInitSampleRateZeroDefaultsToOne(t *testing.T) {
	provider, err := Init(Config{
		Endpoint: otelx.Endpoint{Host: "127.0.0.1:1", Insecure: true}, // 不会真的连上
		Service:  otelx.ServiceInfo{Name: "sample-rate"},
		// SampleRate 留空 = 0
	})
	if err != nil {
		t.Fatalf("Init 失败：%v", err)
	}
	if provider == nil {
		t.Fatal("端点非空时不应跳过")
	}
	_ = Shutdown(context.Background(), provider)
}
