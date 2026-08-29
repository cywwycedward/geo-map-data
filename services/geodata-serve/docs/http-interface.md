# geodata-serve HTTP interface

> 状态：v1 协议。本文定义 skill 和其他本地调用方使用服务时必须遵守的完整 interface。

## 1. 连接发现

服务启动成功后，在传入的 `runtime-dir` 中原子写入 `server.json`：

```json
{
  "interface_version": 1,
  "pid": 18420,
  "address": "http://127.0.0.1:48173",
  "token": "base64url-random-token",
  "database": "D:\\chosen\\path\\data.duckdb",
  "started_at": "2026-08-27T14:30:22Z"
}
```

规则：

- `address` 必须是 `http://127.0.0.1:<随机端口>`；
- token 由 `crypto/rand` 生成，并使用 URL-safe 编码；
- 状态文件属于本地运行状态，不得提交 Git；
- 调用方读取后先调用 `/health`，不能只根据 PID 判断进程可用；
- 服务只删除 PID 和 token 都与自身匹配的状态文件；
- `interface_version` 不兼容时，调用方必须停止并报告版本不匹配。

## 2. 通用规则

### 2.1 鉴权

除 `GET /health` 外，所有请求都必须包含：

```http
Authorization: Bearer <token>
```

缺少或错误 token 返回 `401 Unauthorized`。服务不得在响应或日志中回显 token。

对于受保护路径，鉴权失败优先于 HTTP method 校验；只有携带有效 token 的错误 method 才返回 `405 Method Not Allowed`。

### 2.2 JSON

- 普通请求和非流式响应使用 `application/json; charset=utf-8`；
- `/execute` 成功进入执行流程后使用 `application/x-ndjson; charset=utf-8`；
- 时间统一使用 UTC RFC 3339；
- 时长字段使用整数毫秒，调用方超时字段使用整数秒；
- 未列出的请求字段视为 `invalid_request`，避免拼写错误被静默忽略。

### 2.3 普通错误体

在 NDJSON 流开始前发生的错误使用：

```json
{
  "error": {
    "code": "invalid_request",
    "message": "mode must be read or write"
  }
}
```

HTTP 状态码与错误分类：

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `invalid_request` | JSON、字段、模式、SQL 或 timeout 不合法。 |
| 401 | `unauthorized` | token 缺少或不匹配。 |
| 404 | `request_not_found` | 请求 ID 不存在。 |
| 405 | `method_not_allowed` | 路径存在但 HTTP method 不正确。 |
| 409 | `service_shutting_down` | 服务已进入关闭流程。 |
| 415 | `unsupported_media_type` | JSON 请求未使用正确 Content-Type。 |
| 500 | `internal_error` | 尚未开始流式响应时发生内部错误。 |

## 3. `GET /health`

不需要 token，也不执行 DuckDB SQL。

正常响应：

```http
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
```

```json
{
  "status": "ok",
  "interface_version": 1,
  "service_version": "0.1.0",
  "duckdb_version": "1.4.5",
  "pid": 18420
}
```

关闭过程中返回 `503 Service Unavailable`：

```json
{
  "status": "shutting_down",
  "interface_version": 1,
  "service_version": "0.1.0"
}
```

健康检查只说明当前进程及其启动时已验证的数据库可用，不保证外部 URL 或数据源可访问。

## 4. `POST /execute`

### 4.1 请求

```http
POST /execute HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json
```

```json
{
  "mode": "read",
  "sql": "SELECT name FROM wuhan_university_roads ORDER BY name",
  "timeout_seconds": 600
}
```

字段：

| 字段 | 必需 | 规则 |
| --- | --- | --- |
| `mode` | 是 | 只能为 `read` 或 `write`。Go 信任该声明，不解析 SQL 验证。 |
| `sql` | 是 | 非空原始 DuckDB SQL，原样交给驱动。 |
| `timeout_seconds` | 否 | 正整数；省略时不设置服务级 SQL 超时。 |

`sql_file`、项目名、数据集名等不属于服务 interface。重要 SQL 的保存由 skill 在调用前后完成。

