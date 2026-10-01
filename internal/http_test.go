package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gin-template/internal/router"
	"gin-template/internal/router/middleware"
	"gin-template/pkg/otelx"
	"gin-template/pkg/response"
	"gin-template/pkg/trace"
)

// TestHealthEndpointsClosedLoop 端到端验证"请求 ID 闭环"与健康检查契约（无需数据库）：
//
//   - /healthz 存活探针：200 + 统一信封
//   - /readyz  就绪探针：依赖不可用时 503 + 业务码 50300，且不泄漏内部细节
//   - 响应头 X-Request-ID ≡ 响应体 trace_id（= 链路 TraceID，未配置 OTLP 时也必须有值）
func TestHealthEndpointsClosedLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 未配置 OTLP 端点：Provider 仍会生成 TraceID —— 这正是"请求 ID"的来源
	provider, err := trace.Init(trace.Config{Service: otelx.ServiceInfo{Name: "http-test"}})
	if err != nil {
		t.Fatalf("初始化 trace 失败：%v", err)
	}
	t.Cleanup(func() {
		if err := trace.Shutdown(context.Background(), provider); err != nil {
			t.Errorf("关闭 trace 失败：%v", err)
		}
	})

	newEngine := func(readiness router.ReadinessChecker) *gin.Engine {
		e := gin.New()
		e.Use(trace.Middleware("http-test")) // 产生 server span / TraceID
		e.Use(middleware.TraceID())          // 回写响应头 X-Request-ID
		router.RegisterHealth(e, readiness)
		return e
	}

	get := func(e *gin.Engine, path string) (*httptest.ResponseRecorder, string) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec, rec.Header().Get(trace.TraceIDHeader)
	}

	t.Run("healthz 存活探针", func(t *testing.T) {
		e := newEngine(func(context.Context) error { return nil })
		rec, header := get(e, "/healthz")

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, want 200", rec.Code)
		}
		var body response.Body
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应体失败：%v", err)
		}
		if body.Code != 0 {
			t.Errorf("业务码 = %d, want 0", body.Code)
		}
		if header == "" {
			t.Error("响应头 X-Request-ID 不应为空")
		}
		if body.TraceID != header {
			t.Errorf("响应体 trace_id (%q) 与响应头 (%q) 不一致", body.TraceID, header)
		}
	})

	t.Run("readyz 依赖不可用", func(t *testing.T) {
		e := newEngine(func(context.Context) error {
			return errors.New("mysql: dial tcp 127.0.0.1:3306: connect: connection refused")
		})
		rec, header := get(e, "/readyz")

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("状态码 = %d, want 503", rec.Code)
		}
		var body response.Body
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应体失败：%v", err)
		}
		if body.Code != 50300 {
			t.Errorf("业务码 = %d, want 50300", body.Code)
		}
		if body.Message != "依赖未就绪" {
			t.Errorf("消息 = %q, want 依赖未就绪", body.Message)
		}
		if strings.Contains(rec.Body.String(), "3306") {
			t.Errorf("响应体泄漏了依赖细节：%s", rec.Body.String())
		}
		if body.TraceID == "" || body.TraceID != header {
			t.Errorf("失败响应同样要带 trace_id（header=%q body=%q）", header, body.TraceID)
		}
	})

	t.Run("readyz 依赖正常", func(t *testing.T) {
		e := newEngine(func(context.Context) error { return nil })
		rec, _ := get(e, "/readyz")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, want 200", rec.Code)
		}
	})
}

// TestTraceIDContinuesUpstreamTraceparent 上游传入 traceparent 时，
// 响应头必须续接上游 TraceID（跨服务链路不断），而不是自己新起一条链。
func TestTraceIDContinuesUpstreamTraceparent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	provider, err := trace.Init(trace.Config{Service: otelx.ServiceInfo{Name: "propagation-test"}})
	if err != nil {
		t.Fatalf("初始化 trace 失败：%v", err)
	}
	t.Cleanup(func() { _ = trace.Shutdown(context.Background(), provider) })

	const upstreamTraceID = "5f0af201e4905c0eeab1603c31ab1540"

	e := gin.New()
	e.Use(trace.Middleware("propagation-test"))
	e.Use(middleware.TraceID())
	router.RegisterHealth(e, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("traceparent", "00-"+upstreamTraceID+"-c9a26a0dddcd8b12-01")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get(trace.TraceIDHeader); got != upstreamTraceID {
		t.Errorf("响应头 X-Request-ID = %q, want 上游 TraceID %q", got, upstreamTraceID)
	}
}
