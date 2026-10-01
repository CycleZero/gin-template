// Package log 提供以标准库 log/slog 为门面、zap 为后端的日志能力。
//
// slog 与 zap 之间通过 go.uber.org/zap/exp/zapslog 桥接：
// 业务代码只依赖 slog API，而编码（dev 彩色控制台 / prod JSON）、
// 文件切割（lumberjack）与异步落盘（law）仍复用既有的 zapcore 配置。
//
// 用法：
//
//	slog.Info("服务已启动", "addr", addr)
//	slog.Error("连接数据库失败", "error", err)
//
// 本包不保存全局 Logger：入口处用 NewLogger 构建，并交给 slog.SetDefault，
// 之后包级 slog.Info / slog.Error 与显式注入的 *slog.Logger 使用同一后端
// （项目内 biz/service 由 Wire 注入）。
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gin-template/internal/conf"

	"github.com/fatih/color"
	"github.com/shengyanli1982/law"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var ColorResetStr = "\x1b[0m"
var LenColorResetStr = len(ColorResetStr)

var (
	Red    = color.New(color.FgHiRed).SprintFunc()
	Blue   = color.New(color.FgHiBlue).SprintFunc()
	Yellow = color.New(color.FgHiYellow).SprintFunc()
	Green  = color.New(color.FgHiGreen).SprintFunc()
)

var (
	// writerMu 保护异步文件写入器列表（供 Close 统一 flush）
	writerMu     sync.Mutex
	asyncWriters []*law.WriteAsyncer
)

// NewLogger 构建以 zap 为后端（zapslog 桥接）的 slog.Logger。
//
// 日志级别由 log.level 决定，并交给 zapcore 的 LevelEnabler 过滤；
// 控制台与文件使用不同的编码器，文件始终输出无语义色彩的纯文本/JSON。
func NewLogger(cfg *conf.Bootstrap) (*slog.Logger, error) {
	mode := cfg.GetLog().GetMode()
	level := cfg.GetLog().GetLevel()
	logDir := cfg.GetLog().GetDir()
	logPath := GetLogPath(logDir)

	// 创建日志目录
	if logPath != "" {
		dir := filepath.Dir(logPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败：%w", err)
		}
	}

	// 解析日志级别
	zLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	// 编码器配置（带颜色，仅用于控制台）
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:          "T",
		LevelKey:         "L",
		NameKey:          "N",
		CallerKey:        "C",
		MessageKey:       "M",
		StacktraceKey:    "S",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      customLevelColorEncoder,
		EncodeTime:       customTimeEncoder,
		EncodeDuration:   zapcore.StringDurationEncoder,
		EncodeCaller:     zapcore.ShortCallerEncoder,
		ConsoleSeparator: " ",
	}

	// 无颜色的编码器配置（用于文件，避免 ANSI 转义污染日志文件）
	plainEncoderConfig := zapcore.EncoderConfig{
		TimeKey:          "T",
		LevelKey:         "L",
		NameKey:          "N",
		CallerKey:        "C",
		MessageKey:       "M",
		StacktraceKey:    "S",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      zapcore.CapitalLevelEncoder,
		EncodeTime:       zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration:   zapcore.StringDurationEncoder,
		EncodeCaller:     zapcore.ShortCallerEncoder,
		ConsoleSeparator: " ",
	}

	isDev := mode == "dev"

	var consoleEncoder zapcore.Encoder
	var fileEncoder zapcore.Encoder

	if isDev {
		consoleEncoder = &CustomEncoder{zapcore.NewConsoleEncoder(encoderConfig)}
		color.NoColor = false
		fileEncoder = zapcore.NewConsoleEncoder(plainEncoderConfig)
	} else {
		consoleEncoder = &CustomEncoder{zapcore.NewJSONEncoder(encoderConfig)}
		fileEncoder = zapcore.NewJSONEncoder(plainEncoderConfig)
	}

	consoleWriterSyncer := zapcore.AddSync(color.Output)

	var fileWriteSyncer zapcore.WriteSyncer
	if logPath != "" {
		fileWriter := NewFileWriter(logPath)
		fileAsyncWriter := NewAsyncWriter(fileWriter)
		writerMu.Lock()
		asyncWriters = append(asyncWriters, fileAsyncWriter)
		writerMu.Unlock()
		fileWriteSyncer = zapcore.AddSync(fileAsyncWriter)
	}

	var cores []zapcore.Core
	cores = append(cores, zapcore.NewCore(consoleEncoder, consoleWriterSyncer, zLevel))

	if logPath != "" && fileWriteSyncer != nil {
		cores = append(cores, zapcore.NewCore(fileEncoder, fileWriteSyncer, zLevel))
	}

	core := zapcore.NewTee(cores...)

	// slog -> zap 桥接：caller 取 slog 调用点，Error 及以上附带堆栈
	handler := zapslog.NewHandler(core,
		zapslog.WithCaller(true),
		zapslog.AddStacktraceAt(slog.LevelError),
	)
	return slog.New(handler), nil
}

