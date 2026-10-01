package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gin-template/conf"

	"github.com/fatih/color"
	"github.com/shengyanli1982/law"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
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

type Logger struct {
	*zap.Logger
}

var globalLogger *Logger

func NewLogger(vc *viper.Viper) (*Logger, error) {
	mode := vc.GetString("log.mode")
	level := vc.GetString("log.level")
	logDir := vc.GetString("log.dir")
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

	// 编码器配置（带颜色）
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

	// 无颜色的编码器配置（用于文件）
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
		fileEncoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	consoleWriter := color.Output
	consoleWriterSyncer := zapcore.AddSync(consoleWriter)

	var fileWriteSyncer zapcore.WriteSyncer
	if logPath != "" {
		fileWriter := NewFileWriter(logPath)
		fileAsyncWriter := NewAsyncWriter(fileWriter)
		fileWriteSyncer = zapcore.AddSync(fileAsyncWriter)
	}

	var cores []zapcore.Core
	cores = append(cores, zapcore.NewCore(consoleEncoder, consoleWriterSyncer, zLevel))

	if logPath != "" && fileWriteSyncer != nil {
		cores = append(cores, zapcore.NewCore(fileEncoder, fileWriteSyncer, zLevel))
	}

	core := zapcore.NewTee(cores...)
	return &Logger{zap.New(core, zap.WithCaller(true))}, nil
}

type CustomEncoder struct {
	zapcore.Encoder
}

func (c *CustomEncoder) EncodeEntry(entry zapcore.Entry, fields []zap.Field) (*buffer.Buffer, error) {
	buf, err := c.Encoder.EncodeEntry(entry, fields)
	if err != nil {
		return buf, err
	}

	switch entry.Level {
	case zapcore.WarnLevel:
		e := buf.String()[:strings.LastIndex(buf.String(), ColorResetStr)+LenColorResetStr]
		t := buf.String()[strings.LastIndex(buf.String(), ColorResetStr)+LenColorResetStr:]
		buf.Reset()
		buf.WriteString(e + Yellow(t))
	case zapcore.ErrorLevel:
		e := buf.String()[:strings.LastIndex(buf.String(), ColorResetStr)+LenColorResetStr]
		t := buf.String()[strings.LastIndex(buf.String(), ColorResetStr)+LenColorResetStr:]
		buf.Reset()
		buf.WriteString(e + Red(t))
	}
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

func GetLogger() *Logger {
	if globalLogger == nil {
		var err error
		globalLogger, err = NewLogger(conf.GetConfig())
		if err != nil {
			fmt.Println("致命错误: 创建logger失败，触发panic", err)
			panic("致命错误: 创建logger失败, 触发panic" + err.Error())
		}
	}
	return globalLogger
}

func SugaredLogger() *zap.SugaredLogger {
	return GetLogger().Sugar()
}

func SetGlobalLogger(l *Logger) {
	globalLogger = l
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
	conf := law.NewConfig()
	conf.WithBufferSize(1024 * 1024 * 2)
	return law.NewWriteAsyncer(w, conf)
}
