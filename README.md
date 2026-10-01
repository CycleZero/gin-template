# Gin Template

基于 Gin 框架的 Go 后端项目通用起步模板，提取自生产级项目的最佳实践。

## 技术栈

| 技术 | 说明 |
|------|------|
| [Gin](https://github.com/gin-gonic/gin) | HTTP Web 框架 |
| [Wire](https://github.com/google/wire) | 依赖注入代码生成 |
| [buf](https://buf.build) + [protobuf](https://protobuf.dev) | 配置结构定义与代码生成 |
| [Kratos config](https://go-kratos.dev/docs/component/config/) | 配置加载（file + env source，可扩展 etcd 等） |
| [log/slog](https://pkg.go.dev/log/slog) | 结构化日志门面（业务代码统一入口） |
| [Zap](https://github.com/uber-go/zap) | 日志后端，经 zapslog 桥接到 slog |
| [GORM](https://gorm.io) | ORM 框架 (MySQL) |
| [Redis](https://github.com/redis/go-redis) | 缓存/会话 |
| [JWT](https://github.com/golang-jwt/jwt) | 认证授权 |

## 项目结构

```
gin-template/
├── cmd/
│   └── main/                   # 程序入口（package main）
│       ├── main.go             # 启动、信号处理
│       └── wire.go / wire_gen.go  # Wire 依赖注入
├── makefile                    # 构建命令
├── buf.yaml / buf.gen.yaml     # protobuf 模块与代码生成配置
├── config.yaml.example         # 配置文件示例
│
├── pkg/                        # 可复用基础设施
│   ├── log/                    # 日志模块
│   │   └── logger.go           # slog 门面 + zap 后端（zapslog 桥接）
│   └── infra/                  # 基础设施层
│       ├── provider.go         # Wire ProviderSet
│       └── data.go             # MySQL + Redis 初始化
│
├── model/                      # 数据模型
│   └── demo.go                 # 示例模型
│
├── internal/                   # 内部模块
│   ├── app.go                  # 应用封装（Gin Engine，package internal）
│   ├── provider.go             # 内部 Wire 聚合
│   ├── conf/                   # 配置模块
│   │   ├── conf.proto          # 配置结构定义（protobuf，唯一真相源）
│   │   ├── conf.pb.go          # buf 生成（make config），入库以保证无工具链可构建
│   │   └── config.go           # Kratos config 加载（file + env source）+ DSN/Addr/Validate
│   ├── common/                 # 公共组件
│   │   └── request_meta.go     # 请求元数据
│   ├── domain/                 # 业务领域（DDD 分层）
│   │   ├── hub.go              # ServiceHub 服务聚合
│   │   ├── provider.go         # Domain Wire 聚合
│   │   └── demo/               # 示例业务模块
│   │       ├── provider.go     # 模块 Wire Set
│   │       ├── service.go      # HTTP 处理层
│   │       ├── biz.go          # 业务逻辑层
│   │       ├── repo.go         # 数据访问层
│   │       └── dto.go          # 数据传输对象
│   └── router/                 # 路由层
│       ├── provider.go         # 中间件注册
│       ├── root.go             # 路由注册
│       └── middleware/         # 中间件
│           ├── cors.go         # 跨域处理
│           ├── auth.go         # JWT 认证
│           └── metadata.go     # 请求元数据
```

## 分层架构

每个业务模块遵循三层架构：

```
HTTP 请求 → service.go (HTTP 层) → biz.go (业务逻辑层) → repo.go (数据访问层) → DB
```

- **service.go**: 处理 HTTP 请求解析、参数校验、响应格式化
- **biz.go**: 业务逻辑、数据校验、流程编排
- **repo.go**: 数据库操作封装（GORM）
- **dto.go**: 请求/响应数据结构定义

## 日志

业务代码统一使用标准库 `log/slog`，后端由 zap 驱动（经
[`zapslog`](https://pkg.go.dev/go.uber.org/zap/exp/zapslog) 桥接），
既保留 slog 的简洁 API，又复用 zap 的编码、lumberjack 切割与异步落盘能力。

```go
slog.Info("创建成功", "id", id, "user_id", uid)
slog.Error("查询失败", "error", err)
```

- `log.GetLogger()` 构建后端并注册为 slog 默认 Logger（`slog.SetDefault`），
  因此任意包内直接调用 `slog.Info` / `slog.Error` 等包级函数即可，无需层层传递。
- 需要显式注入或替换时使用 `*slog.Logger`：`biz.go` / `service.go` 由 Wire 注入，
  测试时可通过 `log.SetGlobalLogger` 或注入自定义 handler 替换。
- 初始化阶段的致命错误使用 `log.Fatal(...)`：它先 flush 再 `os.Exit(1)`
  （`os.Exit` 不会执行 defer，直接用 os.Exit 会丢掉文件日志）。
- 进程退出前 `main` 中的 `defer log.Close()` 会停止异步写入器并 flush；
  异步写入器默认 5s 空闲或写满 2MB 才落盘，不 flush 会丢失缓冲日志。

`config.yaml` 的 `log` 段控制行为：

```yaml
log:
  mode: dev        # dev = 控制台彩色文本；prod = JSON
  level: debug     # 低于该级别的日志被 zapcore 直接丢弃
  dir: ./data/log  # 按日期分目录、按时间命名，10MB 切割；留空则只输出到控制台
```

日志中的 `caller` 始终指向实际调用点（`zapslog.WithCaller`），
`Error` 及以上级别自动附带堆栈；文件输出始终为无 ANSI 转义的纯文本/JSON。

## 快速开始

### 环境要求

- Go 1.27.1+
- MySQL 8.0+
- Redis 6.0+（可选）

### 安装步骤

```bash
# 1. 克隆模板
git clone <your-repo-url> myproject
cd myproject

# 2. 修改模块名（全局替换 gin-template → your-module-name）
# 修改 go.mod 第一行

# 3. 复制配置文件
cp config.yaml.example config.yaml
# 编辑 config.yaml，修改数据库连接信息

# 4. 安装依赖
go mod tidy

# 5. 生成依赖注入代码（通过 go run 使用 go.mod 锁定的 wire 版本，无需预装 wire 二进制）
make wire

# 6. 启动服务
make run
```

服务启动后访问：
- API: `http://localhost:8000/api/demo`
- pprof: `http://localhost:6060/debug/pprof/`

## 配置说明

配置由 [Kratos v3 的 config 组件](https://go-kratos.dev/docs/component/config/) 加载，
按 source 顺序覆盖（**后面的覆盖前面的**）：

1. **file source**：`config.yaml`（默认值，随版本库管理）
2. **env source**：`APP_` 前缀环境变量（部署覆盖）

配置文件里的 `${KEY:default}` 占位符会从「合并后的配置」解析，因此嵌套项也能被
环境变量覆盖，密钥无需写进文件：

```bash
APP_DB_HOST=10.0.0.12 APP_DB_PORT=3400 APP_DB_PASSWORD=secret ./app
```

配置结构由 [internal/conf/conf.proto](internal/conf/conf.proto) 定义（protobuf 是唯一真相源），
用 `make config` 经 buf 生成 `conf.pb.go`；`config.Scan` 通过 protojson 解码
（protojson 同时接受原始字段名与 lowerCamelCase，因此 YAML 键名即字段名，无需转换），
手写的 `DSN()` / `Addr()` / `Validate()` 放在同包的 [internal/conf/config.go](internal/conf/config.go)：

```yaml
data:
  db:                  # MySQL 配置
    host: "${DB_HOST:localhost}"
    port: "${DB_PORT:3306}"
    user: "${DB_USER:root}"
    password: "${DB_PASSWORD:your_password}"
    db_name: "${DB_NAME:gin_template}"
  redis:               # Redis 配置
    host: "${REDIS_HOST:localhost}"
    port: "${REDIS_PORT:6379}"
    password: "${REDIS_PASSWORD:}"

server:
  http:
    host: "${HTTP_HOST:0.0.0.0}"
    port: "${HTTP_PORT:8000}"
    pprof:             # 性能分析
      enable: "${PPROF_ENABLE:true}"
      host: "${PPROF_HOST:0.0.0.0}"
      port: "${PPROF_PORT:6060}"

log:
  mode: "${LOG_MODE:dev}"      # dev | prod
  level: "${LOG_LEVEL:debug}"  # debug | info | warn | error
  dir: "${LOG_DIR:./data/log}"

app:
  dev_mode: "${DEV_MODE:true}"
  enable_db_debug: "${ENABLE_DB_DEBUG:true}"
```

配置文件路径可用 `-conf` 指定（支持单个文件或目录）：

```bash
go run ./cmd/main -conf /etc/myapp/config.yaml
```

**扩展配置中心**：接入 etcd 等远程配置源时，只需在 [internal/conf/config.go](internal/conf/config.go)
的 `config.WithSource(...)` 中追加对应 source（如 `contrib/config/etcd/v3`），
再用 `conf.Watch(key, observer)` 注册热更新回调，业务代码无需改动。
注意 `conf.Close()` 必须在进程退出前调用以释放 watcher（[cmd/main/main.go](cmd/main/main.go)
已通过 defer 处理）。

## API 文档

内置 Demo 模块提供 CRUD 示例：

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | /api/demo | 创建 |
| GET | /api/demo | 列表 |
| GET | /api/demo/:id | 详情 |
| PUT | /api/demo/:id | 更新 |
| DELETE | /api/demo/:id | 删除 |

## 构建命令

```bash
make config      # 生成 protobuf 代码（buf lint + buf generate，需 buf 与 protoc-gen-go）
make wire        # 生成 Wire 依赖注入代码（go run，无需预装 wire）
make build       # 编译
make rebuild     # config + wire + build
make run         # 直接运行
make tidy        # 整理依赖
make build-linux # 交叉编译 Linux
```

## 添加新业务模块

1. 在 `internal/domain/` 下创建新目录，例如 `user/`
2. 创建 `provider.go`、`service.go`、`biz.go`、`repo.go`、`dto.go`
3. 在 `internal/domain/hub.go` 的 `ServiceHub` 中添加新 Service
4. 在 `internal/domain/provider.go` 中引入新模块的 ProviderSet
5. 在 `internal/router/root.go` 中注册新路由
6. 运行 `make wire` 重新生成依赖注入代码

## 许可证

MIT
