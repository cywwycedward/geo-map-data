# geodata-serve 设计实现

> 状态：v1 实现依据。本文描述已经确认的 Go + DuckDB 服务设计及当前实现边界。

## 1. 文档地位

实现时按以下顺序解释要求：

1. 仓库根目录与本服务的 `AGENTS.md`；
2. 本文和同目录的 `http-interface.md`；
3. 仓库级 `docs/architecture/data-module-v1.md`；
4. `docs/research/` 中的调研记录。

如果调研建议与本文冲突，以本文记录的最终 v1 决定为准。项目目录、`DATA.md`、重要 SQL 保存和数据登记属于后续 skill 设计；服务只消费 skill 提供的路径和 SQL。

## 2. 目标

`geodata-serve` 为一个地理项目提供唯一的本地 DuckDB 数据进程，使主 Agent 与多个子 Agent 可以：

- 自行编写并执行原始 DuckDB SQL；
- 直接查询 GeoParquet、GeoJSON、Shapefile 等文件；
- 使用 DuckDB Spatial 完成空间读取、计算和转换；
- 在同一进程内安全共享持久化数据库；
- 获得可取消、可观察、不会累积完整结果的流式执行体验；
- 在修改数据库前得到可恢复的完整备份。

成功标准不是“提供更多地理方法”，而是让调用方只学习一个小 HTTP interface，就能可靠使用 DuckDB 的完整 SQL 能力。

## 3. 非目标

v1 不实现：

- 道路、行政区、缓冲区等领域专用查询方法；
- SQL 白名单、SQL 解析、SQL 重写或自动判断读写；
- 用户项目目录结构、数据来源登记、`DATA.md` 或 `sql/` 管理；
- 多项目共用一个进程、跨机器访问、用户账号或企业权限；
- 异步任务持久化、稍后读取结果、断点续跑或自动重试；
- 百分比进度、分布式队列、增量备份或数据血缘系统；
- Arrow 返回、地图渲染或 QGIS 交互。

## 4. Seam 与 Module

### 4.1 外部 seam

外部 seam 是仅监听 `127.0.0.1` 的 HTTP interface。调用方只需要知道：

- 如何读取服务状态文件并取得地址与 token；
- 如何提交 `read` 或 `write` SQL；
- NDJSON 事件顺序和错误语义；
- 如何查看请求状态和关闭服务。

HTTP 是 adapter，不拥有数据规则。未来改用 chi 或增加另一个本地传输时，数据运行 Module 不应改变。

### 4.2 数据运行 Module

数据运行 Module 是核心深 Module。其建议 interface 为：

```go
type Runtime interface {
    Execute(context.Context, Command, EventSink) error
    Status(RequestID) (RequestStatus, bool)
    Close(context.Context) error
}
```

上述代码只表达 seam，不是当前代码。`Command` 包含服务生成的请求 ID、调用方声明的模式、原始 SQL 和可选超时；`EventSink` 接收状态、字段、行、汇总或错误事件。

这个小 interface 隐藏以下 implementation：

- DuckDB connector、连接池和每连接扩展加载；
- 2 个读槽位和 1 个写槽位；
- 写请求队列、写前备份和备份保留；
- 请求状态机和进程内状态登记；
- context 取消、连接清理和未提交事务回滚；
- DuckDB 行到协议值的转换；
- 服务关闭时的停止接收、取消和资源释放顺序。

调用方和核心行为测试都跨这个 interface。不要把 scheduler、backup manager、registry 等内部 implementation 细节升级为公共 interface。

### 4.3 依赖分类

| 依赖 | 分类 | 测试方式 |
| --- | --- | --- |
| DuckDB | local-substitutable | 在 `t.TempDir()` 中打开真实临时 `.duckdb` 数据库。 |
| 项目文件系统 | local-substitutable | 使用真实临时目录与文件，不创建文件系统 port。 |
| HTTP | 外部 seam 的 adapter | 使用 `httptest` 跨同一 HTTP interface 验证。 |
| DuckDB 官方扩展仓库 | true external，仅初始化使用 | 独立网络 smoke test；普通测试使用已准备的同版本扩展。 |

