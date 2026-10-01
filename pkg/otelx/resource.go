package otelx

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// ServiceInfo 服务身份信息，是 trace / metrics / log 三类信号共享的 Resource 真源。
//
// 三处统一调用 Resource()，保证属性集合完全一致（否则会出现"日志能按版本过滤、
// 指标不能"这类漂移）。
type ServiceInfo struct {
	// Name 服务名，写入 service.name。
	Name string
	// ID 实例标识，写入 service.instance.id（通常取主机名或启动时生成的 UUID）。
	ID string
	// Version 构建版本，写入 service.version（建议由 -ldflags 注入真实 git 版本）。
	Version string
	// Environment 部署环境，写入 deployment.environment（如 production / staging）。
	Environment string
}

// Resource 构建统一的 OTel Resource。
//
// 属性仅在字段非空时写入——避免 service.version="" / deployment.environment=""
// 这类空值污染，使后端无法按"是否有值"过滤。
//
// 同时启用 WithFromEnv()（允许 OTEL_RESOURCE_ATTRIBUTES 覆盖）与
// WithTelemetrySDK()（自动写入 telemetry.sdk.* 属性）。
// 构建失败时降级为最小 Resource，绝不阻断进程启动。
func (s ServiceInfo) Resource() *resource.Resource {
	attrs := s.attributes()
	res, err := resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		// 资源解析失败（如 OTEL_RESOURCE_ATTRIBUTES 非法）时降级，不阻断启动。
		return resource.NewSchemaless(attrs...)
	}
	return res
}

// attributes 把非空字段转换为 OTel 属性。
//
// 四个键都使用字面量（而非 semconv 常量），以保证跨 semconv/otel 版本升级时
// 键名与行为保持稳定——这些键名本身就是规范的一部分。
func (s ServiceInfo) attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 4)
	if s.Name != "" {
		attrs = append(attrs, attribute.String("service.name", s.Name))
	}
	if s.Version != "" {
		attrs = append(attrs, attribute.String("service.version", s.Version))
	}
	if s.ID != "" {
		attrs = append(attrs, attribute.String("service.instance.id", s.ID))
	}
	if s.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment", s.Environment))
	}
	return attrs
}
