# geodata-serve

`geodata-serve` 是面向本地地理项目的 Go + DuckDB 数据服务。它让主 Agent 与子 Agent 通过同一个本地进程执行原始 DuckDB SQL，并统一处理连接、并发、写前备份、取消、状态和流式结果。

当前目录包含 geodata-serve v1 的 Go module、Runtime Module、HTTP adapter、启动准备和离线恢复实现。

若要连同本仓库的 HTTP 测试台一键启动服务，请从仓库根目录按 [`apps/geodata-serve-web` 的一键启动说明](../../apps/geodata-serve-web/README.md#一键启动推荐) 配置后运行 `npm run dev`。

## 文档

- [设计实现](docs/design.md)：目标、Module、seam、运行流程、状态、备份、生命周期和实现阶段。
- [HTTP interface](docs/http-interface.md)：端点、请求、NDJSON 事件、状态和错误语义。
- [开发规范](docs/development.md)：工具链、依赖、计划目录、编码规则和验证矩阵。
- [局部 Agent 说明](AGENTS.md)：本目录下所有后续开发必须遵守的约束。

仓库级设计依据是 [数据模块 v1](../../docs/architecture/data-module-v1.md)，技术选择依据见 [Go + DuckDB 调研](../../docs/research/go-duckdb-embedded-data-module.md) 和 [Go 第三方库调研](../../docs/research/go-duckdb-go-third-party-libraries.md)。

## v1 范围

- 每个地理项目运行一个本地常驻进程，并打开一个持久化 DuckDB 数据库。
- Agent 自行编写原始 SQL；服务不提供领域查询方法，也不解析或改写 SQL。
- 每个项目最多同时执行 2 个读请求和 1 个写请求；写请求串行。
- 写请求执行前使用 `EXPORT DATABASE` 创建完整备份。
- 查询结果通过 NDJSON 同步流式返回。
- `spatial` 与 `httpfs` 由初始化流程安装，由每个新连接加载。

项目目录、`DATA.md`、重要 SQL 的保存规则以及数据登记方式由后续 skill 设计，不由本服务决定。
