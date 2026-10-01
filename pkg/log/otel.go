package log

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"gin-template/pkg/otelx"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

// 本文件把 zap 日志桥接到 OTLP 日志导出器（OTLP/HTTP 主动推送）。
//
// 它最重要的职责是**日志与链路的关联**：zap 的 Core.Write 拿不到 context，
// 链路信息只能以普通字段（trace.id / span.id）随日志一起传递。本 core 在写入时
// 把这些字段还原成 OTel SpanContext 并挂到日志记录的 context 上，OTel SDK 据此
// 为日志记录填充 trace_id / span_id，使后端能把一条日志直接跳转到对应 trace。
//
// 日志侧无需额外配置：与 trace/metrics 共用同一个 OTLP 端点，仅默认路径不同
// （/v1/logs）。

// otlpCore 是挂到 zap Tee 的 OTLP 日志 Core。
type otlpCore struct {
	emitter  otellog.Logger
	minLevel zapcore.Level
	fields   []zapcore.Field
}

// newOTLPCore 初始化 OTLP 日志导出，返回可挂到 zap Tee 的 Core 与关闭函数。
//
// endpoint 为零值（未配置）时返回 (nil, nil, nil)：由调用方降级为纯本地日志。
// 初始化失败返回 error，调用方应降级而非阻断进程启动。
func newOTLPCore(
	service otelx.ServiceInfo,
	endpoint otelx.Endpoint,
	minLevel zapcore.Level,
) (zapcore.Core, func(context.Context) error, error) {
	if endpoint.IsZero() {
		return nil, nil, nil
	}

	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint.Host)}
	if endpoint.Path != "" {
		opts = append(opts, otlploghttp.WithURLPath(endpoint.Path))
	}
	if endpoint.Insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	if len(endpoint.Headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(endpoint.Headers))
	}

	exporter, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 OTLP 日志导出器失败：%w", err)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(service.Resource()),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	name := service.Name
	if name == "" {
		name = "gin-template"
	}

	return &otlpCore{emitter: provider.Logger(name), minLevel: minLevel}, provider.Shutdown, nil
}

// Enabled 仅放行不低于阈值的级别。
func (c *otlpCore) Enabled(lvl zapcore.Level) bool { return lvl >= c.minLevel }

// With 派生子 Core 并合并字段，保证带模块字段的日志（logger.With("module", ...)）
// 同样携带上下文上报。
func (c *otlpCore) With(fields []zapcore.Field) zapcore.Core {
	merged := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	merged = append(merged, c.fields...)
	merged = append(merged, fields...)
	return &otlpCore{emitter: c.emitter, minLevel: c.minLevel, fields: merged}
}

// Check 按级别决定是否把当前 Core 加入写入链。
func (c *otlpCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write 把一条 zap 日志转换为 OTLP 日志记录并提交导出。
//
// 写入本身不做网络 I/O（由 SDK 的 BatchProcessor 异步批量发送），
// 因此不会阻塞业务线程。
func (c *otlpCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	all := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	all = append(all, c.fields...)
	all = append(all, fields...)

	var record otellog.Record
	record.SetTimestamp(entry.Time)
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(toOTelSeverity(entry.Level))
	record.SetSeverityText(entry.Level.CapitalString())
	record.SetBody(attribute.StringValue(entry.Message))
	record.AddAttributes(attributesFromFields(all)...)

	// 关键：把 zap 字段里的链路信息还原为 SpanContext，
	// SDK 会据此给日志记录填 trace_id / span_id。
	c.emitter.Emit(contextWithTrace(all), record)
	return nil
}

// Sync 无需额外动作：刷出由 SDK 的 BatchProcessor 与 Provider.Shutdown 负责。
func (c *otlpCore) Sync() error { return nil }

// contextWithTrace 从日志字段还原链路上下文；trace_id 无效时返回空 context。
//
// 这是"日志可跳转 trace"的实现核心：zap core 拿不到 ctx，只能靠字段回传。
func contextWithTrace(fields []zapcore.Field) context.Context {
	traceID, spanID, ok := traceContextFromFields(fields)
	if !ok {
		return context.Background()
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

// traceContextFromFields 从 zap 字段解析 trace/span ID。
//
// 同时兼容 `trace.id`（本包使用的写法）与 `trace_id` 两种键名。
// 后写字段覆盖前值；空值或非法值也必须清除旧请求编号。
func traceContextFromFields(fields []zapcore.Field) (trace.TraceID, trace.SpanID, bool) {
	var traceID trace.TraceID
	var spanID trace.SpanID

	for _, f := range fields {
		switch f.Key {
		case FieldTraceID, "trace_id":
			traceID = trace.TraceID{}
			if f.Type == zapcore.StringType {
				traceID, _ = trace.TraceIDFromHex(f.String)
			}
		case FieldSpanID, "span_id":
			spanID = trace.SpanID{}
			if f.Type == zapcore.StringType {
				spanID, _ = trace.SpanIDFromHex(f.String)
			}
		}
	}
	return traceID, spanID, traceID.IsValid()
}

// toOTelSeverity 把 zap 级别映射为 OTel 严重度。
func toOTelSeverity(lvl zapcore.Level) otellog.Severity {
	switch lvl {
	case zapcore.DebugLevel:
		return otellog.SeverityDebug
	case zapcore.InfoLevel:
		return otellog.SeverityInfo
	case zapcore.WarnLevel:
		return otellog.SeverityWarn
	case zapcore.ErrorLevel:
		return otellog.SeverityError
	default:
		return otellog.SeverityFatal
	}
}

// attributesFromFields 把 zap 字段转换为 OTel 属性。
//
// 链路字段（trace.id / span.id 等）由日志记录自身的链路上下文承载，
// 此处跳过以免重复写入。
func attributesFromFields(fields []zapcore.Field) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(fields))
	for _, f := range fields {
		if f.Key == "" || isTraceField(f.Key) {
			continue
		}
		attrs = append(attrs, attributeFromField(f))
	}
	return attrs
}

// isTraceField 判断字段是否为链路关联字段（跳过，避免与记录上下文重复）。
func isTraceField(key string) bool {
	switch key {
	case FieldTraceID, FieldSpanID, "trace_id", "span_id":
		return true
	default:
		return false
	}
}

// attributeFromField 按 zap 字段类型转换为 OTel 属性，未知类型退化为字符串。
func attributeFromField(f zapcore.Field) attribute.KeyValue {
	switch f.Type {
	case zapcore.StringType:
		return attribute.String(f.Key, f.String)
	case zapcore.BoolType:
		return attribute.Bool(f.Key, f.Integer == 1)
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
		zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type, zapcore.UintptrType:
		return attribute.Int64(f.Key, f.Integer)
	case zapcore.Float64Type:
		return attribute.Float64(f.Key, math.Float64frombits(uint64(f.Integer)))
	case zapcore.Float32Type:
		return attribute.Float64(f.Key, float64(math.Float32frombits(uint32(f.Integer))))
	case zapcore.DurationType:
		return attribute.String(f.Key, time.Duration(f.Integer).String())
	case zapcore.TimeType:
		return attribute.String(f.Key, time.Unix(0, f.Integer).Format(time.RFC3339Nano))
	case zapcore.StringerType:
		if s, ok := f.Interface.(fmt.Stringer); ok {
			return attribute.String(f.Key, s.String())
		}
	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			return attribute.String(f.Key, err.Error())
		}
	}
	if f.Interface != nil {
		return attribute.String(f.Key, fmt.Sprint(f.Interface))
	}
	return attribute.String(f.Key, strings.TrimSpace(f.String))
}
