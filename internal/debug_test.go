package internal

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gin-template/internal/conf"
)

func pprofConfig(enable bool, host string, port uint32) *conf.Bootstrap {
	return &conf.Bootstrap{
		Server: &conf.Server{
			Http: &conf.HTTP{
				Pprof: &conf.Pprof{Enable: enable, Host: host, Port: port},
			},
		},
	}
}

// TestNewDebugServerDisabled 未启用、或启用但没配端口时都不启动调试服务。
//
// 关键安全语义：端口未配置时**跳过**，而不是把 pprof 退化挂到业务端口上。
func TestNewDebugServerDisabled(t *testing.T) {
	if srv := newDebugServer(pprofConfig(false, "127.0.0.1", 6060)); srv != nil {
		t.Error("pprof.enable=false 时不应创建调试服务")
	}
	if srv := newDebugServer(pprofConfig(true, "127.0.0.1", 0)); srv != nil {
		t.Error("pprof.port 未配置时不应创建调试服务（不会挂到业务端口）")
	}
}

// TestNewDebugServerEnabled 启用且配置端口时，监听配置的 host:port。
func TestNewDebugServerEnabled(t *testing.T) {
	srv := newDebugServer(pprofConfig(true, "127.0.0.1", 6060))
	if srv == nil {
		t.Fatal("pprof 启用且配置了端口时应创建调试服务")
	}
	if srv.Name() != "pprof" {
		t.Errorf("服务名 = %q, want pprof", srv.Name())
	}
	if srv.Addr() != "127.0.0.1:6060" {
		t.Errorf("监听地址 = %q, want 127.0.0.1:6060", srv.Addr())
	}
}

// TestDebugServerServesPprof 真正把 pprof 跑起来并访问 /debug/pprof/（随机端口，避免占用 6060）。
func TestDebugServerServesPprof(t *testing.T) {
	srv := NewServer("pprof", "127.0.0.1:0", pprofHandler())
	if _, err := srv.Listen(); err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	go func() {
		if err := srv.Start(); err != nil {
			t.Errorf("pprof 服务异常退出：%v", err)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Shutdown(t.Context())
	})

	client := &http.Client{Timeout: 5 * time.Second}

	// 索引页应列出各类 profile（goroutine 等）
	resp, err := client.Get("http://" + srv.Addr() + "/debug/pprof/")
	if err != nil {
		t.Fatalf("访问 pprof 索引失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "goroutine") {
		t.Error("pprof 索引页未包含 goroutine profile，路由可能未正确注册")
	}

	// cmdline 端点（非 profile 子路径，需显式注册）也应可用
	cmdline, err := client.Get("http://" + srv.Addr() + "/debug/pprof/cmdline")
	if err != nil {
		t.Fatalf("访问 /debug/pprof/cmdline 失败：%v", err)
	}
	defer func() { _ = cmdline.Body.Close() }()
	if cmdline.StatusCode != http.StatusOK {
		t.Errorf("/debug/pprof/cmdline 状态码 = %d, want 200", cmdline.StatusCode)
	}
}
