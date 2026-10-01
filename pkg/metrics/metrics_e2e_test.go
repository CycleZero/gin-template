package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"gin-template/pkg/otelx"

	"go.opentelemetry.io/otel"
	colmetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// TestOTLPMetricsEndToEndHTTP 端到端验证：指标真的经 OTLP/HTTP 主动推送到了接收端。
func TestOTLPMetricsEndToEndHTTP(t *testing.T) {
	var (
		mu       sync.Mutex
		received *colmetric.ExportMetricsServiceRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 OTLP 请求体失败：%v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var req colmetric.ExportMetricsServiceRequest
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
		Service:  otelx.ServiceInfo{Name: "e2e-metrics", Version: "v1.2.3", Environment: "test"},
		Endpoint: otelx.ParseEndpoint(parsed.Host, true, "/v1/metrics"),
		// 周期设得足够长，测试内手动 ForceFlush，避免等待一个周期。
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Init 失败：%v", err)
	}
	if provider == nil {
		t.Fatal("端点非空时不应优雅跳过")
	}

	counter, err := otel.Meter("e2e").Int64Counter("e2e_requests_total")
	if err != nil {
		t.Fatalf("创建计数器失败：%v", err)
	}
	counter.Add(context.Background(), 3)

	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush 失败：%v", err)
	}
	if err := Shutdown(context.Background(), provider); err != nil {
		t.Fatalf("Shutdown 失败：%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("接收端未收到 metrics 导出请求")
	}

	resourceMetrics := received.GetResourceMetrics()
	if len(resourceMetrics) == 0 || len(resourceMetrics[0].GetScopeMetrics()) == 0 {
		t.Fatalf("OTLP 请求中缺少指标：%+v", received)
	}

	// 至少应包含我们手动记录的计数器。
	// 注意：同一 Provider 里还有 Go runtime 埋点（独立的 scope），必须遍历全部
	// scope 查找，不能只看第一个。
	found := false
	for _, rm := range resourceMetrics {
		for _, sm := range rm.GetScopeMetrics() {
			for _, metric := range sm.GetMetrics() {
				if metric.GetName() != "e2e_requests_total" {
					continue
				}
				found = true
				points := metric.GetSum().GetDataPoints()
				if len(points) == 0 {
					t.Fatal("计数器没有数据点")
				}
				if got := points[0].GetAsInt(); got != 3 {
					t.Errorf("计数器值 = %d, 期望 3", got)
				}
			}
		}
	}
	if !found {
		t.Error("未在导出结果中找到 e2e_requests_total")
	}

	// 统一服务身份 Resource。
	resourceAttrs := make(map[string]string)
	for _, kv := range resourceMetrics[0].GetResource().GetAttributes() {
		resourceAttrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	for k, want := range map[string]string{
		"service.name":           "e2e-metrics",
		"service.version":        "v1.2.3",
		"deployment.environment": "test",
	} {
		if resourceAttrs[k] != want {
			t.Errorf("Resource 属性 %s = %q, 期望 %q", k, resourceAttrs[k], want)
		}
	}
}

// TestInitDisabledIsGraceful 端点为空时应跳过上报（返回 nil），Shutdown(nil) 为空操作。
func TestInitDisabledIsGraceful(t *testing.T) {
	provider, err := Init(Config{Service: otelx.ServiceInfo{Name: "disabled"}})
	if err != nil {
		t.Fatalf("未配置端点不应报错：%v", err)
	}
	if provider != nil {
		t.Error("未配置端点时应返回 nil provider")
	}
	if err := Shutdown(context.Background(), nil); err != nil {
		t.Errorf("Shutdown(nil) 应为空操作，实际：%v", err)
	}
}
