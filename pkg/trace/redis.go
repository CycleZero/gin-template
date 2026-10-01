package trace

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	// redisTracerName Redis 追踪使用的 tracer / meter 名称。
	redisTracerName = "pkg/trace/redis"
	// redisHistogramName Redis 命令耗时直方图名称（毫秒）。
	redisHistogramName = "redis_client_operation_duration_ms"
)

// redisHook 是 go-redis 的 Hook 实现：为每条 Redis 命令产生 span 与耗时指标。
//
// 采用 Hook（而非包装 client）是 go-redis 的原生扩展点：无需改动任何调用方代码，
// 只要在构造 client 时 AddHook 即可全量覆盖，并对 pipeline 等批量形式同样生效。
//
// ⚠️ 基数纪律：只记录命令名（有限集合）与结果，
// **绝不记录 key / 参数 / 返回值**——那既是 PII 泄漏，也会让指标标签基数爆炸。
type redisHook struct {
	tracer oteltrace.Tracer
	hist   metric.Float64Histogram
}

var (
	redisHistOnce sync.Once
	redisHist     metric.Float64Histogram
)

// NewRedisHook 构造 Redis 追踪 Hook。
//
// 指标仪器在首次调用时绑定当时的全局 MeterProvider，因此 metrics.Init
// 必须先于数据层构造执行（main 中的顺序即是如此）。
func NewRedisHook() redis.Hook {
	return &redisHook{
		tracer: otel.Tracer(redisTracerName),
		hist:   redisHistogram(),
	}
}

// HookRedis 为 go-redis 客户端挂载追踪 Hook，并原样返回该客户端。
//
// 供需要自定义连接参数（Network / 超时 / DB 等）的场景使用：
// 把 `redis.NewClient(opts)` 包成 `trace.HookRedis(redis.NewClient(opts))` 即可，
// 不改变任何连接参数与行为。
func HookRedis(client *redis.Client) *redis.Client {
	if client == nil {
		return nil
	}
	client.AddHook(NewRedisHook())
	return client
}

// redisHistogram 惰性创建耗时直方图；创建失败时返回 nil，
// 调用方据此跳过指标记录（链路追踪不受影响）。
func redisHistogram() metric.Float64Histogram {
	redisHistOnce.Do(func() {
		h, err := otel.GetMeterProvider().Meter(redisTracerName).
			Float64Histogram(redisHistogramName,
				metric.WithDescription("Redis 操作耗时（毫秒）"),
				metric.WithUnit("ms"))
		if err == nil {
			redisHist = h
		}
	})
	return redisHist
}

// DialHook 建连阶段不产生 span：一条连接会服务大量命令，
// 为建连单独埋点只会制造噪声，且不反映业务耗时。
func (h *redisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 包裹单条命令的执行，产生一个客户端 span 与一条耗时记录。
func (h *redisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		op := strings.ToUpper(cmd.Name())
		start := time.Now()
		ctx, span := h.start(ctx, "Redis "+op, op, 0)

		err := next(ctx, cmd)

		h.finish(ctx, span, op, start, err)
		return err
	}
}

// ProcessPipelineHook 包裹 pipeline 执行。
//
// 整批只产生一个 span：pipeline 的语义就是"一次往返"，逐条埋点会高估网络开销；
// 批量大小作为属性记录，便于排查异常大的批量。
func (h *redisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		ctx, span := h.start(ctx, "Redis PIPELINE", "pipeline", len(cmds))

		err := next(ctx, cmds)

		h.finish(ctx, span, "pipeline", start, err)
		return err
	}
}

// start 开启客户端 span；batchSize > 0 时额外记录批量大小。
func (h *redisHook) start(ctx context.Context, spanName, op string, batchSize int) (context.Context, oteltrace.Span) {
	attrs := []attribute.KeyValue{
		attribute.String("db.system", "redis"),
		attribute.String("db.operation.name", op),
	}
	if batchSize > 0 {
		attrs = append(attrs, attribute.Int("db.operation.batch_size", batchSize))
	}
	return h.tracer.Start(ctx, spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(attrs...),
	)
}

// finish 结束 span 并记录耗时指标。
//
// redis.Nil 表示"键不存在"，属正常业务结果而非故障，不计入错误率——
// 否则缓存未命中会被误报为错误，污染告警。
func (h *redisHook) finish(ctx context.Context, span oteltrace.Span, op string, start time.Time, err error) {
	failed := err != nil && !errors.Is(err, redis.Nil)

	if failed {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()

	if h.hist == nil {
		return
	}
	result := "success"
	if failed {
		result = "error"
	}
	h.hist.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(
		attribute.String("db.operation.name", op),
		attribute.String("result", result),
	))
}
