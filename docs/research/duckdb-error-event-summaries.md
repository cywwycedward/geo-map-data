# DuckDB SQL 错误事件摘要调研

> 调研日期：2026-08-29
> 范围：`geodata-serve` 的 NDJSON `error.message` 如何保留 SQL 失败原因，同时不回显原始 SQL、token 或凭据。

## 结论

`error.message` 应返回**经过清理且有长度上限的 DuckDB 原因摘要**，而不是固定文案或完整驱动错误。摘要保留 DuckDB 错误类别、主因与建议行；丢弃 `LINE n:` 和插入符号携带的 SQL 源码上下文，并脱敏 Bearer token、`token=` / `password=` 等赋值以及 URL 用户密码。

这与服务现有的稳定 `code` 分工一致：`code` 用于程序判断，`message` 用于本地人工诊断，二者不依赖对原始错误字符串的分支。

## 一手资料与运行时证据

- DuckDB 官方把 Parquet 的直接读取接口定义为 `read_parquet(...)`（或以 `.parquet` 文件名作为表源），而不是 `ST_Read`。[Parquet overview](https://duckdb.org/docs/lts/data/parquet/overview) [Querying Parquet](https://duckdb.org/docs/current/guides/file_formats/query_parquet)
- `ST_Read` 使用 Spatial 内嵌的 GDAL；可用格式必须由当前运行时的 `ST_Drivers()` 决定。官方明确指出内嵌 GDAL 未必支持系统 GDAL 的全部格式。[ST_Read / ST_Drivers](https://duckdb.org/docs/stable/core_extensions/spatial/functions#st_read) [GDAL integration](https://duckdb.org/docs/current/core_extensions/spatial/gdal)
- 固定的 `duckdb-go/v2 v2.5.6` 把底层错误消息原样存入 `duckdb.Error.Msg`，因此 Go Runtime 能获取原始 DuckDB 原因。[duckdb-go v2.5.6 errors.go](https://github.com/duckdb/duckdb-go/blob/v2.5.6/errors.go#L254-L295)
- 本机 DuckDB `v1.4.5` CLI 对缺表错误输出了错误类别、原因、建议行以及随后 `LINE 1: ...` 的 SQL 上下文。后半段会回显提交的 SQL，不能直接放入 HTTP 事件。

## 已采用的策略

1. 保持现有 `sql_failed`、`backup_failed` 等稳定错误码，不用错误文本驱动控制流。
2. 对未分类的 DuckDB 执行错误：按行收集原因，遇到 `LINE n:`、插入符号或 SQL 关键字开头的上下文即停止；保留前面的原因和建议。
3. 对摘要做凭据脱敏，并限制为 512 个 Unicode 字符。
4. 为普通错误、凭据脱敏以及真实 `httptest` + DuckDB SQL 失败分别建立回归测试。

## 验收标准

- `SELECT * FROM missing_table` 的终态 NDJSON 仍为 `code: "sql_failed"`，但 `message` 包含 `Catalog Error` 和缺失表名。
- 该 `message` 不包含 `LINE 1` 或提交 SQL 的完整上下文。
- 错误文本中的 URL 密码和查询参数 token 不会出现在事件中。