// CustomEncoder 为 Warn/Error 级别的整行着色的编码器包装。
type CustomEncoder struct {
	zapcore.Encoder
}

func (c *CustomEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	buf, err := c.Encoder.EncodeEntry(entry, fields)
	if err != nil {
		return buf, err
	}

	var colorize func(a ...any) string
	switch entry.Level {
	case zapcore.WarnLevel:
		colorize = Yellow
	case zapcore.ErrorLevel:
		colorize = Red
	default:
		return buf, nil
	}

	// 颜色关闭时编码结果中不含 ANSI 复位标记，此时保持原样
	idx := strings.LastIndex(buf.String(), ColorResetStr)
	if idx < 0 {
		return buf, nil
	}

	head := buf.String()[:idx+LenColorResetStr]
	tail := buf.String()[idx+LenColorResetStr:]
	buf.Reset()
	buf.WriteString(head + colorize(tail))
	return buf, nil
}

func customLevelColorEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	var colorize func(a ...any) string
	switch level {
	case zapcore.DebugLevel:
		colorize = Blue
	case zapcore.InfoLevel:
		colorize = Green
	case zapcore.WarnLevel:
		colorize = Yellow
	case zapcore.ErrorLevel:
		colorize = Red
	default:
		colorize = color.New(color.FgHiWhite).SprintFunc()
	}
	enc.AppendString(colorize(level.CapitalString()))
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(color.New(color.FgCyan).SprintFunc()(t.Format("2006-01-02 15:04:05.000")))
}

// Close 停止异步文件写入器并 flush 缓冲，进程退出前调用一次即可。
//
// 异步写入器依赖空闲超时（默认 5s）或缓冲区写满才会落盘，不调用 Close
// 会丢失进程退出前尚未刷新的日志。
func Close() error {
	writerMu.Lock()
	writers := asyncWriters
	asyncWriters = nil
	writerMu.Unlock()

	for _, w := range writers {
		w.Stop()
	}
	return nil
}

// Fatal 记录一条 Error 级日志，flush 日志缓冲后以状态码 1 退出进程。
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

	_ = Close()
	os.Exit(1)
}

func GetLogPath(logDir string) string {
	if logDir == "" {
		return ""
	}
	p := logDir + "/" + time.Now().Format("2006-01-02") + "/" + time.Now().Format("2006-01-02-15-04-05") + ".log"
	return p
}

func NewFileWriter(logPath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename: logPath,
		MaxSize:  10, // megabytes
	}
}

func NewAsyncWriter(w io.Writer) *law.WriteAsyncer {
	lawConf := law.NewConfig()
	lawConf.WithBufferSize(1024 * 1024 * 2)
	return law.NewWriteAsyncer(w, lawConf)
}
