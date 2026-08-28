# Go + DuckDB 数据程序的第三方库选择

> 调研日期：2026-08-27  
> 问题：本项目的本地 Go + DuckDB 数据程序，为了尽快可用且便于后续扩展，应当引入哪些第三方 Go 库？

> 后续决定：v1 的命名、Module 边界和代码布局以 [数据模块 v1](../architecture/data-module-v1.md) 与 [`geodata-serve` 服务设计](../../services/geodata-serve/docs/design.md) 为准；本文保留第三方库选型依据。

## 结论

v1 只直接引入一个第三方模块：

```text
github.com/duckdb/duckdb-go/v2
```

HTTP 服务、CLI 参数、JSON、日志、取消、并发、优雅关闭和测试均先使用 Go 标准库。这样不是放弃扩展，而是保留清晰的替换位置：所有服务接口使用 `net/http`，未来若接口变多可无痛接入兼容 `net/http` 的路由器；数据访问只经由一个内部 DuckDB 包，未来可替换调用细节而不影响 HTTP 层。

## 已核实的事实

### DuckDB 驱动

- 应使用官方维护的 [`github.com/duckdb/duckdb-go/v2`](https://github.com/duckdb/duckdb-go)。它是 DuckDB 的 primary client，通过 Go 标准库的 `database/sql` 工作；旧的 `marcboeker/go-duckdb` 导入路径已经迁移。[官方 README](https://github.com/duckdb/duckdb-go#background)
- 调研时，DuckDB `v1.5.5` 对应的首个驱动版本是 `v2.10505.0`；版本号编码了 DuckDB 版本。因此应固定具体的驱动版本，而不是使用 `latest`。[版本对应表](https://github.com/duckdb/duckdb-go#go-sql-driver-for-duckdb)
- 驱动 `v2.10505.0` 要求 Go `1.24.0`；项目应把 Go 版本作为构建前提写入 `go.mod` 和开发文档。[该版本的 `go.mod`](https://raw.githubusercontent.com/duckdb/duckdb-go/v2.10505.0/go.mod)
- 驱动默认把预编译 DuckDB 静态库链接入二进制文件，包含 Windows amd64 库。Windows 本机编译仍需要正确版本的 GCC 和运行库，官方建议 MSYS2 UCRT64 工具链。[安装与链接说明](https://github.com/duckdb/duckdb-go#installation)
- Arrow 接口在 v2 中是可选构建功能；没有把 DuckDB 大批量结果传给 Go 的需求时，不应启用 `duckdb_arrow` build tag。[驱动 breaking changes](https://github.com/duckdb/duckdb-go#the-arrow-dependency-is-now-opt-in)
- 官方支持用 `duckdb.NewConnector` 和 `sql.OpenDB` 为每个新连接运行初始化回调。这适合本项目在每个连接中加载 Spatial、httpfs 并设置临时目录。[连接器示例](https://github.com/duckdb/duckdb-go#usage)

### Go 标准库已覆盖的能力

- `net/http` 已提供 HTTP server、路由器 `ServeMux`、请求 context 和 `Server.Shutdown`；自定义 `http.Server` 还能配置读写超时。这个项目 v1 只有健康检查、执行 SQL、关闭服务等少量端点，无需第三方 Web 框架。[`net/http`](https://pkg.go.dev/net/http)
- `log/slog` 是 Go 标准库的结构化日志。它适合记录请求 ID、SQL 文件名、耗时、是否写入和错误等字段，无需先引入 zap 或 zerolog。[`log/slog`](https://pkg.go.dev/log/slog)
- `flag` 可实现 `serve`、`init`、`restore` 这类少量子命令；`encoding/json` 可读写本地运行状态；`context` 和 `os/signal` 可传递取消并在主 Agent 停止任务时关闭服务。[`flag`](https://pkg.go.dev/flag)、[`context`](https://pkg.go.dev/context)、[`os/signal`](https://pkg.go.dev/os/signal)
- `testing`、`net/http/httptest` 和 `t.TempDir` 足以对嵌入式 DuckDB 创建隔离临时数据库并测试 HTTP 服务，不需要容器。[`testing`](https://pkg.go.dev/testing)、[`httptest`](https://pkg.go.dev/net/http/httptest)

## v1 直接依赖

建议初始 `go.mod` 只声明：

```go
module <项目模块路径>

go 1.24.0

require github.com/duckdb/duckdb-go/v2 v2.10505.0
```

应用代码使用的标准库包包括：

```text
context, crypto/rand, database/sql, encoding/json, errors,
flag, log/slog, net, net/http, os, os/signal, path/filepath,
sync, syscall, testing, net/http/httptest
```

这不表示项目最终只有一个依赖。`duckdb-go` 本身会带来它需要的传递依赖和静态 DuckDB 库；项目只是不直接绑定一个 HTTP 框架、配置框架、日志框架或容器测试框架。

## 不在 v1 引入的库

| 类别 | v1 决定 | 何时再引入 |
| --- | --- | --- |
| HTTP 路由 | 不引入；用 `net/http.ServeMux` | 出现版本化 API、嵌套路由或大量共用中间件时，选 `github.com/go-chi/chi/v5`。它兼容标准 `net/http`，可作为替换，不必重写处理函数。[chi 官方 README](https://github.com/go-chi/chi) |
| CLI | 不引入；用 `flag` | 命令超过少量固定子命令，或需要自动补全、复杂帮助和大量嵌套参数时，选 `github.com/spf13/cobra`。[Cobra 官方仓库](https://github.com/spf13/cobra) |
| 日志 | 不引入；用 `log/slog` | 经性能分析确认日志是热点，或需要更复杂的采样和输出管道时，再评估 `go.uber.org/zap`。它的主要价值是高性能结构化日志，不是 v1 的必要能力。[Zap 官方仓库](https://github.com/uber-go/zap) |
| 配置 | 不引入；运行状态使用 JSON，启动参数使用 `flag` | 有多环境配置、配置文件合并、环境变量覆盖等真实需求时再评估配置库。 |
| SQL 迁移 | 不引入 | 当前项目把可复现的数据变更保存为 `sql/`，而不是维护应用表结构的版本迁移。若 Go 自身开始拥有稳定的内部 schema 并需要升级路径，再选择迁移工具。 |
| 任务队列 | 不引入 | v1 只有单进程的读/写调度，用 `sync` 和 channel 即可。需要断电后继续任务、定时任务或跨进程任务时再选持久化队列。 |
| 容器测试 | 不引入 | DuckDB 嵌入 Go 进程，集成测试可直接使用临时 `.duckdb` 文件。只有未来必须联测 Postgres、对象存储等外部依赖时，才用 `testcontainers-go`；该库的用途正是创建和清理容器化测试依赖。[官方仓库](https://github.com/testcontainers/testcontainers-go) |
| Arrow | 不启用 | 只有 Go 服务需要以 Arrow 形式大量处理或转发 DuckDB 结果时，才在构建时启用 `duckdb_arrow`。 |

## 推荐的内部边界

不依赖框架也应保留很小的代码边界：

```text
cmd/geodata-serve/  进程启动、参数、信号处理
internal/server/    HTTP 请求/响应、认证、关闭
internal/data/      DuckDB 连接、读写队列、备份、执行 SQL
internal/runtime/   server.json 的读写和健康检查
```

`internal/server` 只依赖 `internal/data` 提供的“执行请求”接口；它不应直接持有 `*sql.DB`。这样未来增加 chi、CLI 子命令或 MCP 调用方式时，DuckDB 并发和备份逻辑不需要复制。

## 构建和升级规则

1. 固定 `duckdb-go/v2` 具体版本，并用其版本确认绑定的 DuckDB 版本。
2. 提交 `go.mod` 与 `go.sum`；先不提交 `vendor/`。若之后需要完全离线或可复现的发布构建，再使用驱动官方支持的 `go mod vendor`，它会带入预编译 DuckDB 库。[Vendoring 说明](https://github.com/duckdb/duckdb-go#vendoring)
3. 在 Windows CI 或开发环境首先验证 GCC/MSYS2 编译、服务启动、`LOAD spatial`、`LOAD httpfs` 和持久化数据库重启。
4. 升级驱动时把 DuckDB、Spatial、httpfs 视为一组测试：使用真实 GeoJSON、GeoParquet、Shapefile 和一次备份恢复测试后才升级。

## 建议

当前最合适的决定是：v1 仅直接依赖 `duckdb-go/v2`，使用 Go 标准库建立本地 HTTP 服务、CLI 和测试；预先记录未来可引入 chi、Cobra、Zap、Testcontainers 的明确触发条件，但不把它们加入依赖树。

这比一开始引入全套 Web/配置/日志框架更容易升级：所有候选库都围绕标准 `net/http`、`context` 和 `database/sql`，因此以后增加时不会改变 Agent 提交原始 SQL 的核心设计。
