// Package log 提供项目统一的日志器：以标准库 log/slog 为门面、zap 为后端，
// 并把它同时接到多个下游：
//
//   - 控制台：本地可读，始终存在
//   - 文件：配置了日志目录时按大小轮转
//   - OTLP：主动推送并与 trace 关联，使日志可在后端一键跳转到链路
//
// 本包**不保存全局 Logger，也不读取全局配置**：New 构建后由入口（main）
// 显式注入；需要包级 slog.Info/Error 时，由入口调用一次 slog.SetDefault。
//
// 文件分工：encoder.go 编码器、writer.go 写入器、file.go 文件路径与轮转、
// otel.go OTLP 桥接、print.go 格式化辅助。
package log

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"gin-template/pkg/otelx"

	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

// 结构化日志的链路关联字段键名。
//
// 沿用 `trace.id` / `span.id` 写法（与 Kratos、OTLP 生态一致），使本地日志与
// OTLP 日志记录使用同一套键名，排查时无需在脑中做映射。
const (
	FieldTraceID = "trace.id"
	FieldSpanID  = "span.id"
)

// 运行模式。
const (
	// ModeDev 彩色控制台 + 文本文件（人类可读优先）。
	ModeDev = 0
	// ModeProd JSON 控制台（WARN/ERROR 补色）+ JSON 文件（可被采集器解析）。
	ModeProd = 1
)

// Config 日志配置。
type Config struct {
	// Mode ModeDev 或 ModeProd。
	Mode int
	// Level debug / info / warn / error。
	Level string
	// Dir 日志目录；为空则只写控制台。
	Dir string
	// ServiceName 服务名：用于日志文件路径分段与 OTLP logger 名。
	ServiceName string
	// Service 服务身份（实例 / 版本 / 环境），写入 OTLP Resource（三信号共用）。
	Service otelx.ServiceInfo
	// OTLP 日志上报端点；零值表示不上报（默认，安全）。
	OTLP otelx.Endpoint
}

// Logger 日志句柄。
//
// 内嵌 *slog.Logger，因此 Info / Error / InfoContext / With 等方法可直接使用；
// 另外持有需要显式释放的资源（异步文件写入器、OTLP Provider），由 Close 统一下线。
type Logger struct {
	*slog.Logger

	closers []func(context.Context) error
}

// New 组装日志器。写入链最多三路：
//
//  1. 控制台（始终存在）
//  2. 文件（配置了日志目录时）
//  3. OTLP（配置了 OTLP 端点时，与 trace 关联）
//
// 可选 sink 初始化失败只降级、不阻断——日志始终至少有一条本地路径可用。
func New(cfg Config) (*Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("解析日志级别 %q 失败：%w", cfg.Level, err)
	}

	logPath := GetLogPath(cfg.Dir, cfg.ServiceName)
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败：%w", err)
		}
	}

	consoleEncoder, fileEncoder := buildEncoders(cfg.Mode)

	closers := make([]func(context.Context) error, 0, 2)
	cores := []zapcore.Core{
		zapcore.NewCore(consoleEncoder, buildConsoleSyncer(), level),
	}

	if syncer, closeFn := buildFileSyncer(logPath); syncer != nil {
		cores = append(cores, zapcore.NewCore(fileEncoder, syncer, level))
		if closeFn != nil {
			closers = append(closers, closeFn)
		}
	}

	// OTLP core：主动推送并与 trace 关联；初始化失败降级为本地日志。
	otlpCore, otlpClose, err := newOTLPCore(cfg.Service, cfg.OTLP, level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化 OTLP 日志导出失败，降级为本地日志：%v\n", err)
	} else if otlpCore != nil {
		cores = append(cores, otlpCore)
		closers = append(closers, otlpClose)
	}

	// slog -> zap 桥接：编码仍由本包编码器完成，caller 取自 slog 记录的调用点。
	handler := zapslog.NewHandler(zapcore.NewTee(cores...),
		zapslog.WithCaller(true),
		zapslog.AddStacktraceAt(slog.LevelError),
	)

	registerClosers(closers...)
	return &Logger{Logger: slog.New(handler), closers: closers}, nil
}

