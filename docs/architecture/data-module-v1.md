# 数据模块 v1

> 状态：已接受。本文是 `geodata-serve` v1 的仓库级架构依据；详细实现以 [服务设计](../../services/geodata-serve/docs/design.md)、[HTTP 协议](../../services/geodata-serve/docs/http-interface.md) 和 [ADR-0003](../adr/0003-go-duckdb-geodata-serve.md) 为准。

## 已确认

- v1 的数据能力以供 Codex、Claude Code 等 Agent 使用的 skill 形式提供。
- 该 skill 将 Agent 写出的原始 SQL 交给本地 Go 数据程序执行；Go 与 DuckDB 共同负责项目的数据获取、保存和分析。
- Agent 可以自行编写并执行 DuckDB SQL，包括直接查询 GeoParquet、GeoJSON、Shapefile 等文件，以及使用 DuckDB Spatial 的空间函数。
- 项目使用持久化 DuckDB 数据库保存需要保留的数据、分析结果和项目元数据。
- v1 引入本地 Go 数据服务 `geodata-serve`。多个 Agent 或子 Agent 通过它执行原始 DuckDB SQL，而不是各自直接打开调用方传入的数据库。
- Go 数据程序不限制 Agent 使用的 SQL 或空间函数；它负责打开项目数据库、安排写入顺序、返回执行结果和保存执行记录。
- 每个项目同一时刻只运行一个常驻 Go 数据程序。主 Agent 在数据任务开始时检查并启动它；所有子 Agent 读取本地运行状态并请求该程序，不自行打开数据库或启动服务。主 Agent 在任务结束后关闭服务；服务意外退出时，下次请求通过健康检查发现并由主 Agent 重启。
- 同一项目最多 2 个读 SQL 并行执行，最多 1 个写 SQL 执行；额外读请求排队，写请求始终按顺序执行。Go 的数据库连接池最多保留 3 个连接。读请求不会看到另一写请求尚未提交的中间数据；健康检查和请求状态查询不占用数据库连接。
- 每次请求由调用 Agent 明确标记为读或写。Go 不解析 SQL；标记为写的请求进入写队列，并在备份后执行。prompt 要求 Agent 如实标记。
- 项目初始化时安装 `spatial` 与 `httpfs` DuckDB 扩展。Go 每次创建数据库连接时都明确执行 `LOAD spatial` 和 `LOAD httpfs`；Agent 的业务 SQL 不负责加载扩展。
- `geodata-serve init --runtime-dir <path>` 将 DuckDB 扩展目录设为 `<runtime-dir>/extensions/`，并从 DuckDB 官方扩展仓库安装与固定 DuckDB 版本、当前平台匹配的 `spatial` 和 `httpfs` 扩展。扩展二进制不提交 Git；日常服务启动不安装或下载扩展。未来离线发布时，可将已校验的同版本扩展文件随安装包分发。
- v1 使用 DuckDB 默认的内存上限、线程数和临时磁盘上限，不预先设定机器相关的固定数值。Go 将 DuckDB 临时文件目录设为 `<runtime-dir>/duckdb-tmp/`，避免临时文件散落到系统目录；DuckDB 当前默认最多使用可用磁盘空间的约 90%。
- Go 数据程序直接依赖仅使用官方 `github.com/duckdb/duckdb-go/v2`，固定为 `v2.5.6`（DuckDB `v1.4.5`），并使用 Go `1.26.5`。HTTP、CLI、JSON、日志、取消、并发与测试优先使用 Go 标准库；不在 v1 引入 Web、CLI、日志、配置、任务队列或容器测试框架。
- Windows 构建环境须提供 DuckDB Go 驱动要求的 GCC/MSYS2 工具链。v1 不启用可选的 DuckDB Arrow 构建支持。
- 调用方通过 `--database`、`--runtime-dir`、`--backup-dir` 和 `--working-dir` 明确提供数据库文件、服务运行目录、完整备份目录和 Agent SQL 相对路径的解析基准。`geodata-serve` 不推断项目目录，也不规定这些路径在项目中的位置。
- Go 数据程序使用标准库 `net/http` 提供本地 HTTP 服务，仅监听 `127.0.0.1` 的随机端口，提供 `GET /health`、`POST /execute`、`GET /requests/{request_id}` 和 `POST /shutdown` 端点。运行状态（进程标识、服务地址、随机 token、数据库路径等）保存在 `<runtime-dir>/server.json`，不提交 Git；除健康检查外，子 Agent 使用 token 请求同一服务。
- v1 不设置全局 SQL 超时。Agent 可在请求中选填超时；请求连接断开或 Agent 主动取消时，Go 将请求 context 的取消传给 DuckDB。每次 SQL 使用独立数据库连接，结束、失败或取消后关闭结果和连接；取消写请求时尝试回滚未提交的事务。服务关闭时停止接收新请求并取消未完成 SQL，不实现断点续跑或自动重试。
- `POST /execute` 接收 `mode`（`read` 或 `write`）、原始 `sql` 和可选 `timeout_seconds`，并在响应头中返回服务生成的请求 ID。请求格式错误在流开始前返回 HTTP 400 JSON；SQL 执行错误作为 NDJSON 的最后一行返回。
- 查询结果使用 NDJSON 流式返回：Go 先输出请求状态和字段说明，之后每读取一行 DuckDB 结果就输出一行 JSON，最后输出汇总或错误。Go 不设置返回行数上限，也不在内存中收集完整结果集；如需可阅读的几何，Agent 在 SQL 中明确使用 `ST_AsGeoJSON` 或 `ST_AsText`。
- v1 的 `POST /execute` 同步执行并直接流式返回结果。`GET /requests/{request_id}` 只返回状态、开始/结束时间和错误摘要，不返回查询结果；状态记录只保留到服务退出。异步任务模式（提交请求、查看状态、之后读取持久化结果）作为后续需求，届时再设计结果临时存储、清理、恢复和重试。
- Go 不设置全局坐标轴顺序。DuckDB 的 `ST_Transform` 通过每次函数调用的 `always_xy` 参数决定坐标解释；Go 保持 Agent SQL 原样执行，具体空间转换约定属于后续 skill 设计。
- 数据获取或分析产生需要保留的数据集、分析结果时，保存对应 SQL 到项目的 `sql/` 目录。SQL 文件应写明输入来源和输出表或文件；临时检查查询不需要保存。
- v1 不限制 Agent 可查询的本机路径或网址。prompt 要求 Agent 只访问与用户当前任务相关的数据源；Agent 执行的 SQL 仍具有当前用户账号可用的 DuckDB 文件和网络访问能力。
- Agent 可以直接提交对项目数据库的任意修改。每次会修改数据的 SQL 应使用事务、保存到 `sql/`，并在执行前备份项目数据库。
- v1 在每个写请求执行前使用 `EXPORT DATABASE` 创建完整数据库备份，保存到 `<backup-dir>/<UTC时间>-<请求ID>/`；备份失败时不执行写入。只保留最近 5 份备份，在新备份成功创建后删除更旧备份。恢复时停止 Go 数据程序，将选定备份导入新数据库并验证，不能直接覆盖当前数据库，再重启服务。
- 直接 `SELECT` 外部文件只用于临时读取，不自动复制到项目数据库。Agent 明确执行 `CREATE TABLE ... AS SELECT ...` 等保存语句时，结果才成为项目数据库中的数据。
- 每个用户地理项目维护一份 `DATA.md`。skill 在成功保存或更新项目数据后更新它，记录项目内本地数据文件和调用方所选 DuckDB 数据库中数据表的用途、位置、格式、CRS、几何列、行数、范围、来源、更新时间，以及生成它们的 SQL 文件。
- 保存项目数据时，记录输入文件路径或网址、保存时间、本地文件大小和哈希；Shapefile 记录相关的 `.shp`、`.shx`、`.dbf` 等文件；远程数据记录 URL、release 或 ETag；同时记录 DuckDB 和 Spatial 版本。

## 使用 Go 的原因

- 多个 Agent 或子 Agent 需要通过同一个本地程序访问项目数据；
- 项目 DuckDB 数据库应只由该程序写入，避免多个独立进程同时写同一个数据库文件；
- 后续可由该程序统一管理长时间运行的任务、进度、取消或恢复；
- 后续可将数据能力打包为不依赖本机 DuckDB CLI 的独立程序。

## 留给后续 skill 设计

- `DATA.md` 的填写、校验及其与数据库信息的对应关系属于后续 skill 设计，不在当前 Go + DuckDB 设计中决定；
- 项目目录、数据库表和缓存文件的具体结构属于后续 skill 设计，不在当前 Go + DuckDB 设计中决定；
- 已保存表的字段、几何列、CRS、行数、范围和来源信息如何登记与更新，属于后续 skill 设计，不在当前 Go + DuckDB 设计中决定。
