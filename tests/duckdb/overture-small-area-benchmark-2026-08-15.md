# Overture 小区域 DuckDB 获取测速（2026-08-15）

## 口径

- 数据版本：`2026-07-22.0`（当日 Overture 当前发布）。
- 客户端：DuckDB CLI `v1.4.5`，已安装 `httpfs` 与 `spatial` 扩展。
- 数据源：STAC 返回的 Overture 官方 AWS HTTPS GeoParquet asset；而非对整个 `type=place` 目录使用通配符。这样先按 STAC item 的 bbox 选出候选文件，再由 DuckDB 做 Parquet bbox 谓词下推。
- 查询列：`id`、主名称、主分类、`bbox`、`geometry`；输出为本地 ZSTD GeoParquet。
- 计时：DuckDB CLI 的 `.timer` 围住单条 `COPY`。扩展安装、STAC 查询、结果行数/文件大小核验均不在计时内。
- 缓存：每个正式样本使用新 DuckDB 进程，并设置 `enable_external_file_cache = false` 与 `enable_http_metadata_cache = false`。这只能称为 **cold-ish**：DNS、TLS、CDN 和操作系统缓存无法由该测试完全清空。

## 结果

| 区域与 bbox（west,south,east,north） | STAC 候选资产 | 输出行数 | GeoParquet 大小 | `COPY` 时间 |
| --- | ---: | ---: | ---: | ---: |
| Boston places `-71.068,42.353,-71.058,42.363` | 1 | 2,438 | 129,646 B | 127.339 s |
| Paris places `2.294,48.850,2.304,48.860` | 1 | 901 | 53,174 B | 7.548 s |
| Tokyo places `139.755,35.675,139.765,35.685` | 1 | 1,834 | 102,091 B | 10.810 s |

生成的数据位于同目录的 `results/`：`boston_places.parquet`、`paris_places.parquet`、`tokyo_places.parquet`。另有一次未计入表格的同会话 NYC 热缓存检查：6,411 行、331,644 B、5.937 s；它和 Boston 使用同一远程 asset，因此不能作为独立 cold-ish 样本比较。

## 解读

这些数值是本机、当时网络和代理路径下的端到端等待时间，不是 Overture 的固定网络带宽。输出文件只有约 52–127 KiB，但 DuckDB 仍须读取远程 Parquet 元数据和命中的 row group，因而不能用“输出文件大小 ÷ 时间”称作下载带宽。Boston 首次完整远程 `COPY` 的 127 秒也说明冷启动/对象读取路径的波动可以远大于小区域结果本身。

探索时曾手动测试一个不相交的相邻 Paris 资产，它返回 0 行；随后通过脚本的 STAC bbox 筛选确认该区域只需一个资产。表格使用的是这个正确单资产查询的结果，而不是包含该多余文件的 30.059 秒探索性运行。

若要获得可用于趋势比较的指标，应在同一网络窗口内分别运行每个样本至少 3 次 cold-ish 与 3 次 warm，并记录中位数、最小/最大值、DuckDB profile 的 `TOTAL_BYTES_READ` 和 `LATENCY`。可用 [run-overture-small-area-benchmark.ps1](run-overture-small-area-benchmark.ps1) 重跑；先执行 `-PlanOnly` 可只查看 STAC 会选中的文件。

## 依据

- [Overture：发布日历](https://docs.overturemaps.org/release-calendar/)
- [Overture：目录、STAC 与 theme/type](https://docs.overturemaps.org/getting-data/cloud-sources/)
- [Overture：DuckDB 查询示例](https://docs.overturemaps.org/getting-data/duckdb/)
- [DuckDB：Parquet 投影与过滤下推](https://duckdb.org/docs/current/data/parquet/overview)
