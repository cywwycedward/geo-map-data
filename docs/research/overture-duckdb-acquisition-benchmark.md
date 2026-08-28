# Overture 数据获取与 DuckDB 小区域测速调研

> 范围：为「使用 DuckDB 从 Overture 获取小区域数据」设计可复现的效率测试；覆盖数据发布、可用获取路径、分区与下推、计时方法和比较边界。调研日期：2026-08-15。除特别标注的本机连通性观察外，结论均来自 Overture 或 DuckDB 的官方资料。

## 结论与测速口径

- Overture 的**正式数据源**是 AWS S3 和 Azure Blob Storage 上、按月发布的 GeoParquet。对小区域数据获取，DuckDB 是直接读取远程 Parquet、做筛选和投影后写本地 GeoParquet 的合适基线；无需也不应先下载整个主题目录。[访问目录](https://docs.overturemaps.org/getting-data/cloud-sources/) [DuckDB 指南](https://docs.overturemaps.org/getting-data/duckdb/)
- 测试应固定 `RELEASE = '2026-07-22.0'`、主题/类型、bbox、列集合、输出格式和 DuckDB 配置。该版本是调研时官方当前发布、schema 为 `v1.18.0`；公开 S3/Azure 桶仅保留约 60 天的两个发布版本，故不能把该固定路径当作长期可用依赖。[发布日历](https://docs.overturemaps.org/release-calendar/)
- “下载速度”至少有两种不同指标，必须分开报告：
  1. **远程扫描**：`SELECT count(*)` 或相同筛选的分析查询的延迟、读取字节和扫描行数，反映请求/远程读取/Parquet 解压与筛选。
  2. **端到端数据获取**：同一筛选 `COPY` 成本地 GeoParquet 的 wall-clock、输出行数、输出字节数，反映实际得到本地数据集的等待时间。

  GeoJSON、GPKG 等 GDAL 输出还包含格式序列化，不能被称为纯远程下载速度。官方也建议先写 GeoParquet、再转换目标格式，因为分两步会显著更快。[DuckDB 区域提取说明](https://docs.overturemaps.org/getting-data/duckdb/)

## 已验证：数据、发布与获取路径

### 正式数据与发布

核心地图数据是 GeoParquet（列式 Parquet 的空间扩展），每个发布包含地图数据、PMTiles、STAC、GERS registry、bridge files 和 changelog；全部托管在 S3 与 Azure。此处测试只针对核心 GeoParquet，不将 PMTiles 的展示读取或完整目录复制混入结果。[访问目录](https://docs.overturemaps.org/getting-data/cloud-sources/)

```text
s3://overturemaps-us-west-2/release/<RELEASE>/theme=<THEME>/type=<TYPE>/*.parquet
https://overturemapswestus2.blob.core.windows.net/release/<RELEASE>/theme=<THEME>/type=<TYPE>/*.parquet
```

目录以 `theme=.../type=...` 作 Hive 风格分区；`*` 扩展为该类型的全部 Parquet 文件。`<RELEASE>` 的格式为 `yyyy-mm-dd.x`，其中末位可表示热修订。月度发布允许 minor schema 变更，季度可能有删除字段或类型变更等 major schema 变更；因此查询须按发布版本校验 schema，不能将今天的列选择无条件套用于未来发布。[访问目录](https://docs.overturemaps.org/getting-data/cloud-sources/) [发布与 schema 政策](https://docs.overturemaps.org/release-calendar/)

| 主题 | 可用 `type` |
| --- | --- |
| `addresses` | `address` |
| `base` | `bathymetry`, `infrastructure`, `land`, `land_cover`, `land_use`, `water` |
| `buildings` | `building`, `building_part` |
| `divisions` | `division`, `division_area`, `division_boundary` |
| `places` | `place` |
| `transportation` | `segment`, `connector` |

这是官方的主题/类型映射；不同类型的几何、属性宽度、密度和文件数不同，不能把跨类型的吞吐直接解释为同一个网络速度。[官方映射](https://docs.overturemaps.org/getting-data/cloud-sources/)

### 获取渠道及其适用性

| 渠道 | 机制 | 在本测试中的定位 |
| --- | --- | --- |
| DuckDB + S3 | `httpfs` 读取 `s3://` GeoParquet，SQL 筛选后 `COPY`。 | **主测对象**。 |
| DuckDB + Azure | `azure` 扩展读取 Azure Blob URL。 | 可作为独立端点对照；不得与 S3 结果混合求平均。 |
| Overture Python client / CLI | `overturemaps download` 支持 bbox，流式输出 GeoJSON、GeoJSONSeq 或 GeoParquet，并默认借 STAC 加速/定位最新发布。 | 可留作工具对照，不应与 DuckDB 实现细节混为同一基线。 |
| STAC | 提供最新发布、每个 theme/type 的空间范围、要素数、列、各 Parquet asset 的 S3/Azure 链接和空间范围。 | 预先选发布/审查范围，或另做「STAC 预选文件」实验。 |
| AWS CLI / AzCopy | 递归复制整个 Parquet 目录。 | 仅适合 bulk download，不适合小区域效率。 |
| BigQuery、Databricks、Snowflake 等镜像 | 由合作伙伴维护，可能有同步延迟。 | 非正式源，不放入本轮 DuckDB/S3 基线。 |

上述渠道、Python CLI 的 bbox/流式语义和正式源/镜像边界分别由 Overture 文档说明。[Python client](https://docs.overturemaps.org/getting-data/overturemaps-py/) [数据镜像](https://docs.overturemaps.org/getting-data/data-mirrors/) [访问目录](https://docs.overturemaps.org/getting-data/cloud-sources/)

STAC 的官方设计是避免把发布路径硬编码：顶层 catalog 通常有 `latest`，而具体 release catalog 记录 schema 版本和各主题；collection/item 还包含 feature count、列及各个文件的 bbox/云链接。[STAC 说明](https://docs.overturemaps.org/blog/2026/02/11/stac/) 对本机在 2026-08-15 的只读连通性检查中，顶层 `https://stac.overturemaps.org/catalog.json` 返回 404，而固定发布的 `https://stac.overturemaps.org/2026-07-22.0/catalog.json` 返回 200。因此，本轮速度实验应显式固定发布；把「取得 latest」作为独立可用性探针记录，不纳入数据读取时间。

## 已验证：DuckDB 读取机制与性能影响

### 最小运行环境

Overture 要求 DuckDB 至少为 1.1.0，以直接读写 GeoParquet。首次安装扩展不应计入基准；每个 DuckDB 会话仍须加载所需扩展。`spatial` 使 GeoParquet `geometry` 成为几何列并支持空间函数/GDAL；S3 使用 `httpfs`，Azure 使用 `azure`。S3 桶位于 `us-west-2`。[Overture DuckDB 指南](https://docs.overturemaps.org/getting-data/duckdb/)

```sql
-- 一次性准备，基准计时之外
INSTALL spatial;
INSTALL httpfs;

-- 每个 S3 测试会话
LOAD spatial;
LOAD httpfs;
SET s3_region = 'us-west-2';
```

`read_parquet(..., hive_partitioning = 1)` 会识别路径中的 `theme=` 和 `type=`。当前 DuckDB 版本可自动推断 Hive 分区；显式写出使基准 SQL 的意图和跨版本行为更清楚。DuckDB 1.3 起 `filename` 也自动作为虚拟列提供，测试不应选择它，除非需要诊断文件分布。[DuckDB Parquet 文档](https://duckdb.org/docs/current/data/parquet/overview)

### 为什么 bbox + 列投影是基线查询

DuckDB 会把投影和筛选下推到 Parquet scan：只读取查询需要的列；若过滤列带有适用的 Parquet zonemap 统计，可跳过不匹配的文件部分。Overture 的数据按 theme/type 分目录、一个全局类型分散为多个文件，并且官方说明文件内 row-group 统计可帮助查询引擎跳过无关 chunk。因此，`SELECT *`、先读取后在客户端筛选、或仅为小样本加 `LIMIT` 都不是公平的小区域数据获取基线。[DuckDB Parquet 下推](https://duckdb.org/docs/current/data/parquet/overview) [Overture STAC 设计](https://docs.overturemaps.org/blog/2026/02/11/stac/)

推荐的空间粗筛是直接引用 `bbox` 子字段：

- **点**（`places`、`address`）：`bbox.xmin BETWEEN west AND east AND bbox.ymin BETWEEN south AND north`；对点而言 min/max 坐标相同。
- **线/面相交**（`segment`、`building`）：`xmin < east AND xmax > west AND ymin < north AND ymax > south`，可保留跨边界要素。
- 需要严格裁剪为行政区或多边形时，先 bbox 粗筛，再加 `ST_Intersects(boundary, geometry)`；这是一项额外几何计算，应另列为“精确裁剪”测试而不能与 bbox-only 结果直接比较。

这是 Overture 自己在 points、roads 和 regional extracts 示例中给出的语义与性能顺序；bbox 不等于精确几何关系。[DuckDB 示例](https://docs.overturemaps.org/getting-data/duckdb/)

## 推荐的小区域实验设计

### 目标和固定条件

目标是获得本机、当前网络下「典型点/线/面主题获取一个小区域」的量级，而非宣称 Overture 或网络的全球固定速度。三个测试各运行 3 次 cold-ish 与 3 次 warm，报告中位数和最小/最大值。

所有样本固定：

- `RELEASE = '2026-07-22.0'`、S3 `us-west-2`、同一个 DuckDB 版本与 `spatial`/`httpfs` 扩展版本；记录 `SELECT version()` 和 `duckdb_extensions()`。
- `SET threads = <固定值>`、相同的 `memory_limit`、相同本地 SSD 目标目录；在结果中记录 CPU、内存、操作系统、网络所在地和开始/结束时间。
- 输出为 `FORMAT PARQUET, COMPRESSION ZSTD`，只取下表列。保留 `geometry` 是为了测得实际可用的空间数据集；不要在主测中写 GeoJSON/GPKG。
- 每次写入唯一的输出/分析文件名，写完后在**本地文件**上 `SELECT count(*)` 和读取文件大小；后者不计入远程获取时间。

| 用例 | 代表的数据 | bbox（west, south, east, north） | 查询列 | 过滤语义 |
| --- | --- | --- | --- | --- |
| P：Boston places | 稀疏/中密度点 | `-71.068, 42.353, -71.058, 42.363` | `id`, `names.primary`, `categories.primary`, `bbox`, `geometry` | 点落框内 |
| B：Boston buildings | 高密度多边形 | 同 P | `id`, `height`, `class`, `bbox`, `geometry` | bbox 相交 |
| R：Paris roads | 线几何 | `2.276, 48.865, 2.314, 48.882` | `id`, `class`, `names.primary`, `bbox`, `geometry` | bbox 相交 |

Boston 的框来自官方 quickstart 的建筑示例，Paris 框来自官方道路示例。三个用例是不同工作负载的代表，**不是**三者谁“更快”的横向排名；要比较面积/密度影响，应在同一 `type` 下等比例扩大 bbox 并保持列与谓词完全一致。[Quickstart](https://docs.overturemaps.org/getting-data/) [DuckDB 道路示例](https://docs.overturemaps.org/getting-data/duckdb/)

以 P 为例，端到端测试 SQL 的查询主体应为（B/R 仅替换路径、列和 bbox 谓词）：

```sql
COPY (
  SELECT id, names.primary, categories.primary, bbox, geometry
  FROM read_parquet(
    's3://overturemaps-us-west-2/release/2026-07-22.0/theme=places/type=place/*.parquet',
    hive_partitioning = 1
  )
  WHERE bbox.xmin BETWEEN -71.068 AND -71.058
    AND bbox.ymin BETWEEN 42.353 AND 42.363
) TO '<unique-output>.parquet'
  (FORMAT PARQUET, COMPRESSION ZSTD);
```

「远程扫描」样本使用与该 `COPY` 相同的 `FROM` 和 `WHERE`（只保留 `count(*)` 所必需的谓词列）；「端到端数据获取」样本运行上面的 `COPY`。两类样本须在**独立进程/独立缓存状态**中运行，不能先在同一 cold-ish 会话执行 `count(*)` 再执行 `COPY`，否则前者会污染后者的缓存。两者回答不同问题，均应保留。

### 冷、暖两种运行

| 运行组 | 做法 | 要回答的问题 |
| --- | --- | --- |
| cold-ish | 每次测试用新的 DuckDB 进程/内存数据库；计时前运行 `LOAD`，但不计 `INSTALL`；`SET enable_external_file_cache = false` 与 `SET enable_http_metadata_cache = false`；写唯一文件。 | 初次请求某一小区域的可感知耗时。操作系统、DNS、TLS 或 CDN 状态仍未必能完全清空，故不得称为严格 cold。 |
| warm | 同一进程、相同 SQL 先跑一次不记录，然后重复 3 次；保持默认 external file cache。 | 连续工作中缓存能提供的体验上限。 |

DuckDB 当前配置中 external file cache 默认开启，而 HTTP metadata cache 默认关闭；显式设置能避免把两种缓存状态悄悄混在同一组中。[DuckDB configuration](https://duckdb.org/docs/current/configuration/overview)

### 记录项和速度计算

使用外部 wall-clock 包住**目标 SQL**，并在 DuckDB 1.5+ 对每次运行输出 JSON profile。官方 profiling 支持 `coverage = 'all'` 和独立保存路径；其指标包括 `LATENCY`、`CPU_TIME`、`CUMULATIVE_ROWS_SCANNED`、`TOTAL_BYTES_READ`、`TOTAL_BYTES_WRITTEN` 及峰值内存。[DuckDB profiling](https://duckdb.org/docs/current/dev/profiling) [指标定义](https://duckdb.org/docs/current/dev/metrics)

```sql
CALL enable_profiling(
  format := 'json',
  save_location := '<unique-profile>.json',
  coverage := 'all',
  metrics := [
    'LATENCY', 'CPU_TIME', 'CUMULATIVE_ROWS_SCANNED',
    'TOTAL_BYTES_READ', 'TOTAL_BYTES_WRITTEN',
    'SYSTEM_PEAK_BUFFER_MEMORY', 'OPERATOR_TIMING'
  ]
);
-- 紧接着执行唯一一条待测 SELECT 或 COPY
```

每个样本至少记录：运行组与序号、开始 UTC、release/schema、云端、theme/type、bbox、谓词、列、输出压缩、DuckDB/扩展版本、`threads`、wall-clock、profile 的 latency/CPU/读取字节/扫描行/内存、输出行数及输出文件字节数。可额外计算：

- `effective scan MiB/s = TOTAL_BYTES_READ / LATENCY`：端到端扫描的有效速率，含远程请求、Parquet 解压和筛选，**不是**裸网络带宽。
- `acquired MiB/s = output_file_bytes / COPY_wall_clock`：实际落地数据集的有效获取速率，含读取、计算和写入/压缩，**不是**云端原始压缩字节的速率。

`EXPLAIN ANALYZE` 可作为诊断而非计时主结果：它会执行查询，提供各算子实际时间/基数，并在多文件读取时显示文件名。多线程时各算子时间是累计时间，不能简单相加为 wall-clock。[EXPLAIN ANALYZE](https://duckdb.org/docs/current/guides/meta/explain_analyze)

## 公平比较与解读边界

1. **筛选语义必须一致。** bbox 相交、bbox 完全包含、`ST_Intersects` 会返回不同要素集合和不同计算量；不能比较它们后将差异归因于网络。
2. **输出格式是主要变量。** `COPY` 到 GeoParquet 与 GeoJSON/GPKG 的速度不是同一件事；若用户关心可视化格式，应在已经完成 GeoParquet 主测后，单独在本地文件上测转换。
3. **属性宽度、几何复杂度、选择率和文件/row-group 命中数均会改变结果。** 报告必须同时给出输出行数和字节数；不要只凭一个 0.01° 方框外推全市或全球。
4. **不要把 `LIMIT` 用于区域完整性测试。** 它会缩短查询且结果未必覆盖整个 bbox，只适合作为连通性 smoke test。
5. **S3 与 Azure 是独立实验。** 端点、客户端扩展、请求路径、网络路由及当时云端负载不同；若比较，应对同一固定发布、相同 SQL 和同一时间窗分别至少重复 3 次。
6. **缓存、并行度和写盘不能隐去。** 明示 cold-ish/warm、`threads`、profile 数据和输出目录所在介质；否则一次短暂的 CDN 命中或慢磁盘可被误说成 DuckDB 性能。
7. **数据会变。** 发布更新会改变 schema、要素数和文件布局；旧发布被移除后重测应新开一组，记录新 release/schema，而非与旧数值求一个不具可比性的平均。

## 执行前检查清单

- `SELECT version();`；确认 DuckDB >= 1.1.0，并保存 `duckdb_extensions()` 中 `spatial`、`httpfs` 版本。
- 在正式计时前运行一次最多 `LIMIT 1` 的 smoke test，验证 S3、`s3_region`、schema 及 `geometry` 读取；不要将其纳入样本。
- 为每个 B/R 用例确认采用 bbox 相交而不是完全包含；对 P 确认是点数据。
- 计时前确认本地磁盘空间充足；每个样本输出到唯一、可追溯的文件。
- 计时后记录本地输出行数/大小、profile 与错误；任何失败样本单列，不以零秒或重试后唯一一次替代。

## 官方来源

- [Overture：发布日历与留存政策](https://docs.overturemaps.org/release-calendar/)
- [Overture：核心目录、theme/type、STAC 与批量下载](https://docs.overturemaps.org/getting-data/cloud-sources/)
- [Overture：DuckDB 获取和区域提取](https://docs.overturemaps.org/getting-data/duckdb/)
- [Overture：Quickstart（Boston bbox、STAC）](https://docs.overturemaps.org/getting-data/)
- [Overture：Python client](https://docs.overturemaps.org/getting-data/overturemaps-py/)
- [Overture：STAC 的文件 bbox/metadata 作用](https://docs.overturemaps.org/blog/2026/02/11/stac/)
- [DuckDB：Parquet 读取、投影/过滤下推](https://duckdb.org/docs/current/data/parquet/overview)
- [DuckDB：profiling](https://duckdb.org/docs/current/dev/profiling)
- [DuckDB：profiling metrics](https://duckdb.org/docs/current/dev/metrics)
- [DuckDB：配置与外部文件缓存](https://duckdb.org/docs/current/configuration/overview)
