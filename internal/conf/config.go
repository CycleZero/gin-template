// Package conf 基于 Kratos v3 的 config 组件加载配置。
//
// 配置结构由 internal/conf/conf.proto 定义，经 buf 生成为 conf.pb.go
// （make config）；本文件只放加载流程与手写方法（DSN/Addr/Validate）。
//
// 本包不保存任何全局配置：Load 读取并校验后返回 conf.Bootstrap，
// 由调用方显式注入到各构造函数（项目内通过 Wire 完成）。
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
// 注意：env source 产出的 key 是「去掉前缀后的那一层」，不含嵌套路径，
// 因此只有写成占位符的字段才能被环境变量覆盖。
//
// 需要接配置中心时，在 config.WithSource 里追加对应 source
// （如 github.com/go-kratos/kratos/contrib/config/etcd/v3），
// 再用返回的 Source.Watch 注册热更新回调。
package conf

import (
	"errors"
	"fmt"
	"net"
	"strconv"

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

// Source 是一次配置加载的来源句柄（底层 Kratos config 实例）。
//
// 进程退出前必须 Close 以停止各 source 的 watcher；需要热更新时用 Watch
// 注册回调。它不持有配置数据本身，配置数据由 Load 单独返回。
type Source struct {
	c config.Config
}

// Watch 为某个 key 注册热更新回调（key 为点分路径，如 data.db）。
//
// file / env source 自带 watcher，配置中心（etcd 等）接入后同样生效。
// 回调中必须自行校验，并以原子方式替换可变状态；监听地址、驱动等
// 启动期配置不应依赖热更新。
func (s *Source) Watch(key string, o config.Observer) error {
	if s == nil || s.c == nil {
		return errors.New("conf: 配置来源未初始化")
	}
	return s.c.Watch(key, o)
}

// Close 停止所有 source 的 watcher，进程退出前调用一次即可。
func (s *Source) Close() error {
	if s == nil || s.c == nil {
		return nil
	}
	return s.c.Close()
}

// Load 读取配置、解码为 conf.Bootstrap 并校验，返回配置数据与来源句柄。
//
// 调用方负责在退出前关闭句柄（释放 watcher）：
//
//	cfg, src, err := conf.Load(path)
//	if err != nil {
//		return err
//	}
//	defer src.Close()
func Load(paths ...string) (*Bootstrap, *Source, error) {
	path := DefaultPath
	if len(paths) > 0 && paths[0] != "" {
		path = paths[0]
	}

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
	return out, &Source{c: c}, nil
}
