package log

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/fatih/color"
	"github.com/shengyanli1982/law"
	"go.uber.org/zap/zapcore"
)

// 本文件集中存放日志写入器与 sink 装配，供 logger.go 组装 zap core 时选用。
//
// 三条写入路径：
//   - 控制台直写（默认，见 buildConsoleSyncer）
//   - bufferedWriteSyncer：bufio + 互斥锁，简单
//   - asyncWriteSyncer / law.WriteAsyncer：channel 或 MPSC 队列解耦 I/O

// UseLawAsyncWriter 文件日志异步引擎选择：
//
//	true:  law.WriteAsyncer（MPSC 队列，默认）
//	false: chan + 自实现（asyncWriteSyncer），零外部依赖
var UseLawAsyncWriter = true

// UseLawConsoleWriter 控制台写入引擎选择。
//
// 默认 false（直写 color.Output）：控制台面向开发者，不应为了缓冲而延迟输出；
// law 默认要等 5s 空闲或缓冲写满才落盘，调试时体验很差。
var UseLawConsoleWriter = false

// buildConsoleSyncer 控制台写入器。
func buildConsoleSyncer() zapcore.WriteSyncer {
	if UseLawConsoleWriter {
		return zapcore.AddSync(NewLawAsyncWriter(color.Output))
	}
	return zapcore.AddSync(color.Output)
}

// buildFileSyncer 文件写入器；未配置日志目录时返回 (nil, nil)，调用方据此跳过文件 sink。
//
// 第二个返回值是关闭函数：异步写入器带缓冲，进程退出前必须 flush，
// 否则会丢失最后一段日志（law 默认 5s 空闲才落盘）。
func buildFileSyncer(logPath string) (zapcore.WriteSyncer, func(context.Context) error) {
	if logPath == "" {
		return nil, nil
	}

	fileWriter := NewFileWriter(logPath)
	if UseLawAsyncWriter {
		async := NewLawAsyncWriter(fileWriter)
		return zapcore.AddSync(async), func(context.Context) error {
			async.Stop()
			return nil
		}
	}

	async := NewAsyncWriteSyncer(fileWriter, 256*1024)
	return zapcore.AddSync(async), func(context.Context) error {
		async.Close()
		return nil
	}
}

// bufferedWriteSyncer 缓冲写入器：包装 bufio.Writer，减少 write syscall 次数。
// 用互斥锁保证并发安全——同一个 core 可能被多个 goroutine 写入。
type bufferedWriteSyncer struct {
	w  *bufio.Writer
	mu sync.Mutex
}

// NewBufferedWriteSyncer 创建带指定缓冲大小的写入器。
func NewBufferedWriteSyncer(w io.Writer, size int) *bufferedWriteSyncer {
	return &bufferedWriteSyncer{w: bufio.NewWriterSize(w, size)}
}

func (s *bufferedWriteSyncer) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.w.Write(p)
	s.mu.Unlock()
	return n, err
}

func (s *bufferedWriteSyncer) Sync() error {
	s.mu.Lock()
	err := s.w.Flush()
	s.mu.Unlock()
	return err
}

// asyncWriteSyncer 异步写入器：用 channel 解耦日志生产与磁盘 I/O。
//
// channel 满时 Write 阻塞，以此保证日志不丢失（宁可慢也不丢）。
type asyncWriteSyncer struct {
	ch      chan []byte
	bufPool sync.Pool
	wg      sync.WaitGroup
	w       io.WriteCloser
	bufW    *bufio.Writer
	once    sync.Once
}

// NewAsyncWriteSyncer 创建异步写入器并启动后台 drain goroutine。
func NewAsyncWriteSyncer(w io.WriteCloser, bufSize int) *asyncWriteSyncer {
	s := &asyncWriteSyncer{
		ch:   make(chan []byte, 4096),
		w:    w,
		bufW: bufio.NewWriterSize(w, bufSize),
	}
	s.bufPool.New = func() any { return make([]byte, 0, 4096) }
	s.wg.Add(1)
	go s.drain()
	return s
}

// drain 消费 channel 中的日志；channel 关闭后刷出缓冲并关闭底层 writer。
func (s *asyncWriteSyncer) drain() {
	defer s.wg.Done()
	for data := range s.ch {
		_, _ = s.bufW.Write(data)
		s.bufPool.Put(data[:0])
	}
	_ = s.bufW.Flush()
	_ = s.w.Close()
}

func (s *asyncWriteSyncer) Write(p []byte) (int, error) {
	buf := s.bufPool.Get().([]byte)
	buf = append(buf[:0], p...)
	s.ch <- buf
	return len(p), nil
}

func (s *asyncWriteSyncer) Sync() error { return nil }

// Close 关闭 channel 并等待 drain 完成，确保缓冲中的日志落盘。
// 可安全重复调用。
func (s *asyncWriteSyncer) Close() {
	s.once.Do(func() {
		close(s.ch)
		s.wg.Wait()
	})
}

// NewLawAsyncWriter 基于 shengyanli1982/law 的异步写入器（10MB 缓冲）。
//
// 调用方必须在进程退出前调用其 Stop() 刷出缓冲。
func NewLawAsyncWriter(w io.Writer) *law.WriteAsyncer {
	lawConf := law.NewConfig()
	lawConf.WithBufferSize(1024 * 1024 * 10)
	return law.NewWriteAsyncer(w, lawConf)
}
