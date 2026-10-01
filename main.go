package main

import (
	"gin-template/conf"
	"gin-template/pkg/log"
	"os"
	"os/signal"
)

func main() {
	vc := conf.GetConfig()
	logger := log.GetLogger()

	// 进程退出（含 panic）前刷新异步日志文件写入器，避免丢失缓冲日志
	defer func() {
		if err := log.Close(); err != nil {
			logger.Error("关闭日志失败", "error", err)
		}
	}()

	app := initApp(vc, logger)

	done := make(chan os.Signal, 1)
	go func() {
		defer func() {
			done <- os.Interrupt
		}()
		logger.Info("服务已启动")
		if err := app.StartServer(); err != nil {
			logger.Error("服务崩溃", "error", err)
			return
		}
	}()

	signal.Notify(done, os.Interrupt)
	<-done
	logger.Info("服务退出")
}
