// Package metrics 初始化 OpenTelemetry MeterProvider（OTLP/HTTP 主动推送）。
//
// 出口策略：周期推送（PeriodicReader）直连可观测后端，无需在集群里额外部署
// 采集器。endpoint 为空表示关闭指标上报，此时不创建 Provider（全局保持
// NoopMeterProvider，埋点自动降级为零开销）。
//
// 注意：OTel 的 Provider 是进程级全局（otel.SetMeterProvider），这是 OTel API
// 的既定设计（各埋点库通过全局取 meter）。本包不引入额外的配置全局：
// Provider 句柄由调用方持有并负责 Shutdown。
package metrics

import (
	"context"
	"fmt"
	"time"

	"gin-template/pkg/otelx"

	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// DefaultInterval 指标默认推送周期。
const DefaultInterval = 15 * time.Second

// Config 指标初始化配置。
type Config struct {
	// Service 服务身份，写入指标 Resource（三信号共用）。
	Service otelx.ServiceInfo
	// Endpoint OTLP 端点；零值表示关闭指标上报。
	Endpoint otelx.Endpoint
	// Interval 推送周期；<= 0 时使用 DefaultInterval。
	Interval time.Duration
}

// Init 初始化全局 MeterProvider，并启动 Go runtime 指标采集。
//
// 返回 nil 表示未启用（Endpoint 为空）；此时不会有任何网络开销，
// 调用方可直接跳过 Shutdown。
func Init(cfg Config) (*sdkmetric.MeterProvider, error) {
	if cfg.Endpoint.IsZero() {
		return nil, nil
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	reader, err := newOTLPReader(cfg.Endpoint, interval)
	if err != nil {
		return nil, err
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(cfg.Service.Resource()),
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(provider)

	// Go runtime 指标（goroutine / 内存 / GC 等）随全局 Provider 周期上报；
	// 失败仅提示，不影响其余指标与业务。
	if err := otelruntime.Start(otelruntime.WithMeterProvider(provider)); err != nil {
		return provider, fmt.Errorf("启动 Go runtime 指标失败：%w", err)
	}
	return provider, nil
}

// Shutdown 刷出并关闭 MeterProvider；未启用（nil）时为空操作，可安全重复调用。
//
// OTLP 是周期推送，进程退出前必须调用，否则会丢失最后一个周期内的数据。
func Shutdown(ctx context.Context, provider *sdkmetric.MeterProvider) error {
	if provider == nil {
		return nil
	}
	return provider.Shutdown(ctx)
}

// newOTLPReader 创建 OTLP/HTTP 指标导出器与周期读取器。
func newOTLPReader(endpoint otelx.Endpoint, interval time.Duration) (sdkmetric.Reader, error) {
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(endpoint.Host)}
	if endpoint.Path != "" {
		opts = append(opts, otlpmetrichttp.WithURLPath(endpoint.Path))
	}
	if endpoint.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	if len(endpoint.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(endpoint.Headers))
	}

	exporter, err := otlpmetrichttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("创建 OTLP 指标导出器失败：%w", err)
	}
	return sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval)), nil
}
