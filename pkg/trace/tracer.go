// Package trace 初始化 OpenTelemetry TracerProvider（OTLP/HTTP 主动推送），
// 并提供 Gin HTTP 与 go-redis 的埋点接入点。
//
// endpoint 为空且未传入额外 SpanProcessor 时优雅跳过：不创建 Provider、
// 不设置全局 Tracer，全局保持 NoopTracerProvider，埋点开销趋近于零。
//
// 与 OTel 的全局 API 的关系：Provider 句柄由调用方（main）持有并负责 Shutdown，
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
	// Endpoint OTLP/HTTP 端点；零值表示不上报（若同时没有额外 processor 则整体跳过）。
	Endpoint otelx.Endpoint
	// Service 服务身份，写入 span 的 Resource（三信号共用）。
	Service otelx.ServiceInfo
	// SampleRate 采样率 (0.0-1.0)；0 视为 1.0（全量采样）。
	SampleRate float64
}

// Init 初始化全局 TracerProvider。
//
// extraProcessors 用于挂载额外的 SpanProcessor（例如自定义导出/二次处理）。
// 返回 nil 表示未启用（既无端点也无额外 processor），调用方可直接跳过 Shutdown。
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

	// 无任何 processor：优雅跳过，不创建 Provider（全局保持 Noop）。
	if len(processors) == 0 {
		return nil, nil
	}

	// W3C TraceContext + Baggage 传播器：没有它，跨服务/跨中间件的
	// traceparent 无法 inject/extract，各段 span 会各用各的 trace id 形成断链。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	opts := []sdktrace.TracerProviderOption{
		// ParentBased：入口按比例采样，下游沿用上游的采样决定，保证链路完整。
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
		sdktrace.WithResource(cfg.Service.Resource()),
	}
	for _, p := range processors {
		opts = append(opts, sdktrace.WithSpanProcessor(p))
	}

	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	return provider, nil
}

// Shutdown 刷出并关闭 TracerProvider；未启用（nil）时为空操作。
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
