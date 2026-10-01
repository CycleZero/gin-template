# Gin Template

基于 Gin 框架的 Go 后端项目通用起步模板，提取自生产级项目的最佳实践。

## 技术栈

| 技术 | 说明 |
|------|------|
| [Gin](https://github.com/gin-gonic/gin) | HTTP Web 框架 |
| [Wire](https://github.com/google/wire) | 依赖注入代码生成 |
| [buf](https://buf.build) + [protobuf](https://protobuf.dev) | 配置结构定义与代码生成 |
| [OpenTelemetry](https://opentelemetry.io) | 可观测性：trace / metrics / logs 三路 OTLP 主动推送 |
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
│   ├── log/                    # 日志模块（slog 门面 + zap 后端）
│   │   ├── logger.go           # 装配控制台/文件/OTLP 多路 sink + 链路字段注入
│   │   ├── encoder.go          # 控制台彩色 / 文件无颜色编码器
│   │   ├── writer.go           # 写入器（law 异步 / channel 异步 / 直写）
│   │   ├── file.go             # 日志路径与按大小轮转
│   │   ├── otel.go             # OTLP 日志 core（与 trace 关联）
│   │   └── print.go            # Sprint 格式化辅助
│   ├── otelx/                  # OTel 公共件
│   │   ├── endpoint.go         # OTLP 端点规范化（三信号共用）
│   │   └── resource.go         # ServiceInfo → Resource（三信号统一身份）
│   ├── trace/                  # trace 接入
│   │   ├── tracer.go           # OTLP 主动推送 + 采样 + W3C 传播器
│   │   ├── gin.go              # Gin HTTP 埋点（server span + http.server.* 指标）
│   │   └── redis.go            # go-redis Hook（命令级 span + 耗时指标）
│   ├── metrics/                # metrics 接入
│   │   └── metrics.go          # OTLP 周期推送 + Go runtime 指标
│   └── infra/                  # 基础设施层
│       ├── provider.go         # Wire ProviderSet
│       └── data.go             # MySQL + Redis 初始化
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
│   │   └── demo/               # 示例业务模块（按层分目录）
│   │       ├── provider.go     # 模块 Wire Set（按层聚合）
│   │       ├── service/        # HTTP 层：service.go + dto.go + provider.go
│   │       ├── biz/            # 业务层：领域模型 demo.go + biz.go + provider.go
│   │       └── data/           # 数据层：模型 demo.go + repo.go + provider.go
│   └── router/                 # 路由层
│       ├── provider.go         # 中间件注册
│       ├── root.go             # 路由注册
│       └── middleware/         # 中间件
│           ├── cors.go         # 跨域处理
│           ├── auth.go         # JWT 认证
│           └── metadata.go     # 请求元数据
```

## 分层架构

每个业务模块按层分目录，依赖方向**单向向下**（不会出现循环导入）：

```
HTTP 请求 → <svc>/service/ (HTTP 层) → <svc>/biz/ (业务层) → <svc>/data/ (数据层) → DB
```

```
internal/domain/<svc>/
├── provider.go     # 模块 Wire Set：聚合下面三层的 ProviderSet
├── service/        # HTTP 层（DDD interface 层）
│   ├── service.go  # 请求解析、参数校验、领域错误 → HTTP 状态码
│   ├── dto.go      # DTO 定义 + biz 领域模型 ↔ DTO 转换
│   └── provider.go # service.ProviderSet
├── biz/            # 业务层（DDD domain 层）
│   ├── demo.go     # 领域模型 + 仓储接口（依赖倒置）+ 领域错误
│   ├── biz.go      # 业务规则与流程编排（依赖仓储接口，不依赖 data）
│   └── provider.go # biz.ProviderSet
└── data/           # 数据层（DDD infrastructure 层）
    ├── demo.go     # 数据库模型（GORM PO）
    ├── repo.go     # 实现 biz 的仓储接口 + PO ↔ 领域模型转换
    └── provider.go # data.ProviderSet
```

各层职责与依赖（严格 DDD + 依赖倒置）：

| 层 | 定义什么 | 负责的转换 | 允许依赖 |
|---|---|---|---|
| `service/` | DTO | biz 领域模型 ↔ DTO | `biz` |
| `biz/` | **领域模型** + **仓储接口** + 领域错误 | — | 标准库（仅依赖仓储接口） |
| `data/` | 数据库模型（PO） | PO ↔ biz 领域模型 | `biz`、`pkg/infra` |

- 依赖方向是 `service → biz ← data`：**biz 不 import data**（仓储接口定义在 biz，由 data 实现），
  所以没有循环依赖，且 biz 可注入假仓储做单元测试。
- 领域错误（如 `biz.ErrDemoNotFound`）由 data 层在未命中时返回，service 层用 `errors.Is`
  映射为 404 —— 上层不依赖 `gorm.ErrRecordNotFound` 这类存储细节。
- `context.Context` 从 `c.Request.Context()` 一路向下传递，链路追踪（OTel）与事务
  （[pkg/tx](pkg/tx/tx.go)）都能沿 ctx 传播。

## 日志

业务代码统一使用标准库 `log/slog`，后端由 zap 驱动（经
[`zapslog`](https://pkg.go.dev/go.uber.org/zap/exp/zapslog) 桥接）；日志同时写到三路 sink：

1. **控制台**（始终）：dev 彩色文本、prod JSON（WARN/ERROR 补色）
2. **文件**（配置了 `log.dir` 时）：无颜色文本/JSON，按大小轮转
3. **OTLP**（配置了 `otel.endpoint` 时）：主动推送并与 trace 关联

```go
slog.Info("创建成功", "id", id)            // 包级函数（main 已 slog.SetDefault）
s.logger.Error("创建失败", "error", err)    // 或注入的 *slog.Logger
```

- `log.New(log.Config{...})` 在入口构造成 `*log.Logger`（内嵌 `*slog.Logger`）；
  `main` 调用一次 `slog.SetDefault(logger.Logger)`，包级调用与注入的 logger 共用同一后端。
- 本包**不保存全局 Logger，也不读取全局配置**：需要显式注入时使用 `*slog.Logger`
  （`biz.go` / `service.go` 由 Wire 注入），测试时可直接传自定义 logger。
- **链路关联**：`logger.Ctx(ctx)` 或 `log.WithContext(injectedLogger, ctx)` 会注入
  `trace.id` / `span.id` 字段——OTLP sink 在写入时把这些字段还原成 SpanContext，
  后端即可从一条日志一键跳转到对应 trace。
- 初始化阶段的致命错误使用 `log.Fatal(...)`：它会 flush 各 sink 后再 `os.Exit(1)`
  （`os.Exit` 不执行 defer，否则会丢掉缓冲日志）。
- 进程退出前 `main` 会执行 `logger.Close(ctx)`：异步文件写入器（law 默认 5s 空闲才落盘）
  与 OTLP BatchProcessor 都带缓冲，不关闭会丢失最后一段日志。

`config.yaml` 的 `log` 段：

```yaml
log:
  mode: dev        # dev = 控制台彩色文本；prod = JSON（文件始终无颜色）
  level: debug     # 低于该级别的日志被 zapcore 直接丢弃
  dir: ./data/log  # <dir>/<日期>/<服务名>/<时间戳>.log，256MB 轮转、保留 7 天
```

`caller` 指向实际调用点（`zapslog.WithCaller`），`Error` 及以上级别自动附带堆栈。

## 可观测性（OpenTelemetry）

trace / metrics / logs 三路**共用一个 OTLP/HTTP 端点**主动推送，仅 exporter 默认路径不同
（`/v1/traces`、`/v1/metrics`、`/v1/logs`）；`otel.endpoint` 留空表示全部关闭（默认，零开销）。

```yaml
otel:
  endpoint: "http://127.0.0.1:4318"   # otel-collector / SigNoz / Jaeger / Tempo ...
  insecure: true                      # endpoint 带 http:// 时自动为 true
  service_name: "gin-template"
  service_version: "v0.0.0"           # 建议用 -ldflags 注入真实版本
  environment: "production"           # 写入 deployment.environment
  trace_sample_rate: 1.0              # 0 视为 1.0；ParentBased 保证链路完整
  metrics_interval_seconds: 15
  # metrics_endpoint / logs_endpoint：把某一路单独指向别的后端
  # headers：ingestion key 等额外请求头（建议来自环境变量）
```

| 信号 | 实现 | 出口 |
|---|---|---|
| traces | [pkg/trace/tracer.go](pkg/trace/tracer.go)：`trace.Init` + W3C TraceContext/Baggage 传播器 | OTLP/HTTP（BatchSpanProcessor） |
| metrics | [pkg/metrics/metrics.go](pkg/metrics/metrics.go)：`metrics.Init` + Go runtime 指标 | OTLP/HTTP（PeriodicReader，默认 15s） |
| logs | [pkg/log/otel.go](pkg/log/otel.go)：zap core → OTel Logs SDK | OTLP/HTTP（BatchProcessor，与 trace 关联） |

三路共享同一个 `ServiceInfo` → Resource（[pkg/otelx](pkg/otelx/resource.go)），
保证 `service.name` / `service.version` / `service.instance.id` /
`deployment.environment` 完全一致，不会出现"日志能按版本过滤、指标不能"的漂移。

**埋点覆盖**（本模板自带技术栈）：

- **HTTP**：[pkg/trace/gin.go](pkg/trace/gin.go) 的 `trace.Middleware(serviceName)` 挂在 Gin 上，
  产生 server span 并记录 `http.server.*` 指标（仅在配置了 OTLP 端点时挂载）。
- **Redis**：[pkg/infra/data.go](pkg/infra/data.go) 构造客户端时挂 `trace.HookRedis`，
  产生命令级 span 与 `redis_client_operation_duration_ms` 指标；只记录命令名与结果，
  不记录 key / 参数（既避免 PII 泄漏，也避免指标标签基数爆炸）。
- **Go runtime**：`metrics.Init` 内启动，goroutine / 内存 / GC 随周期上报。

**生命周期**：[cmd/main/main.go](cmd/main/main.go) 按 `logger → trace → metrics → Wire` 顺序
初始化，退出时**逆序** flush（trace → metrics → 日志 → 配置），每步 5s 超时预算。
`Init` 在端点为空时返回 `nil` 表示未启用，`Shutdown(nil)` 为空操作，可安全重复调用。

> 注意：`log.Fatal` 路径（例如启动时连不上数据库）会 flush 日志后立即 `os.Exit(1)`，
> 不会 flush trace/metrics——此时通常还没有值得上报的 span/指标。运行期需要保证上报的
> 场景，请把错误返回给 `main`，走正常退出路径。

**验证**：三路都有端到端测试，用进程内 `httptest` 充当 OTLP 接收端（不依赖任何外部后端）：

```bash
go test ./pkg/log/ ./pkg/trace/ ./pkg/metrics/ ./pkg/otelx/
```

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

本包**不保存全局配置**：`conf.Load` 读取并校验后返回 `*conf.Bootstrap` 与来源句柄，
由入口显式注入到各构造函数（项目内通过 Wire 完成），测试时可直接构造替换：

```go
cfg, src, err := conf.Load(*confPath) // src 用于 Close / Watch
if err != nil {
    fmt.Fprintln(os.Stderr, "启动失败:", err)
    os.Exit(1)
}
defer src.Close()

logger, err := log.NewLogger(cfg)
slog.SetDefault(logger)
app := initApp(cfg, logger) // cfg 一路随构造函数显式传递
```

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
再用 `Load` 返回的 `*conf.Source` 注册热更新回调（`src.Watch(key, observer)`），
业务代码无需改动。`src.Close()` 必须在进程退出前调用以释放 watcher
（[cmd/main/main.go](cmd/main/main.go) 已通过 defer 处理）。

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

以新增 `user` 模块为例（按层分目录，可直接复制 `demo/` 的骨架）：

1. 创建目录与文件：
   - `internal/domain/user/service/{service.go,dto.go,provider.go}`：DTO + biz 模型 ↔ DTO 转换
   - `internal/domain/user/biz/{user.go,biz.go,provider.go}`：**领域模型** `User` + **仓储接口** `UserRepo` + 业务逻辑
   - `internal/domain/user/data/{user.go,repo.go,provider.go}`：**PO**（GORM 模型）+ 仓储实现 + PO ↔ 领域模型转换
   - `internal/domain/user/provider.go`：聚合三层 ProviderSet
2. 依赖方向 `service → biz ← data`：**biz 不 import data**（仓储接口定义在 biz），
   `data.NewUserRepo` 直接返回 `biz.UserRepo`，Wire 无需 `wire.Bind`
3. 在 `internal/domain/hub.go` 的 `ServiceHub` 中添加新的 Service 字段
4. 在 `internal/domain/provider.go` 中引入 `user.ProviderSet`
5. 在 `internal/router/root.go` 中注册新路由
6. 运行 `make wire` 重新生成依赖注入代码

## 许可证

MIT
