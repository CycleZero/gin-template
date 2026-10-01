package internal

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// HTTP 服务的超时预算。http.Server 的零值意味着"永不超时"：
// 一个慢客户端就能长期占住连接与 goroutine，进而拖垮整个服务，因此必须显式设置。
const (
	readHeaderTimeout = 10 * time.Second  // 只等请求头，防御 Slowloris
	readTimeout       = 30 * time.Second  // 读取整个请求体（含 body）的上限
	writeTimeout      = 60 * time.Second  // 从读请求到写完响应的上限（含 handler 处理时间）
	idleTimeout       = 120 * time.Second // keep-alive 连接空闲上限
)

// Server 封装 http.Server 的启动与优雅停机。
//
// 单独成类型是为了可测：优雅停机的关键行为（停止接收新连接、排空在途请求、
// 超时后强制关闭）可以脱离 Wire 装配独立验证，见 server_test.go。
type Server struct {
	name string
	srv  *http.Server
	ln   net.Listener
}

// NewServer 按统一超时预算构建 HTTP 服务。
func NewServer(name, addr string, handler http.Handler) *Server {
	return &Server{
		name: name,
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
	}
}

// Name 返回服务名（用于日志区分 main / pprof）。
func (s *Server) Name() string { return s.name }

// Addr 返回监听地址；调用 Listen 之后是实际地址（addr 传 ":0" 时由系统分配）。
func (s *Server) Addr() string { return s.srv.Addr }

// Listen 显式监听端口并记录实际地址。
//
// addr 为 ":0" 时由系统分配端口，测试需要先拿到真实端口再发请求，因此与 Start 拆开。
func (s *Server) Listen() (net.Addr, error) {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.srv.Addr = ln.Addr().String()
	return ln.Addr(), nil
}

// Start 阻塞式运行，直到服务被关闭或监听失败。
//
// 优雅停机时 http.Server.Serve 返回 http.ErrServerClosed，这属于正常结束，
// 统一归一化为 nil，避免调用方把它当异常处理。
func (s *Server) Start() error {
	if s.ln == nil {
		if _, err := s.Listen(); err != nil {
			return err
		}
	}
	if err := s.srv.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅停机：立刻停止接收新连接，等待在途请求处理完成；
// 若 ctx 超时仍未排空，http.Server 会强制关闭剩余连接并返回 ctx 的错误。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
