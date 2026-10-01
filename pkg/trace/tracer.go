// Package trace 初始化 OpenTelemetry TracerProvider（OTLP/HTTP 主动推送），
// 并提供 Gin HTTP 与 go-redis 的埋点接入点。
//
// **始终安装 Provider**：即使没有配置 OTLP 端点，Init 也会装上 W3C 传播器与一个
// 不采样、不导出的 Provider。原因是本项目的"请求 ID"就是链路 TraceID：
// 没有 Provider 就取不到 TraceID，请求 ID 会凭空消失。装上之后：
//   - 每个请求都有真实 TraceID，并沿用上游 traceparent（跨服务链路不断）
//   - 采样器为 NeverSample 且无 exporter：不记录 span、不导出，几乎没有额外开销
//
// 与 OTel 全局 API 的关系：Provider 句柄由调用方（main）持有并负责 Shutdown，
// 本包只在初始化时调用 otel.SetTracerProvider / SetTextMapPropagator——
// 这是 OTel 的既定设计（各埋点库与自动埋点通过全局取 tracer）。
package trace

import (
	"context"
	"errors"
	"fmt"

	"gin-template/pkg/otelx"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config trace 初始化配置。
type Config struct {
	// Endpoint OTLP/HTTP 端点；零值表示不导出 span（但仍会生成与传播 TraceID）。
	Endpoint otelx.Endpoint
	// Service 服务身份，写入 span 的 Resource（三信号共用）。
	Service otelx.ServiceInfo
	// SampleRate 采样率 (0.0-1.0)；0 视为 1.0（全量采样）。
	SampleRate float64
}

// Init 初始化全局 TracerProvider 并安装 W3C TraceContext/Baggage 传播器。
//
// 无论是否配置 OTLP 端点都会返回可用的 Provider（不再返回 nil）：
// 无端点时使用 NeverSample + 无导出器，只保留 TraceID 的生成与传播能力，
// 使"请求 ID = TraceID"在未接入链路后端时同样成立。
//
// extraProcessors 用于挂载额外的 SpanProcessor（例如自定义导出或二次处理）。
func Init(cfg Config, extraProcessors ...sdktrace.SpanProcessor) (*sdktrace.TracerProvider, error) {
	// 配置未显式设置采样率时（proto3 数值默认 0）走全量采样。
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}

	processors := make([]sdktrace.SpanProcessor, 0, 1+len(extraProcessors))
	if !cfg.Endpoint.IsZero() {
		exporter, err := newOTLPExporter(cfg.Endpoint)
		if err != nil {
			return nil, err
		}
		processors = append(processors, sdktrace.NewBatchSpanProcessor(exporter))
	}
	processors = append(processors, extraProcessors...)

	// W3C TraceContext + Baggage 传播器：没有它，跨服务/跨中间件的
	// traceparent 无法 inject/extract，各段 span 会各用各的 trace id 形成断链。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(cfg.Service.Resource()),
	}

	if len(processors) == 0 {
		// 未接入链路后端：NeverSample 表示不记录 span、不导出，
		// 但 Tracer 仍会为每个 span 生成合法的 TraceID（请求 ID 依赖它）。
		opts = append(opts, sdktrace.WithSampler(sdktrace.NeverSample()))
	} else {
		opts = append(opts,
			// ParentBased：入口按比例采样，下游沿用上游的采样决定，保证链路完整。
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
		)
		for _, p := range processors {
			opts = append(opts, sdktrace.WithSpanProcessor(p))
		}
	}

	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	return provider, nil
}

// Shutdown 刷出并关闭 TracerProvider；provider 为 nil 时为空操作（防御性）。
//
// 进程退出前必须调用，否则 BatchSpanProcessor 中尚未导出的 span 会随进程退出丢失。
// 即使 ForceFlush 失败也继续 Shutdown，两个错误一并返回。
func Shutdown(ctx context.Context, provider *sdktrace.TracerProvider) error {
	if provider == nil {
		return nil
	}
	flushErr := provider.ForceFlush(ctx)
	shutdownErr := provider.Shutdown(ctx)
	return errors.Join(flushErr, shutdownErr)
}

// newOTLPExporter 创建 OTLP/HTTP trace 导出器。
func newOTLPExporter(endpoint otelx.Endpoint) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint.Host)}
	if endpoint.Path != "" {
		opts = append(opts, otlptracehttp.WithURLPath(endpoint.Path))
	}
	if endpoint.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if len(endpoint.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(endpoint.Headers))
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("创建 OTLP Trace 导出器失败：%w", err)
	}
	return exporter, nil
}
