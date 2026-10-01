package main

import (
	"flag"
	"os"
	"os/signal"

	"gin-template/conf"
	"gin-template/pkg/log"
)

func main() {
	confPath := flag.String("conf", conf.DefaultPath, "配置文件路径（文件或目录）")
	flag.Parse()

	cfg := conf.GetConfig(*confPath)
	logger := log.GetLogger()

	// 进程退出（含 panic）前刷新异步日志、释放配置 source watcher
	defer func() {
		if err := log.Close(); err != nil {
			logger.Error("关闭日志失败", "error", err)
		}
		if err := conf.Close(); err != nil {
			logger.Error("关闭配置失败", "error", err)
		}
	}()

	app := initApp(cfg, logger)

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
