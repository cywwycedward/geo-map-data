# DuckDB Spatial 功能调研

> 调研问题：DuckDB 的空间数据能力有哪些、边界在哪里，以及在项目中应如何选型和使用？<br>
> 调研日期：2026-07-29。本文以 DuckDB **current 1.5** 文档为准；只采用 DuckDB 官方文档、官方博客、官方维护的源码仓库和官方社区扩展目录。函数清单会随发布演进，落地前应以运行时 `SELECT * FROM ST_Drivers()` 和对应版本文档复核。

## 结论摘要

- DuckDB Spatial 是官方维护的**矢量**空间扩展：它提供空间函数、GDAL 矢量 I/O、PROJ 坐标转换和 R-tree 索引；在 v1.5 中，`GEOMETRY` 类型本身已下沉为 DuckDB 内建类型，但大多数计算函数仍需加载 `spatial`。因此，“列能存 `GEOMETRY`”不等于“不需要 `LOAD spatial`”。[Geometry Data Type](https://duckdb.org/docs/current/sql/data_types/geometry) [Spatial Extension](https://duckdb.org/docs/current/core_extensions/spatial/overview)
- 应将其看作“分析型 SQL 中的空间矢量处理层”：可把普通事实表、Parquet/GeoParquet、JSON 和地理要素放在同一查询里做过滤、关联、聚合、转换与导出；不应把它误当成原生栅格引擎或完整 GIS 服务器。[Spatial extension introduction](https://duckdb.org/2023/04/28/spatial) [GDAL integration limitations](https://duckdb.org/docs/current/core_extensions/spatial/gdal)
- v1.5 是重要分界线：`GEOMETRY` 的持久化改为基于小端 WKB，可做 shredding、行组 extent/类型统计和 CRS 类型参数；这些存储/优化收益要求数据库 storage version 至少为 v1.5。[Geometry storage and optimization](https://duckdb.org/docs/current/sql/data_types/geometry)
- 最大的正确性风险是**单位、CRS 与轴顺序**。普通 `ST_Area`、`ST_Distance`、`ST_Buffer` 是平面计算，单位取决于输入 CRS；椭球函数则对 EPSG:4326 和轴顺序有明确要求，并且 v1.5 正在通过 `geometry_always_xy` 迁移到更常见的 `x=longitude, y=latitude` 语义。[Spatial function reference](https://duckdb.org/docs/current/core_extensions/spatial/functions) [DuckDB 1.5 spatial changes](https://duckdb.org/2026/03/09/announcing-duckdb-150)

## 1. 架构、安装与版本定位

### 1.1 分层模型

当前能力可以分成四层：

| 层 | 责任 | 关键接口 | 适用判断 |
| --- | --- | --- | --- |
| DuckDB 核心类型层 | `GEOMETRY` 的存储、统计、CRS 类型参数 | `GEOMETRY`、`GEOMETRY('OGC:CRS84')`、`&&` | v1.5 起可作为其他扩展共享的几何值类型；多数空间计算不在此层。[官方类型文档](https://duckdb.org/docs/current/sql/data_types/geometry) |
| `spatial` 扩展 | Simple Features 风格的谓词、构造、量测、拓扑/处理、投影、MVT、R-tree | `ST_*`、`CREATE INDEX ... USING RTREE` | 常规矢量空间分析的主体；扩展本身不是 autoloadable。[官方概览](https://duckdb.org/docs/current/core_extensions/spatial/overview) [R-tree 文档](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes) |
| 外部矢量 I/O | GDAL 读取、导出；原生 Shapefile、OSM PBF 路径 | `ST_Read`、`ST_Read_Meta`、`ST_ReadSHP`、`ST_ReadOSM`、`COPY ... FORMAT GDAL` | 格式和驱动能力以当前打包的 GDAL 与 `ST_Drivers()` 结果为准。[GDAL integration](https://duckdb.org/docs/current/core_extensions/spatial/gdal) [table functions](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 栅格（另选） | 官方社区 `raster` 扩展把栅格切片/波段作为 datacube 表处理 | `RT_Read`、`RT_ReadCells`、`RT_Cube*`、`COPY ... FORMAT RASTER` | **不是** `spatial` 的组成部分；需另装 `raster FROM community`，并评估社区扩展的维护与发布兼容性。[官方社区 raster 页面](https://duckdb.org/community_extensions/extensions/raster) |

`spatial` 使用 GDAL 处理矢量格式，并随扩展打包自己的 GDAL；`ST_Transform` 由 PROJ 支持，并使用扩展自带的静态坐标系数据库。这样可避免系统级依赖，但也意味着可用 GDAL 驱动、PROJ CRS 集合可能与机器上另装的 GIS 工具不同。[GDAL integration](https://duckdb.org/docs/current/core_extensions/spatial/gdal) [ST_Transform](https://duckdb.org/docs/current/core_extensions/spatial/functions)

### 1.2 安装、加载与最小探针

官方安装语法如下；`LOAD` 是每个会话都应显式执行的步骤，因为 `spatial` 不会自动加载。[Spatial Extension](https://duckdb.org/docs/current/core_extensions/spatial/overview)

```sql
INSTALL spatial;  -- 首次从 DuckDB 扩展仓库安装
LOAD spatial;     -- 当前连接注册 ST_*、GDAL、R-tree 等能力

SELECT duckdb_proj_version();
SELECT * FROM ST_Drivers();  -- 不假定任一 GDAL driver 必然存在
```

DuckDB 将 `spatial` 列为 DuckDB 团队维护的 *Secondary* core extension，即 best-effort 支持层级，仍会随新版本发布修复与更新；生产部署应锁定 DuckDB/扩展版本并做回归测试，而非依赖 nightly 行为。[Core extensions support tiers](https://duckdb.org/docs/current/core_extensions/overview)

### 1.3 v1.5 的实质变化

在 v1.5 前，`GEOMETRY` 属于 `spatial`；v1.5 后该逻辑类型进入 DuckDB 核心，故其他扩展可原生生产/消费几何值，但面积、距离、相交等绝大多数空间函数仍属于 `spatial`。该变更也让存储引擎和优化器能处理几何列的专用压缩、统计与 CRS 信息。[v1.5 release note](https://duckdb.org/2026/03/09/announcing-duckdb-150) [Geometry Data Type](https://duckdb.org/docs/current/sql/data_types/geometry)

对于已有数据库，storage version 低于 v1.5 时，旧自定义二进制会在存储层自动转换，但不会获得持久几何统计等新收益；不要把 `GEOMETRY` 内部 bytes 当成对外稳定协议。[Geometry storage compatibility](https://duckdb.org/docs/current/sql/data_types/geometry)

## 2. 几何数据模型、类型与序列化

### 2.1 `GEOMETRY` 语义

`GEOMETRY` 概念上遵循 Simple Features 数据模型，可表达 7 类：`POINT`、`LINESTRING`、`POLYGON`、`MULTIPOINT`、`MULTILINESTRING`、`MULTIPOLYGON` 和可混合/嵌套的 `GEOMETRYCOLLECTION`；WKT 是其文本表示，字符串可显式转换为 `GEOMETRY`。[Geometry types](https://duckdb.org/docs/current/sql/data_types/geometry)

```sql
CREATE TABLE feature (
  id BIGINT,
  geom GEOMETRY
);

INSERT INTO feature VALUES
  (1, 'POINT (121.4737 31.2304)'::GEOMETRY),
  (2, 'POLYGON ((121 31, 122 31, 122 32, 121 32, 121 31))'::GEOMETRY);

SELECT id, ST_GeometryType(geom), ST_AsText(geom) FROM feature;
```

坐标维度可为 2D、`Z`、`M` 或 `ZM`，但**同一个几何及其 collection 内所有顶点必须维度一致**；多数几何函数除非特别说明，通常只使用 X/Y。空几何（如 `POINT EMPTY`）是合法值，特别适合表示无相交的拓扑结果。[多维与空几何](https://duckdb.org/docs/current/sql/data_types/geometry)

除通用类型外，函数还有 `POINT_2D`、`LINESTRING_2D`、`POLYGON_2D`、`BOX_2D`/`BOX_2DF`、`POINT_3D`、`POINT_4D` 等专用参数/返回类型重载；例如 `ST_Point2D` 返回 `POINT_2D`，`ST_Extent` 返回 `BOX_2D`。写通用数据表时优先用 `GEOMETRY`，在热点表达式中再利用具体函数的专用重载。[Spatial function signatures](https://duckdb.org/docs/current/core_extensions/spatial/functions)

### 2.2 内部存储、WKB 与稳定边界

`GEOMETRY` 在内部以类似 `BLOB` 的字节序列存储；截至 storage version v1.5，持久格式基于小端 WKB，但官方明确说明**精确内部格式尚未稳定**、未来可变。因此跨系统、长期归档和外部接口必须使用 `ST_AsWKB`/`ST_GeomFromWKB` 或 WKT/GeoJSON，而不是直接 cast 内存/内部 bytes。[Geometry storage](https://duckdb.org/docs/current/sql/data_types/geometry) [WKB functions](https://duckdb.org/docs/current/core_extensions/spatial/functions)

| 方向 | 推荐函数 | 备注 |
| --- | --- | --- |
| WKT → `GEOMETRY` | `ST_GeomFromText(wkt)` 或 `wkt::GEOMETRY` | 可传 `ignore_invalid`；WKT 是 SQL 调试和 fixture 的易读格式。[函数参考](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| WKB/`BLOB` → `GEOMETRY` | `ST_GeomFromWKB(wkb)` | 输入可为 `WKB_BLOB` 或 `BLOB`。[函数参考](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| `GEOMETRY` → WKB | `ST_AsWKB(geom)` | 返回 `WKB_BLOB`；必要时再 `::BLOB`。[函数参考](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| HEX WKB ↔ `GEOMETRY` | `ST_AsHEXWKB` / `ST_GeomFromHEXWKB` | 当前不区分 WKB 与 EWKB，`...HEXEWKB` 是别名，不应假定 SRID/EWKB 差异会被保留。[函数参考](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| GeoJSON geometry fragment ↔ `GEOMETRY` | `ST_GeomFromGeoJSON` / `ST_AsGeoJSON` | 后者返回 geometry fragment 而非完整 Feature；Z 可输出，M 会被忽略；需 JSON 扩展来组装 Feature/FeatureCollection。[函数参考](https://duckdb.org/docs/current/core_extensions/spatial/functions) |

### 2.3 v1.5 存储优化

当一个 row group 的值均为同一几何子类型且顶点维度一致时，DuckDB 可将其拆成 `STRUCT`、`LIST`、`DOUBLE` 的 shredding 段并独立压缩；`GEOMETRYCOLLECTION`、空几何、混合子类型或过小行组不会 shredding。阈值默认约为最大 row group 的 25%（约 30,000 行），可由 `geometry_minimum_shredding_size` 设置为 `0`（总是）或 `-1`（关闭）。[Shredding and compression](https://duckdb.org/docs/current/sql/data_types/geometry)

这是一项**存储压缩**优化，不能据此假设所有空间函数都直接在拆分列上矢量执行；官方说明目前仍需在执行前重组，未来才计划直接暴露 shredded 表示。[Geometry storage roadmap](https://duckdb.org/docs/current/sql/data_types/geometry)

## 3. CRS、投影与地球量测

### 3.1 CRS 是类型信息，而不是可忽略的标签

v1.5 可写 `GEOMETRY('OGC:CRS84')`，把 CRS 作为列/表达式类型的一部分。默认只登记少量 CRS，而加载 `spatial` 会登记 EPSG 数据集中的 7,000 余个；可用 `duckdb_coordinate_systems()` 查看。多输入空间函数通常会检查 CRS 一致性，且两个带不同显式 CRS 的 `GEOMETRY` 列不能隐式互转，这能阻断“经纬度与米制坐标直接相交”的静默错误。[CRS type system](https://duckdb.org/docs/current/sql/data_types/geometry)

```sql
LOAD spatial;

CREATE TABLE poi (
  id BIGINT,
  geom GEOMETRY('OGC:CRS84')
);

INSERT INTO poi VALUES (1, 'POINT (121.4737 31.2304)'); -- CRS84 是 x=longitude, y=latitude

SELECT ST_CRS(geom),
       ST_Transform(geom, 'EPSG:3857') AS web_mercator
FROM poi;
```

`ST_SetCRS` 只赋予/改写 CRS 类型信息，**不**变换坐标；真正重投影使用 `ST_Transform`。未知 CRS 默认报错，若确有仅需读入的未知标识可设 `ignore_unknown_crs = true`，但 DuckDB 会丢弃 CRS 而不是伪装支持它，之后的转换/GeoParquet 元数据能力也会受限。[CRS assignment and unknown CRS](https://duckdb.org/docs/current/sql/data_types/geometry)

### 3.2 `ST_Transform` 与轴顺序

`ST_Transform` 可转换 `GEOMETRY`、`POINT_2D`、`BOX_2D`，source/target CRS 接受 PROJ 支持的格式。其 `always_xy` 参数可强制把输入输出解释为 `[easting, northing]`，这对 GeoJSON 常见的 `[longitude, latitude]` 特别重要；该扩展使用自带静态 PROJ CRS 数据库，和本机 GIS 的可用 CRS 不一定相同。[ST_Transform documentation](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- GeoJSON/WGS84 常见的 (lon, lat) 输入，显式消除歧义
SELECT ST_Transform(
  ST_Point(121.4737, 31.2304),
  'EPSG:4326', 'EPSG:3857',
  always_xy := true
);
```

截至 v1.5，`ST_Transform` 和下列球/椭球函数的历史默认仍遵循 EPSG:4326 的 `(x=latitude, y=longitude)`；设置 `geometry_always_xy = true` 可提前采用 `(x=longitude, y=latitude)` 新语义。官方迁移计划是 v2.0 将未显式设置的告警变为错误，v2.1 默认启用新语义；因此新项目应在会话/部署配置中**显式**设置该项并用已知点断言结果。[v1.5 axis-order migration](https://duckdb.org/2026/03/09/announcing-duckdb-150)

```sql
-- 推荐明确声明，避免未来升级改变含义
SET geometry_always_xy = true;
```

### 3.3 平面与椭球函数不可混用

`ST_Area`、`ST_Length`、`ST_Perimeter`、`ST_Distance`、`ST_DWithin`、`ST_Buffer` 等普通计算在笛卡尔平面进行；结果和 distance 参数都使用输入坐标的单位，`ST_Buffer` 也明确不考虑地球曲率。对经纬度直接使用它们，数值单位是“度”而不是米。[面积与 buffer](https://duckdb.org/docs/current/core_extensions/spatial/functions) [distance signatures](https://duckdb.org/docs/current/core_extensions/spatial/functions)

需要地球表面量测时，使用 `ST_Distance_Sphere`（haversine、仅 `POINT`）或 `ST_Distance_Spheroid`、`ST_DWithin_Spheroid`、`ST_Area_Spheroid`、`ST_Length_Spheroid`、`ST_Perimeter_Spheroid`。文档要求这些椭球函数输入 EPSG:4326/WGS84 且按其指定轴顺序；它们以米/平方米返回结果，使用 GeographicLib 的椭球模型，精度更高也更慢。应根据误差预算选择“先投影后平面计算”或“椭球函数”，而不是把两种距离结果直接比较。[Spheroid functions](https://duckdb.org/docs/current/core_extensions/spatial/functions)

## 4. 函数面：从建模到拓扑、聚合与制图

完整列表很长，下面按工作流归类而非逐项抄录；每个类别均来自官方函数索引。[Spatial function index](https://duckdb.org/docs/current/core_extensions/spatial/functions)

| 工作 | 常用函数 | 说明 |
| --- | --- | --- |
| 构造/解析 | `ST_Point`、`ST_MakePoint`、`ST_MakeLine`、`ST_MakePolygon`、`ST_MakeEnvelope`、`ST_GeomFromText/WKB/GeoJSON` | `ST_MakePoint`/`ST_Point` 创建点；由点/线和边界构造线、面、矩形。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 输出/交换 | `ST_AsText`、`ST_AsWKB`、`ST_AsHEXWKB`、`ST_AsGeoJSON`、`ST_AsSVG` | 用于可读调试、二进制交互、前端 GeoJSON/SVG；GeoJSON fragment 需自行封装 Feature。 [输出函数](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 访问/质检 | `ST_GeometryType`、`ST_IsEmpty`、`ST_IsValid`、`ST_IsSimple`、`ST_IsClosed`、`ST_HasZ/M`、`ST_X/Y/Z/M`、`ST_NPoints`、`ST_Dump` | 应在导入后先判空、判有效、识别类型/维度；`ST_Dump` 可配 `UNNEST` 将 collection 展成带 path 的子几何。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 范围/量测 | `ST_Extent`、`ST_Envelope`、`ST_Area`、`ST_Length`、`ST_Perimeter`、`ST_Distance`、`ST_Azimuth`、`ST_DWithin` | 常规平面范围与量测；要明确 CRS/单位。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 拓扑谓词 | `ST_Intersects`、`ST_Contains`、`ST_Within`、`ST_Covers`、`ST_CoveredBy`、`ST_Disjoint`、`ST_Touches`、`ST_Crosses`、`ST_Overlaps`、`ST_Equals` | 用于空间过滤和关联。`ST_Intersects_Extent` 是 extent（包络）层面测试，不应代替精确 `ST_Intersects`。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 几何处理 | `ST_Buffer`、`ST_Intersection`、`ST_Union`、`ST_Difference`、`ST_MakeValid`、`ST_Simplify`、`ST_SimplifyPreserveTopology`、`ST_ConvexHull`、`ST_ConcaveHull`、`ST_VoronoiDiagram`、`ST_Polygonize` | 可完成叠置、裁剪、修复、概化与派生分析；`ST_SimplifyPreserveTopology` 是保持拓扑的变体。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 线/网络与维度 | `ST_LineMerge`、`ST_LineSubstring`、`ST_LineLocatePoint`、`ST_LineInterpolatePoint(s)`、`ST_LocateAlong/Between`、`ST_InterpolatePoint` | 支持按长度或 M 值定位/截取；M 相关函数应配合 `ST_HasM` 和输入规范测试。 [函数索引](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 仿射/瓦片 | `ST_Affine`、`ST_Translate`、`ST_Scale`、`ST_Rotate*`、`ST_TileEnvelope`、`ST_AsMVTGeom`、`ST_AsMVT` | 旋转、缩放是 `ST_Affine` 的宏；MVT 先裁剪/量化再聚合为 BLOB。 [macro/MVT docs](https://duckdb.org/docs/current/core_extensions/spatial/functions) |

### 4.1 聚合与 coverage

聚合包含 `ST_Extent_Agg`/别名 `ST_Envelope_Agg`、`ST_Intersection_Agg`、`ST_Union_Agg`、`ST_MemUnion_Agg`、`ST_AsMVT` 及 coverage 聚合。`ST_MemUnion_Agg` 会逐个合并，文档注明通常更慢但可能比 `ST_Union_Agg` 更省内存；大规模融合应先在真实数据上压测内存和耗时。[Aggregate functions](https://duckdb.org/docs/current/core_extensions/spatial/functions)

`ST_CoverageInvalidEdges(_Agg)`、`ST_CoverageSimplify(_Agg)` 与 `ST_CoverageUnion(_Agg)`面向共享边界的 polygon coverage：前者找无效边，后两者在保持 coverage 的条件下概化/融合。它们不是任意重叠多边形的通用“更快 union”承诺，输入是否为 coverage 是调用方的业务前提。[Coverage aggregate functions](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- 行转聚合：按行政区合并地块，并同时保留范围
SELECT district_id,
       ST_Union_Agg(geom)  AS merged_geom,
       ST_Extent_Agg(geom) AS bounding_polygon
FROM parcels
GROUP BY district_id;
```

### 4.2 MVT 示例

`ST_AsMVT` 接受含 geometry 与属性的 `STRUCT` 行并生成单个 Mapbox Vector Tile `BLOB`；通常要先以 `ST_TileEnvelope` 取瓦片包络，调用 `ST_AsMVTGeom` 做变换/裁剪/量化，再聚合。官方示例假定数据已经是 Web Mercator（EPSG:3857），所以 web 地图生产管线要先确认/转换 CRS。[MVT documentation](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- 源几何应已处于 EPSG:3857；此处以 z=12, x=3446, y=1652 为例。
COPY (
  SELECT ST_AsMVT({
    "geometry": ST_AsMVTGeom(
      geom, ST_Extent(ST_TileEnvelope(12, 3446, 1652)), 4096, 256, false
    ),
    "name": name
  }, 'places', 4096, 'geometry', 'id')
  FROM places
  WHERE ST_Intersects(geom, ST_TileEnvelope(12, 3446, 1652))
) TO 'out/places.mvt' (FORMAT 'BLOB');
```

## 5. 矢量 I/O、GeoParquet、OSM 与外部格式

### 5.1 GDAL 向量读取/写出

`ST_Read(path, ...)` 通过 GDAL translator 把各种**矢量**地理文件作为 DuckDB 表读取；只有 `path` 必填，可选择 layer、driver、open options、`spatial_filter`/`spatial_filter_box` 等。空间过滤是否真正下推由 driver 决定，否则由 GDAL 过滤且可能较慢；GDAL 本身单线程，故 `ST_Read` 不能吃满 DuckDB 并行度。[ST_Read reference](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- 常规导入：先以 meta 确认图层和 CRS，再物化为 DuckDB 表
SELECT layers[1].geometry_fields[1].crs.auth_name,
       layers[1].geometry_fields[1].crs.auth_code
FROM ST_Read_Meta('data/roads.fgb');

CREATE TABLE roads AS
SELECT *
FROM ST_Read('data/roads.fgb', spatial_filter_box :=
  {min_x: 121.0, min_y: 31.0, max_x: 122.0, max_y: 32.0}::BOX_2D);
```

`ST_Read` 也提供 replacement scan：`.shp`、`.gpkg`、`.fgb` 可直接写在 `FROM` 中；这是 `ST_Read` 的语法糖，性能相同，需参数时仍应使用函数。`keep_wkb := true` 会返回 `WKB_BLOB` 而非 `GEOMETRY`，适合暂存 DuckDB 目前无法表示的 GDAL 异型几何；`ST_ReadSHP` 则是不依赖 GDAL 的 Shapefile 读取路径。[ST_Read and ST_ReadSHP](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- 简写（只适合无额外选项的 shp/gpkg/fgb）
SELECT * FROM 'data/zones.gpkg';

-- 导出；SRS 只是写入目标元数据，不会重投影源几何
COPY roads TO 'out/roads.geojson'
WITH (
  FORMAT GDAL,
  DRIVER 'GeoJSON',
  LAYER_CREATION_OPTIONS 'WRITE_BBOX=YES',
  SRS 'EPSG:4326'
);
```

GDAL `COPY` 中的 `FORMAT GDAL` 为必填、`DRIVER` 可由 `ST_Drivers()` 枚举、`SRS` 可填 WKT/EPSG/proj string，但它只设置导出元数据而不做投影。Spatial 打包的 GDAL 未必是最新，且文档明确说许多 driver 尚未充分测试，故生产格式兼容性要以目标 driver 的集成测试验证。[GDAL export and caveats](https://duckdb.org/docs/current/core_extensions/spatial/gdal) [ST_Drivers caveat](https://duckdb.org/docs/current/core_extensions/spatial/functions)

### 5.2 Parquet / GeoParquet 与生态连接点

DuckDB 在 v1.1 宣布了经正常 Parquet reader 自动把 GeoParquet geometry 列转换为 `GEOMETRY`；当前 `enable_geoparquet_conversion` 默认 `true`，在 `spatial` 已加载时会尝试 GeoParquet 的 geometry 解码/编码。v1.5 将 CRS 纳入 `GEOMETRY` 类型，官方也明确把这作为导入/导出嵌入 CRS 元数据（如 GeoParquet）所需完整 CRS 定义的基础。[DuckDB 1.1 GeoParquet announcement](https://duckdb.org/2024/09/09/announcing-duckdb-110) [Configuration reference](https://duckdb.org/docs/current/configuration/overview) [CRS and GeoParquet](https://duckdb.org/docs/current/sql/data_types/geometry)

实践上应将 GeoParquet 的读取纳入 smoke test：确认 `DESCRIBE` 的列类型/CRS、对一个已知 geometry 做 `ST_AsText`，再以 `ST_CRS` 和范围断言验证，而不要仅因文件扩展名是 `.parquet` 就假定元数据已正确解释。

### 5.3 OSM PBF

`ST_ReadOSM('*.osm.pbf')` 直接读压缩 OSM PBF，使用多线程和 zero-copy protobuf 解析，通常快于 `ST_Read()` 的 OSM driver；代价是只输出原始 Nodes/Ways/Relations，不会构建几何。点要素可由 `lat/lon` 构造点；线和面要在 SQL 中以 refs 与 nodes join，自行承担内存代价。[ST_ReadOSM reference](https://duckdb.org/docs/current/core_extensions/spatial/functions)

```sql
-- 注意设置 axis 语义并明确 lon/lat 到 x/y 的映射
SET geometry_always_xy = true;
SELECT id, ST_Point(lon, lat) AS geom, tags
FROM ST_ReadOSM('data/city.osm.pbf')
WHERE kind = 'node' AND tags['amenity'] != [];
```

## 6. 栅格：核心 Spatial 的边界与社区 raster 扩展

`spatial` 的 GDAL 集成仅支持**矢量** driver；官方文档明确写明不能读写 raster 格式。因此 GeoTIFF、COG、Zarr 等不能通过 `ST_Read`/`COPY FORMAT GDAL` 当作“Spatial 核心功能”来规划。[GDAL limitations](https://duckdb.org/docs/current/core_extensions/spatial/gdal)

需要栅格 SQL 时，DuckDB 官方社区目录提供独立的 `raster` 扩展：

```sql
INSTALL raster FROM community;
LOAD raster;
LOAD spatial;  -- 需要以矢量剪裁 raster 时另行加载

SELECT * FROM RT_Drivers();
SELECT * FROM RT_Read('data/image.tif', blocksize_x := 512, blocksize_y := 512);
```

它将栅格按 tile 输出：空间位置/范围/geometry、行列数、JSON metadata，以及每个波段的 BLOB（或单个 datacube BLOB）。`RT_Read` 可对非 BLOB 列做 filter pushdown；`RT_ReadCells` 可展开成每像元一行；`RT_Cube*` 支持波段代数、统计、clip/burn/polygon 等；`COPY ... FORMAT RASTER` 可借 GDAL driver 写回 GTiff/COG 等。其定位是“把栅格当 datacube 表做 SQL”，不是 Spatial 扩展的稳定 API 保证。[Official raster extension documentation](https://duckdb.org/community_extensions/extensions/raster)

```sql
-- 例：NDVI（红光与近红外 band 的 tile 级代数）
WITH tiles AS (
  SELECT databand_1 AS red, databand_2 AS nir
  FROM RT_Read('data/sentinel.tif', blocksize_x := 512, blocksize_y := 512)
)
SELECT RT_Cube2TypeFloat((nir - red) / (nir + red)) AS ndvi
FROM tiles;
```

选型建议：只有矢量分析就只装 `spatial`；需要 raster 时将 `raster` 视为单独依赖，锁版本，并用实际格式/驱动、NODATA、块大小、内存预算和写回结果做验收。上述建议是基于核心 GDAL 的矢量限制和社区扩展的独立安装/数据模型作出的工程推断。[GDAL limitations](https://duckdb.org/docs/current/core_extensions/spatial/gdal) [raster extension](https://duckdb.org/community_extensions/extensions/raster)

## 7. 索引、行组裁剪与性能策略

### 7.1 R-tree

R-tree 把每个几何的近似最小包围矩形（MBR）和行 ID 放在叶节点，内部节点保存子节点 MBR；查询先做廉价包络排除，最后只对候选行执行昂贵的精确空间谓词。它解决大表空间过滤默认全表扫描的问题，但 MBR 只是候选过滤，精确谓词仍然必要。[How DuckDB R-tree works](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)

```sql
LOAD spatial;
CREATE INDEX parcels_geom_rtree ON parcels USING RTREE (geom);

EXPLAIN
SELECT count(*)
FROM parcels
WHERE ST_Within(geom, ST_MakeEnvelope(121.40, 31.20, 121.50, 31.30));
```

当前使用约束非常重要：R-tree 只支持 `GEOMETRY`；只会在 `WHERE` 中使用一组“蕴含相交”的谓词（包括 `ST_Intersects`、`ST_Within`、`ST_Contains`、`ST_Equals`、`ST_Touches`、`ST_Crosses`、`ST_Overlaps`、`ST_Covers`、`ST_CoveredBy`、`ST_ContainsProperly`）进行 index scan；并且谓词一边必须是计划期可知的常量。因此它目前不能自动加速一般的两表空间 join。[R-tree limitations](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)

批量导入后再建索引更快：官方在已填充表上用 Sort-Tile-Recursive bottom-up bulk loading，通常重叠更少、查询更好；大量更新/删除后性能劣化可考虑 drop/recreate。R-tree buffer 在磁盘模式下懒加载，但扫描过的索引页目前不会在连接期间卸载，会计入 `memory_limit`，这决定了不能只看首个查询的耗时。[R-tree maintenance and memory](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)

### 7.2 不依赖显式索引的 v1.5 裁剪

v1.5 几何列按 row group 追踪 extent、几何子类型和顶点维度；优化器可据此跳过不可能匹配的 row group。当前官方仅承诺 `WHERE` 中 `&&`（两个 geometry 的包络相交）能用上这些统计，`ST_Intersects`、`ST_Distance` 等更多谓词仍在开发，因此不要凭直觉以为精确谓词已自动获得同等 pruning。[Geometry statistics](https://duckdb.org/docs/current/sql/data_types/geometry)

```sql
-- 粗筛先让 v1.5 geometry statistics 有机会裁剪，再做精确谓词。
-- 常量区域须与表列使用同一 CRS。
WITH q AS (
  SELECT ST_MakeEnvelope(121.40, 31.20, 121.50, 31.30)::GEOMETRY AS region
)
SELECT p.*
FROM parcels p, q
WHERE p.geom && q.region
  AND ST_Intersects(p.geom, q.region);
```

该“粗筛 + 精确验证”是基于文档中 `&&` 的统计支持和 R-tree 的 MBR 候选逻辑给出的查询模式推断；需要用 `EXPLAIN`/`EXPLAIN ANALYZE` 在实际版本、CRS、数据分布上验证计划，而不能把它当成无条件更快的规则。[Geometry statistics](https://duckdb.org/docs/current/sql/data_types/geometry) [R-tree example](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)

### 7.3 两表空间连接：`SPATIAL_JOIN`（v1.3+）

不要把“持久 R-tree 不能用于一般两表 join”理解为两表空间连接必然退化为嵌套循环。DuckDB v1.3 引入专用 `SPATIAL_JOIN` 物理算子：优化器会物化预期更小的右输入、临时建立 R-tree，然后流式探测左输入；`EXPLAIN` 中出现 `SPATIAL_JOIN` 才说明该优化实际被采用。常规 SQL 仍是普通的 `JOIN ... ON ST_Intersects/Contains/...`，没有新的 SQL 语法。[Spatial joins in DuckDB](https://duckdb.org/2025/08/08/spatial-joins)

```sql
EXPLAIN
SELECT p.id, d.name
FROM points AS p
JOIN districts AS d
  ON ST_ContainsProperly(d.geom, p.geom);
```

此算子的边界与持久索引不同：当前右输入必须整体放入内存；连接条件只能是**单个**空间谓词函数；支持 `INNER`、`LEFT`、`RIGHT`、`FULL OUTER`，不支持 `SEMI`/`ANTI`，也不能在同一 `ON` 中再合并 `AND p.kind = d.kind` 等额外条件。因此大侧放左、小侧放右只是有利于优化器的查询形状，仍需以 `EXPLAIN` 和内存上限下的基准测试作为最终依据。[Spatial join architecture and limitations](https://duckdb.org/2025/08/08/spatial-joins)

## 8. 实战工作流与可执行 SQL 骨架

### 8.1 安全导入、规范化、索引与查询

```sql
INSTALL spatial;
LOAD spatial;

-- 对新代码明确地理轴约定；这里约定所有经纬度 x=lon, y=lat。
SET geometry_always_xy = true;

-- 1) 先读元数据，明确图层的 CRS；随后选择对应的 column type。
SELECT * FROM ST_Read_Meta('data/buildings.gpkg');

-- 2) 导入并校验：无效几何在下游 buffer/overlay 前被隔离。
CREATE TABLE buildings_raw AS
SELECT * FROM ST_Read('data/buildings.gpkg', layer := 'buildings');

CREATE TABLE buildings AS
SELECT *
FROM buildings_raw
WHERE geom IS NOT NULL AND NOT ST_IsEmpty(geom) AND ST_IsValid(geom);

-- 3) 必要时显式变换到米制投影，再做距离/缓冲。
CREATE TABLE buildings_3857 AS
SELECT *, ST_Transform(geom, 'EPSG:4326', 'EPSG:3857', always_xy := true) AS geom_3857
FROM buildings;

CREATE INDEX buildings_3857_rtree ON buildings_3857 USING RTREE (geom_3857);

-- 4) 常量窗口：使用 R-tree 可识别的谓词；仍保留精确谓词。
SELECT id
FROM buildings_3857
WHERE ST_Intersects(
  geom_3857,
  ST_MakeEnvelope(13520000, 3640000, 13530000, 3650000)
);
```

这段骨架分别利用了 `ST_Read_Meta`、GDAL `ST_Read`、`ST_IsValid`、`ST_Transform`、R-tree 的官方接口；但示例把源 CRS 假定为 EPSG:4326，实际项目必须由导入 metadata 决定，不能照抄该假定。[ST_Read metadata](https://duckdb.org/docs/current/core_extensions/spatial/functions) [CRS conversion](https://duckdb.org/docs/current/sql/data_types/geometry) [R-tree usage](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)

### 8.2 覆盖区面积与邻近查询

```sql
-- 前提：geom 已在适合面积的投影 CRS 中，单位为米。
SELECT zone_id,
       sum(ST_Area(geom)) AS area_square_meters,
       ST_Union_Agg(geom) AS dissolved_geom
FROM land_parcels
GROUP BY zone_id;

-- 前提：同一投影 CRS；500 的含义是 500 米。
SELECT a.id, b.id
FROM sites a
JOIN roads b
  ON ST_DWithin(a.geom, b.geom, 500.0);
```

普通 `ST_Area`/`ST_DWithin` 的单位由 CRS 决定，所以上例只有在适当投影坐标下才是平方米/米；若数据保留 WGS84 经纬度，应改用满足输入限制的 spheroid 路径或先投影。[ST_Area](https://duckdb.org/docs/current/core_extensions/spatial/functions) [ST_DWithin](https://duckdb.org/docs/current/core_extensions/spatial/functions) [spheroid distance](https://duckdb.org/docs/current/core_extensions/spatial/functions)

## 9. 已验证的限制、语义陷阱与落地检查单

| 风险/限制 | 后果 | 应对 |
| --- | --- | --- |
| `spatial` 不 autoload | 新连接调用 `ST_*` 失败 | 连接初始化统一 `LOAD spatial`。[官方概览](https://duckdb.org/docs/current/core_extensions/spatial/overview) |
| `GEOMETRY` 内部 bytes 未稳定 | 直接存取内部 BLOB 后升级可能不兼容 | 对外交互只用 WKT/WKB/GeoJSON 转换函数。[Geometry storage](https://duckdb.org/docs/current/sql/data_types/geometry) |
| 平面函数不懂地球曲率 | 把经纬度的“度”误当米，buffer/面积/距离错误 | 投影后平面计算，或使用 spheroid/sphere 函数并测试轴顺序。[空间函数](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| EPSG:4326 轴顺序迁移中 | 同一 SQL 在版本/设置下含义可能不同 | 显式 `SET geometry_always_xy`，`ST_Transform(..., always_xy := true)`，以已知控制点测试。[v1.5 release note](https://duckdb.org/2026/03/09/announcing-duckdb-150) |
| CRS metadata ≠ 坐标变换 | `COPY ... SRS` 或 `ST_SetCRS` 只改标签，几何位置没变 | 需要重投影时只用 `ST_Transform`；导入后断言 `ST_CRS`。[GDAL COPY](https://duckdb.org/docs/current/core_extensions/spatial/gdal) [CRS functions](https://duckdb.org/docs/current/sql/data_types/geometry) |
| GDAL driver 覆盖/线程性 | 特定格式可能不可用，`ST_Read` 不能并行满速 | 部署时跑 `ST_Drivers()`，用目标样本压测，重要路径物化为 DuckDB/Parquet。[ST_Drivers/ST_Read](https://duckdb.org/docs/current/core_extensions/spatial/functions) |
| 核心 GDAL 只支持矢量 | 误把 GeoTIFF 交给 `ST_Read`/GDAL COPY | 栅格选独立 `raster` 扩展或外部工具。[GDAL limitations](https://duckdb.org/docs/current/core_extensions/spatial/gdal) |
| R-tree predicate/常量限制 | 以为能自动加速任意空间 join，实则全表扫描 | 用 `EXPLAIN` 验证；范围查询用常量区域；两表 join 另行基准测试/分桶设计。[R-tree limitations](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes) |
| `SPATIAL_JOIN` 的 build side | 大右表或复合 join 条件可能不能使用专用临时 R-tree | 令较小的几何表处于右侧、保持单一空间谓词，并以 `EXPLAIN` 确认；否则按普通 join 预算性能/内存。[Spatial join limitations](https://duckdb.org/2025/08/08/spatial-joins) |
| v1.5 统计覆盖有限 | 仅写 `ST_Intersects` 不一定得到 row-group pruning | 必要时先加同 CRS 的 `&&` 粗筛，再精确谓词并验证计划。[Geometry statistics](https://duckdb.org/docs/current/sql/data_types/geometry) |

## 10. 尚需按项目决策的事项

以下不是 DuckDB 已声明的事实，而是接入本仓库前应补齐的选择：

1. **坐标契约**：确定所有经纬度数据统一使用 `OGC:CRS84`（lon/lat）还是严格 EPSG:4326 轴序，并把 `geometry_always_xy` 固化在连接初始化与集成测试中。
2. **持久化格式**：矢量湖仓优先用带 CRS metadata 的 GeoParquet，或 DuckDB 原生库；需要跨系统传输时固定 WKB/WKT/GeoJSON 边界。此处“优先”是工程建议，不是官方强制要求。
3. **性能目标**：用真实的几何复杂度、选择率、更新频率和内存上限比较“行组统计 + `&&`”“R-tree”“预计算瓦片/Hilbert 分桶”；不要只以小样本 benchmark 选型。
4. **栅格依赖治理**：若引入 `raster`，单独记录版本、可用 driver、NODATA/重采样/输出 CRS 规则，因为它是社区扩展而不是 `spatial` 的矢量 GDAL 路径。

## 官方来源索引

- [DuckDB Spatial extension overview](https://duckdb.org/docs/current/core_extensions/spatial/overview)
- [Spatial function reference](https://duckdb.org/docs/current/core_extensions/spatial/functions)
- [Geometry data type, v1.5 storage/CRS/statistics](https://duckdb.org/docs/current/sql/data_types/geometry)
- [R-tree indexes](https://duckdb.org/docs/current/core_extensions/spatial/r-tree_indexes)
- [GDAL integration](https://duckdb.org/docs/current/core_extensions/spatial/gdal)
- [DuckDB 1.1 release: GeoParquet](https://duckdb.org/2024/09/09/announcing-duckdb-110)
- [Spatial joins in DuckDB](https://duckdb.org/2025/08/08/spatial-joins)
- [DuckDB 1.5 release: spatial migration](https://duckdb.org/2026/03/09/announcing-duckdb-150)
- [DuckDB Community `raster` extension](https://duckdb.org/community_extensions/extensions/raster)