### 4.2 响应开始

请求通过协议验证后，服务立即生成 ID 并开始 NDJSON 响应：

```http
HTTP/1.1 200 OK
Content-Type: application/x-ndjson; charset=utf-8
Cache-Control: no-store
X-Request-ID: req_7f4a91d8
```

从此以后，即使 SQL 或备份失败，HTTP 状态仍保持 200；执行结果由最后一个 NDJSON 事件表示。

### 4.3 NDJSON 事件顺序

允许的事件类型：

```text
status → [status ...] → [schema → row ...] → summary
status → [status ...] → error
```

每行都是一个完整 JSON 对象，以 `\n` 结束。服务可以小批量缓冲 row 事件，但不能累积完整结果；状态、schema 和终态事件应立即 flush。

#### 状态事件

```json
{
  "type": "status",
  "request_id": "req_7f4a91d8",
  "state": "queued",
  "at": "2026-08-27T14:31:00Z"
}
```

流中允许的非终态：

- `queued`；
- `backing_up`，只用于 `write`；
- `running`。

#### 字段事件

只有执行结果包含字段时才发送一次：

```json
{
  "type": "schema",
  "request_id": "req_7f4a91d8",
  "columns": [
    {"name": "name", "duckdb_type": "VARCHAR"},
    {"name": "road_length", "duckdb_type": "DOUBLE"}
  ]
}
```

列名允许重复，因此 row 使用与 `columns` 相同顺序的数组，而不是 JSON object。

#### 行事件

```json
{
  "type": "row",
  "request_id": "req_7f4a91d8",
  "values": ["樱花大道", 812.4]
}
```

#### 成功汇总

成功流必须且只能以一个 `summary` 结束：

```json
{
  "type": "summary",
  "request_id": "req_7f4a91d8",
  "state": "finished",
  "row_count": 1284,
  "queued_ms": 12,
  "execution_ms": 438
}
```

无结果字段的语句不发送 schema 或 row，`row_count` 表示驱动能够可靠报告的返回行数；无法判断受影响行数时使用 `null`，不能伪造为 0。

#### 错误事件

失败或取消流必须且只能以一个 `error` 结束：

```json
{
  "type": "error",
  "request_id": "req_7f4a91d8",
  "state": "failed",
  "code": "sql_failed",
  "message": "DuckDB query failed",
  "queued_ms": 12,
  "execution_ms": 73
}
```

执行阶段错误分类：

| code | state | 含义 |
| --- | --- | --- |
| `backup_failed` | `failed` | 写入前完整备份失败；调用方 SQL 未执行。 |
| `sql_failed` | `failed` | DuckDB 执行失败。 |
| `result_encoding_failed` | `failed` | 结果值无法按协议编码。 |
| `cancelled` | `cancelled` | 客户端断开、显式取消、服务关闭或 context 取消。 |
| `deadline_exceeded` | `cancelled` | `timeout_seconds` 到期。 |
| `internal_error` | `failed` | 流开始后的未分类内部错误。 |

`message` 用于本地诊断，可以包含 DuckDB 的安全错误摘要，但不得包含 token、Go stack trace 或完整原始 SQL。

### 4.4 值编码

schema 中的 `duckdb_type` 保留原始类型，row 值按以下规则编码：

| DuckDB 值 | JSON 表达 |
| --- | --- |
| `NULL` | `null` |
| `BOOLEAN` | boolean |
| `TINYINT`、`SMALLINT`、`INTEGER` | number |
| `BIGINT`、无符号大整数、`HUGEINT`、`DECIMAL` | 十进制 string，避免调用方丢失精度 |
| 有限 `FLOAT`、`DOUBLE` | number |
| 非有限浮点数 | string：`NaN`、`Infinity` 或 `-Infinity` |
| `VARCHAR`、`ENUM`、`UUID` | string |
| 日期、时间、timestamp、interval | DuckDB 的稳定文本表示 string |
| `BLOB`、原始 `GEOMETRY` | `{"encoding":"base64","data":"..."}` |
| `LIST`、`ARRAY`、`STRUCT`、`UNION`、`JSON` | 递归 JSON 值；不能无损表示时终止为 `result_encoding_failed` |
| `MAP` | 字符串 key 编码为 JSON object；其他 key 编码为按 key 文本稳定排序的 `[{"key": ..., "value": ...}]` 数组，保留 key 的 JSON 类型。 |

