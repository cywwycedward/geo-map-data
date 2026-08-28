# Go + DuckDB 本地数据模块调研

> 调研日期：2026-08-27  
> 范围：验证项目采用 Go 程序嵌入 DuckDB，供多个 Agent 进行本地地理数据获取、保存和分析的可行性，并给出以“先可用、后优化”为目标的建议。

> 后续决定：已接受的 v1 路径、并发、备份和 HTTP interface 以 [数据模块 v1](../architecture/data-module-v1.md)、[ADR-0003](../adr/0003-go-duckdb-geodata-serve.md) 与 [`geodata-serve` 服务设计](../../services/geodata-serve/docs/design.md) 为准；本文保留可行性调研与一手资料。

## 结论

这个选型成立。

DuckDB 的官方 Go 客户端 `duckdb-go` 通过 Go 标准库的 `database/sql` 工作，可以打开一个持久化的 `.duckdb` 文件。DuckDB Spatial 能直接读取常见矢量地理文件，并提供空间 SQL 函数。因此，Go 程序可以作为项目唯一的数据执行入口，Agent 仍然自己写原始 DuckDB SQL。

但有一个必须正视的边界：DuckDB 对“同一进程中的多连接读写”支持很好；多个独立进程同时写同一个数据库文件则不是它的常规使用方式。多个 Agent 共享数据程序时，应让它们请求同一个 Go 程序，而不是各自直接打开 `.duckdb` 文件写入。

## 官方资料确认的事实

### Go 客户端和持久化数据库

