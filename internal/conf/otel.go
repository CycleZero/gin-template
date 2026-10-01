package conf

import "gin-template/pkg/otelx"

// DefaultServiceName 未配置 otel.service_name 时使用的默认服务名。
const DefaultServiceName = "gin-template"

// ServiceName 返回服务名（未配置时回退默认值）。
//
// 供日志文件路径分段与 HTTP 埋点使用，是服务名的唯一真源。
func (x *Bootstrap) ServiceName() string {
	if name := x.GetOtel().GetServiceName(); name != "" {
		return name
	}
	return DefaultServiceName
}

// OtelServiceInfo 汇总 otel 配置为三信号（trace/metrics/logs）共用的服务身份。
//
// 只在此处做默认值兜底，避免三路各自拼装 Resource 导致属性漂移
// （例如"日志能按版本过滤、指标不能"）。
func (x *Bootstrap) OtelServiceInfo(instanceID string) otelx.ServiceInfo {
	o := x.GetOtel()
	return otelx.ServiceInfo{
		Name:        x.ServiceName(),
		ID:          instanceID,
		Version:     o.GetServiceVersion(),
		Environment: o.GetEnvironment(),
	}
}
