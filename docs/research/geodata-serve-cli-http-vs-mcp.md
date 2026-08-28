# geodata-serve：CLI/HTTP 与 MCP 的职责比较

> 调研日期：2026-08-28  
> 问题：`geodata-serve` 为什么先设计为带 CLI 管理命令的本地 HTTP 服务，而不是直接设计为 MCP server？

## 结论

当前方案不是“CLI 和 MCP 二选一”。它实际上分为两部分：

- CLI 管理进程和数据库生命周期：`init`、`serve`、`restore`、`version`；
- `serve` 启动后，Agent 通过 loopback HTTP 提交 SQL、读取 NDJSON 结果、查询状态和关闭服务。

MCP 是 Agent 调用工具的标准协议，CLI 是启动、初始化和恢复本地程序的方式，两者不在同一层。即使以后增加 MCP，扩展安装、服务启动、数据库恢复和版本检查仍然需要 CLI 或其他管理入口。

v1 保留“CLI 管理 + 小型 HTTP 数据协议”是合理的，主要原因是：

1. 多个 Agent 必须共享一个 DuckDB 进程；MCP stdio 默认由每个客户端启动自己的子进程，不能单独保证这一点。
2. MCP Streamable HTTP 可以共享进程，但它本身仍是一个独立 HTTP 常驻服务，仍需解决启动、随机地址、token 和项目数据库路径发现。
3. 当前 v1 要求 SQL 结果无隐式行数上限并逐行流出；标准 MCP `tools/call` 把工具结果放在最终 JSON-RPC response 中，没有标准的“逐行工具结果”语义。SSE 可以发送进度通知，但进度通知不是查询结果数据流。
4. Codex、Claude Code 对 MCP tool 有各自的超时、输出大小和配置规则。直接把大查询结果作为 MCP tool result，会让服务行为受调用客户端限制。
5. `geodata-serve` 还需要被测试程序、恢复工具和未来非 Agent 调用方使用。内部 Runtime 和中立的本地协议先稳定后，再增加 MCP adapter，能够避免把 DuckDB 生命周期与某一版 MCP 协议绑在一起。

这不表示 MCP 不适合本项目。建议把 MCP 记录为 v1 之后的 Agent adapter：它调用同一个 Runtime 或转发到当前 HTTP 服务，不重新打开 DuckDB，也不复制并发、备份和状态逻辑。

## 1. 先纠正“当前是 CLI”的理解

当前设计中的 CLI 只包含：

```text
geodata-serve init ...
geodata-serve serve ...
geodata-serve restore ...
geodata-serve version
```

Agent 不会为每条 SQL 启动一次 CLI 进程。`serve` 会常驻并打开一个持久化 DuckDB；Agent 的 SQL 请求走 `POST /execute`。这是为了让多个 Agent 共用同一个数据库连接池和写队列。具体依据见 [服务设计](../../services/geodata-serve/docs/design.md) 和 [HTTP interface](../../services/geodata-serve/docs/http-interface.md)。

因此实际需要比较的是：

```text
当前：Agent → 本地 HTTP adapter → Runtime → DuckDB
候选：Agent → MCP adapter       → Runtime → DuckDB
```

CLI 仍位于两种方案之外，负责 Runtime 启动前和停止后的操作。

## 2. MCP 能做什么

MCP server 可以暴露 tools、resources 和 prompts；数据库查询是官方明确列出的 tool 使用场景。Tool 通过 JSON Schema 描述输入，模型可以发现并调用；tool result 可以返回文本、结构化 JSON、内嵌资源或资源链接。[MCP Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)

本项目完全可以暴露不受领域限制的原始 SQL，例如：

```json
{
  "name": "query_sql",
  "arguments": {
    "sql": "SELECT class, count(*) FROM wuhan_university_roads GROUP BY class",
    "timeout_seconds": 600
  }
}
```

