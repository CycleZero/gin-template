package internal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestServerGracefulShutdownDrainsInflight 验证优雅停机的核心语义：
// 已经在处理中的请求会被**排空**（不掐断），停机后不再接受新连接。
func TestServerGracefulShutdownDrainsInflight(t *testing.T) {
	inFlight := make(chan struct{})
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(inFlight)
		<-release // 模拟慢请求：直到测试放行才响应
		_, _ = io.WriteString(w, "done")
	})

	srv := NewServer("test", "127.0.0.1:0", handler)
	if _, err := srv.Listen(); err != nil {
		t.Fatalf("监听失败：%v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Start() }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr() + "/")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		got <- result{body: string(body), err: err}
	}()

	// 等请求真正进入 handler，确保它是"在途请求"
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("请求未在 5s 内进入 handler")
	}

	// 在途请求存在时发起优雅停机（不释放 handler，先看它是否会掐断）
	shutdownErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr <- srv.Shutdown(ctx)
	}()

	// 放行在途请求：它应当正常拿到响应，而不是被停机中断
	close(release)

	select {
	case res := <-got:
		if res.err != nil {
			t.Errorf("在途请求被中断：%v", res.err)
		}
		if res.body != "done" {
			t.Errorf("在途请求响应体 = %q, want done", res.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("优雅停机后，在途请求仍未返回")
	}

	if err := <-shutdownErr; err != nil {
		t.Errorf("Shutdown 返回错误：%v", err)
	}
	if err := <-serveErr; err != nil {
		t.Errorf("Start 应把优雅停机视为正常结束，实际返回：%v", err)
	}

	// 停机后不再接受新请求
	if _, err := http.Get("http://" + srv.Addr() + "/"); err == nil {
		t.Error("停机后仍能建立新连接，监听器未关闭")
	}
}

// TestServerStartListenError 监听失败应返回错误（端口被占用/地址非法）。
func TestServerStartListenError(t *testing.T) {
	srv := NewServer("bad", "127.0.0.1:-1", http.NotFoundHandler())
	if err := srv.Start(); err == nil {
		t.Fatal("非法地址应返回错误")
	} else if !strings.Contains(err.Error(), "invalid port") && !strings.Contains(err.Error(), "too many") {
		// 不同平台错误文案不同，只要确保不是 nil 即可
		t.Logf("监听错误（非致命断言）：%v", err)
	}
}
