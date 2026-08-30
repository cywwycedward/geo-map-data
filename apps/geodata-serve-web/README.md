# geodata-serve-web

面向 [`services/geodata-serve`](../../services/geodata-serve/) 的本地 HTTP 测试台。它提供一个浏览器界面，用于充分测试 geodata-serve v1 的 HTTP interface：流式 NDJSON、读写调度、取消与超时、写前备份、值编码与鉴权。

技术栈：**Node.js（仅标准库）+ 原生 HTML/CSS/JS**，零 npm 依赖、零构建步骤。

## 为什么需要一个代理层

geodata-serve 只监听 `127.0.0.1`，**不发送 CORS 头**，且浏览器无法读取本地的 `server.json`（地址 + token 发现文件）。`server.mjs` 同时扮演两个角色：

1. 提供同源静态前端（浏览器无需跨域）；
2. 作为反向代理，读取 `server.json` 并把请求转发给 geodata-serve，注入 `Authorization: Bearer <token>`，并把 NDJSON 原样流式传回（客户端断开时销毁上游连接，从而触发服务端取消）。

代理不改动被测试的服务本身。

## 目录结构

```text
apps/geodata-serve-web/
├── server.mjs              # 静态服务器 + 反向代理 + 备份/恢复辅助端点
├── static/
│   ├── index.html          # 页面结构
│   ├── app.js              # 前端逻辑（调用代理，渲染结果）
│   ├── ndjson.mjs          # NDJSON 解析与状态累积（纯逻辑，可单测）
│   └── style.css
├── scripts/
│   ├── geodata-dev.mjs     # 一键启动时的服务生命周期监督器
│   └── mock-geodata.mjs    # 无 Go 环境时的 UI 预览替身（不含 DuckDB）
└── test/                   # node --test 测试（无第三方测试框架）
```

## 一键启动（推荐）

在仓库根目录首次执行一次：

```powershell
npm install
Copy-Item geodata-serve.local.example.json geodata-serve.local.json
```

编辑未提交的 `geodata-serve.local.json`，为 `database`、`runtimeDir`、`backupDir` 和 `workingDir` 填入绝对路径；`webPort` 可省略，默认是 `8787`。之后日常启动只需：

```powershell
npm run dev
```

该命令会构建服务、通过服务 CLI 初始化 DuckDB 扩展、启动 `geodata-serve`、等待其 `server.json` 和 health 就绪，然后启动本测试台。按 `Ctrl+C` 会先关闭测试台，再调用服务的公开 `/shutdown` 端点完成关闭。

其他统一命令：

| 命令 | 用途 |
| --- | --- |
| `npm run build` | 构建本地服务二进制到已忽略的 `.local/`。 |
| `npm run init` | 仅执行扩展初始化。 |
| `npm run stop` | 通过公开接口关闭该配置对应的服务。 |
| `npm test` | 依次运行 Go 测试/vet 和 Web 测试。 |
| `npm run test:go` / `npm run test:web` | 仅运行相应项目的检查。 |

`Taskfile.yml` 是实际的跨语言命令目录；根 `package.json` 只锁定 Task CLI 并提供 npm 入口。它们不把仓库定义为 npm workspace。真实本地配置、构建二进制和 token 都不会提交。

## 手动启动

```powershell
# 1. 启动 geodata-serve（真实服务，见其 README/docs）
geodata-serve serve `
  --database D:\geo\data.duckdb `
  --runtime-dir D:\geo\runtime `
  --backup-dir D:\geo\backups `
  --working-dir D:\geo\project

# 2. 在 apps/geodata-serve-web 下启动测试台
node server.mjs --runtime-dir D:\geo\runtime

# 可选：启用备份列表与恢复
node server.mjs --runtime-dir D:\geo\runtime --backup-dir D:\geo\backups --database D:\geo\data.duckdb --geodata-serve-bin D:\path\to\geodata-serve.exe

# 3. 浏览器打开 http://127.0.0.1:8787
```

