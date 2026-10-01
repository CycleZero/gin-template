package trace

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// TraceIDHeader 关联 ID 的响应头名。
//
// 请求 ID 与链路 TraceID 是同一个值；响应头沿用业界通用的 X-Request-ID，
// 因为客户端、网关与日志采集器的默认约定都是它（换成 X-Trace-ID 反而要额外配置）。
const TraceIDHeader = "X-Request-ID"

// TraceID 返回当前请求的链路 TraceID；没有有效 span 时返回空字符串。
//
// 这是"请求 ID 闭环"的唯一取值来源：
//   - 中间件用它回写响应头 X-Request-ID
//   - pkg/response 用它填充响应体 trace_id
//   - pkg/log 用它填充日志字段 trace.id
//
// 三个位置同源，因此"客户端拿到的 ID"≡"日志里的 ID"≡"链路系统里的 ID"，
// 不再需要单独维护一个请求 ID，也就不会出现两套 ID 不一致的问题。
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