DuckDB 和文件系统已有可靠本地替代，不为测试增加 repository interface 或 mock。HTTP adapter 测试可以使用最小内存 Runtime adapter，但核心 Runtime 自身必须用真实 DuckDB 验证。

## 5. 进程模型

- 每个地理项目同一时刻最多运行一个 `geodata-serve` 进程。
- 一个进程只打开一个持久化 DuckDB 数据库。
- 不同项目可各自启动进程，并使用不同随机端口和状态文件。
- Agent 不直接打开该数据库；所有共享访问经过本服务。
- 服务启动时先成功打开数据库，再发布状态文件。第二个进程若无法取得 DuckDB 写锁，必须启动失败，不能覆盖已有状态文件。

## 6. 路径输入与目录责任

skill 决定用户项目目录结构。服务启动时接收显式路径，不从当前仓库、用户主目录或固定项目布局中猜测：

| 参数 | 含义 |
| --- | --- |
| `--database` | 持久化 DuckDB 数据库文件的绝对路径。 |
| `--runtime-dir` | 服务私有运行目录根；服务在其中使用扩展、临时文件和状态文件。 |
| `--backup-dir` | 完整数据库备份目录根。 |
| `--working-dir` | Agent SQL 中相对文件路径的解析基准。 |

服务在 `runtime-dir` 内拥有以下内部布局：

```text
runtime-dir/
├── extensions/
├── duckdb-tmp/
└── server.json
```

这是服务内部布局，不等于用户项目布局。服务不得创建或管理 `DATA.md`、`sql/`、原始数据目录或导出目录。

所有启动路径在使用前转为清理后的绝对路径。`working-dir` 必须存在；数据库、运行目录和备份目录按对应命令安全创建。实现不得用 `~`、未解析环境变量或进程启动位置推断目标。

## 7. CLI interface

v1 计划使用单一二进制 `geodata-serve` 和 Go 标准库 `flag`：

```text
geodata-serve init --runtime-dir <path>
geodata-serve serve --database <path> --runtime-dir <path> --backup-dir <path> --working-dir <path>
geodata-serve restore --database <path> --runtime-dir <path> --backup <path>
geodata-serve version
```

### `init`

1. 创建服务运行目录；
2. 将 DuckDB `extension_directory` 指向 `runtime-dir/extensions/`；
3. 从 DuckDB 官方扩展仓库安装与固定 DuckDB 版本和当前平台匹配的 `spatial`、`httpfs`；
4. 加载两个扩展并执行最小能力检查，包括 `ST_Drivers()`；
5. 输出 DuckDB、Spatial、平台和扩展目录信息。

失败时返回非零退出码，且不得留下表示初始化成功的状态文件。

### `serve`

1. 验证并解析路径；
2. 确认所需扩展存在；
3. 打开 DuckDB connector 与连接池；
4. 在新连接初始化回调中执行 `LOAD spatial`、`LOAD httpfs`；
5. 创建 Runtime、HTTP adapter、随机 token 和 loopback listener；
6. 原子写入 `server.json`；
7. 接收请求直到收到关闭请求、进程信号或致命错误；
8. 按关闭顺序清理资源和状态文件。

`serve` 不自动联网安装扩展；缺失扩展时应提示先执行 `init`。

### `restore`

恢复必须在服务未运行时执行：

1. 拒绝对仍可通过健康检查访问的项目执行恢复；
2. 在临时数据库中执行 `IMPORT DATABASE`；
3. 完整验证临时数据库可打开；
4. 关闭所有数据库句柄；
5. 保留当前数据库为带时间戳的恢复前副本；
6. 将已验证的临时数据库替换为目标数据库。