- 官方客户端是 [`duckdb-go`](https://duckdb.org/docs/current/clients/go)，使用 Go 的 `database/sql` 接口。
- 应使用官方维护的 v2 模块路径 `github.com/duckdb/duckdb-go/v2`，不要继续采用已迁移的 `marcboeker/go-duckdb` 路径。调研时该项目所含 DuckDB 为 `v1.5.5`，对应首个 `duckdb-go` tag 是 `v2.10505.0`；实现时应在 `go.mod` 固定具体版本，不使用不受控的 `latest`。[官方仓库](https://github.com/duckdb/duckdb-go#background)
- 用数据库文件路径打开连接，就会使用持久化数据库；例如 `sql.Open("duckdb", databasePath)`，其中 `databasePath` 由调用方提供。可参考 [`duckdb-go` 的连接示例](https://github.com/duckdb/duckdb-go#opening-a-database)。
- `duckdb-go` 提供 `NewConnector`，可在新连接建立时执行初始化逻辑，例如加载扩展和设置数据库选项。[客户端文档](https://github.com/duckdb/duckdb-go#using-the-connector)也说明了这一点。
- 必须关闭 `Rows`、连接、`DB` 和 Appender 等对象。官方客户端特别说明，未关闭这些对象可能导致 WAL 不能及时同步，进而影响数据落盘。[客户端 README](https://github.com/duckdb/duckdb-go#appender)有此提醒。
- `ExecContext` 和 `QueryContext` 支持 Go 的 `context.Context`。这使 Go 可以给每次 SQL 加超时，并在 Agent 取消任务时中断查询。[连接实现](https://github.com/duckdb/duckdb-go/blob/main/connection.go)。

### Windows 构建条件

`duckdb-go` 默认把预编译的 DuckDB 静态库链接进 Go 二进制文件，这适合发布一个不依赖 DuckDB CLI 的本地程序，但会增大二进制体积。官方仓库同时说明：Windows 构建需要正确版本的 GCC 和相应运行库，例如通过 MSYS2 的 UCRT64 工具链提供。项目面向 Windows 时，这应作为第一个验收项，而不是到发布时才处理。[Windows 安装说明](https://github.com/duckdb/duckdb-go#windows)

### 多 Agent 时的并发边界

DuckDB 官方的并发说明是：

- 同一个进程内，多个连接可以并发读写持久化数据库。DuckDB 用 MVCC 和乐观并发控制处理冲突。
- 多个进程可以同时以只读方式打开同一数据库文件。
- 当任一进程写数据库时，其他进程不应同时写这个文件；跨进程写入是 DuckDB 不建议的模式。

来源：[DuckDB concurrency](https://duckdb.org/docs/current/connect/concurrency)。

这正是 Go 的实际价值：它不是替 Agent 写空间 SQL，而是把多个 Agent 的请求集中到一个进程，避免它们分别持有数据库写连接。

### 地理数据读取和空间计算

- Spatial 是 DuckDB 的扩展，需要先 `INSTALL spatial`，再在每个新数据库会话中 `LOAD spatial`；它不会自动加载。[Spatial overview](https://duckdb.org/docs/current/core_extensions/spatial/overview)
- `ST_Read` 基于 GDAL，可读取多种矢量格式，包括 GeoJSON、GeoParquet 和 Shapefile；`ST_Read_Meta` 可先检查数据元信息。[Spatial functions](https://duckdb.org/docs/current/core_extensions/spatial/functions)
- Shapefile 不是单一文件。除 `.shp` 外，通常还需要同名的 `.shx` 等配套文件，因此采集时应把整个数据集一起保存、记录。[`ST_Read` 文档](https://duckdb.org/docs/current/core_extensions/spatial/functions#st_read)
- Spatial 自带的 GDAL 驱动并不等同于系统 GDAL；应在实际运行环境用 `ST_Drivers()` 核验所需格式是否可读。[GDAL integration](https://duckdb.org/docs/current/core_extensions/spatial/gdal)
- 读取远程 HTTP(S) 数据时，DuckDB 需要 `httpfs` 扩展；对于 Parquet，HTTP Range 请求可减少下载量。[httpfs overview](https://duckdb.org/docs/current/core_extensions/httpfs/overview)

### 写入、备份和资源控制

- DuckDB 支持标准事务 `BEGIN`、`COMMIT` 和 `ROLLBACK`。[事务文档](https://duckdb.org/docs/current/sql/statements/transactions)
- 可用 `EXPORT DATABASE` 导出完整数据库，再用 `IMPORT DATABASE` 恢复到新数据库。这是可靠但对大库偏重的备份方式。[导入导出文档](https://duckdb.org/docs/current/sql/statements/export)
- DuckDB 默认内存限制约为可用内存的 80%；设置临时目录后，默认最多使用可用磁盘空间的约 90%。可按需设置 `memory_limit`、`threads`、`temp_directory`、`max_temp_directory_size`。这些设置应由 Go 程序统一设置，而不是依赖每个 Agent 临时记得写 SQL。[配置项文档](https://duckdb.org/docs/current/configuration/overview)

## 对当前项目的建议

以下是设计建议，不是 DuckDB 强制要求。

### 先做可用版本：一个小型 Go 常驻程序

虽然“一次命令执行一条 SQL”的 CLI 最快，但它会让不同 Agent 启动不同进程，从而重新遇到跨进程写数据库的问题。既然项目已经确认有多个 Agent 和子 Agent 共用数据程序，最小可用版本应直接是一个很小的本地 Go 常驻程序。

它只做以下几件事：

1. 打开调用方显式传入的项目数据库路径，并持有数据库连接池。
2. 每个新连接执行固定初始化：`LOAD spatial`；需要远程文件时再 `LOAD httpfs`；设置统一的内存、线程和临时目录限制。
3. 接收 Agent 提交的原始 SQL 和执行超时。
4. 同一项目的写请求排队执行；读请求可在同一 Go 进程内使用独立连接执行。
5. 关闭查询结果和连接；把结果的有限预览、执行状态和错误返回给 Agent。
6. 对会改变项目数据库的请求，在执行前做备份；成功后由调用方更新 `DATA.md`，并保存重要 SQL。

这不是领域 API。Go 不解释“道路”“缓冲区”或“行政区”；它只负责运行 Agent 提交的原始 SQL、管理连接和协调写入。

### 一个具体流程：`wuhan_university_roads`

1. Agent 找到武汉大学道路数据的 GeoJSON 或 Shapefile，并将数据源和下载时间记到将要保存的 SQL 注释中。
2. Agent 通过 Go 程序提交原始 SQL。例如，Shapefile 已完整放在项目数据目录时：

   ```sql
   BEGIN;

   CREATE OR REPLACE TABLE wuhan_university_roads AS
   SELECT *
   FROM ST_Read('data/raw/wuhan_university_roads/roads.shp');

   COMMIT;
   ```

   这里不能在未核实来源坐标参考系（CRS）时擅自写成 `4326`。若源数据没有可靠 CRS，先用元信息和数据提供方说明确认；需要补充或转换 CRS 时，再把相应 SQL 明确写入这次导入脚本。

3. Agent 在执行请求中明确标记这是“写入项目数据库”。Go 程序先做约定的备份，然后在写队列中执行。若出错则回滚，并返回错误；成功后返回表名、行数、几何列和范围等预览信息。
4. Agent 把这次可复现的获取 SQL 保存到 `sql/`，例如 `sql/20260827_import_wuhan_university_roads.sql`。
5. Agent 更新项目 `DATA.md`：说明原始文件位置、格式、坐标参考系、表名、几何列、行数、范围、来源 URL、下载时间、文件哈希及对应 SQL 文件。
6. 后续分析仍是原始 SQL，例如查询道路长度或与校园边界相交的道路；如果结果只用于回答问题，不必保存。只有执行 `CREATE TABLE AS SELECT` 或 `COPY ... TO ...` 时，才作为项目内新数据记录到 `DATA.md`。

这里的重点是：SQL 的内容由 Agent 决定；Go 只保证多个请求不会无序地抢写同一个数据库文件。

## v1 必须定下来的少数规则

为了尽快开始实现，建议只先定以下规则：

| 问题 | v1 建议 |
| --- | --- |
| 数据库 | 每个项目一个持久化 DuckDB；数据库路径由调用方显式提供。 |
| 进程 | 每个项目一个本地 Go 常驻程序；Agent 不直接打开数据库文件。 |
| SQL | 接受原始 DuckDB SQL；不做领域 SQL 白名单。 |
| 并发 | 写请求单队列；读请求在同一 Go 进程内并发。 |
| 扩展 | 启动时明确加载 `spatial`；实际需要远程数据时明确加载 `httpfs`。 |
| 可取消性 | 每个请求携带超时；调用方取消时取消 Go context。 |
| 变更 | 调用方把请求标为“读”或“写”；写 SQL 使用事务，重要 SQL 保存到 `sql/`。v1 不实现自制 SQL 解析器来验证这个标记。 |
| 备份 | v1 采用完整导出或副本备份，先正确可恢复；数据库变大后再改成增量或快照策略。 |
| 数据说明 | 每次保存或更新数据后更新 `DATA.md`。 |

“只在 prompt 中约束文件和 URL 访问”的决定可以继续保留。需要明确的是，原始 DuckDB SQL 能读取 Agent 所在用户本来有权读取的本地文件，也能在已加载网络扩展后请求远程地址；这不是 Go 能在不限制 SQL 的前提下自动避免的。相关风险可参考 [DuckDB security overview](https://duckdb.org/docs/current/operations_manual/securing_duckdb/overview)。

## 不应在 v1 过早实现的内容

- 领域专用 API 或 SQL 白名单。
- 任务系统、进度页面、分布式队列或多项目服务。
- 自定义 SQL 解析器来猜测一条语句是否写数据。
- 复杂的增量备份、版本控制和自动数据血缘系统。
- 让多个 Go 程序同时写同一个 `.duckdb` 文件。

## 开发前仍需确认的事项

这些事项会影响实现细节，但不需要阻塞最小版本的目录和主流程：

1. 本地进程的调用方式：HTTP、stdio 还是本地 socket。对于 Codex/Claude Code skill，stdio 最轻；若要真正共享一个长驻进程，则本地 HTTP 或 socket 更直接。
2. 备份的精确形式和保留数量：`EXPORT DATABASE`、复制 `.duckdb` 文件，还是两者组合。
3. `DATA.md` 和数据库中的元数据表谁是权威来源；建议先以 `DATA.md` 为人工可读的项目记录，之后再增加可查询的元数据表。
4. 固定的 DuckDB、`duckdb-go`、Spatial 及 GDAL 版本，以及扩展在离线/首次安装时如何提供。DuckDB 扩展与主程序版本需要匹配，不能把“在线安装永远可用”当成前提。[扩展安装说明](https://duckdb.org/docs/current/extensions/advanced_installation_methods)
5. 数据库目录、原始数据目录、导出文件目录和备份目录的具体位置。

## 建议的验收测试

在写真实功能前，做一个小验证程序即可确认核心风险：

1. 创建、关闭、重启 Go 程序后仍可查询同一张 DuckDB 表。
2. 加载 Spatial 后读取一份 GeoJSON、一份 GeoParquet 和一份带完整配套文件的 Shapefile。
3. 两个并发读请求与两个连续写请求都得到正确结果；写请求没有数据库锁冲突。
4. 取消一个长空间分析后，程序仍能继续执行下一条 SQL。
5. 在一次导入前备份，模拟失败或误写后能恢复原数据。
6. 导入 `wuhan_university_roads` 后，`DATA.md` 中的行数、范围和来源 SQL 能与数据库实际查询对应。
