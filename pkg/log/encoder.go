package log

import (
	"strings"
	"time"

	"github.com/fatih/color"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// 本文件集中存放日志编码器：控制台/文件两种配置、生产 JSON 补色包装、级别与时间着色函数。

var ColorResetStr = "\x1b[0m"

var LenColorResetStr = len(ColorResetStr)

var (
	Red    = color.New(color.FgHiRed).SprintFunc()
	Blue   = color.New(color.FgHiBlue).SprintFunc()
	Yellow = color.New(color.FgHiYellow).SprintFunc()
	Green  = color.New(color.FgHiGreen).SprintFunc()
)

// buildEncoders 按运行模式构建控制台与文件编码器。
//
// Dev：控制台与文件都用控制台编码器（人类可读优先），控制台额外上色；
// Prod：两者都用 JSON（可被采集器解析），控制台再包一层 CustomEncoder
// 给 WARN/ERROR 上色，便于肉眼定位。
//
// 文件编码器始终使用无颜色的 plainEncoderConfig——ANSI 转义会污染日志文件、
// 破坏采集器解析。
func buildEncoders(mode int) (consoleEncoder, fileEncoder zapcore.Encoder) {
	if mode == ModeDev {
		consoleEncoder = zapcore.NewConsoleEncoder(colorEncoderConfig())
		color.NoColor = false
		return consoleEncoder, zapcore.NewConsoleEncoder(plainEncoderConfig())
	}
	return &CustomEncoder{zapcore.NewJSONEncoder(plainEncoderConfig())},
		zapcore.NewJSONEncoder(plainEncoderConfig())
}

// colorEncoderConfig 控制台（Dev）编码器配置：短键名 + 彩色级别与时间。
//
// 刻意不设置 CallerKey——Dev 下每行都带调用点过于嘈杂；需要调用点时看文件日志
// （文件使用 plainEncoderConfig，含 C 键）。
func colorEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:          "T",
		LevelKey:         "L",
		NameKey:          "N",
		MessageKey:       "M",
		StacktraceKey:    "S",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      customLevelColorEncoder,
		EncodeTime:       customTimeEncoder,
		EncodeDuration:   zapcore.StringDurationEncoder,
		EncodeCaller:     zapcore.ShortCallerEncoder,
		ConsoleSeparator: " ",
	}
}

// plainEncoderConfig 无颜色编码器配置：用于文件写入与生产 JSON 输出。
func plainEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
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
}

// CustomEncoder 给生产 JSON 输出补色：WARN 黄、ERROR 红。
//
// 实现方式是在已编码内容之后追加 ANSI 序列，因此**只应挂在控制台**——
// 挂到文件会让日志里出现转义字符。
type CustomEncoder struct {
	zapcore.Encoder
}

func (c *CustomEncoder) EncodeEntry(entry zapcore.Entry, fields []zap.Field) (*buffer.Buffer, error) {
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

	// 颜色关闭时编码结果中不含 ANSI 复位标记，此时保持原样（否则会把首段字节搬错位置）。
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

// customLevelColorEncoder 按级别着色输出级别名。
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

// customTimeEncoder 时间编码器（青色）。
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(color.New(color.FgCyan).SprintFunc()(t.Format("2006-01-02 15:04:05.000")))
}