服务不自动识别或转换几何。需要文本或 GeoJSON 时，调用方 SQL 使用：

```sql
ST_AsText(geom)
ST_AsGeoJSON(geom)
```

## 5. `GET /requests/{request_id}`

需要 token。该端点查询进程内状态，不取得 DuckDB 连接。

```http
GET /requests/req_7f4a91d8 HTTP/1.1
Authorization: Bearer <token>
```

```json
{
  "request_id": "req_7f4a91d8",
  "mode": "write",
  "state": "running",
  "accepted_at": "2026-08-27T14:31:00Z",
  "started_at": "2026-08-27T14:31:02Z",
  "finished_at": null,
  "row_count": null,
  "error_code": null
}
```

终态请求仍可查询到服务退出：

```json
{
  "request_id": "req_7f4a91d8",
  "mode": "write",
  "state": "finished",
  "accepted_at": "2026-08-27T14:31:00Z",
  "started_at": "2026-08-27T14:31:02Z",
  "finished_at": "2026-08-27T14:31:07Z",
  "row_count": 0,
  "error_code": null
}
```

该端点不返回原始 SQL、schema、row 或完整错误文本，也不保证服务重启后仍有记录。

## 6. `POST /shutdown`

需要 token，请求体为空：

```http
POST /shutdown HTTP/1.1
Authorization: Bearer <token>
```

服务接受关闭后先返回：

```http
HTTP/1.1 202 Accepted
Content-Type: application/json; charset=utf-8
```

```json
{
  "status": "shutting_down"
}
```

随后停止接收新执行请求，取消排队和运行请求，关闭 HTTP 与 DuckDB，并删除属于当前进程的状态文件。重复关闭请求可返回相同 202，或在 listener 已停止后连接失败；调用方不能依赖第二次响应。

## 7. 取消语义

v1 没有单独的取消端点。执行请求通过以下任一方式取消：

- 调用方关闭 `/execute` HTTP 连接；
- 可选 `timeout_seconds` 到期；
- 服务关闭；
- 进程收到受支持的终止信号。

调用方若仍需从其他 goroutine 或进程取消，应关闭持有的 HTTP 请求；未来异步任务模式再设计显式取消端点。

## 8. 完整例子

读取请求：

```json
{
  "mode": "read",
  "sql": "SELECT name, ST_AsGeoJSON(geom) AS geometry FROM wuhan_university_roads LIMIT 2"
}
```

响应：

```text
{"type":"status","request_id":"req_a1","state":"queued","at":"2026-08-27T14:31:00Z"}
{"type":"status","request_id":"req_a1","state":"running","at":"2026-08-27T14:31:00Z"}
{"type":"schema","request_id":"req_a1","columns":[{"name":"name","duckdb_type":"VARCHAR"},{"name":"geometry","duckdb_type":"VARCHAR"}]}
{"type":"row","request_id":"req_a1","values":["樱花大道","{\"type\":\"LineString\",...}"]}
{"type":"row","request_id":"req_a1","values":["珞珈山路","{\"type\":\"LineString\",...}"]}
{"type":"summary","request_id":"req_a1","state":"finished","row_count":2,"queued_ms":0,"execution_ms":17}
```

写请求：

```json
{
  "mode": "write",
  "sql": "BEGIN; CREATE OR REPLACE TABLE wuhan_university_roads AS SELECT * FROM ST_Read('data/raw/wuhan_university_roads/roads.shp'); COMMIT;"
}
```

响应先出现 `queued`、`backing_up`、`running`，最后是 `summary`；若备份失败，则以 `backup_failed` 结束且 SQL 不执行。