任何步骤失败都不得删除当前数据库。恢复不通过 HTTP 暴露。

## 8. DuckDB 生命周期

### 8.1 版本

- Go：使用构建环境提供的固定版本（当前验收环境为 `1.26.5`）；
- 驱动：`github.com/duckdb/duckdb-go/v2 v2.5.6`；
- DuckDB：由驱动绑定的 `v1.4.5`；
- v1 不启用 `duckdb_arrow` build tag。

升级驱动时必须把 DuckDB、Spatial、httpfs 和 GDAL 驱动能力作为一组重新验收。

### 8.2 连接

- 使用 `duckdb.NewConnector` 和 `sql.OpenDB`；
- 数据库连接池最大连接数为 3；
- 每次 SQL 使用专用 `*sql.Conn`，请求结束后关闭；
- 新连接加载 `spatial` 与 `httpfs`；
- DuckDB 使用默认内存、线程和临时磁盘上限；
- `temp_directory` 指向 `runtime-dir/duckdb-tmp/`；
- 服务关闭前关闭所有 Rows、专用连接、`*sql.DB` 和 connector。

### 8.3 SQL 权限与责任

服务原样执行调用方 SQL。`mode` 是调度声明，不是安全验证：

- `read` 进入读槽位；
- `write` 进入唯一写队列，并触发备份；
- Go 不检查 SQL 是否真的只读或写入；
- Go 不插入 `LIMIT`，不转换 CRS，不自动改写几何；
- 原始 SQL 拥有当前操作系统用户与 DuckDB 已加载扩展可用的文件和网络能力。

多语句写入需要调用方在 SQL 中显式使用 `BEGIN` / `COMMIT`。服务不自动包裹事务，因为任意原始 SQL 可能自行管理事务；如果调用方省略事务，DuckDB 的自动提交可能导致前一语句已提交、后一语句失败。

## 9. 并发和排队

Runtime 使用固定调度规则：

```text
读槽位：2
写槽位：1
最大数据库连接：3
```

- 前两个读请求立即执行，额外读请求进入读队列；
- 写请求始终按到达顺序串行；
- 一个写请求可以和最多两个读请求同时运行；
- 写请求的备份阶段占用写槽位；
- 排队期间 context 被取消的请求直接进入 `cancelled`，不得取得连接；
- `/health` 和请求状态查询不取得数据库连接。

v1 不提供可配置并发数。真实负载证明需要调整后，再把它变成显式版本决策。

## 10. 请求状态机

请求状态只能按以下方向变化：

```text
accepted
   └── queued
         ├── backing_up   （仅 write）
         │      └── running
         └──────── running（read）
                    ├── finished
                    ├── failed
                    └── cancelled
```

规则：

- 请求 ID 在写 HTTP 响应头前生成；
- 每次状态变化先更新进程内 registry，再向当前响应写状态事件；
- 终态不可再次变化；
- 状态记录包含 ID、模式、状态、排队/开始/结束时间、行数和稳定错误分类；
- registry 不保存原始 SQL、token 或完整结果；
- 状态只保留到进程退出，不写入 DuckDB 或磁盘。

异步任务模式未来需要独立设计持久化状态和结果；不得通过延长本 registry 的生命周期偷偷实现。

## 11. 读取执行流程

1. HTTP adapter 验证 token、方法、Content-Type 和 JSON 字段；
2. 生成请求 ID，并设置 `X-Request-ID`；
3. Runtime 登记请求并等待读槽位；
4. 为请求取得专用连接；
5. 使用 HTTP request context 或带可选超时的子 context 调用 DuckDB；
6. 先发送 `status` 与 `schema`，再逐行调用 EventSink；
7. 完成时发送 `summary`，登记 `finished`；
8. 关闭 Rows 和专用连接。

如果客户端断开，EventSink 写入失败会取消请求 context，DuckDB 查询应被中断，状态变为 `cancelled`。

