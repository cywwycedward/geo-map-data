# Overture Maps DuckDB 指南：指令含义

> 调研范围：解释 Overture Maps 官方「DuckDB」指南中的安装、下载、导出及区域裁剪 SQL；不改变其示例中的数据版本或查询条件。调研日期：2026-08-15。主指南最后更新于 2026-07-24。[Overture 官方 DuckDB 指南](https://docs.overturemaps.org/getting-data/duckdb/)

## 结论摘要

- 这些 SQL 的工作模式是：DuckDB 直接查询 Overture 发布在对象存储中的 GeoParquet，选择所需列、按条件筛选结果，再用 `COPY` 写为本地地理文件；不是先下载整个数据集。[官方指南](https://docs.overturemaps.org/getting-data/duckdb/)
- `spatial` 负责把 GeoParquet 的 `geometry` 当作几何值并通过 GDAL 导出 GeoJSON、GeoPackage、Shapefile 等格式；`httpfs` 或 `azure` 用于访问相应云对象存储。[官方指南：安装与导出](https://docs.overturemaps.org/getting-data/duckdb/)
- 示例 URL 中的 `release/2026-07-22.0` 是固定发布版本，复用时应按所需版本替换路径，而不能把它误解为自动指向“最新”。这是根据 URL 中显式版本目录作出的工程判断。[官方指南示例](https://docs.overturemaps.org/getting-data/duckdb/)

## 1. 连接与读取准备

| 指令 | 含义 | 何时需要 |
| --- | --- | --- |
| `INSTALL spatial;` | 下载并安装 DuckDB Spatial 扩展；通常只需在本机首次使用时执行。 | 要处理/导出几何数据时。 |
| `LOAD spatial;` | 在**当前 DuckDB 会话**注册空间函数和 GDAL 导出能力。 | 每次新会话、且后续用到 `geometry`、`ST_Intersects` 或 `FORMAT GDAL` 时。 |
| `INSTALL httpfs;` / `INSTALL azure;` | 安装读取 Amazon S3 / Azure Blob Storage 所需的扩展。 | 数据源位于对应对象存储时。指南的 Detroit 示例还显式 `LOAD httpfs;`。 |
| `SET s3_region='us-west-2';` | 指定 DuckDB 访问 `s3://overturemaps-us-west-2/...` 时采用的 AWS 区域。 | 读取该 S3 路径之前。 |
| `read_parquet('s3://.../*', filename=true, hive_partitioning=1)` | 从匹配的远程 Parquet 文件集合创建可查询的表源；`*` 匹配多个文件；`filename=true` 额外提供来源文件名；`hive_partitioning=1` 从类似 `theme=places/type=place` 的目录名推导分区列。 | 所有以 Overture 发布目录为来源的查询。 |

以上各项及其用途均来自 Overture 的安装说明和示例。[官方指南：Installation](https://docs.overturemaps.org/getting-data/duckdb/)

## 2. 所有下载示例共有的 SQL 结构

```sql
COPY (
  SELECT <需要的列>
  FROM read_parquet('<发布路径>')
  WHERE <属性条件> AND <空间条件>
) TO '<本地输出文件>' WITH (FORMAT GDAL, DRIVER '<格式驱动>');
```

- `SELECT` 决定输出字段。`names.primary AS name` 是从嵌套 `names` 结构取主名称并起别名；`AS` 不改变原始数据，只改变查询结果的列名。[官方指南：places 示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `CAST(socials AS JSON)` 把嵌套属性转为 JSON，以便 GeoJSON 能序列化该字段；`geometry` 保留为 DuckDB 识别的几何列。[官方指南：places 示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `WHERE` 在写文件前过滤记录。属性条件如 `categories.primary = 'pizza_restaurant'`、`subtype = 'county'`、`country = 'US'` 依次按主题 schema 筛选目标要素。[官方指南：下载示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `COPY (<query>) TO ...` 把查询结果落盘；`FORMAT GDAL` 表示经 GDAL 写出，`DRIVER` 选择格式，例如 `GeoJSON`、`GeoJSONSeq` 或 `GPKG`。[官方指南：下载与格式转换](https://docs.overturemaps.org/getting-data/duckdb/)
- `LIMIT 100` 只保留前 100 行，适合验证查询或限制示例输出，不能表示地理上的完整范围。[官方指南：Detroit buildings 示例](https://docs.overturemaps.org/getting-data/duckdb/)

## 3. 边界框条件的区别

`bbox` 是每个要素的包围盒，包含 `xmin`、`ymin`、`xmax`、`ymax`。指南对点采用前两项定位；对道路则结合四项表达“相交”或“完全包含”。

| 写法 | 选择的对象 | 指南中的原因 |
| --- | --- | --- |
| `bbox.xmin BETWEEN west AND east AND bbox.ymin BETWEEN south AND north` | 位于矩形范围内的**点**。 | 对点而言最小/最大 x、y 是同一坐标；指南的 places 与 peaks 示例如此使用。 |
| `bbox.xmin < east AND bbox.ymin < north AND bbox.xmax > west AND bbox.ymax > south` | 包围盒与矩形有重叠的线/面候选，即“相交”筛选。 | 可选中穿过边界的道路；指南明确说明结果不只包含完全落在框内的道路。 |
| `bbox.xmin > west AND bbox.ymin > south AND bbox.xmax < east AND bbox.ymax < north` | 包围盒完全落在矩形内的线/面。 | 指南将其称为只下载被该框完整包含的道路。 |

这些是包围盒测试；它们依据的是要素范围而非逐顶点的精确几何关系。[官方指南：roads 示例及说明](https://docs.overturemaps.org/getting-data/duckdb/)

## 4. 各示例额外指令

- `CAST(elevation * 3.28084 AS INT) AS elevation_ft`：将高程从米乘以米到英尺的换算系数，截断/转换为整数，并将结果列命名为 `elevation_ft`。[官方指南：mountains 示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `TO 'pennsylvania_counties.gpkg' WITH (FORMAT GDAL, DRIVER 'GPKG')`：将筛出的宾夕法尼亚 county 面要素写入 GeoPackage，而不是 GeoJSON。[官方指南：counties 示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `TO 'places.parquet'`：在读入端已被识别为 GeoParquet 的 `geometry` 会自动成为 `GEOMETRY`；写出时 DuckDB 会写入 GeoParquet 所需元数据。[官方指南：GeoParquet 示例](https://docs.overturemaps.org/getting-data/duckdb/)
- `DRIVER 'ESRI Shapefile'`、`DRIVER 'GPKG'` 或 `DRIVER 'flatgeobuf'`：同一查询可替换 GDAL driver 改变输出格式；每个格式的字段/类型限制仍需针对目标格式验证。[官方指南：格式转换](https://docs.overturemaps.org/getting-data/duckdb/)
- Bash 循环 `for f in *.parquet; do ... done`：逐个本地 Parquet 文件启动 DuckDB，并以 `st_geomfromwkb(geometry)` 将 WKB 字段构造成空间几何后输出 `.fgb`（FlatGeobuf）。`'$f'` 和 `'$f.fgb'` 分别是当前输入、输出文件名。[官方指南：批量转换脚本](https://docs.overturemaps.org/getting-data/duckdb/)

## 5. 区域裁剪（Regional Extracts）

区域示例分三阶段，目的是先用行政区定义范围，再高效筛 Buildings：

1. `SET variable division_id = (...)` 将一个 GERS 行政区 ID 存入 DuckDB 变量；子查询按 `names.primary = 'Marion County'`、`subtype = 'county'` 查出一个 ID。若已知 ID，可直接设置字面量。`getvariable('division_id')` 在后续 SQL 中取回它。[官方指南：Regional Extracts](https://docs.overturemaps.org/getting-data/duckdb/)
2. `CREATE OR REPLACE TABLE bounds AS (...)` 物化该行政区的 `geometry` 和 `bbox`。`OR REPLACE` 表示若同名表已存在则以新结果替换它。[官方指南：Regional Extracts](https://docs.overturemaps.org/getting-data/duckdb/)
3. 将 `bbox` 的四个值和边界几何分别存入变量。Buildings 查询先用四个 `bbox` 条件做快速候选过滤，再用 `ST_INTERSECTS(getvariable('boundary'), geometry)` 保留真正与行政边界相交的要素，最后先写 `extract.parquet`，再转换为 `extract.geojsonseq`。[官方指南：Regional Extracts](https://docs.overturemaps.org/getting-data/duckdb/)

指南给出的性能说明是：在 `WHERE` 中使用变量比与 `bounds` 表连接更快，且先写 GeoParquet、再转最终格式，通常比一步直接写最终格式更快。该结论是该示例的经验说明，应以目标数据量、DuckDB 版本和目标格式实测验证。[官方指南：Regional Extracts 性能说明](https://docs.overturemaps.org/getting-data/duckdb/)

## 已验证的注意事项

- 指南要求 DuckDB 至少为 1.1.0，以读取和写入 GeoParquet；实际使用应检查本机版本并确认安装/加载的扩展与其兼容。[官方指南：概述与安装](https://docs.overturemaps.org/getting-data/duckdb/)
- 范围数值是经纬度坐标示例，`xmin/ymin` 只是快速 bbox 过滤；若需求是“完整包含”或“精确相交”，应采用对应的 bbox 关系，区域示例再加 `ST_INTERSECTS` 进行精确判断。[官方指南：roads 与区域裁剪](https://docs.overturemaps.org/getting-data/duckdb/)
- `release/2026-07-22.0`、主题（如 `places` / `buildings`）和类型（如 `place` / `building`）都写在路径中；查询不同发布或主题时必须同步调整路径、字段名与过滤条件，并参照对应 schema。[官方指南：下载示例与 schema 链接](https://docs.overturemaps.org/getting-data/duckdb/)

## 官方来源

- [Overture Maps: DuckDB guide](https://docs.overturemaps.org/getting-data/duckdb/)
- [DuckDB Spatial extension overview](https://duckdb.org/docs/stable/core_extensions/spatial/overview.html)
- [DuckDB httpfs extension](https://duckdb.org/docs/stable/core_extensions/httpfs/overview.html)