`--runtime-dir` 指向 geodata-serve 写入 `server.json` 的运行目录；代理据此发现地址与 token。服务重启后代理自动读到新的地址与 token。

## 覆盖的测试能力

| 需求 | 界面位置 |
| --- | --- |
| 1. HTTP 接口（health / execute / requests / shutdown） | 总览（健康检查、鉴权、关闭）+ 执行 SQL |
| 2. NDJSON 流式结果（status/schema/row/summary/error） | 执行 SQL：实时状态条、schema、逐行表格、终态卡、原始 NDJSON |
| 3. Spatial / httpfs（GeoJSON / GeoParquet / Shapefile） | 执行 SQL 的“预设”下拉（GeoJSON/Shapefile 用 `ST_Read`，GeoParquet 用 `read_parquet`） |
| 4. 并发调度（2 读 + 1 写 FIFO，写与两个读并行） | “并发”页：同时发起 N 读 + M 写，绘制时间线并统计最大并发 |
| 5. 取消与超时（断连 / 超时 / 关闭） | 执行 SQL 的“取消当前请求”（AbortController → 断连取消）、超时输入框 |
| 6. 写前备份（备份失败不执行 SQL、保留 5 份） | “备份与恢复”页：列出备份目录（verified 标记）；保留策略观察 |
| 7. 安全恢复（验证备份、保留恢复前副本） | “备份与恢复”页“恢复到此备份”（经 CLI 离线执行） |
| 8. 值编码（大整数 / DECIMAL / BLOB / 时间 / 嵌套 / JSON / 非有限浮点） | 执行 SQL 的“值编码”预设组 |
| 9. 鉴权与日志（除 health 外需 Bearer token） | 总览“鉴权验证”（无 token → 401）；日志不记录 SQL/token 由服务保证 |

## 运行测试

```powershell
node --test
```

测试层次：

- `test/ndjson.test.mjs`：NDJSON 解析器与状态累积（纯逻辑）。
- `test/proxy.test.mjs`：代理层——转发、鉴权注入、NDJSON 透传、断连取消上游、config/backups/restore 端点（用进程内 mock geodata-serve）。
- `test/frontend.test.mjs`：前端脚本语法检查与装配检查。
- `test/e2e.test.mjs`：真实进程级串通——`mock-geodata.mjs` + `server.mjs` CLI + 静态资源 + 流式执行 + 关闭。

## 无 Go 环境时预览 UI

没有构建 geodata-serve 时，可用 mock 替身预览界面连线（**不含 DuckDB，不能用于验证 geodata-serve 行为**）：

```powershell
node scripts/mock-geodata.mjs --runtime-dir D:\geo\runtime
# 另开终端
node server.mjs --runtime-dir D:\geo\runtime --backup-dir D:\geo\backups
```

## 限制与说明

- **恢复（restore）不在 HTTP interface 内**：geodata-serve 的恢复是离线 CLI（`geodata-serve restore`），要求服务已停止。本测试台通过 `--geodata-serve-bin` 调用该二进制并回显结果；服务停止会删除 `server.json`，因此离线恢复还必须传入 `--database`。
- **日志**：geodata-serve 的结构化日志输出在它自己的控制台，本工具不捕获；“日志不记录原始 SQL 或 token”由服务实现保证（见其 `docs/`）。
- **GeoParquet / Shapefile 预设**引用相对路径示例文件（如 `testdata/points.geojson`）；需要把 `--working-dir` 指向包含这些文件的目录，或用真实路径替换预设。GeoParquet 使用 DuckDB 原生 `read_parquet`，而 `ST_Read` 只用于当前 Spatial/GDAL 包中可用的矢量驱动。
- 本工具只调用服务的公开 HTTP interface，不读取服务内部状态；`/api/backups` 与 `/api/restore` 是对本地文件系统与 CLI 的辅助能力，不是服务的接口。
