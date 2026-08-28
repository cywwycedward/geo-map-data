# geodata-serve 开发规范

> 当前状态：只有设计与规范，尚未建立 Go module。本文中的命令在 `go.mod` 创建后生效。

## 1. 开发环境

### 必需版本

- Go `1.24.0`；
- Windows amd64；
- MSYS2 UCRT64 GCC 与对应运行库；
- Git 从仓库根目录管理。

`duckdb-go` 使用 CGO，并默认链接预编译 DuckDB 静态库。Windows 开发机应将 MSYS2 UCRT64 的 `bin` 目录加入当前 shell 的 `PATH`，但不得把开发机绝对路径提交到仓库配置。

### 唯一直接第三方依赖

```text
github.com/duckdb/duckdb-go/v2 v2.10505.0
```

计划 `go.mod`：

```go
module <仓库模块路径>/services/geodata-serve

go 1.24.0

require github.com/duckdb/duckdb-go/v2 v2.10505.0
```

模块路径在创建 `go.mod` 时根据仓库最终模块命名确定；当前文档不猜测。提交 `go.mod` 和 `go.sum`，v1 不提交 `vendor/`，也不启用 `duckdb_arrow`。

## 2. 依赖规则

以下能力使用 Go 标准库：

| 能力 | 标准库 |
| --- | --- |
| HTTP server/client | `net/http`、`httptest` |
| 路由 | `http.ServeMux` |
| CLI | `flag` |
| JSON / NDJSON | `encoding/json` |
| 日志 | `log/slog` |
| 取消与超时 | `context` |
| 信号 | `os/signal` |
| 并发 | `sync`、channel |
| token / 请求 ID | `crypto/rand` |
| 测试 | `testing` |

不在 v1 引入 chi、Cobra、Viper、Zap、Testify、Testcontainers、任务队列或 SQL migration 框架。

增加直接依赖必须同时满足：

1. 已出现当前实现无法简单解决的真实需求；
2. 标准库方案的具体不足可被测试或维护成本证明；
3. 新依赖不会扩大外部 HTTP interface；
4. 设计文档和依赖调研同步更新。

## 3. 计划 package 责任

最终 package 数量以真实实现为准，不为目录图创建空壳。

### `cmd/geodata-serve`

- 解析 `init`、`serve`、`restore`、`version`；
- 组装具体 implementation；
- 处理进程信号与退出码；
- 不包含 SQL 执行、调度或协议编码规则。

### `internal/runtime`

- 实现设计文档中的深 Runtime Module；
- 拥有 DuckDB connector、连接池、读写调度、备份、状态和取消；
- 对 HTTP adapter 暴露最小 interface；
- 不依赖 `net/http`。

如果 scheduler、registry、backup 的代码可以保持清楚，先放在同一 package；只有出现独立不变量和真实替代 implementation 时再拆分。

### `internal/httpserver`

- token 鉴权；
- 端点和 JSON 验证；
- Runtime 命令与 NDJSON 事件转换；
- HTTP 生命周期；
- 不直接 import DuckDB 驱动，不持有 `*sql.DB`。

### `internal/bootstrap`

- 路径验证；
- 扩展目录、临时目录和状态文件；
- `init` 与 `serve` 启动准备；
- 原子写入、匹配删除 `server.json`。

### `internal/restore`

- 服务停止检查；
- 临时数据库导入与验证；
- 当前数据库保留和安全替换；
- 不与在线 Runtime 共享连接。

若 restore 的 implementation 很小，可并入 bootstrap，而不是保留浅 package。

## 4. Interface 与错误

- 外部 interface 以 `docs/http-interface.md` 为准；修改协议必须先修改文档和 contract tests。
- Runtime interface 使用稳定错误分类，不把 DuckDB 原始字符串当成控制流。
- handler 只负责把 Module 错误映射成 HTTP 或 NDJSON；同一错误映射只能存在一处。
- 错误使用 `%w` 保留 cause；面向调用方的 message 简短，不包含 token、stack trace 或完整 SQL。
- 请求 mode 是可信声明；实现不尝试“辅助”判断或纠正。

建议内部错误分类至少覆盖：

```text
invalid request, shutting down, backup failed,
SQL failed, encoding failed, cancelled, deadline exceeded
```

## 5. Context 与资源

- `context.Context` 是每次调用的第一个参数，不存入 struct。
- HTTP request context 一直传到排队、备份和 `ExecContext` / `QueryContext`。
- 独立 rollback cleanup 使用新的短 context，不能复用已取消 context。
- 所有取得的 Rows、专用连接、`*sql.DB`、connector、listener 和文件都在同一职责范围内关闭。
- 关闭错误不可吞掉；与主错误同时出现时记录 cleanup error，但保留主错误语义。
- 不使用无界 goroutine。每个 goroutine 必须有 owner、退出条件和可测试的关闭路径。

## 6. 并发规则

- 读槽位固定为 2；写槽位固定为 1；连接池最大为 3。
- 并发控制只能由 Runtime 拥有。HTTP handler 不自行开 goroutine 绕过队列。
- 写队列保持 FIFO；排队取消不得继续占槽位。
- 状态 registry 与事件输出的顺序必须一致；终态只写一次。
- 测试不得依赖 `time.Sleep` 猜测并发顺序。使用 channel、barrier 或可观察状态协调。

## 7. SQL 与 DuckDB

