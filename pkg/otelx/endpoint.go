// Package otelx 提供 OpenTelemetry 相关的小工具，供 trace / metrics / log 三类信号共用。
//
// 设计目标：
//  1. **单一端点配置源**：一个 OTLP 端点同时驱动 trace/metrics/logs 三种 exporter，
//     三者仅默认路径不同（/v1/traces、/v1/metrics、/v1/logs）。
//  2. **单一 Resource 真源**：service.name / service.version / service.instance.id /
//     deployment.environment 只在此处构建一次，避免各信号包各自拼装导致属性漂移。
//
// 背景（真实故障模式）：配置里若把端点写成 "http://127.0.0.1:4318"，而 OTel 各
// exporter 的 WithEndpoint 只接受 host:port，就会二次拼接协议头，生成
// "http://http:%2F%2F127.0.0.1:4318/v1/traces" 这类畸形 URL 导致上报静默失败。
// ParseEndpoint 即为此而设：统一规范化端点写法。
package otelx

import (
	"net/url"
	"strings"
)

// Endpoint 规范化后的 OTLP/HTTP 端点。
//
// 零值（Host 为空）表示"该信号未配置"：调用方据此跳过 exporter 创建，
// 使「不配置即关闭上报」成为默认的安全行为。
type Endpoint struct {
	// Host 形如 host:port（不含 scheme），可直接传给各 exporter 的 WithEndpoint。
	Host string
	// Path 仅在自定义路径时非空；为空表示使用 exporter 自身默认路径。
	Path string
	// Insecure 是否明文（HTTP）传输。https:// 写法会强制为 false。
	Insecure bool
	// Headers 额外请求头（如可观测后端要求的 ingestion key）。
	// ParseEndpoint 不填充该字段，由调用方按需注入（密钥建议来自环境变量）。
	Headers map[string]string
}

// IsZero 判断端点是否未配置（调用方据此跳过 exporter 创建）。
func (e Endpoint) IsZero() bool { return strings.TrimSpace(e.Host) == "" }

// ParseEndpoint 规范化 OTLP/HTTP 端点，兼容带 scheme 的配置写法。
//
// 参数：
//   - raw：原始配置值。支持 "host:port"、"host:port/path"、
//     "http://host:port"、"https://host:port[/path]"。
//   - insecure：调用方默认是否明文传输；会被 raw 中的 scheme 覆盖。
//   - signalPath：该信号的 exporter 默认路径（如 "/v1/traces"），
//     用于判断是否需要重复设置路径。
//
// 规则：
//   - 空值/纯空白：返回完全零值（Insecure 一并归零），由调用方通过 IsZero()
//     判断并跳过 exporter 创建（也可由 OTel 标准环境变量 OTEL_EXPORTER_OTLP_* 兜底）。
//   - 无 "://"：按 host:port[/path] 处理，沿用调用方传入的 insecure。
//   - "http://"：剥离 scheme 并强制明文（覆盖 insecure=false）。
//   - "https://"：剥离 scheme 并强制 TLS（覆盖 insecure=true）。
//   - 解析失败（URL 非法或 Host 为空）：退化为"裸剥离 scheme"，保证进程仍能启动
//     并交由 exporter 报错，绝不 panic。
//   - 路径去尾斜杠；若与 signalPath（同样去尾斜杠）相同则置空，
//     避免与 exporter 默认值重复拼接。
func ParseEndpoint(raw string, insecure bool, signalPath string) Endpoint {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Endpoint{}
	}

	// 无 scheme：可能是 host:port，也可能带自定义路径 host:port/path。
	if !strings.Contains(trimmed, "://") {
		host, p, found := strings.Cut(trimmed, "/")
		if !found {
			return Endpoint{Host: host, Insecure: insecure}
		}
		return Endpoint{Host: host, Path: normalizePath("/"+p, signalPath), Insecure: insecure}
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		// 解析失败时降级为裸剥离 scheme，保证进程可启动（由 exporter 上报错误）。
		fallback := strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://"), "http://")
		return Endpoint{Host: fallback, Insecure: insecure}
	}

	// scheme 决定传输安全语义：http 强制明文，https 强制 TLS。
	switch strings.ToLower(u.Scheme) {
	case "https":
		insecure = false
	case "http":
		insecure = true
	}

	return Endpoint{Host: u.Host, Path: normalizePath(u.Path, signalPath), Insecure: insecure}
}

// normalizePath 归一化路径：去尾斜杠；与 exporter 默认路径相同时返回空串
// （空串表示"用 exporter 默认值"，避免重复拼接出 /v1/traces/v1/traces）。
func normalizePath(p, signalPath string) string {
	p = strings.TrimSuffix(p, "/")
	if p == strings.TrimSuffix(signalPath, "/") {
		return ""
	}
	return p
}
