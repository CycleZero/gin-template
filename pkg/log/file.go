package log

import (
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// GetLogPath 生成日志文件路径：<dir>/<date>/<service>/<timestamp>.log。
//
// 命名精确到秒，即"每次进程启动一个新文件"，因此 lumberjack 的按大小轮转
// 只在单进程写入量超过 MaxSize 时触发（保持单文件可读性的同时兜底极端情况）。
// logDir 为空表示只写控制台，返回空串。
func GetLogPath(logDir, serviceName string) string {
	if strings.TrimSpace(logDir) == "" {
		return ""
	}
	if serviceName == "" {
		serviceName = "app"
	}
	return logDir +
		"/" + time.Now().Format("2006-01-02") +
		"/" + serviceName +
		"/" + time.Now().Format("2006-01-02-15-04-05") + ".log"
}

// NewFileWriter 返回按大小轮转的日志文件写入器。
//
// 轮转参数：单文件最大 256MB，保留最近 5 个备份、最多 7 天，并压缩归档。
func NewFileWriter(logPath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    256, // megabytes
		MaxBackups: 5,
		MaxAge:     7, // days
		Compress:   true,
	}
}
