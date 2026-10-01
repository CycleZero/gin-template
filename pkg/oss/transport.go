package oss

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NewTracedTransport 用 OTel 追踪包装 HTTP Transport。
//
// 适用场景：部分服务直接用 COS SDK（*cos.Client）做预签名 / 分片上传，
// 不经过 OSS 接口，装饰器无从介入。此时包裹其 http.Client.Transport 即可覆盖
// 全部网络调用（含 SDK 内部重试），且只需在构造处改一行。
//
// ⚠️ 基数与隐私纪律：**绝不记录 URL 路径**——COS 的路径就是 object key。
// 只记录 HTTP 方法与主机名（主机名含 bucket，bucket 数量有界）。
func NewTracedTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &tracedTransport{base: base}
}

type tracedTransport struct {
	base http.RoundTripper
}

func (t *tracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tracer, _ := ossInstruments()

	ctx, span := tracer.Start(req.Context(), "COS HTTP "+req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.request.method", req.Method),
			attribute.String("server.address", req.URL.Host),
		),
	)
	defer span.End()

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	// 仅 5xx 标记为错误：4xx 多为预签名过期 / 权限等业务侧可处理的结果，
	// 计入错误率会掩盖真实故障。
	if resp.StatusCode >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, resp.Status)
	}
	return resp, nil
}