所以“不采用 MCP 是因为 MCP 会限制 Agent 只能调用固定领域方法”并不成立。MCP tool 可以只做通用的 SQL 传递；是否限制 SQL 取决于服务实现，不取决于 MCP。

Codex 和 Claude Code 都支持本地 stdio MCP server 和 HTTP MCP server。Codex 可在项目或用户配置中注册 command 或 URL，并支持 bearer token、可配置启动超时和 tool 超时；Claude Code 也支持 stdio 与 HTTP，并允许项目级配置。[Codex MCP](https://developers.openai.com/codex/mcp)；[Claude Code MCP](https://code.claude.com/docs/en/mcp)

Go 也已有官方 MCP SDK，因此“不使用 MCP”不能归因于 Go 没有正式实现。[MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)

## 3. 为什么 MCP stdio 不适合作为共享数据库进程

MCP stdio 的标准进程模型是：客户端启动 MCP server 子进程，通过该子进程的 stdin/stdout 交换 JSON-RPC 消息；客户端关闭输入流时，server 随之退出。[MCP stdio transport](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio)

如果 Codex、Claude Code 和另一个独立 Agent 客户端都配置：

```text
command = geodata-serve mcp
```

那么不同客户端进程通常会分别启动自己的 `geodata-serve` 子进程。MCP 规范并不保证这些客户端共用同一个 stdio 子进程；同一宿主内部的子 Agent 是否共享连接也属于宿主实现，不能作为数据安全不变量。

若这些 stdio server 都直接打开同一个 `.duckdb` 并尝试写入，就重新引入了当前 Go 常驻服务要消除的跨进程写入问题。DuckDB 官方支持同一进程内多连接并发；多个进程同时写同一个数据库不是其主要并发模型。[DuckDB concurrency](https://duckdb.org/docs/stable/connect/concurrency)

可以让每个 stdio MCP server 只做一个很薄的代理，读取 `server.json` 后转发给共享的 HTTP Runtime。这样是可行的，但此时架构已经变成：

```text
Codex ── stdio MCP bridge ─┐
Claude ─ stdio MCP bridge ─┼─ loopback HTTP ─ geodata-serve Runtime ─ DuckDB
其他 Agent ────────────────┘
```

它证明 MCP 适合做 adapter，也证明 MCP stdio 不能替代共享核心服务。

## 4. MCP Streamable HTTP 可以共享，但仍不能替代 CLI

MCP Streamable HTTP 的 server 是独立进程，可以处理多个客户端连接；当前规范要求一个 MCP endpoint，每个 JSON-RPC 请求使用 HTTP POST，response 可以是单个 JSON 或该请求专属的 SSE stream。[MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)

因此从“多个 Agent 共用一个进程”看，MCP Streamable HTTP 是可行方案，而且比 stdio 更符合本项目。

但采用它以后仍需回答：

- 谁安装 `spatial` 和 `httpfs`；
- 谁提供数据库、runtime、backup、working directory；
- 谁启动和停止常驻进程；
- 服务未运行时如何执行离线恢复；
- 如何发现每个项目随机端口与随机 token；
- 如何判断旧的状态文件是否对应仍存活的服务。

这些都不是 MCP tool discovery 解决的问题。Codex 和 Claude Code 的 MCP 配置接收 command、URL、header 或环境变量，但当前官方配置方式不会自动读取本项目 `server.json` 中的随机 URL 和 token。若坚持每次启动使用随机端口，就需要预启动脚本、环境变量注入、动态重配或一个稳定的本地 bridge。这是基于两者官方配置格式作出的项目推论。[Codex MCP 配置](https://developers.openai.com/codex/mcp)；[Claude Code MCP 配置](https://code.claude.com/docs/en/mcp)

所以直接采用 MCP Streamable HTTP，不是“删除 CLI 和 HTTP”，而是：

```text
保留 CLI + 常驻 HTTP server
把自定义 /execute 协议替换或补充为 /mcp JSON-RPC 协议
```

## 5. 大结果是当前 HTTP 方案更合适的主要原因

当前 v1 已确认：

- 不设置服务端结果行数上限；
- 不把完整结果集收集到 Go 内存；
- schema 后逐行输出 NDJSON；
- 调用方断开时取消 DuckDB 查询。

MCP Streamable HTTP 可以用 SSE 在最终 response 前发送进度或消息，但标准 `tools/call` 的查询结果仍作为一个最终 `CallToolResult` 返回。MCP 定义了 text、structured content 和 resource link，却没有定义“每一行都是一个可由所有 MCP 客户端逐行消费的 tool-result chunk”。这是根据 transport 与 tools 两部分规范作出的判断。[MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)；[MCP Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)

具体例子：

```sql
SELECT id, class, ST_AsGeoJSON(geometry)
FROM wuhan_university_roads;
```

假设返回十万行：

- 当前 HTTP：服务逐行输出，调用方可以边读边写文件、统计或主动停止；Go 不保存十万行。
- 直接 MCP tool：server 最终需要构造一个 tool result，或者先把结果保存成文件再返回 resource link。

第二种做法中的“保存结果文件并返回资源”当然可以实现，但它随即需要决定结果目录、清理时间、权限、失败恢复和稍后读取。这正是当前 v1 明确推迟的异步/持久化结果设计。

调用端本身也会施加限制：Codex 的 MCP tool 默认执行超时是 60 秒，可配置；Claude Code 对 MCP 输出超过 10,000 tokens 发出警告，默认最大为 25,000 tokens，较大文本结果可能被持久化并替换成文件引用。[Codex MCP 超时配置](https://developers.openai.com/codex/mcp)；[Claude Code MCP 输出与超时](https://code.claude.com/docs/en/mcp#mcp-output-limits-and-warnings)

这不代表 Agent 应把十万行都读进模型上下文。合理 SQL 应尽量聚合、过滤或把大结果写入项目表/文件。但底层数据服务仍应避免因为某个 Agent 客户端的 tool-result 限制而丢失流式能力。

## 6. 生命周期操作不适合全部做成模型工具

MCP tools 是供模型发现并主动调用的能力；规范建议敏感操作应允许用户确认。[MCP Tools user interaction model](https://modelcontextprotocol.io/specification/2026-07-28/server/tools#user-interaction-model)

本服务的几个命令性质不同：

| 操作 | 更合适的入口 | 原因 |
| --- | --- | --- |
| 执行读 SQL | HTTP 或未来 MCP tool | 正常在线数据操作。 |
| 执行写 SQL | HTTP 或未来明确标记为写的 MCP tool | 需要写队列和写前备份。 |
| `init` | CLI | 服务尚未启动，需准备扩展和运行目录。 |
| `serve` | CLI/进程管理器 | 它本身用于创建在线接口。 |
| `restore` | CLI | 要求服务停止，且必须安全替换数据库。 |
| `version` | CLI，同时可在 health/MCP discovery 返回 | 既供人工和脚本使用，也供在线调用方检查。 |

特别是 `restore`：如果服务必须停止才能恢复，就无法只依赖该服务内部的 MCP tool 完成恢复。CLI 是自然且容易自动测试的离线入口。

## 7. 对 Agent 可用性的实际影响

### 当前 skill + HTTP

优点：

- Codex、Claude Code 及普通脚本都能使用；
- skill 可以在运行时读取 `server.json`，适应随机地址和 token；
- SQL 仍是完整原始 DuckDB SQL；
- NDJSON 可逐行处理；
- 不需要用户先把动态 endpoint 写入每种 Agent 的 MCP 配置。

代价：

- skill 需要明确教 Agent 如何启动服务、发送 HTTP 和解释事件；
- Agent 客户端不能通过 `tools/list` 自动发现 `query_sql`；
- Codex 和 Claude Code 的用户权限界面无法天然把该能力显示为统一的 MCP tool。

### 直接 MCP

优点：

- Agent 自动发现工具和参数；
- Codex、Claude Code 可以在自己的工具调用和授权界面中展示调用；
- 标准化 tool error、取消、进度和 server instructions；
- 未来除 SQL 外增加数据库 schema resource 时更自然。

代价：

- stdio 不保证多个独立客户端共用一个进程；
- Streamable HTTP 仍需外部启动和动态发现；
- 长 SQL 受各 MCP host 的 tool timeout 影响；
- 大结果需要截断、分页、保存为 resource，或接受客户端输出限制；
- 需要跟踪 MCP 协议版本、SDK 与 Codex/Claude Code 支持差异；
- 对普通脚本仍可能需要保留 CLI 或 HTTP adapter。

## 8. 推荐的分层方案

### v1

保持当前设计：

```text
geodata-serve CLI
    ├── init
    ├── serve ── HTTP adapter ── Runtime ── DuckDB
    ├── restore
    └── version
```

先证明以下核心行为：

- 持久化数据库能稳定重启；
- 2 个读与 1 个写按规则执行；
- 写前备份和恢复正确；
- 取消能传到 DuckDB；
- GeoJSON、GeoParquet、Shapefile 与 Spatial 可用；
- NDJSON 在大结果下不积压完整结果。

### v1 之后

在不改变 Runtime 的前提下增加 MCP adapter。优先比较两种方式：

1. `geodata-serve mcp --runtime-dir <path>`：每个 Agent host 启动一个轻量 stdio bridge，bridge 读取 `server.json` 并连接唯一 Runtime。bridge 不打开 DuckDB。
2. 在常驻服务上增加 `/mcp` Streamable HTTP endpoint：适合 endpoint 能在 Agent 会话开始前稳定配置，或未来有统一项目进程管理器的情况。

MCP adapter 可以先暴露两个通用工具，而不是领域 API：

```text
query_sql(sql, timeout_seconds?)
execute_write_sql(sql, timeout_seconds?)
```

分成两个工具不是限制 SQL，而是让 Agent host 能清楚区分只读和写入授权；两者都接受原始 DuckDB SQL。MCP adapter 必须复用 Runtime 的读写调度和写前备份，不得自己实现第二套队列。

大结果处理应在增加 MCP adapter 时单独决定：

- 仅返回有明确上限的预览；或
- 要求大结果通过 SQL 写入项目表/文件；或
- 新增持久化结果 resource，并完整设计清理与恢复。

不能在没有记录行为变化的情况下，把当前“无行数上限的 NDJSON”静默改成 MCP tool result 截断。

## 9. 何时应提前采用 MCP

满足以下条件时，可以把 MCP adapter 提前到接近 v1 的阶段：

1. 实际调用方确定只有支持 MCP 的 Agent host；
2. 已决定如何让 Codex、Claude Code 和子 Agent 连接到同一个服务实例；
3. 服务 endpoint/token 能在 Agent 会话开始前配置，或已实现只转发、不打开 DuckDB 的 stdio bridge；
4. 已接受查询结果预览上限，或已经设计 resource/file 结果；
5. MCP tool timeout 能覆盖实际空间分析时长；
6. 愿意把官方 MCP Go SDK作为第二个直接第三方依赖，并维护协议兼容测试。

如果这些条件尚未成立，先稳定 Runtime 与 HTTP seam，再增加 MCP adapter，返工更少。

## 最终判断

当前 v1 不直接以 MCP 为唯一接口，理由充分，但设计文档应该避免简称为“CLI 方案”。准确说法应是：

> `geodata-serve` 是由 CLI 管理生命周期、由 loopback HTTP 提供在线数据能力的本地常驻服务；MCP 是未来可增加的 Agent adapter，而不是 CLI、Runtime 或 DuckDB 进程模型的替代品。

如果后续实测表明 Agent 经 skill 调用 HTTP 的操作成本明显高于 MCP，最合理的演进不是重写 `geodata-serve`，而是在现有 Runtime 前增加薄 MCP adapter。