## 12. 写入执行流程

1. HTTP adapter 完成与读取相同的协议验证；
2. Runtime 登记请求并进入写队列；
3. 取得写槽位和专用连接；
4. 在 `backup-dir/<UTC时间>-<请求ID>/` 执行 `EXPORT DATABASE`；
5. 备份失败：发送 `backup_failed`，不执行调用方 SQL；
6. 备份成功：保留最近 5 份，删除更旧备份；
7. 使用同一请求 context 执行调用方原始 SQL；
8. 成功时发送结果事件或汇总；失败或取消时尝试执行 `ROLLBACK`；
9. 关闭连接并释放写槽位。

备份名称由服务生成，不能直接拼接调用方自由文本。删除旧备份前必须验证目标位于传入的 `backup-dir` 内，且新备份已完整成功。

## 13. 取消与关闭

### 13.1 单个请求

请求 context 来自 HTTP 连接。可选 `timeout_seconds` 通过 `context.WithTimeout` 叠加；未提供时没有服务级 SQL 超时。

- 排队取消：从等待中退出，不取得连接；
- 读取消：中断 DuckDB 调用，关闭 Rows 与连接；
- 写取消：中断 DuckDB 调用，使用独立短 context 尝试 `ROLLBACK`，再关闭连接；
- 取消不自动重试。

### 13.2 服务关闭

`POST /shutdown` 或进程信号触发同一关闭流程：

1. 标记 `shutting_down`，拒绝新执行请求；
2. 先向关闭调用方返回确认；
3. 取消排队和运行中的请求；
4. 等待各请求完成清理，受关闭 context 限制；
5. 调用 `http.Server.Shutdown`；
6. 关闭 DuckDB 连接池与 connector；
7. 删除与当前 PID、token 匹配的 `server.json`；
8. 退出进程。

不得删除不属于当前进程的状态文件。

## 14. 备份和恢复不变量

- 只有调用方标记为 `write` 的请求触发备份；
- 备份在调用方 SQL 之前完成；
- 备份失败意味着写 SQL 必须完全不执行；
- 完整备份最多保留 5 份；
- 备份只保护 DuckDB 内容，不保护原始文件、`DATA.md` 或导出物；
- 恢复永远导入到新数据库并验证，不能在当前数据库上直接覆盖导入；
- 任何自动删除只作用于已验证的备份根和服务生成的目录。

## 15. HTTP 与 NDJSON

详细协议见 [HTTP interface](http-interface.md)。核心约束是：

- `/execute` 同步保持连接并流式返回；
- 协议错误在流开始前使用 HTTP 状态码与 JSON；
- SQL 或备份错误发生在流开始后，使用最后一个 NDJSON `error` 事件；
- 每一行都是独立合法 JSON；
- 行值按 schema 顺序使用数组表达，允许重复列名；
- BLOB/原始 GEOMETRY 不自动解释，调用方需要时在 SQL 中使用 `ST_AsGeoJSON` 或 `ST_AsText`；
- 服务不保存结果供稍后读取。

## 16. 安全模型

v1 的安全目标是防止远程和意外调用，不是限制已授权 Agent 的 SQL：

- listener 只能绑定 `127.0.0.1`；
- token 使用 `crypto/rand` 生成，状态文件不提交 Git；
- 除健康检查外都要求 Bearer token；
- 比较 token 时使用恒定时间比较；
- 日志不得包含 token 或原始 SQL；
- `init` 只使用 DuckDB 官方扩展仓库；
- SQL 的本地文件和网络访问只受当前操作系统账号与 prompt 软约束限制。

不应把 loopback + token 描述成对恶意本地同账号进程的强隔离。

## 17. 日志与诊断

使用 `log/slog` 输出结构化日志，至少包含：

```text
service_version, request_id, mode, state,
queued_ms, execution_ms, row_count, error_code
```

