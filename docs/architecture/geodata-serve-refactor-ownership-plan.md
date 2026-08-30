# geodata-serve 所有权收拢实施设计

> 状态：proposed for implementation
>
> 设计依据：[调研](../research/geodata-serve-refactor-ownership.md)、[数据模块 v1](data-module-v1.md)、[服务设计](../../services/geodata-serve/docs/design.md)。

## 目标与兼容性约束

本设计收拢三个已分散的 implementation：已验证备份、服务路径布局与 DuckDB 打开规则。

下列行为必须保持不变：

- 现有 HTTP interface、NDJSON 事件、CLI 子命令和参数；
- 调用方显式提供数据库、运行目录、备份目录和 SQL 工作目录；
- 原始 SQL、read/write 声明、2 读 + 1 写调度和连接池上限；
- 写前 EXPORT DATABASE、成功后最多保留 5 份、restore 在新数据库导入并保留原数据库；
- DuckDB、文件系统均用真实临时替代测试；不引入 fake adapter、repository interface 或新直接依赖。

CONTEXT.md 中的项目、数据集、分析和地图成果术语不变；这是一项运行 implementation 重构，不新增领域术语。

## 目标结构

    cmd/geodata-serve
      ├── bootstrap.RuntimeLayout / Paths
      │     └── 唯一计算运行目录的 extensions、duckdb-tmp、server.json
      ├── runtime.RuntimeModule
      │     ├── duckdbconn（在线 pool）
      │     └── backup.Store（创建、验证、发布、保留）
      └── restore
            ├── bootstrap.RuntimeLayout
            ├── backup.Artifact（重验备份）
            └── duckdbconn（独立临时 pool）

Paths、duckdbconn 和 backup 都是 concrete module，不是为测试制造的 adapter。HTTP adapter 仍只依赖 Runtime 的既有 interface。

## 1. bootstrap：唯一的服务布局 owner

### 责任

新增或提炼 RuntimeLayout，使其只包含由 runtime-dir 唯一决定的绝对路径：

- RuntimeDir
- ExtensionsDir
- DuckDBTempDir
- ServerStateFile

现有 Paths 保留四个显式启动输入，并组合一个 RuntimeLayout。ResolvePaths 仍是 serve 的 CLI seam；它必须一次完成 absolute + clean、已存在的 WorkingDir 检查和现有 symlink 限制。

为 init 和 restore 提供最小的解析入口：它们只获得自己真正需要的 RuntimeLayout 和数据库路径，绝不为了复用 serve 的完整配置而伪造 WorkingDir、项目目录或备份根。

EnsureDirectories 仍只创建服务拥有的目录和数据库父目录。DATA.md、sql、原始数据与导出目录绝不能进入这个 module。

### 迁移

- Runtime 的构造配置接收已解析 Paths（或等价的 canonical 值），删除 ExtensionDir、TempDir 的可覆盖默认值以及二次 absolute/MkdirAll。
- restore 只通过 RuntimeLayout 取 extensions、duckdb-tmp 与状态文件。
- InstallExtensions 只通过 RuntimeLayout 取得扩展和临时目录。
- 非 bootstrap 代码不得再拼接 runtimeDir/extensions、runtimeDir/duckdb-tmp 或 runtimeDir/server.json。

这会缩小 Runtime 的 interface：caller 不再同时理解用户路径与服务私有布局；布局变更只在一个 module 验证，获得 locality。

## 2. internal/duckdbconn：集中 DuckDB 打开规则

### 责任

增加一个具体的内部 module（建议命名 internal/duckdbconn），集中：

- 从绝对数据库路径、扩展目录、临时目录生成 DSN；
- 为每个新底层连接执行 LOAD spatial、LOAD httpfs；
- 为 SQL 工作目录设置 file_search_path 和 home_directory；
- 设置调用方指定的连接池上限；
- 使 sql.DB 与 duckdb.Connector 有单一、可验证的关闭 owner。

其小 interface 是一个配置值加 Open/Close handle，不暴露给 HTTP adapter。配置值包含 DatabasePath、ExtensionDir、TempDir、WorkingDir、MaxOpenConns 与 LoadExtensions。Runtime 对在线 pool 保持唯一 owner；backup verification、restore、init 各自创建和关闭短生命周期 pool，绝不共享在线 sql.DB。

Runtime.New 不得再调用 `os.Chdir`。已在固定 DuckDB `1.4.5` 与 Spatial 扩展上验证：`file_search_path`/`home_directory` 能解析 DuckDB 原生文件读取（例如 `read_csv`），但 Spatial/GDAL 的 `ST_Read('relative.geojson')` 仍直接使用进程 CWD。因而 `serve` 启动边界必须由 `bootstrap.EnterWorkingDirectory` 一次建立 `WorkingDir`，在服务生命周期结束后恢复；Runtime 的连接 callback 继续设置 session 路径供原生读取。该进程级 owner 只覆盖单项目服务的启动到关闭阶段，不在请求或连接 setup 中反复切换 CWD。

