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
| [JWT](https://github.com/golang-jwt/jwt) | 认证授权（中间件工厂已就绪，默认未挂载到路由） |

## 快速开始

### 环境要求

- Go 1.27.1+
- MySQL 8.0+
- Redis 6.0+（可选）
- buf + protoc-gen-go（仅在修改 `conf.proto` 时需要；`conf.pb.go` 已入库）

### 安装步骤

```bash
# 1. 克隆模板
git clone <your-repo-url> myproject
cd myproject

# 2. 修改模块名：go.mod 第一行 + 全局替换 import 路径 gin-template → your-module-name

# 3. 复制配置文件（生成的 conf.pb.go 已入库，不改 proto 则无需生成代码）
cp config.yaml.example config.yaml
# 编辑 config.yaml，填写数据库连接信息

# 4. 安装依赖
go mod tidy

# 5. 生成依赖注入代码（go run 使用 go.mod 锁定的 wire 版本，无需预装 wire 二进制）
make wire

# 6. 启动服务
make run
```

服务启动后：

| 地址 | 说明 |
|---|---|
| `http://localhost:8000/api/demo` | Demo CRUD 示例接口 |
| `http://localhost:8000/healthz` | 存活探针（不查依赖） |
| `http://localhost:8000/readyz` | 就绪探针（探测 MySQL / Redis） |
| `http://localhost:6060/debug/pprof/` | 性能分析（需开启 `server.http.pprof.enable`） |

> 连不上数据库时进程会直接退出（`log.Fatal`），所以第 3 步的数据库配置必须先填对；
> 见[已知限制](#已知限制与后续可扩展)。

## 项目结构

```
gin-template/
├── cmd/
│   └── main/                   # 程序入口（package main）
│       ├── main.go             # 启动、信号处理（SIGINT/SIGTERM）与优雅停机
│       ├── telemetry.go        # 配置 → log/trace/metrics 的映射
│       └── wire.go / wire_gen.go  # Wire 依赖注入
├── makefile                    # 构建命令
├── buf.yaml / buf.gen.yaml     # protobuf 模块与代码生成配置
├── config.yaml.example         # 配置文件示例
├── LICENSE                     # MIT
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
│   │   ├── tracer.go           # 始终安装 Provider（无端点则 NeverSample）+ 采样 + 传播器
│   │   ├── requestid.go        # 请求 ID = TraceID 的取值来源与响应头常量
│   │   ├── gin.go              # Gin HTTP 埋点（server span + http.server.* 指标）
│   │   └── redis.go            # go-redis Hook（命令级 span + 耗时指标）
│   ├── metrics/                # metrics 接入
│   │   └── metrics.go          # OTLP 周期推送 + Go runtime 指标
│   ├── cache/                  # 缓存抽象（Redis / 内存实现，可选能力库）
│   ├── oss/                    # 对象存储抽象（MinIO / COS + trace 装饰器，可选）
│   ├── tx/                     # 事务管理（ctx 传递 *sql.Tx，biz 控制边界，可选）
│   ├── errs/                   # 统一错误体系（业务码 + HTTP 状态码 + 对外消息）
│   ├── response/               # 统一响应封装（OK / Fail(4xx) / Error(5xx) + trace_id）
│   └── infra/                  # 基础设施层
│       ├── provider.go         # Wire ProviderSet
│       └── data.go             # MySQL + Redis 初始化 + 连接池 + Health/Close
│
├── internal/                   # 内部模块
│   ├── app.go                  # 应用封装：中间件装配 + Start/Shutdown（优雅停机）
│   ├── server.go               # http.Server 封装（超时预算 + 优雅停机）
│   ├── debug.go                # pprof 调试服务（按配置，独立端口）
│   ├── provider.go             # 内部 Wire 聚合
│   ├── conf/                   # 配置模块
│   │   ├── conf.proto          # 配置结构定义（protobuf，唯一真相源）
│   │   ├── conf.pb.go          # buf 生成（make config），入库以保证无工具链可构建
│   │   └── config.go           # Kratos config 加载（file + env source）+ DSN/Addr/Validate
│   ├── common/                 # 公共组件
│   │   └── request_meta.go     # 请求元数据（含 TraceID）
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
│       ├── health.go           # /healthz 存活探针与 /readyz 就绪探针
│       └── middleware/         # 中间件
│           ├── cors.go         # 跨域处理
│           ├── auth.go         # JWT 认证
│           ├── traceid.go      # 回写响应头 X-Request-ID（值 = 链路 TraceID）
│           └── metadata.go     # 请求元数据（客户端 IP/UA/TraceID）
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
- 领域错误（如 `biz.ErrDemoNotFound`）由 data 层在未命中时返回，service 层交给
  `respondBizError` 按 4xx/5xx 分流到 `response.Fail` / `response.Error`
  —— 上层既不认识 `gorm.ErrRecordNotFound`，也不逐个判断错误类型。
- `context.Context` 从 `c.Request.Context()` 一路向下传递，链路追踪（OTel）与事务
  （[pkg/tx](pkg/tx/tx.go)）都能沿 ctx 传播。

## HTTP 契约与错误码

所有接口返回同一信封；**业务数据统一放在 `data` 字段下**，前端只需一套解析逻辑：

```json
{
  "code": 0,
  "message": "ok",
  "data": { "id": 1 },
  "trace_id": "5f0af201e4905c0eeab1603c31ab1540"
}
```

三个出口，语义严格区分（状态码与业务码由 [pkg/errs](pkg/errs/errs.go) 统一映射，handler 不写分支）：

| 出口 | 语义 | 典型错误 | 日志 |
|---|---|---|---|
| `response.OK(c, data)` | 成功（200） | — | — |
| `response.Fail(c, err)` | **请求方错误（4xx）** | `InvalidArgument` / `Unauthorized` / `Forbidden` / `NotFound` / `Conflict` / `TooManyRequests` | 不写日志（避免告警噪声） |
| `response.Error(c, err)` | **服务端错误（5xx）** | `Internal` / `Unavailable` / `Timeout`，或任何未归一化的 error | 记 error 级别（含 cause、method/path 与 trace_id） |

- 分页、列表等业务结构由各 service 的 DTO 定义，整体作为 `data` 返回（如 demo 的 `ListDemoResponse`），`pkg/response` 不规定业务结构。
- 选错出口不会改变对外语义：`Fail` 收到 5xx、`Error` 收到 4xx 时仍按错误自身状态码返回，但各记一条告警提示修正调用点（有测试覆盖）。
- 对 biz 返回的错误，service 用一个分流函数决定走哪个出口（见 [respondBizError](internal/domain/demo/service/service.go)），保证监控面板里 4xx/5xx 归类正确。

业务码沿用 `HTTP 状态码 × 100`：`40000` 参数错误、`40100` 未认证、`40300` 无权限、`40400` 不存在、`40900` 冲突、`42900` 限流、`50000` 内部错误、`50300` 依赖不可用、`50400` 超时。

错误分层约定：

| 层 | 责任 |
|---|---|
| `data/` | 把存储错误翻译成**领域错误**（如 `biz.ErrDemoNotFound = errs.NotFound("记录不存在")`），上层不认识 `gorm.ErrRecordNotFound` |
| `biz/` | 定义领域错误；基础设施错误用 `errs.Internal(...).WithCause(err)` 归一化（cause 只进日志） |
| `service/` | 只调 `respondBizError`（按 4xx/5xx 分流到 `Fail`/`Error`），不判断具体错误类型 |

未归一化的 error 一律按 `500` + 固定文案返回，**绝不透出 SQL/DSN 等内部细节**（`pkg/errs`、`pkg/response` 有测试锁死这条）。

### 请求 ID = TraceID

不再单独生成请求 ID：**请求 ID 就是链路 TraceID**，四个位置天然同源：

| 位置 | 内容 |
|---|---|
| 响应头 `X-Request-ID` | TraceID（`middleware.TraceID` 回写） |
| 响应体 `trace_id` | TraceID（`pkg/response`） |
| 日志字段 `trace.id` | TraceID（`log.WithContext`） |
| 链路系统 | 同一个 TraceID |

- 上游若传 `traceparent`，TraceID 由 W3C 传播器续接，跨服务链路不断（有测试覆盖）。
- **未配置 `otel.endpoint` 时也必须有 TraceID**：`trace.Init` 始终安装 Provider（无端点则 `NeverSample` + 无导出器），因此请求 ID 在纯本地开发环境同样可用。
- 客户端只需拿响应头，即可在日志/链路平台检索整条请求。

## 健康检查、pprof 与优雅停机

### 探针

| 路径 | 语义 | 行为 |
|---|---|---|
| `GET /healthz` | 存活探针（liveness） | 进程能响应即 200，**不检查依赖**（依赖抖动不应导致容器被反复重启） |
| `GET /readyz` | 就绪探针（readiness） | 探测 MySQL/Redis，不可用返回 503（K8s 摘除流量但不重启容器） |

两者都返回统一信封，并带 `trace_id` 便于对齐排查。

### pprof（按配置、独立端口）

```yaml
server:
  http:
    pprof:
      enable: true
      host: 127.0.0.1   # 生产建议只监听本机/内网
      port: 6060
```

- **只在独立端口提供**，绝不挂到业务端口（pprof 能读进程内存，暴露等于信息泄露面）。
- `enable: true` 但 `port` 为 0 时**跳过并告警**，不会退化到业务端口。
- 基于标准库 `net/http/pprof`，因此不再依赖 `gin-contrib/pprof`。

### 优雅停机

`main` 监听 `SIGINT` 与 `SIGTERM`（K8s 停止容器发的是 SIGTERM），收到信号后按依赖顺序收尾：

```
停止接收新连接 → 排空在途请求（超时 5s）→ 关闭 pprof → 释放 MySQL/Redis
  → flush trace/metrics → 关闭日志 → 关闭配置来源
```

- HTTP 超时预算：`ReadHeaderTimeout 10s` / `ReadTimeout 30s` / `WriteTimeout 60s` / `IdleTimeout 120s`
  （零值即"永不超时"，一个慢客户端就能长期占住连接）。
- 在途请求会被**排空**而不是掐断（`internal/server_test.go` 专门验证了这条语义）。

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
（`/v1/traces`、`/v1/metrics`、`/v1/logs`）。

`otel.endpoint` 留空（默认）时的行为需要区分清楚：

- **traces**：仍安装 Provider 并生成/传播 TraceID（`NeverSample` + 无导出器）——
  因为「请求 ID = TraceID」依赖它；不记录、不导出 span，开销可忽略
- **metrics / logs**：不启用（`metrics.Init` 返回 `nil` 表示未启用）

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
  产生 server span 并记录 `http.server.*` 指标。**始终挂载**——即使未配置端点也挂，
  因为请求 ID 取自 span 的 TraceID（此时 span 不记录、不导出）。
- **Redis**：[pkg/infra/data.go](pkg/infra/data.go) 构造客户端时挂 `trace.HookRedis`，
  产生命令级 span 与 `redis_client_operation_duration_ms` 指标；只记录命令名与结果，
  不记录 key / 参数（既避免 PII 泄漏，也避免指标标签基数爆炸）。
- **Go runtime**：`metrics.Init` 内启动，goroutine / 内存 / GC 随周期上报。

**生命周期**：[cmd/main/main.go](cmd/main/main.go) 按 `logger → trace → metrics → Wire` 顺序
初始化；退出（SIGINT/SIGTERM）时按 `HTTP 排空 → 释放 DB/Redis → trace → metrics → 日志 → 配置`
收尾，各步共用 5s 超时预算（详见[优雅停机](#优雅停机)）。
`trace.Init` 始终返回可用 Provider；`metrics.Init` 在端点为空的返回 `nil` 表示未启用，
`Shutdown(nil)` 为空操作，可安全重复调用。

> 注意：`log.Fatal` 路径（例如启动时连不上数据库）会 flush 日志后立即 `os.Exit(1)`，
> 不会 flush trace/metrics——此时通常还没有值得上报的 span/指标。运行期需要保证上报的
> 场景，请把错误返回给 `main`，走正常退出路径。

**验证**：三路都有端到端测试，用进程内 `httptest` 充当 OTLP 接收端（不依赖任何外部后端）：

```bash
go test ./pkg/log/ ./pkg/trace/ ./pkg/metrics/ ./pkg/otelx/
```

## 配置说明

配置由 [Kratos v3 的 config 组件](https://go-kratos.dev/docs/component/config/) 加载，
按 source 顺序覆盖（**后面的覆盖前面的**）：

1. **file source**：`config.yaml`（默认值，随版本库管理）
2. **env source**：`APP_` 前缀环境变量（部署覆盖）

本包**不保存全局配置**：`conf.Load` 读取并校验后返回 `*conf.Bootstrap` 与来源句柄，
由入口显式注入到各构造函数（项目内通过 Wire 完成），测试时可直接构造替换。
[cmd/main/main.go](cmd/main/main.go) 的实际装配（日志 → trace → metrics → Wire）：

```go
cfg, cfgSource, err := conf.Load(*confPath) // cfgSource 用于 Close / Watch
if err != nil {
    fmt.Fprintln(os.Stderr, "启动失败:", err)
    os.Exit(1)
}

service := cfg.OtelServiceInfo(hostname())                  // 三信号共用的服务身份
tracesEP, metricsEP, logsEP := otelEndpoints(cfg.GetOtel()) // 一个端点派生三路

logger, err := log.New(logConfig(cfg, service, logsEP))     // 控制台 + 文件 + OTLP
slog.SetDefault(logger.Logger)                              // 包级 slog 共用同一后端

tracerProvider, _ := trace.Init(traceConfig(cfg, service, tracesEP))
meterProvider, _ := metrics.Init(metricsConfig(cfg, service, metricsEP))

app := initApp(cfg, logger.Logger) // cfg 与 logger 一路随构造函数显式传递
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
    pprof:             # 性能分析（仅在独立端口提供，默认关闭）
      enable: "${PPROF_ENABLE:false}"
      host: "${PPROF_HOST:127.0.0.1}"
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
（[cmd/main/main.go](cmd/main/main.go) 在优雅停机的最后一步调用）。

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

> `conf.pb.go` 与 `wire_gen.go` 都入库：克隆后无需任何代码生成工具即可构建。

## 测试

模板的测试**不依赖任何外部服务**（无需 MySQL / Redis / OTLP collector），直接跑：

```bash
go test ./...              # 全部测试
go test ./... -race        # 竞态检测
go vet ./...               # 静态检查
gofmt -l .                 # 格式检查（无输出即通过）
go test ./pkg/response/ -v # 单包详情
```

| 包 | 覆盖内容 |
|---|---|
| `internal/conf` | 配置加载与覆盖、DSN/Addr、校验 |
| `internal/domain/demo/data` | 领域模型 ↔ PO 映射完整性（往返结构体比较，漏映射即失败） |
| `internal` | 优雅停机排空在途请求、pprof 配置门控与真实可访问、`/healthz` 与 `/readyz` 契约、请求 ID 闭环、`traceparent` 续接 |
| `pkg/errs` | 错误归一化、cause 不外泄、哨兵错误不被污染、按业务码 `Is` 匹配 |
| `pkg/response` | 响应信封、4xx/5xx 出口语义与日志行为、`trace_id` 同源 |
| `pkg/otelx` | OTLP 端点解析（含非法输入不 panic）、Resource 属性 |
| `pkg/trace` | 无端点也必须有 TraceID、采样兜底、Shutdown 语义 |
| `pkg/log` | OTLP 日志端到端（假接收端）+ `trace.id` 关联字段 |
| `pkg/metrics` | OTLP 指标端到端（假接收端） |
| `pkg/cache`、`pkg/oss`、`pkg/tx` | 可选能力库自带用例 |

几点说明：

- **三路信号的端到端测试**用进程内 `httptest` 充当 OTLP 接收端，解开 protobuf 后断言
  `trace_id` / `span_id` / body / Resource 属性，因此不部署 collector 也能验证"真的推出了"。
- **数据层映射用结构体整体比较**断言：给领域模型加字段却忘记改映射时直接失败，不会静默丢字段。
- 需要真实数据库的集成测试**未包含**（模板刻意不引入外部依赖）；要补的话可用 testcontainers
  或纯 Go 的 sqlite 驱动跑仓储层。

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

## 已知限制与后续可扩展

当前实现刻意保持精简。以下按优先级列出已知缺口，投入生产前建议逐项确认：

| 项 | 现状 | 建议 |
|---|---|---|
| 启动依赖 | 连不上 MySQL 直接 `log.Fatal` 退出（只 flush 日志，不 flush trace/metrics） | 若要"依赖不可用也先起服务、由 `/readyz` 报告"，让 `infra.NewData` 返回 error（Wire 支持 error provider），由 `main` 走正常退出路径 |
| 认证 | JWT 中间件工厂已注册，但**未挂载到任何路由**，密钥仍是示例常量 | 密钥接入配置/密钥管理，按路由分组挂载，并把鉴权结果写入 `common.RequestMetadata.UserID` |
| 请求体与代理 | 未限制请求体大小，未设置 `SetTrustedProxies` | `c.ClientIP()` 目前会信任任意 `X-Forwarded-For`，生产必须显式配置可信代理 |
| HTTP 超时 | 编译期常量（见 [internal/server.go](internal/server.go)） | 需要按环境调参时加进 `conf.proto` 并重跑 `make config` |
| 数据库迁移 | 仅 `AutoMigrate`（开发够用） | 生产建议引入版本化迁移（goose / atlas / golang-migrate） |
| 可选能力库 | `pkg/cache`、`pkg/oss`、`pkg/tx` 尚未被业务引用 | 接入方式：`pkg/tx` 在 data 层用 `tx.WithContext(ctx, r.db)` 即可复用 biz 开启的事务；`pkg/cache` 可做 cache-aside 装饰器 |
| 数据库埋点 | HTTP / Redis 有 span 与指标，GORM 没有 | SQL 通常是延迟大头，可接 `gorm.io/plugin/opentelemetry` |
| 工程化 | 无 CI、无 Dockerfile、无 lint 配置 | 建议 `make verify`（fmt + vet + test + 生成物漂移检查）+ GitHub Actions |
| 配置热更新 | `conf.Source.Watch` 能力已就绪但未使用 | 需要动态开关/灰度时可基于它实现 |

## 许可证

[MIT](LICENSE) © 2026 CycleZero
