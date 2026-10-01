package main

import (
	"os"
	"time"

	"gin-template/internal/conf"
	"gin-template/pkg/log"
	"gin-template/pkg/metrics"
	"gin-template/pkg/otelx"
	"gin-template/pkg/trace"
)

// otelEndpoints 从 otel 配置派生三路端点。
//
// 三类信号共用一个 base endpoint，仅 exporter 默认路径不同
// （/v1/traces、/v1/metrics、/v1/logs）；metrics_endpoint / logs_endpoint
// 是逃生口，用于把某一路单独指向别的后端。
//
// endpoint 为空时返回零值端点，各 Init 据此跳过上报（安全默认）。
func otelEndpoints(o *conf.Otel) (tracesEP, metricsEP, logsEP otelx.Endpoint) {
	base := o.GetEndpoint()
	insecure := o.GetInsecure()

	tracesEP = otelx.ParseEndpoint(base, insecure, "/v1/traces")
	metricsEP = otelx.ParseEndpoint(firstNonEmpty(o.GetMetricsEndpoint(), base), insecure, "/v1/metrics")
	logsEP = otelx.ParseEndpoint(firstNonEmpty(o.GetLogsEndpoint(), base), insecure, "/v1/logs")

	// 额外请求头（如 ingestion key）三路共用。
	if headers := o.GetHeaders(); len(headers) > 0 {
		tracesEP.Headers = headers
		metricsEP.Headers = headers
		logsEP.Headers = headers
	}
	return tracesEP, metricsEP, logsEP
}

// logConfig 把应用配置映射为日志配置。
func logConfig(cfg *conf.Bootstrap, service otelx.ServiceInfo, logsEndpoint otelx.Endpoint) log.Config {
	mode := log.ModeDev
	if cfg.GetLog().GetMode() == "prod" {
		mode = log.ModeProd
	}
	return log.Config{
		Mode:        mode,
		Level:       cfg.GetLog().GetLevel(),
		Dir:         cfg.GetLog().GetDir(),
		ServiceName: service.Name,
		Service:     service,
		OTLP:        logsEndpoint,
	}
}

// traceConfig 把应用配置映射为 trace 配置。
func traceConfig(cfg *conf.Bootstrap, service otelx.ServiceInfo, tracesEndpoint otelx.Endpoint) trace.Config {
	return trace.Config{
		Endpoint:   tracesEndpoint,
		Service:    service,
		SampleRate: cfg.GetOtel().GetTraceSampleRate(),
	}
}

// metricsConfig 把应用配置映射为 metrics 配置。
func metricsConfig(cfg *conf.Bootstrap, service otelx.ServiceInfo, metricsEndpoint otelx.Endpoint) metrics.Config {
	return metrics.Config{
		Service:  service,
		Endpoint: metricsEndpoint,
		Interval: time.Duration(cfg.GetOtel().GetMetricsIntervalSeconds()) * time.Second,
	}
}

// hostname 返回实例标识（写入 service.instance.id）；取不到时回退 "unknown"。
func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}

// firstNonEmpty 返回第一个非空字符串（用于"专用端点优先，否则回退 base"）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