// Close 逆序关闭各 sink（先 OTLP flush，再文件 flush），进程退出前调用一次即可。
//
// 异步写入器与 OTLP BatchProcessor 都带缓冲：不调用 Close 会丢失进程退出前
// 尚未刷新的日志（law 默认 5s 空闲才落盘，OTLP 默认 1s 批量间隔）。
func (l *Logger) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	errs := make([]error, 0, len(l.closers))
	for i := len(l.closers) - 1; i >= 0; i-- {
		if err := l.closers[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Ctx 返回注入了链路字段（trace.id / span.id）的派生 logger。
//
// 用法：logger.Ctx(c.Request.Context()).Info("处理完成", "cost_ms", n)
func (l *Logger) Ctx(ctx context.Context) *slog.Logger {
	if l == nil {
		return slog.Default()
	}
	return WithContext(l.Logger, ctx)
}

// WithContext 为任意 *slog.Logger 注入链路字段。
//
// 供业务层使用——项目把 stdlib 的 *slog.Logger 注入 biz/service（与日志实现解耦），
// 这些 logger 通过本函数获得链路关联能力：
//
//	slog := log.WithContext(s.logger, ctx)
//	slog.Error("创建 Demo 失败", "error", err)
//
// 说明：zap 的 Core 拿不到 context，链路信息只能以字段形式随日志传递，
// OTLP sink 会在写入时把这些字段还原成 SpanContext（见 otel.go）。
func WithContext(l *slog.Logger, ctx context.Context) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	if ctx == nil {
		return l
	}
	fields := correlationFields(ctx)
	if len(fields) == 0 {
		return l
	}
	return l.With(fields...)
}

// correlationFields 从 ctx 提取链路字段；仅在取值有效时产出，避免 trace.id=""
// 这类空值噪声污染日志与检索。
func correlationFields(ctx context.Context) []any {
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return nil
	}
	fields := make([]any, 0, 4)
	fields = append(fields, FieldTraceID, sc.TraceID().String())
	if sc.HasSpanID() {
		fields = append(fields, FieldSpanID, sc.SpanID().String())
	}
	return fields
}

// ============================================================
// 致命退出路径
// ============================================================

var (
	registryMu sync.Mutex
	// registeredClosers 记录 New 创建的 sink 关闭函数，仅用于 Fatal 的 flush。
	// 这是**进程级资源登记表**（不是配置全局）：os.Exit 不执行 defer，
	// 初始化阶段的致命错误需要它来保证最后一条日志（尤其文件日志）落盘。
	registeredClosers []func(context.Context) error
)

// registerClosers 登记 sink 关闭函数，供 Fatal 在退出前 flush。
func registerClosers(closers ...func(context.Context) error) {
	if len(closers) == 0 {
		return
	}
	registryMu.Lock()
	registeredClosers = append(registeredClosers, closers...)
	registryMu.Unlock()
}

// flushRegistered 尽力刷出所有已登记的 sink；错误忽略（正在退出，无法补救）。
func flushRegistered(ctx context.Context) {
	registryMu.Lock()
	closers := registeredClosers
	registryMu.Unlock()

	for i := len(closers) - 1; i >= 0; i-- {
		_ = closers[i](ctx)
	}
}

// Fatal 记录一条 Error 级日志，flush 各 sink 后以状态码 1 退出进程。
//
// os.Exit 不会执行 defer，因此初始化阶段的致命错误必须经由 Fatal 记录，
// 否则异步文件日志会随进程退出而丢失。
//
// 这里手工构造 Record 并传入调用方 PC，使 caller 指向 Fatal 的调用点，
// 而不是本文件中的包装函数。
func Fatal(msg string, args ...any) {
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])

	ctx := context.Background()
	handler := slog.Default().Handler()
	if handler.Enabled(ctx, slog.LevelError) {
		record := slog.NewRecord(time.Now(), slog.LevelError, msg, pcs[0])
		record.Add(args...)
		_ = handler.Handle(ctx, record)
	}

	flushRegistered(ctx)
	os.Exit(1)
}