- 使用官方驱动路径 `github.com/duckdb/duckdb-go/v2`，不要使用旧 `marcboeker/go-duckdb`。
- 使用 `duckdb.NewConnector` 为每个新连接加载 `spatial` 和 `httpfs`。
- 调用方 SQL 原样传入 DuckDB，不拼接领域条件。
- 服务自己的 `EXPORT DATABASE`、配置和检查 SQL 集中在 Runtime/bootstrap，不散落在 handler。
- 服务生成路径写入内部 SQL 前必须清理、验证位于目标根目录，并使用驱动支持的参数化方式；若语法不能参数化，使用单一经过测试的 DuckDB literal quoting 函数。
- 不假设所有 Shapefile 只有 `.shp`；测试夹具必须包含读取所需的配套文件。
- 不擅自给来源数据设置 EPSG:4326；`ST_Transform(..., always_xy := true)` 由调用方 SQL根据数据语义决定。

## 8. 日志

使用 `slog`，日志以事件而不是句子命名。推荐字段：

```text
request_id, mode, state, queued_ms, execution_ms,
row_count, error_code, service_version, duckdb_version
```

禁止记录：

- Bearer token；
- 完整原始 SQL；
- 远程数据源凭据；
- 未经必要性评估的完整本地文件内容。

允许在 debug 场景记录经过明确截断和脱敏的 SQL hash 或调用方提供的安全 label，但这些都不是 v1 必需字段。

## 9. 测试策略

### 9.1 测试层次

| 层次 | 测试 seam | 主要证据 |
| --- | --- | --- |
| Runtime 行为 | Runtime interface + 真实临时 DuckDB | 调度、备份、取消、持久化、扩展、状态。 |
| HTTP contract | `httptest` + Runtime adapter | 鉴权、字段验证、headers、NDJSON 顺序、错误映射。 |
| 组合集成 | 真实 HTTP listener + 真实 Runtime | 断连取消、状态查询、关闭和状态文件。 |
| Windows smoke | 编译后的 exe | CGO、GCC、扩展加载、数据库重启。 |

测试跨 interface 断言可观察结果，不读取 private channel、锁或 registry map。内部并发测试可以使用 package-private fixture，但不能扩大生产 interface。

### 9.2 必需场景

#### 初始化与格式

- 首次安装 Spatial 和 httpfs；
- 缺少扩展时 `serve` 明确失败；
- 读取 GeoJSON；
- 读取 GeoParquet；
- 读取包含配套文件的 Shapefile；
- `ST_Drivers()` 包含夹具所需 driver。

#### 持久化与连接

- 创建表、关闭服务、重新打开后数据仍存在；
- 每个新连接都能调用 Spatial/httpfs；
- 所有 Rows 和连接在成功、错误、取消路径释放。

#### 并发

- 两个 read 同时进入 running；
- 第三个 read 保持 queued，直到槽位释放；
- write FIFO；
- 一个 write 与两个 read 可同时运行；
- queued 请求取消后不执行 SQL。

#### 写入与恢复

- write SQL 前先创建可导入备份；
- backup 失败时 write SQL 未执行；
- 第 6 个成功备份创建后只留下最近 5 份；
- 清理拒绝越过 backup root；
- restore 失败不破坏当前数据库；
- restore 成功保留恢复前数据库，并替换为选定状态。

#### HTTP 与 NDJSON

- token 缺失/错误为 401；
- unknown JSON field 为 400；
- request ID 同时出现在 header、事件和 status；
- schema 先于 row，终态最后且唯一；
- 重复列名仍按数组正确返回；
- DECIMAL、大整数、BLOB、NULL、嵌套类型按协议编码；
- SQL 错误在流开始后用 error 事件表达；
- 客户端断连能取消 DuckDB 查询；
- shutdown 后拒绝新请求并取消进行中请求。

### 9.3 夹具

夹具必须：

- 小而确定，可在测试中快速读取；
- 无凭据、许可证允许提交；
- 包含已知 CRS、几何类型、行数和范围；
- Shapefile 配套文件完整；
- 预期值写在测试中，不依赖外部网络。

在线扩展下载单独标记为 smoke test，普通 `go test ./...` 不应因网络暂时不可用而随机失败。

## 10. 开发命令

Go module 建立后，常规检查为：

```powershell
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

Windows release smoke test 至少验证：

```powershell
go build ./cmd/geodata-serve
```

不要在文档阶段创建假的脚本包装不存在的命令。需要重复的构建步骤真实出现后，再增加 PowerShell 或 Go 工具脚本。

## 11. 变更流程

1. 从相关设计条目写出可观察验收条件；
2. 对行为变更先补失败测试；
3. 只实现通过该测试所需的最小改动；
4. 执行格式化、测试、vet 和相应 Windows smoke test；
5. 若 interface 改变，先更新 `docs/http-interface.md`，再修改 caller 与 implementation；
6. 若发现仓库级决策不再成立，更新架构文档或 ADR，不在局部注释中静默改写。

## 12. 何时考虑新工具

| 工具 | 触发条件 |
| --- | --- |
| chi | 出现版本化路由、嵌套路由或大量共用 middleware。 |
| Cobra | 子命令和嵌套参数明显超过标准 `flag` 可清楚维护的程度。 |
| Zap | profiling 证明日志处理是实际性能瓶颈。 |
| Testcontainers | 必须联测 DuckDB 之外的容器化外部依赖。 |
| 持久化任务队列 | 异步任务必须跨进程重启继续。 |
| Arrow | Go 进程需要批量处理或转发大规模列式结果。 |

达到触发条件只表示可以重新评估，不表示自动引入。
