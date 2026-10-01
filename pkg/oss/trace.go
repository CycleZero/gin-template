package oss

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func (t *tracedOSS) PutPrivateObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (err error) {
	ctx, span, start := t.start(ctx, opPut, size)
	defer func() { t.finish(ctx, span, opPut, start, err) }()
	private, ok := t.inner.(PrivateObjectStore)
	if !ok {
		return errors.New("对象存储不支持私有上传")
	}
	return private.PutPrivateObject(ctx, key, reader, size, contentType)
}

func (t *tracedOSS) PutImmutablePublicObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string, metadata map[string]string) (err error) {
	ctx, span, start := t.start(ctx, opPut, size)
	defer func() { t.finish(ctx, span, opPut, start, err) }()
	store, ok := t.inner.(ImmutablePublicStore)
	if !ok {
		return errors.New("对象存储不支持不可变公共素材")
	}
	return store.PutImmutablePublicObject(ctx, key, reader, size, contentType, metadata)
}

const (
	// ossTracerName 对象存储追踪使用的 tracer / meter 名称。
	ossTracerName = "pkg/oss"
	// ossHistogramName 对象存储操作耗时直方图名称（毫秒，见 AGENTS.md 的耗时单位约定）。
	ossHistogramName = "oss_client_operation_duration_ms"
)

// ossOperation 描述一次对象存储操作，把 span 名称与指标标签绑定在一起，
// 避免二者在各方法里各写一遍而漂移。
type ossOperation struct {
	spanName string // span 名称：中文可读，与 GORM 的"数据库查询"命名风格一致
	label    string // 指标 operation 标签：英文短标识，基数有界（9 个取值）
}

var (
	opPut        = ossOperation{"COS 上传", "put"}
	opGet        = ossOperation{"COS 下载", "get"}
	opStat       = ossOperation{"COS 查询元信息", "stat"}
	opDelete     = ossOperation{"COS 删除", "delete"}
	opPresignGet = ossOperation{"COS 预签名URL", "presign_get"}
	opPresignPut = ossOperation{"COS 预签名直传URL", "presign_put"}
	opCopy       = ossOperation{"COS 复制", "copy"}
	opList       = ossOperation{"COS 列举", "list"}
	opThumbnail  = ossOperation{"COS 生成缩略图", "thumbnail"}
)

func (t *tracedOSS) PersistImageThumbnail(ctx context.Context, source, target string, longEdge, quality int) (err error) {
	ctx, span, started := t.start(ctx, opThumbnail, 0)
	defer func() { t.finish(ctx, span, opThumbnail, started, err) }()
	processor, ok := t.inner.(PersistentThumbnailStore)
	if !ok {
		return errors.New("对象存储不支持持久化缩略图")
	}
	return processor.PersistImageThumbnail(ctx, source, target, longEdge, quality)
}

var (
	ossInstrumentsOnce sync.Once
	ossTracer          trace.Tracer
	ossHistogram       metric.Float64Histogram
)

// ossInstruments 惰性创建 tracer 与耗时直方图。
//
// 与 pkg/metrics 的既有约定一致：指标仪器绑定创建时刻的全局 MeterProvider，
// 因此 metrics.Init 必须先于对象存储客户端的构造执行；未初始化时降级为 Noop，
// 链路追踪不受影响。
func ossInstruments() (trace.Tracer, metric.Float64Histogram) {
	ossInstrumentsOnce.Do(func() {
		ossTracer = otel.Tracer(ossTracerName)
		h, err := otel.GetMeterProvider().Meter(ossTracerName).
			Float64Histogram(ossHistogramName,
				metric.WithDescription("对象存储操作耗时（毫秒）"),
				metric.WithUnit("ms"))
		if err == nil {
			ossHistogram = h
		}
	})
	return ossTracer, ossHistogram
}

