# geodata-serve refactor ownership research

> 调研日期：2026-08-30
>
> 问题：评估三个拟议的最小重构——(1) 已验证备份工件的 ownership，(2) `Paths` 对服务内部目录布局的 ownership，(3) DuckDB connector 的打开与配置 ownership——并记录约束、可执行的设计选择和验证办法。

## 结论摘要

三个方向都与已接受的 v1 设计一致，但应保持三个不同的责任边界：

1. **Backup artifact** 是 Runtime 的领域工件。Runtime 负责创建、验证、发布、保留和清理；低层 `duckdbutil` 只保留 SQL literal、路径关系等无状态工具。一个 marker 只能表示“通过本服务验证”，不能被描述为防篡改或跨同账号进程的身份凭证。
2. **`Paths`** 是服务布局的单一来源。它应从显式启动参数产生绝对、clean 的路径，并提供 `extensions/`、`duckdb-tmp/`、`server.json` 等派生位置；Runtime、restore 和 CLI 不再各自拼接这些目录。它不应拥有备份工件的生命周期，也不应拥有 DuckDB 连接池。
3. **Connector** 由实际拥有数据库生命周期的 Module 打开和关闭。在线 Runtime 只创建一个 `duckdb.NewConnector` + `sql.OpenDB`，以 connector callback 固定每个新连接的扩展加载和会话设置；backup verification、restore、init 所需的临时数据库仍由各自流程打开，但应复用同一份路径/连接配置规则，而不是共享在线 `*sql.DB`。

## 当前实现的事实

