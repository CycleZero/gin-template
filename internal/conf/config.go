// Package conf 基于 Kratos v3 的 config 组件加载配置。
//
// 配置结构由 internal/conf/conf.proto 定义，经 buf 生成为 conf.pb.go
// （make config），本文件只放加载流程与手写方法（DSN/Addr/Validate）。
//
// 加载顺序即优先级（后加载的 source 覆盖先加载的）：
//
//	file source：默认值，来自版本库中的 config.yaml
//	env  source：部署覆盖，环境变量前缀 APP_，去掉前缀后的名字即 key
//
// 配置文件中的 ${KEY:default} 占位符会从「合并后的配置」（含环境变量）解析，
// 因此嵌套配置的覆盖写法是：
//
//	data:
//	  db:
//	    host: ${DB_HOST:localhost}   # APP_DB_HOST=127.0.0.1 即可覆盖
//
// 需要接配置中心时，只需在 config.WithSource 里追加对应 source
// （如 github.com/go-kratos/kratos/contrib/config/etcd/v3），
// 再用 Watch 注册热更新回调，调用方代码无需改动。
package conf

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
)

const (
	// DefaultPath 默认配置文件路径。file source 既支持单文件，也支持目录；
	// 传目录时会加载目录下所有非隐藏文件（按扩展名识别格式）。
	DefaultPath = "config.yaml"

	// EnvPrefix 环境变量前缀。APP_DB_HOST 会被映射为 key "DB_HOST"，
	// 供配置文件里的 ${DB_HOST:...} 占位符使用。
	EnvPrefix = "APP_"
)

// DSN 组装 go-sql-driver/mysql 连接串。
func (x *DB) DSN() string {
	if x == nil {
		return ""
	}
	addr := net.JoinHostPort(x.GetHost(), strconv.Itoa(int(x.GetPort())))
	return x.GetUser() + ":" + x.GetPassword() + "@tcp(" + addr + ")/" + x.GetDbName() +
		"?charset=utf8mb4&parseTime=True&loc=Local"
}

// Addr 返回 Redis 的 host:port。
func (x *Redis) Addr() string {
	if x == nil {
		return ""
	}
	return net.JoinHostPort(x.GetHost(), strconv.Itoa(int(x.GetPort())))
}

// Addr 返回 HTTP 监听地址。
func (x *HTTP) Addr() string {
	if x == nil {
		return ""
	}
	return net.JoinHostPort(x.GetHost(), strconv.Itoa(int(x.GetPort())))
}

// Validate 校验启动必需项，避免带着空地址/端口跑到运行期才报错。
//
// 全部通过 getter 访问，因此缺字段时返回明确的错误而不是空指针崩溃。
func (x *Bootstrap) Validate() error {
	if x == nil {
		return errors.New("配置校验失败: 配置为空")
	}
	if x.GetServer().GetHttp().GetPort() == 0 {
		return errors.New("配置校验失败: server.http.port 不能为 0")
	}
	if x.GetData().GetDb().GetHost() == "" || x.GetData().GetDb().GetDbName() == "" {
		return errors.New("配置校验失败: data.db.host / data.db.db_name 不能为空")
	}
	switch mode := x.GetLog().GetMode(); mode {
	case "dev", "prod":
	default:
		return fmt.Errorf("配置校验失败: log.mode 只能是 dev 或 prod，当前为 %q", mode)
	}
	return nil
}

var (
	mu     sync.Mutex // 保护 globalConfig / kratosConfig
	loadMu sync.Mutex // 串行化加载过程（load 不持有 mu，避免自死锁）

	globalConfig *Bootstrap
	kratosConfig config.Config // 保留句柄以便 Watch / Close
)

// load 真正执行加载，不读写全局状态。
func load(path string) (config.Config, *Bootstrap, error) {
	c := config.New(
		config.WithSource(
			file.NewSource(path),     // 默认值：版本库中的配置文件
			env.NewSource(EnvPrefix), // 覆盖：APP_ 前缀环境变量
		),
		// 让 ${KEY:default} 的解析结果按字面量推断类型（bool/int/float）。
		// 不开这个开关时占位符展开结果永远是字符串，无法 Scan 进 int/bool 字段。
		config.WithResolveActualTypes(true),
	)
	if err := c.Load(); err != nil {
		return nil, nil, fmt.Errorf("加载配置失败(%s): %w", path, err)
	}

	out := new(Bootstrap) // conf.proto 生成类型，Scan 走 protojson
	if err := c.Scan(out); err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("解析配置失败(%s): %w", path, err)
	}

	if err := out.Validate(); err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	return c, out, nil
}

// Load 加载配置并解码为 conf.Bootstrap（默认路径 DefaultPath）。
//
// 加载成功后会把内部 config 句柄记录为全局句柄，以便 Close 释放 watcher；
// 返回的 *Bootstrap 与全局配置相互独立。
func Load(paths ...string) (*Bootstrap, error) {
	path := DefaultPath
	if len(paths) > 0 && paths[0] != "" {
		path = paths[0]
	}

	c, out, err := load(path)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	kratosConfig = c
	mu.Unlock()
	return out, nil
}

// GetConfig 返回全局配置单例（首次调用时按 DefaultPath 加载）。
// 加载失败属于启动期致命错误，直接终止进程。
func GetConfig(paths ...string) *Bootstrap {
	mu.Lock()
	if globalConfig != nil {
		out := globalConfig
		mu.Unlock()
		return out
	}
	mu.Unlock()

	loadMu.Lock()
	defer loadMu.Unlock()

	// 双重检查：等待加载锁期间可能已被其他 goroutine 初始化
	mu.Lock()
	if globalConfig != nil {
		out := globalConfig
		mu.Unlock()
		return out
	}
	mu.Unlock()

	path := DefaultPath
	if len(paths) > 0 && paths[0] != "" {
		path = paths[0]
	}

	c, out, err := load(path)
	if err != nil {
		fmt.Println("致命错误:", err)
		panic("致命错误: " + err.Error())
	}

	mu.Lock()
	globalConfig = out
	kratosConfig = c
	mu.Unlock()
	return out
}

// SetConfig 覆盖全局配置，便于测试注入。
func SetConfig(c *Bootstrap) {
	mu.Lock()
	globalConfig = c
	mu.Unlock()
}

// Watch 为某个 key 注册热更新回调（key 为点分路径，如 data.db）。
//
// file / env source 自带 watcher，配置中心（etcd 等）接入后同样生效。
// 回调中必须自行校验，并以原子方式替换可变状态；监听地址、驱动等
// 启动期配置不应依赖热更新。
func Watch(key string, o config.Observer) error {
	mu.Lock()
	c := kratosConfig
	mu.Unlock()

	if c == nil {
		return errors.New("conf: 配置尚未加载，请先调用 Load 或 GetConfig")
	}
	return c.Watch(key, o)
}

// Close 停止所有 source 的 watcher，进程退出前调用一次即可。
func Close() error {
	mu.Lock()
	c := kratosConfig
	kratosConfig = nil
	mu.Unlock()

	if c == nil {
		return nil
	}
	return c.Close()
}