// tracedOSS 是 OSS 接口的追踪装饰器：为每次对象存储调用产生客户端 span 与耗时指标。
//
// 采用装饰器（而非 Hook）是因为 COS/MinIO 的 SDK 没有 hook 机制；
// 包装动作发生在 provider 构造器内部，因此所有调用方自动获得追踪，无需改业务代码。
//
// ⚠️ 基数纪律：只记录操作类型与结果，**绝不记录 object key / 文件路径 / 文件内容**——
// 那既是 PII 泄漏，也会让指标标签基数爆炸。
type tracedOSS struct {
	inner OSS
}

// NewTracedOSS 用追踪装饰器包装 OSS 实现；inner 为 nil 时原样返回。
func NewTracedOSS(inner OSS) OSS {
	if inner == nil {
		return nil
	}
	return &tracedOSS{inner: inner}
}

// start 开启客户端 span；size > 0 时额外记录对象大小。
func (t *tracedOSS) start(ctx context.Context, op ossOperation, size int64) (context.Context, trace.Span, time.Time) {
	start := time.Now()
	attrs := []attribute.KeyValue{attribute.String("oss.operation", op.label)}
	if size > 0 {
		attrs = append(attrs, attribute.Int64("oss.object.size", size))
	}
	tracer, _ := ossInstruments()
	ctx, span := tracer.Start(ctx, op.spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx, span, start
}

// finish 结束 span 并记录耗时指标。
func (t *tracedOSS) finish(ctx context.Context, span trace.Span, op ossOperation, start time.Time, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()

	_, hist := ossInstruments()
	if hist == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	hist.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(
		attribute.String("oss.operation", op.label),
		attribute.String("result", result),
	))
}

// PutObject 上传对象。
func (t *tracedOSS) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	ctx, span, start := t.start(ctx, opPut, size)
	err := t.inner.PutObject(ctx, key, reader, size, contentType)
	t.finish(ctx, span, opPut, start, err)
	return err
}

// GetObject 获取对象。
func (t *tracedOSS) GetObject(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	ctx, span, start := t.start(ctx, opGet, 0)
	reader, info, err := t.inner.GetObject(ctx, key)
	t.finish(ctx, span, opGet, start, err)
	return reader, info, err
}

// StatObject 获取对象元信息。
func (t *tracedOSS) StatObject(ctx context.Context, key string) (*ObjectInfo, error) {
	ctx, span, start := t.start(ctx, opStat, 0)
	info, err := t.inner.StatObject(ctx, key)
	t.finish(ctx, span, opStat, start, err)
	return info, err
}

// DeleteObject 删除对象。
func (t *tracedOSS) DeleteObject(ctx context.Context, key string) error {
	ctx, span, start := t.start(ctx, opDelete, 0)
	err := t.inner.DeleteObject(ctx, key)
	t.finish(ctx, span, opDelete, start, err)
	return err
}

// GetPresignedURL 获取下载用预签名 URL。
func (t *tracedOSS) GetPresignedURL(ctx context.Context, key string, expirySeconds int64) (string, error) {
	ctx, span, start := t.start(ctx, opPresignGet, 0)
	url, err := t.inner.GetPresignedURL(ctx, key, expirySeconds)
	t.finish(ctx, span, opPresignGet, start, err)
	return url, err
}

// GetPresignedPutURL 获取直传用预签名 PUT URL。
func (t *tracedOSS) GetPresignedPutURL(ctx context.Context, key string, contentType string, expirySeconds int64) (string, error) {
	ctx, span, start := t.start(ctx, opPresignPut, 0)
	url, err := t.inner.GetPresignedPutURL(ctx, key, contentType, expirySeconds)
	t.finish(ctx, span, opPresignPut, start, err)
	return url, err
}

// CopyObject 复制对象。
func (t *tracedOSS) CopyObject(ctx context.Context, sourceKey, destKey string) error {
	ctx, span, start := t.start(ctx, opCopy, 0)
	err := t.inner.CopyObject(ctx, sourceKey, destKey)
	t.finish(ctx, span, opCopy, start, err)
	return err
}

// ListObjects 列举对象。
func (t *tracedOSS) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	ctx, span, start := t.start(ctx, opList, 0)
	objects, err := t.inner.ListObjects(ctx, prefix)
	t.finish(ctx, span, opList, start, err)
	return objects, err
}