- `bootstrap.Paths` 已经集中保存四个显式路径，并提供 `ExtensionsDir`, `DuckDBTempDir`, `ServerStateFile` 和 `EnsureDirectories`；见 [`paths.go`](../../services/geodata-serve/internal/bootstrap/paths.go#L11-L112)。
- `runtime.New` 又重新做绝对化、`extensions`/`duckdb-tmp` 默认值、目录创建和 DSN 拼接；见 [`runtime.go`](../../services/geodata-serve/internal/runtime/runtime.go#L113-L181)。restore 也重新拼接 `runtimeDir/server.json`、`runtimeDir/duckdb-tmp` 和 `runtimeDir/extensions`；见 [`restore.go`](../../services/geodata-serve/internal/restore/restore.go#L29-L79) 和 [`restore.go`](../../services/geodata-serve/internal/restore/restore.go#L126-L156)。这是布局规则重复，而不是三个不同的业务规则。
- 在线 Runtime、备份验证、restore 和 init 都直接调用 `duckdb.NewConnector`；见 [`runtime.go`](../../services/geodata-serve/internal/runtime/runtime.go#L177-L193)、[`backup.go`](../../services/geodata-serve/internal/runtime/backup.go#L65-L107)、[`restore.go`](../../services/geodata-serve/internal/restore/restore.go#L126-L156) 和 [`extensions.go`](../../services/geodata-serve/internal/bootstrap/extensions.go#L19-L63)。
- 当前 Runtime 已把备份执行、导入验证、marker 写入和 5 份保留放在一起；见 [`backup.go`](../../services/geodata-serve/internal/runtime/backup.go#L19-L63)。但 marker 常量在 [`duckdbutil.go`](../../services/geodata-serve/internal/duckdbutil/duckdbutil.go#L14-L47)，restore 直接解释 marker，导致“什么算已验证工件”的规则跨 package 分散。
- 已接受设计明确要求：写入前完成 `EXPORT DATABASE`，失败则不执行写 SQL；备份放在 `<backup-dir>/<UTC时间>-<请求ID>/`，只保留最近 5 份；restore 导入新数据库后再替换当前数据库。见 [`data-module-v1.md`](../architecture/data-module-v1.md#L21-L31) 与 [`design.md`](../../services/geodata-serve/docs/design.md#L258-L279)。

## 一手资料确认的约束

### DuckDB 导出/导入是目录工件

DuckDB 官方说明 `EXPORT DATABASE` 导出整个数据库（schema、tables、views、sequences）到指定目录，并生成 `schema.sql`、`load.sql` 及数据文件；`IMPORT DATABASE` 再从该目录加载。官方还明确指出导入目标应是 fresh/empty database，因为导出的 plain `CREATE` 会与已有对象冲突。[DuckDB EXPORT/IMPORT DATABASE](https://duckdb.org/docs/current/sql/statements/export)

因此，备份工件的最小可用不变量是：目标目录由服务生成、导出成功后必须存在可导入内容，并在“导入到空临时数据库 + 基本查询”成功后才可被 restore 选中。仅检查目录非空或仅检查 marker，不足以证明 DuckDB 工件可恢复。

DuckDB 官方还建议导出到空的、尚不存在的位置，并说明导出目录内容会被覆盖。[DuckDB storage migration](https://duckdb.org/docs/current/internals/storage#how-to-move-between-storage-formats) 这支持使用每次请求唯一的 staging 目录，避免把已有目录当作新备份目标。

### Connector 的配置和 per-connection callback

固定版本 `duckdb-go` v2.5.6 的官方源码定义：`NewConnector` 创建固定 DSN 配置的 connector；`Connect` 为每个新连接执行 `connInitFn`，callback 失败时连接创建失败。[`duckdb.go` v2.5.6](https://github.com/duckdb/duckdb-go/blob/v2.5.6/duckdb.go#L52-L117) 官方 README 也将 `NewConnector` + `sql.OpenDB` 作为在打开数据库时执行会话初始化的用法。[duckdb-go v2.5.6 usage](https://github.com/duckdb/duckdb-go/blob/v2.5.6/README.md#usage)

Go 的 `database/sql` 规定 `*sql.DB` 是并发安全的连接池，应一次 `OpenDB` 并长期共享；`*sql.Conn` 用完必须 `Close` 归还池；如果 connector 实现 `io.Closer`，`DB.Close` 会调用 connector 的 `Close`。[`database/sql`](https://pkg.go.dev/database/sql@go1.26.5#OpenDB) [`database/sql/driver.Connector`](https://pkg.go.dev/database/sql/driver@go1.26.5#Connector)

由此，Runtime 应拥有在线 `*sql.DB` 和 connector 的整个生命周期。把 connector 暴露给 HTTP adapter、让每个请求自行 `NewConnector`，或让 Runtime 与 restore 共享同一个 `*sql.DB`，都会破坏连接池、关闭顺序或离线 restore 的边界。

### DuckDB 的目录配置是连接配置，不是业务 SQL

DuckDB 的配置表包含 `extension_directory`、`temp_directory`、`file_search_path` 和 `home_directory`；`file_search_path` 决定相对输入文件从哪些根目录解析，未设置时使用工作目录。[DuckDB configuration](https://duckdb.org/docs/current/configuration/overview) [DuckDB importing data](https://duckdb.org/docs/current/data/overview#file-loading-relative-paths)

这意味着：`Paths` 可以提供这些目录的绝对字符串，但 Runtime 的 connector factory 才应把它们编码到 DSN 或 callback，并负责 `LOAD spatial`/`LOAD httpfs`。路径布局与 DuckDB 会话策略相关但不等价，不能把 `Paths` 变成 DuckDB adapter。

DuckDB 官方列出持久数据库旁可能出现数据库文件、WAL 和临时目录；默认临时目录相对于数据库文件。[DuckDB files created](https://duckdb.org/docs/current/operations_manual/footprint_of_duckdb/files_created_by_duckdb) 因此 v1 的 `runtime-dir/duckdb-tmp` 是有意义的显式服务布局，但必须由同一个路径 owner 计算并传给所有临时连接（在线、验证、restore）。

### Go 文件系统操作的安全边界

- `filepath.Abs` 会把相对路径与当前工作目录组合，并对结果执行 `Clean`；`Join` 也会进行 clean，且使用操作系统分隔符。[Go filepath](https://pkg.go.dev/path/filepath@go1.26.5#Abs) 这支持启动边界统一绝对化，避免各 package 根据当时的 CWD 得出不同位置。
- `os.Lstat` 对符号链接返回链接自身而不跟随；这正是检查数据库、备份目录和 marker 是否为 symlink 的正确原语。[Go os.Lstat](https://pkg.go.dev/os@go1.26.5#Lstat)
- `os.RemoveAll` 会递归删除路径及其子项；对它传入的目标必须先做根目录和名称验证。[Go os.RemoveAll](https://pkg.go.dev/os@go1.26.5#RemoveAll)
- `os.WriteFile` 需要多个系统调用，失败中途可能留下部分文件；因此 marker 不应直接用 `WriteFile` 当作跨进程发布协议。[Go os.WriteFile](https://pkg.go.dev/os@go1.26.5#WriteFile)
- `os.Rename` 在非 Unix 平台即使同目录也不保证 atomic；Windows 路径上不能把 rename 单独当成崩溃安全的发布保证。[Go os.Rename](https://pkg.go.dev/os@go1.26.5#Rename)
- `os.MkdirTemp` 生成的目录不会被并发调用者复用，但目录由调用方负责删除。[Go os.MkdirTemp](https://pkg.go.dev/os@go1.26.5#MkdirTemp)

## 方案 1：已验证备份工件的 ownership

### 推荐的最小设计

在 Runtime/backup 内定义内部的 `VerifiedBackup`（可以只是包含绝对目录路径的非导出值），并让同一个 owner 完成如下状态转换：

```text
service-created staging dir
        -> EXPORT DATABASE
        -> import into empty temp DB + SELECT 1
        -> write/validate marker
        -> published verified artifact
        -> retention cleanup
```

- staging 目录只在 `backup-dir` 下创建，名称不接受调用方文本；失败或取消时递归清理 staging。
- 只有完成 DuckDB 导出和临时导入验证后才把目录视为可恢复工件；保留现有 marker 作为持久化“已通过本服务验证”指示，但把 marker 名称/格式的解释和最终工件类型放回 Runtime backup owner。
- cleanup 只删除服务生成名称、位于已解析 `BackupDir` 下、顶层不是 symlink、marker 是 regular file 且内容准确的工件；未知目录和未完成 staging 不能被当作可删除的备份。
- restore 在导入前重新执行工件校验，并导入到新、空的临时数据库；不要把 `IMPORT DATABASE` 对已有数据库的成功当作前提。restore 可以消费 Runtime 定义的校验函数/值，但不能消费在线 Runtime 的连接。
- marker 不是签名。当前 v1 的同用户进程可写同一目录，任何同用户程序都可能伪造目录和 marker；若未来需要真实性，应设计签名/哈希和密钥 ownership，而不是把纯文本 marker 升级描述为安全边界。

不建议在本重构里改成复制 `.duckdb` 文件、增量快照或持久化备份索引；这会改变已接受的 `EXPORT DATABASE`/`IMPORT DATABASE` 语义和恢复验收范围。

### 验证想法

- 真实临时 DuckDB 写请求：断言 SQL 执行前可见一个完整工件，工件包含 DuckDB 导出文件，marker 只在导入验证成功后出现。
- 导出或验证失败/取消：断言调用方 SQL 未执行、没有带合法 marker 的目录、下一次写请求仍可运行。
- 创建 6 个成功写请求：断言只留下 5 个合法、已验证目录；夹杂普通目录、文件、symlink、伪造名称和坏 marker 时，不删除它们也不把它们计入 5 份。
- restore：坏 marker、非目录、symlink、缺失 `schema.sql` 或不可导入目录都在替换当前数据库前失败；成功恢复后当前数据库来自新临时库，旧数据库仍按设计保留。
- Windows smoke：专门覆盖 staging 清理和 marker 发布失败。由于 Go 官方不保证 Windows `Rename` 原子性，测试应接受“未带有效 marker 的残留目录不可恢复”，而不能断言仅靠 rename 就绝对无残留。

## 方案 2：让 `Paths` ownership 服务布局

### 推荐的最小设计

保留 `bootstrap.ResolvePaths(PathOptions)` 作为 CLI 边界，并使返回的 `Paths` 成为服务布局的 canonical value：

- `Database`、`RuntimeDir`、`BackupDir`、`WorkingDir` 在一次解析中转为 absolute + clean；`WorkingDir` 仍须是已存在目录。
- `ExtensionsDir()`、`DuckDBTempDir()`、`ServerStateFile()` 是唯一的派生规则；`EnsureDirectories()` 只创建服务拥有的 runtime、backup、extensions、duckdb-tmp 和 database parent。
- Runtime 构造接收已解析路径（或接收这些 canonical 字段的窄配置），不再自行接受可覆盖的 `ExtensionDir`/`TempDir` 默认值。CLI 直接把同一个 `Paths` 的值传给 Runtime。
- restore 至少使用 `Paths` 的 runtime layout accessors，而不是再次拼接 `runtimeDir/extensions`、`runtimeDir/duckdb-tmp`、`runtimeDir/server.json`。restore 的 `--backup` 仍是所选工件路径，由 backup owner 校验；不要为了复用完整 serve 参数而虚构 `WorkingDir` 或用户项目目录。
- 不把 `DATA.md`、`sql/`、原始数据目录或导出目录加入 `Paths`；服务设计明确说这些是调用方/后续 skill 的责任。

`Paths` 不是“所有路径相关行为”的新万能 package：备份名称/marker 属于 backup owner，DSN/query 初始化属于 connector owner，数据库替换属于 restore owner。这样才能减少重复而不形成浅层 God object。

### 验证想法

- 用含 `.`、`..`、相对路径和 Windows 分隔符的输入调用 `ResolvePaths`，断言四个公共路径均 absolute + clean，且所有 accessor 与 `runtime.New`/restore 实际使用的目录完全一致。
- 断言 `EnsureDirectories` 只创建服务布局，不创建 `DATA.md`、`sql/` 或外部数据目录。
- 在自定义 `WorkingDir` 下、另一个进程 CWD 中执行 `read_csv`，查询必须由 `file_search_path` 指向同一 `WorkingDir`；同时用真实 Spatial/GDAL 执行 `ST_Read('relative.geojson')`。实现验证表明后者在固定 DuckDB `1.4.5` 上不读取 `file_search_path`，而是读取进程 CWD。因此由启动边界 owner（而非 Runtime 连接 setup）调用一次 `os.Chdir`，并在服务关闭后检查 CWD 恢复；Runtime 本身不得在构造或连接回调中改变 CWD。
- 初始化、在线 Runtime、backup verification、restore 都使用相同 extension/temp 目录；删除或移动一个派生目录后，失败必须是明确的启动/恢复错误，而不是悄悄回退到默认用户目录。

## 方案 3：DuckDB connector 打开/配置 ownership

### 推荐的最小设计

保留 Runtime 对在线 connector 的 ownership，并把配置分成两层：

1. **数据库级 DSN 配置**：绝对 `database` 路径、`extension_directory`、`temp_directory`，由一个内部 connector factory 从 canonical path 值构造。
2. **连接级 callback**：每次 `Connect` 时执行 `LOAD spatial`、`LOAD httpfs`，并设置 `file_search_path`/`home_directory` 等 session 选项。callback 不保存 context；请求 context 只用于实际 SQL。

Runtime 的生命周期顺序保持为 `NewConnector` → `sql.OpenDB` → pool limits → `PingContext` → requests → `db.Close`（其 connector 也会被关闭）。backup verification、restore 和 init 使用同样的 factory 规则，但创建自己的短命 `*sql.DB`，并在每条错误路径关闭 `Rows`/`Conn`/`DB`/connector。

不要把 `duckdb.Connector` 放进 HTTP interface，也不要让 `Paths` 直接 import DuckDB。不要为“复用 helper”而引入新的 repository/mock interface；DuckDB 仍是本地可替代依赖，真实临时数据库验证更符合项目测试策略。

当前 `runtime.New` 通过 `os.Chdir(config.WorkingDir)` 改变进程 CWD，再在 callback 中设置 DuckDB search/home directory。重构实测证明 callback 足以支持原生 `read_csv`，但固定 Spatial/GDAL 的 `ST_Read` 忽略这两个 session 设置并按进程 CWD 打开相对路径。因此 `os.Chdir` 不能从行为中删除，只能从 Runtime 移到服务启动边界：`bootstrap.EnterWorkingDirectory` 在启动前一次设置、服务生命周期结束后恢复。这样避免请求级或每连接级的全局状态竞争，并保留已有相对 Spatial SQL 语义。[Go os.Chdir](https://pkg.go.dev/os@go1.26.5#Chdir)

### 验证想法

- 配置扩展目录和临时目录到 `t.TempDir`，打开真实 `.duckdb`，验证启动、每个新连接上的 `spatial`/`httpfs` 可用，以及原生相对文件由 `WorkingDir` 解析；另以启动边界 CWD owner 验证 `ST_Read` 相对 GeoJSON 与关闭后的 CWD 恢复。
- 令 `MaxOpenConns=3`，执行两个并发读加一个写；断言 connector 只在 Module 创建一次而初始化 callback 对每个新底层连接执行。
- 模拟 callback/`PingContext`/SQL/Rows.Close/DB.Close 错误，断言 connector、DB、专用 connection 均不会泄漏；`go test ./...`、`go vet ./...` 和 Windows CGO smoke 是实现阶段的必需检查。
- 备份验证与 restore 必须能够打开独立临时数据库，即使在线 Runtime 尚未启动；反向地，运行中的 Runtime 不应被 restore 复用或覆盖。

## 实施顺序与非目标

建议按以下顺序落地，且每步保持外部 HTTP interface 不变：

1. 先把布局派生集中到 `Paths`，删除 Runtime/restore 的重复拼接；验证路径和启动/恢复失败语义。
2. 再把 connector factory/callback 配置集中，确保在线、验证、restore 的生命周期仍分别关闭。
3. 最后把 marker/验证/保留收束到 Runtime backup owner，并以 staging/坏工件测试锁定发布边界。

本调研不建议同时改变备份格式、保留数量、HTTP 协议、SQL 读写声明、并发上限或恢复时保留旧数据库的语义；这些都已由 v1 设计固定，属于独立决策。

## 本次验证范围

本调研的初始阶段只读取了本地设计与实现，并核对了上述官方 Go/DuckDB 文档；随后实现阶段用本地扩展目录完成了真实 DuckDB 与 Windows smoke 验证，并记录了 Spatial/GDAL 的进程 CWD 约束。实现仍必须按服务 `AGENTS.md` 的要求执行 `go test ./...`、`go vet ./...`、真实临时 DuckDB 场景和 Windows 构建/启动 smoke。