### 迁移

- 将现有 extensionLoader 和四处 DSN、NewConnector、OpenDB 样板迁移到该 module。
- init 使用同一 DSN 规则但不预先加载待安装扩展；Runtime、backup verification、restore 使用每连接扩展加载。
- duckdbutil 只保留无状态 SQL literal、路径关系及同类小工具；不再成为连接生命周期 owner。

这使 DSN、扩展加载和资源关闭拥有一个 deep implementation；一处 DuckDB 配置变化为在线与临时流程提供 leverage。

## 3. internal/backup：已验证备份 artifact module

### 责任

增加 internal/backup，但它仍是 Runtime 的内部 implementation：Runtime 使用它写前备份；restore 只消费其已验证 artifact，不接触在线 Runtime 或其连接池。

该 module 拥有：

- 服务生成的 staging 与最终目录名称；
- EXPORT DATABASE 后对空临时数据库执行 IMPORT DATABASE 加 SELECT 1 的验证；
- marker 的常量、精确内容、regular-file/symlink 检查与“已通过本服务验证”的含义；
- 只针对服务生成且已验证 artifact 的五份保留和安全清理；
- restore 前的 artifact 重新验证与向新临时数据库导入。

建议的内部值是 Artifact：它仅在结构与 marker 均通过检查后才持有绝对路径。它不是安全凭证；同账号进程可伪造 marker，不能将其描述为防篡改机制。

### 发布顺序

    unique staging directory under BackupDir
      → EXPORT DATABASE
      → import into fresh temporary database
      → SELECT 1 succeeds
      → move to service-generated final name
      → write complete verification marker
      → retain only five valid artifacts

任一步失败或取消：

- 调用方 write SQL 不执行；
- 没有可被 Artifact 识别的有效备份；
- staging 或无 marker 残留不得被 restore 使用，也不得作为有效备份计入保留数量。

Windows 不把目录 rename 当作崩溃安全承诺；marker 只能在验证与最终位置都完成后写入。崩溃后残留目录的安全语义是“不可恢复工件”，不是“必定自动清理”。

restore 的替换和旧数据库保留仍属于 restore module：它向新的临时数据库导入 Artifact，验证成功后才替换目标。备份 module 不管理服务是否正在运行，也不直接覆盖当前数据库。

这集中 backup 完整性规则，提升 locality；Runtime 写入和离线 restore 从同一 artifact interface 获得 leverage。

## 实施顺序

1. 建立 RuntimeLayout/Paths 的 canonical 规则及路径测试，迁移 init、Runtime 和 restore。
2. 以测试先行方式引入 duckdbconn，迁移所有 connector 创建点；验证原生相对 SQL 的 session 配置，并以真实 Spatial/GDAL 回归测试锁定由启动边界 owner 建立 CWD、关闭后恢复的约束。
3. 引入 backup artifact module，迁移 Runtime 写前备份、验证、保留及 restore 导入前校验。
4. 删除变更造成的旧 helper、重复 DSN/路径拼接与 marker 常量，更新实现文档；不改 HTTP 协议。

## 可验证验收条件

- bootstrap 测试证明相对、点和父路径输入均产出 absolute + clean 布局，且 init、Runtime、restore 使用同一派生目录。
- serve 在启动边界建立进程 CWD 后，Runtime 通过显式 WorkingDir 读取相对 GeoJSON；Runtime 创建与关闭不再切换 CWD，serve 生命周期结束后恢复原始 CWD。
- 真实临时 DuckDB 验证每个新连接能加载 spatial/httpfs；两个 read 和一个 write 的现有并发行为保持。
- 成功 write 只在完整导出、空库导入验证和 marker 发布后执行 SQL；失败/取消不会创建有效 artifact。
- 六次成功 write 后仅保留五份有效 artifact；普通目录、symlink、坏 marker、无 marker 和伪造名称均不删除、不计数。
- restore 拒绝坏 artifact，并在成功时从新临时数据库替换、保留旧数据库。
- go test ./...、go vet ./...、go build ./cmd/geodata-serve 通过；按 AGENTS.md 做一次 Windows 编译、启动、真实请求、/shutdown 的 smoke 验证。

## 非目标

不改变备份格式或数量，不复制 .duckdb 文件，不增加增量备份、持久化索引、签名，不改变 SQL 权限、HTTP interface、并发数、项目目录约定或领域功能。
