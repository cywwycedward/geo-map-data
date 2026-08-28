# Overture DuckDB profile 性能分析（2026-08-15）

## 结论

本机本次测试的瓶颈是 **远程 GeoParquet 读取路径与缓存状态**，而非最终的小型 GeoParquet 写盘：Boston Places 从 cold-ish 的 `185.389 s` 降至同进程预热后的 `0.098 s`（约 `1,891x`）。Buildings 和 Roads 在 cold-ish 状态下又显示出明显的 CPU 成本，说明几何及列解压/筛选会随工作负载成为第二个主要变量。

这些是单机、单网络/代理路径的一次 profile 诊断，不能外推为 Overture 的固定服务等级或裸网络带宽。

## 方法

- 固定 Overture release：`2026-07-22.0`；DuckDB CLI：`v1.4.5`。
- 用 Overture STAC collection 的 item bbox 选择相交的官方 AWS GeoParquet asset；STAC 请求不计入 profile。这样测量的是 DuckDB 对已确定候选文件的数据获取，而非全局 `*` 通配符的目录枚举。
- 每个查询只投影实际需要的属性和 `geometry`，使用 `bbox`：Places 为点落框；Buildings 和 Roads 为 bbox 相交。
- 输出为本地 ZSTD GeoParquet，避免将 GeoJSON/GDAL 序列化混入远程数据获取主指标。
- cold-ish：新 DuckDB 进程，`enable_external_file_cache = false`、`enable_http_metadata_cache = false`；每种工作负载各运行一次。
- warm：同一 DuckDB 进程先完整跑一次同 SQL，再对第二次 `COPY` 写 profile，并启用两个缓存。预热运行不计入 warm 延迟。
- Profile 配置：

  ```sql
  SET enable_profiling = 'json';
  SET profiling_output = 'results/profiles/<case>.json';
  ```

Profile 的 `latency` 覆盖目标 `COPY` 的远程元数据/Range 读取、Parquet 解压和筛选、投影、输出压缩与落盘；不含扩展安装、STAC 选文件以及结果文件的后验行数/大小核验。

## 工作负载

| Case | 主题/几何 | bbox | STAC asset 源行数 / row group |
| --- | --- | --- | --- |
| Places Boston | `places/place`，点 | `-71.068,42.353,-71.058,42.363` | 4,646,819 / 512 |
| Buildings Boston | `buildings/building`，面 | `-71.068,42.353,-71.058,42.363` | 5,075,585 / 256 |
| Roads Paris | `transportation/segment`，线 | `2.276,48.865,2.314,48.882` | 2,737,822 / 128 |

## Profile 结果

| Case | 缓存 | latency | 累计 CPU | DuckDB 报告读取量 | 累计扫描行数 | 输出行数 / 大小 | 峰值缓冲内存 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Places Boston | cold-ish | 185.389 s | 45.829 s | 4.38 MiB | 74,349,104 | 2,438 / 126.6 KiB | 34.7 MiB |
| Buildings Boston | cold-ish | 415.964 s | 267.066 s | 4.21 MiB | 81,209,360 | 923 / 93.7 KiB | 19.8 MiB |
| Roads Paris | cold-ish | 349.015 s | 578.391 s | 12.78 MiB | 43,805,152 | 4,905 / 510.4 KiB | 32.7 MiB |
| Places Boston | warm | 0.098 s | 0.322 s | 0 B | 74,349,104 | 2,438 / 126.6 KiB | 40.1 MiB |

`cumulative_rows_scanned` 是 DuckDB 在多个列/算子上的累计计数，不能理解为源 asset 的唯一要素数；各 case 的源行数见上表。`cpu_time` 同样会累加并行线程，因此 Roads 的 `578.391 s` 大于 `349.015 s` 的墙钟时间是预期行为，不能将两者相减而得出网络等待时间。

## 执行树观察

每份 profile 的实质工作都集中在 `READ_PARQUET`，其累计 operator CPU 与 root `cpu_time` 一致；`BATCH_COPY_TO_FILE` 没有单独暴露出显著 operator 时间。这表明 DuckDB v1.4.5 的此 profile 会把远程读取、解压、筛选与必要的几何 materialization 汇总在 Parquet scan 中，而不会把“网络”单列为一个算子。

Profile 中 `total_bytes_written = 0`，即该版本的该指标并不统计本地 `COPY` 文件字节；报告中的输出大小来自实际文件系统，不能以 profile 的该字段判断写盘成本。

## 性能归因

1. **缓存未命中时的远程路径是 Places 的主导因素。** Places 的 CPU 只有 45.829 s，而总延迟为 185.389 s；预热后读取量变为 0 B、总延迟仅 0.098 s。输出文件始终只有 126.6 KiB，故本地压缩写盘不可能解释 cold-ish 的三分钟延迟。
2. **Buildings 增加了显著 CPU 成本。** 它返回的行数与输出字节甚至少于 Places，但 CPU 达 267.066 s；复杂面几何、更多 bbox 子字段及 Parquet 解压/筛选使 `READ_PARQUET` 更重。
3. **Roads 的读取量最高。** 12.78 MiB 的实际读取量约为 Places/Buildings 的三倍，输出也最大；线几何和相交筛选使累计 CPU 最高。由于并行累计 CPU 大于墙钟，不能把 578 s 直接视为串行耗时。
4. **输出大小不是下载量。** 三个 cold-ish case 的输出均小于 0.5 MiB，但 DuckDB 需要读 4.21–12.78 MiB 的 Parquet 片段并处理数千万次列/算子扫描计数；“输出文件大小 ÷ 时间”不是带宽。

## 结论与下一步

若业务是交互式重复查询同一地区/asset，应复用 DuckDB 进程与 external file cache，缓存收益可达数量级提升。若业务是一次性区域提取，应优先：固定 release、借 STAC 限制候选资产、只选择必要列、先写 GeoParquet；不要把完整主题目录复制到本地，也不要把 GeoJSON 导出与远程读取混为同一个速度指标。

若要得到可发布的统计结论，下一轮应在稳定网络窗口内每个 case 至少做 3 次 cold-ish 与 3 次 warm，报告中位数、p10/p90，并独立记录代理/网络状态。当前结果更适合作为性能瓶颈与缓存效应的完整诊断。

## 产物

- JSON profiles：`results/profiles/places-boston-cold.json`、`buildings-boston-cold.json`、`roads-paris-cold.json`、`places-boston-warm.json`
- 对应 GeoParquet：`results/places-boston-cold.parquet`、`buildings-boston-cold.parquet`、`roads-paris-cold.parquet`、`places-boston-warm.parquet`

## 依据

- [Overture：核心数据、STAC 与目录](https://docs.overturemaps.org/getting-data/cloud-sources/)
- [Overture：DuckDB 查询与区域提取](https://docs.overturemaps.org/getting-data/duckdb/)
- [DuckDB：Parquet 读取与下推](https://duckdb.org/docs/current/data/parquet/overview)
- [DuckDB：profiling](https://duckdb.org/docs/current/dev/profiling)
