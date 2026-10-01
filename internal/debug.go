package internal

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"

	"gin-template/internal/conf"
)

// pprofRoutes pprof 的路由表，直接用标准库 net/http/pprof
// （替代 gin-contrib/pprof：调试服务是独立的 net/http Server，不需要 gin）。
var pprofRoutes = map[string]http.HandlerFunc{
	"/debug/pprof/":        pprof.Index,   // 索引；同时按前缀匹配各 profile
	"/debug/pprof/cmdline": pprof.Cmdline, // 进程启动参数（可能含敏感值）
	"/debug/pprof/profile": pprof.Profile, // CPU profile
	"/debug/pprof/symbol":  pprof.Symbol,  //
	"/debug/pprof/trace":   pprof.Trace,   // 执行 trace
}

// pprofHandler 构建 pprof 的 HTTP 处理器（标准库 net/http/pprof）。
//
// 抽成独立函数便于测试：可脱离配置直接挂到随机端口上验证 /debug/pprof/ 可访问。
func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	for path, handler := range pprofRoutes {
		mux.HandleFunc(path, handler)
	}
	return mux
}

// newDebugServer 按配置构建独立的 pprof 调试服务；未启用时返回 nil。
//
// 安全约定：pprof **只监听独立端口，绝不挂到业务端口**。
// 它能读取进程内存（含堆上的令牌/密钥），暴露在业务端口等同于开放信息泄露面。
//
//   - pprof.enable=false         → 不启用
//   - pprof.enable=true, port=0  → 视为端口未配置，跳过并告警（而不是退化到业务端口）
//   - pprof.enable=true, port>0  → 监听 pprof.host:pprof.port
//
// 生产建议：host 配 127.0.0.1（或仅内网可达地址），通过跳板机/端口转发访问。
func newDebugServer(cfg *conf.Bootstrap) *Server {
	pprofCfg := cfg.GetServer().GetHttp().GetPprof()
	if !pprofCfg.GetEnable() {
		return nil
	}
	if pprofCfg.GetPort() <= 0 {
		slog.Warn("pprof 已启用但未配置端口（server.http.pprof.port），已跳过；" +
			"出于安全考虑不会挂到业务端口")
		return nil
	}

	mux := http.NewServeMux()
	for path, handler := range pprofRoutes {
		mux.HandleFunc(path, handler)
	}

	addr := net.JoinHostPort(pprofCfg.GetHost(), strconv.Itoa(int(pprofCfg.GetPort())))
	slog.Info("pprof 调试服务已启用", "addr", addr)

	return NewServer("pprof", addr, mux)
}