启动日志记录 Go、服务、DuckDB、Spatial 版本和监听地址，但不记录 token。请求日志默认不记录原始 SQL；需要关联可保存 SQL 时，由调用方自行记录 SQL 文件。

`GET /health` 只证明 HTTP 进程可响应，并返回服务状态与版本。DuckDB 打开和扩展加载在启动阶段已经验证；v1 不为每次健康检查执行 SQL。

## 18. 计划代码布局

开始实现时使用以下最小布局；当前阶段不提前创建空 package：

```text
services/geodata-serve/
├── cmd/geodata-serve/       # main、flag、信号、进程组装
├── internal/runtime/        # 深数据运行 Module
├── internal/httpserver/     # HTTP adapter 与 NDJSON
├── internal/bootstrap/      # init、serve 路径与扩展准备
├── internal/restore/        # 离线恢复流程
├── testdata/                # 小型、可提交、无凭据的地理夹具
├── docs/
├── go.mod
└── go.sum
```

不要因为目录图存在就预先创建所有 package。只有出现真实代码职责时才创建目录；若实现可以更少，优先合并。

## 19. 实现阶段与验收

### 阶段 1：Windows 构建与扩展验证

- 建立 Go module，固定 Go 和驱动版本；
- 在 Windows amd64 + MSYS2 UCRT64 GCC 完成构建；
- 使用 `init` 安装并加载 Spatial、httpfs；
- 验证 `ST_Drivers()` 包含测试所需格式。

验收：二进制可启动；GeoJSON、GeoParquet 和完整 Shapefile 夹具均可读取。

### 阶段 2：Runtime 读取路径

- 建立 connector、连接池、2 个读槽位；
- 实现状态 registry、context 取消和 EventSink；
- 使用真实临时 DuckDB 测试持久化和两个并发读。

验收：重启后数据仍可查询；第三个读请求排队；取消后下一请求仍可执行。

### 阶段 3：HTTP adapter 与 NDJSON

- 实现 token、四个端点和协议错误；
- 实现 schema/row/summary/error 事件；
- 使用 `httptest` 验证断连取消和流式返回。

验收：调用方只通过 HTTP interface 完成读取、状态查看和关闭。

### 阶段 4：写入、备份和恢复

- 实现单写队列；
- 写前 `EXPORT DATABASE` 与 5 份保留；
- 实现离线 `restore`；
- 验证写失败、取消和错误结果恢复。

验收：两个写请求严格串行；备份失败时 SQL 未执行；误写后能恢复到选定备份。

### 阶段 5：完整验收

- 使用 `wuhan_university_roads` 场景执行检查、导入和分析；
- 并行运行两个读和一个写；
- 验证服务意外退出后的重新打开；
- 执行 `go test ./...`、`go vet ./...` 和 Windows 构建 smoke test。

验收：所有已确认 v1 行为都由自动测试或记录的 Windows 手工验证覆盖。

## 20. 已知代价与后续触发条件

| 当前选择 | 代价 | 重新评估条件 |
| --- | --- | --- |
| 完整 `EXPORT DATABASE` | 数据库变大后写入前等待时间和磁盘占用增加 | 真实项目证明备份成为主要瓶颈。 |
| 同步 HTTP | 调用方需保持连接 | 明确需要提交后做其他工作并稍后取结果。 |
| 2 读 + 1 写 | 高配机器未必完全利用 | 基准测试证明调整能稳定获益。 |
| 状态仅内存 | 重启后无法查看历史请求 | 需要任务恢复或审计历史。 |
| 标准库 HTTP/CLI | 命令和路由变多后样板代码增加 | 出现版本化路由、大量 middleware 或复杂命令树。 |
| 在线初始化扩展 | 第一次初始化依赖网络 | 进入离线安装包发布阶段。 |

这些是明确的未来触发条件，不是 v1 的预留 implementation。没有触发条件前，不增加抽象或依赖。
