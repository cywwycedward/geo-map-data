# AGENTS.md

本文件适用于 `services/geodata-serve/` 及其子目录，并与仓库根目录 `AGENTS.md` 一起生效。根目录规则仍是全仓库不变量；本文件只补充 Go 数据服务的局部技术和验证要求。

## 当前阶段

- 当前已进入 v1 实现阶段；实现、测试和文档必须保持与已接受设计一致。
- 开始或继续实现前，先阅读 `README.md`、`docs/design.md`、`docs/http-interface.md`、`docs/development.md` 和仓库级 `docs/architecture/data-module-v1.md`。
- `go.mod` 已存在，Go 验证命令必须以真实命令和实际输出为依据。
- 不要在本目录运行 `git init`；仓库根目录 `.git/` 是唯一 Git 元数据目录。

## 固定设计

- 使用 Go `1.26.5` 和固定版本的 `github.com/duckdb/duckdb-go/v2 v2.5.6`（DuckDB `1.4.5`）；v1 不启用 Arrow build tag。
- 除 DuckDB 官方驱动外，HTTP、CLI、JSON、日志、并发、取消和测试优先使用 Go 标准库。引入新直接依赖前，必须写明标准库为何不足。
- 外部 seam 是仅监听 `127.0.0.1` 的 HTTP interface。HTTP adapter 只负责鉴权、协议转换和流式编码，不直接访问 `*sql.DB`。
- 数据运行 Module 负责 DuckDB 生命周期、2 读 + 1 写调度、写前备份、请求状态、取消和关闭。调用方与测试都从该 Module 的 interface 进入。
- Agent 提交原始 SQL，并显式标记 `read` 或 `write`。不要增加领域方法、SQL 白名单、自制 SQL 解析器、SQL 重写或隐式坐标转换。
- 服务通过显式启动参数接收数据库、运行目录、备份目录和 SQL 相对路径基准；不要在实现中规定用户项目目录结构。
- 写请求必须先成功创建完整备份再执行。服务不替调用方自动包裹事务；多语句原子性由调用方 SQL 中的 `BEGIN` / `COMMIT` 保证。
- 查询结果逐行编码为 NDJSON，不在内存中累积完整结果集，不设置隐式 `LIMIT`。
- 原始 SQL 和 token 默认不得写入日志；日志记录请求 ID、模式、状态、耗时、行数和错误分类。

## Go 实现规则

- 使用 `context.Context` 贯穿 HTTP 请求、排队、备份和 DuckDB 调用；不要把 context 存入长期对象。
- 每次数据库操作都必须关闭 `Rows`、专用连接、connector 和 `*sql.DB` 等对应资源；错误路径同样需要关闭。
- HTTP server 必须使用显式 `http.Server`，配置必要的 header/read/idle 超时，并通过 `Shutdown` 完成关闭流程。
- 写调度和读并发控制集中在数据运行 Module 内，不得在 handler 或多个 package 中各自维护锁和 channel。
- 内部生成的文件路径先清理和转为绝对路径；服务只使用调用方传入的路径根，不自行寻找项目目录。
- 错误在 Module interface 上使用稳定分类，在 HTTP adapter 中映射成协议错误；不要让 handler 依赖 DuckDB 错误字符串作分支判断。
- 使用 `gofmt`；名称应与 `CONTEXT.md` 的领域词汇以及设计文档中的 Module/Interface/Seam/Adapter 术语一致。

## 测试与完成标准

实现开始后，至少执行：

```powershell
go test ./...
go vet ./...
```

提交前还必须：

- 使用 `gofmt` 格式化改动的 `.go` 文件；
- 通过真实临时 DuckDB 文件验证持久化、Spatial、httpfs、2 个并发读、串行写、取消、备份与恢复；
- 通过 `httptest` 验证 HTTP interface 和 NDJSON 事件，不绕过外部 seam 断言 handler 内部状态；
- 在 Windows amd64 + MSYS2 UCRT64 GCC 环境完成至少一次构建和启动验证；
- 若因当前阶段没有某项自动检查，明确记录未执行原因，不得把“文件存在”当作行为验证。

不要为了测试给生产 interface 增加只供测试使用的方法。DuckDB 和文件系统使用 `t.TempDir()` 中的真实本地替代；只有确实存在第二个实现时才增加内部 seam 或 Adapter。
