# 当前 GIS 框架生态与制图能力调研

> **调研范围**：Web GIS、Python GIS、桌面 GIS 及其关键数据/服务底座；特别辨析交互式网页地图与布局型版面地图（图框、图廓/整饰线、经纬网、图例、比例尺、地图集）。  
> **调研日期**：2026-07-29。仅采用项目、厂商、标准组织维护的官方文档、官方源码仓库或官方产品页；链接均指向支撑该结论的一手资料。  
> **术语约定**：这里的“框架”既包括代码库（如 MapLibre GL JS、GeoPandas），也包括承担同一工作层的产品/引擎（如 QGIS、ArcGIS Pro、PostGIS、GeoServer）；它们不是同类替代品。

## 结论摘要

1. **MapLibre GL JS、Mapbox GL JS、OpenLayers、Leaflet、CesiumJS、deck.gl 与 ArcGIS Maps SDK for JavaScript 的主战场都是浏览器中的动态地图/场景。**它们围绕地图容器、图层/数据源、视图/相机和用户事件建立 API；MapLibre 官方定义为在浏览器用 WebGL 从矢量瓦片渲染交互式地图，Mapbox 则明确以客户端动态渲染、交互和 Web 应用为目标。[MapLibre GL JS](https://maplibre.org/maplibre-gl-js/docs/) [Mapbox GL JS](https://docs.mapbox.com/mapbox-gl-js/)
2. **“能导出一张图”不等于“擅长布局制图”。**Mapbox 可给出 PNG/JPG 静态图，Studio 可高分辨率导出，但官方明确静态地图导出不支持 SVG、EPS、PDF；MapLibre 的 PDF/图像导出位于官方列出的社区插件目录。因此，这两类产品可作为版面中的“主图图像来源”，却没有 QGIS/ArcGIS Pro 那种原生页面、多个独立图框、整饰元素和地图集对象模型。这是基于各自官方 API/功能范围作出的工程判断，而非“网页永远不能制图”的绝对断言。[Mapbox 静态与印刷地图](https://docs.mapbox.com/help/dive-deeper/static-maps/) [MapLibre 插件目录](https://maplibre.org/maplibre-gl-js/docs/plugins/) [QGIS Print Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [ArcGIS Pro Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm)
3. **交付物决定首选工具。**实时查询、缩放、点选、路径、业务面板与海量瓦片：选 Web runtime；空间清洗、批处理、可复现分析：选 Python/SQL/命令行；正式纸图、PDF/SVG、图框/图廓、格网、图例、插图和分幅图集：选 QGIS 或 ArcGIS Pro。把三者串成流水线通常优于强行让其中一个承担全部职责。
4. **推荐的开源基线是“GDAL/PROJ/GEOS + PostGIS/GeoParquet + GeoServer（按需）+ GeoPandas/GRASS + MapLibre 或 OpenLayers + QGIS”。**其中 GDAL 统一栅格/矢量格式抽象，PostGIS 是空间数据库层，GeoServer 提供 OGC 服务，QGIS 承担交互编辑和成图；可分别替换为 Esri 的 ArcGIS Pro + ArcGIS Online/Enterprise + ArcGIS Maps SDK，或在前端换为 Mapbox 的托管服务。[GDAL](https://gdal.org/en/stable/about.html) [PostGIS](https://postgis.net/docs/postgis-en.html) [GeoServer](https://docs.geoserver.org/main/en/user/introduction/overview/) [QGIS 功能](https://docs.qgis.org/3.44/en/docs/about/features.html)

## 1. 先区分五个工作层

GIS 生态不是单一赛道；下表的层次可避免以“前端框架是否能做空间分析”或“桌面 GIS 是否能发布网页”为错误比较标准。

| 工作层 | 主要职责 | 代表组件 | 不能替代什么 |
| --- | --- | --- | --- |
| 数据互操作与坐标底座 | 读写栅格/矢量格式、重投影、几何模型与格式转换 | GDAL/OGR 为调用方提供统一的栅格和矢量抽象；`geometry`/`geography`、栅格和拓扑可由 PostGIS 扩展提供。[GDAL](https://gdal.org/en/stable/about.html) [PostGIS 扩展](https://postgis.net/docs/postgis-en.html) | 不是完整的网页 UI 或排版系统 |
| 数据库、分析与 ETL | 多数据集 SQL、空间索引、清洗、叠加、栅格处理、批处理 | PostGIS、GeoPandas + Shapely/Rasterio/pyproj、GRASS GIS。[PostGIS](https://postgis.net/docs/postgis-en.html) [GeoPandas API](https://geopandas.org/en/latest/docs/reference.html) [GRASS](https://grass.osgeo.org/) | 不会自动生成适合业务交互的 Web 产品或规范图页 |
| 服务与分发 | 以服务、要素、瓦片或地图图像向多个客户端发布数据 | GeoServer 原生面向 WMS/WFS/WCS/WMTS 等 OGC 服务；OGC API - Tiles 定义矢量要素、覆盖物和地图图像瓦片的 Web API。[GeoServer 服务](https://docs.geoserver.org/main/en/user/services/index.html) [OGC API - Tiles](https://ogcapi.ogc.org/tiles/) | WMS/瓦片服务器不是所见即所得的版面编辑器 |
| 交互显示与应用体验 | 浏览器/移动端的缩放、旋转、点击、动画、图层控制、业务 UI 和 2D/3D 场景 | MapLibre/Mapbox/OpenLayers/Leaflet/CesiumJS/deck.gl/ArcGIS JS。各自是渲染或应用 SDK，而非桌面制图台。[MapLibre](https://maplibre.org/maplibre-gl-js/docs/) [OpenLayers](https://openlayers.org/) [CesiumJS](https://cesium.com/learn/cesiumjs-fundamentals/) | 固定纸张、版心、图框与逐页地图集的原生工作流 |
| 交互编辑与布局成图 | 图层符号化、编辑、分析、页面排版、打印/矢量导出、地图集 | QGIS Print Layout、ArcGIS Pro Layout；GRASS 更偏计算与工作流而非专业版面编排。[QGIS Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [ArcGIS Pro Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) [GRASS 接口](https://grass.osgeo.org/grass-stable/manuals/interfaces_overview.html) | 不等于高并发 Web 交互地图服务 |

典型组合如下。箭头表示可替换的边界，不要求每个项目都部署全部组件。

```text
栅格/矢量/传感器文件 ── GDAL/PROJ ──> PostGIS、GeoParquet、COG 等数据资产
                                      │
                         GeoPandas / GRASS / SQL  （清洗、分析、批处理）
                                      │
                     GeoServer/对象存储/瓦片服务 ──> Web GIS（MapLibre 等）
                                      │                         │
                           QGIS / ArcGIS Pro <── 同源数据 ───> 正式 PDF/SVG/纸图/地图集
```

这个分层也说明：Web 地图与成图桌面软件可共享数据、投影、符号资产的一部分，但**“视图样式”不天然等于“版面模板”**；后者还要保存纸张、图框几何、插图、图例、格网、文字和逐页规则。

## 2. Web GIS：产品定位、生态和边界

| 工具 | 生态与许可/依赖关系 | 强项与典型功能 | 布局型成图定位 |
| --- | --- | --- | --- |
| **MapLibre GL JS** | 开源 TypeScript 库，使用 WebGL；与 Android/iOS 等平台的 MapLibre Native 同属 MapLibre 生态；官方源码许可为 BSD-3-Clause。[介绍](https://maplibre.org/maplibre-gl-js/docs/) [许可证](https://github.com/maplibre/maplibre-gl-js/blob/main/LICENSE.txt) | 浏览器矢量瓦片渲染、Style Spec 样式、相机和事件、标注/控件；样式 JSON 定义“画什么、顺序和样式”。适合自托管瓦片、减少供应商锁定的交互应用。[Style Spec](https://maplibre.org/maplibre-style-spec/) | 核心 API 的中心对象是页面中的 `Map` 与 canvas 外控件，而不是纸页/图框/图集。官方插件目录虽列出由社区提供的 PDF/PNG/SVG 导出控件，但应把它当附加能力并验证维护度、字体、跨域瓦片和导出一致性，而非原生桌面版面系统。[API 范围](https://maplibre.org/maplibre-gl-js/docs/) [插件](https://maplibre.org/maplibre-gl-js/docs/plugins/) |
| **Mapbox GL JS + Mapbox 服务** | 商业托管生态：GL JS、Studio、Tilesets、方向/地理编码等 API；访问令牌、用量统计和按产品计费由账户体系管理。[GL JS](https://docs.mapbox.com/mapbox-gl-js/) [账户和计费](https://docs.mapbox.com/accounts/) | 2D/3D 动态地图、浏览器端矢量瓦片样式、交互、Mapbox 数据和服务组合；支持上传自有数据并以 Studio/MTS 等方式进入应用。[GL JS use cases](https://docs.mapbox.com/mapbox-gl-js/) | 静态 API 返回无交互 PNG/JPEG，支持 GeoJSON/标记/路径覆盖；Studio 可出高分辨率 PNG/JPG，但官方列出的地图导出不含 SVG/EPS/PDF。因此适合把底图或专题图输出后再由 HTML/PDF 工具或桌面 GIS 排版；不应把它当作完整页面制图器。[静态 API](https://docs.mapbox.com/api/maps/static-images/) [静态与印刷地图](https://docs.mapbox.com/help/dive-deeper/static-maps/) |
| **OpenLayers** | 完全免费开源的 JavaScript 库，BSD 2-Clause；生态更强调开放服务、格式和投影互操作。[官方概览](https://openlayers.org/) | 动态网页地图；可加载 XYZ、OGC 服务、未切片图层以及 GeoJSON、TopoJSON、KML、GML、Mapbox vector tiles 等矢量数据；`Map` 由 layers、view、interactions、controls 组成。适合 OGC/WMS/WFS/WMTS 重的政企或行业系统。[功能](https://openlayers.org/) [API 概览](https://openlayers.org/en/latest/apidoc/) | 有打印/导出方案时通常由应用侧 CSS、canvas 或 PDF 管线实现；其官方核心模型同样是动态 `Map`，不是布局/图集对象模型。若输出受制图规范约束，使用 QGIS/ArcGIS Pro 或专门的报表/PDF 层。此结论来自官方 API 的对象范围。[API 概览](https://openlayers.org/en/latest/apidoc/) |
| **Leaflet** | 轻量、开源、移动友好的交互地图 JavaScript 库，依靠插件和瓦片提供者形成生态。[官方页面](https://leafletjs.com/download.html) | 快速 2D slippy map、标记、弹窗、GeoJSON、栅格瓦片；适合功能较简的运营地图、嵌入地图和原型。 | 更适合单一浏览器地图视图；复杂制图版面和大量矢量数据/3D 不是其核心产品定位。静态制图须另建导出/排版链路；这也是 Folium 生成交互式 HTML 所依赖的前端运行时。[Leaflet](https://leafletjs.com/download.html) [Folium](https://folium.readthedocs.io/en/latest/) |
| **CesiumJS** | 开源 JavaScript 3D 地球与地图库；可 npm 安装，并可与 Cesium ion 或自有地形/影像服务组合。[下载与许可定位](https://cesium.com/downloads) | 浏览器 WebGL 3D globe/2D map、地形、影像、3D Tiles、相机与时空动态数据；官方特别面向高精度 WGS84 地球和地形流式显示。[基础](https://cesium.com/learn/cesiumjs-fundamentals/) [地形](https://cesium.com/learn/cesiumjs-learn/cesiumjs-terrain/) | 适合数字孪生、城市/地形三维浏览；不是以传统图廓、版面格网和地图集为目标。需要二维规范纸图时，另建 QGIS/ArcGIS Pro/Cartopy 成图路径。 |
| **deck.gl** | Uber 开源的 WebGPU/WebGL2 数据可视化层框架；可与 MapLibre、Mapbox、Esri 等底图集成。[概览](https://deck.gl/docs) | 面向大量点、线、面、栅格/瓦片、H3、热力/格网/六边形聚合、3D 模型和拾取；不是底图服务或全套 GIS 数据管理器。官方说明其层可以高效处理数百万对象。[介绍](https://deck.gl/docs) [Layer 机制](https://deck.gl/docs/developer-guide/using-layers) | 用作 MapLibre/Mapbox/Cesium 等之上的高性能专题可视化层；没有版面/印刷工作流。 |
| **ArcGIS Maps SDK for JavaScript** | Esri Web 开发生态，连接 ArcGIS Location Platform、ArcGIS Online 或 ArcGIS Enterprise，以及 Calcite 设计系统；部分服务需账户/令牌。[入门](https://developers.arcgis.com/javascript/latest/get-started/) [令牌](https://developers.arcgis.com/javascript/latest/get-started-overview/) | Web 地图、2D Map / 3D Scene、图层、客户端分析、组件、图表和 Arcade 表达式；可直接消费在线门户中的 WebMap/WebScene。[参考总览](https://developers.arcgis.com/javascript/latest/references/) [3D 场景](https://developers.arcgis.com/javascript/latest/scenes-3d/) | 与 ArcGIS Online/Enterprise 共享内容与服务非常顺畅，但 Web SDK 本身仍是应用/场景 SDK。正式页面布局和地图集由 ArcGIS Pro 的 Layout/Map Series 承担。 |

### 2.1 Web 工具的选择准则

- **自托管、开源渲染、矢量瓦片与现代交互**：以 MapLibre GL JS 为首选；需自备或采购底图、字体、瓦片、搜索/路径等服务。MapLibre 库开源并不自动附带全球底图或地理编码服务。[MapLibre 介绍](https://maplibre.org/maplibre-gl-js/docs/)
- **希望缩短底图、样式托管、上传瓦片、路径/地理编码等集成时间**：Mapbox 是托管产品路线，但把令牌权限、用量、数据驻留、归因及费用作为架构约束评估。[Mapbox Maps](https://docs.mapbox.com/api/maps/) [Token 管理](https://docs.mapbox.com/accounts/guides/tokens/)
- **以 WMS/WFS/WMTS、不同 CRS、KML/GML 等标准服务为重**：OpenLayers 的官方功能面直接覆盖这些数据与服务形态；MapLibre 更偏 GL Style + 矢量瓦片体验。[OpenLayers 功能](https://openlayers.org/)
- **三维地球、倾斜摄影/地形、3D Tiles、时空轨迹**：CesiumJS；**在既有底图上渲染海量点线面和聚合**：deck.gl（可叠加使用）。[CesiumJS](https://cesium.com/learn/cesiumjs-fundamentals/) [deck.gl](https://deck.gl/docs)
- **既有 Esri 组织、Portal、WebMap/WebScene 和权限模型**：ArcGIS Maps SDK for JavaScript 可降低集成成本；其 Web 端内容可与 ArcGIS Pro 的桌面产出组成同一产品线。[ArcGIS JS 入门](https://developers.arcgis.com/javascript/latest/get-started/) [ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.0/get-started/get-started.htm)

## 3. Python GIS：以数据、分析和可复现制图为中心

Python 生态不是一个“GeoPandas 替代 QGIS”的产品，而是由表数据、几何、投影、栅格 I/O、可视化和 Web 输出等库拼合的工具链。

| 工具 | 在生态中的位置 | 能力与边界 | 对布局成图的意义 |
| --- | --- | --- | --- |
| **GeoPandas** | 在 `pandas` 的 Series/DataFrame 模型上增加 `GeoSeries`、活动几何列和 CRS；几何对象来自 Shapely，文件 I/O 可经 pyogrio/Fiona。[数据结构](https://geopandas.org/en/latest/docs/user_guide/data_structures.html) [read_file](https://geopandas.org/en/latest/docs/reference/api/geopandas.read_file.html) | 空间连接、overlay、clip、投影、文件/PostGIS/Parquet I/O、空间索引与绘图 API；`plot()` 调用 Matplotlib，`explore()` 基于 Folium/Leaflet 产生交互地图。[API](https://geopandas.org/en/latest/docs/reference.html) [交互地图](https://docs.geopandas.org/en/latest/docs/user_guide/interactive_mapping.html) | 适合分析结果的快速专题图和脚本化出图。它本身没有 QGIS Layout/ArcGIS Layout 的页面、图框和地图集模型；若选 Python 成图，应以 Matplotlib/Cartopy 的 Figure/Axes 与模板代码显式实现并做视觉回归。 |
| **Shapely** | 纯平面几何与拓扑运算层，封装 GEOS；提供标量对象和 NumPy ufunc。[官方介绍](https://shapely.readthedocs.io/en/stable/index.html) | 点线面构造、谓词、缓冲、叠加等；不以文件序列化或 CRS 管理为主。其 GEOS 算法通常为二维，缓冲会丢弃 Z。[Shapely](https://shapely.readthedocs.io/en/stable/index.html) | 不负责渲染或排版；应将投影和单位问题交给 pyproj/GeoPandas，并将地图元素交给 Cartopy/Matplotlib 或桌面 GIS。 |
| **pyproj** | Python 到 PROJ 的接口，负责 CRS、投影和坐标转换。[pyproj](https://pyproj4.github.io/pyproj/stable/index.html) | 坐标变换、CRS 定义和 transformation grid 管理；是大多数 Python GIS 栈正确处理坐标的基础。 | 所有距离、面积、格网和比例尺设计的前置条件；`set_crs` 只是赋予 CRS 标签而不改变坐标，真正重投影要 `to_crs`，不能混淆。[GeoPandas set_crs](https://geopandas.org/en/stable/docs/reference/api/geopandas.GeoDataFrame.set_crs.html) |
| **Rasterio** | 基于 GDAL 的 Python 栅格 I/O，使用 NumPy 数组和 GeoJSON 风格接口。[Rasterio](https://rasterio.readthedocs.io/en/stable/) | GeoTIFF 等栅格读取/写入、窗口、掩膜、重投影、矢栅转换；适合遥感、DEM 和栅格预处理。 | 不是版面软件；生成的栅格/派生结果可交给 GeoPandas+Cartopy、QGIS 或 ArcGIS Pro 成图。 |
| **pyogrio** | GDAL/OGR 的批量矢量 I/O，常作为 GeoPandas 的高性能引擎。[pyogrio](https://pyogrio.readthedocs.io/) | 批量读写 Shapefile、GeoPackage、GeoJSON、FlatGeobuf 等；支持 bbox/mask/列过滤。具体 driver 取决于实际 GDAL 构建，不能只凭文件后缀假设可用。[pyogrio 格式](https://pyogrio.readthedocs.io/en/latest/supported_formats.html) | 适合把生产数据稳定送入分析或成图脚本，不提供布局功能。 |
| **Cartopy + Matplotlib** | Cartopy 在 Matplotlib 上添加地理投影与空间对象变换，适合论文、报告和可复现静态图。[Cartopy](https://cartopy.readthedocs.io/stable/) | `GeoAxes` 可处理投影，`Gridliner` 可画经纬网和标注；Matplotlib Figure 可组合多个 axes、图例、文本，并导出 PDF/EPS/SVG/PNG 等。[Cartopy Gridliner](https://cartopy.readthedocs.io/stable/matplotlib/gridliner.html) [Matplotlib Figure/导出](https://matplotlib.org/stable/Matplotlib.pdf) | **Python 中最接近可编程布局成图的组合**。优点是版本化和批量可复现；代价是比例尺、指北针、图框、避让、图例、插图、字体和逐页图集需要自行编码或采用团队模板，不具备 QGIS/Pro 的可视拖放与专用图集 UI。 |
| **Folium** | 将 Python 数据包装为 Leaflet 交互地图并输出独立 HTML；GeoPandas 的 `explore()` 建立在此之上。[Folium](https://folium.readthedocs.io/en/latest/) | Notebook/HTML 分享、图层控制、交互浏览；适合分析结果演示。 | 是 Web 输出，不是静态排版；导出 PDF 仍须浏览器打印或另建渲染/排版链路。 |

### 3.1 Python 的重要正确性约束

- GeoPandas 官方明确空间操作是**平面**的，不能把经纬度中的“度”直接解释为米或平方米；应依据研究区域和指标转换到合适 CRS，或采用适合地球曲面的算法/工具。[GeoPandas overlay](https://geopandas.org/en/stable/docs/reference/api/geopandas.overlay.html)
- 文件格式兼容性由 GDAL driver 决定。GDAL 的职责是提供统一的栅格/矢量抽象和转换工具，不承诺任一 Python wheel 或服务器构建一定包含所有 driver；部署时应用目标格式做读写验收。[GDAL](https://gdal.org/en/stable/about.html) [pyogrio 安装说明](https://pyogrio.readthedocs.io/en/latest/install.html)
- 因而，Python 最适合“以代码生产地图或空间结果”，不一定适合“让制图人员在临近出版时用鼠标微调版面”。后一任务的效率与可审稿性通常更适合 QGIS/ArcGIS Pro，或需要一套经过测试的 Cartopy 模板。

### 3.2 PyQGIS 与 ArcPy：把桌面版面制图脚本化

**定位。**PyQGIS 和 ArcPy 并非与 GeoPandas 同一层的纯 Python 数据库：二者分别是 QGIS 和 ArcGIS Pro 的应用程序/工程对象模型的 Python 绑定。因此它们既可做地理处理，也能直接操作已有的**项目、图层、符号、布局、图框和地图册**；这是用 Python 批量产出规范 PDF/SVG/图像时，较 Cartopy 手写页面元素更直接的路径。[PyQGIS Cookbook](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/index.html) [arcpy.mp](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm)

| 维度 | PyQGIS（QGIS 3.44 API） | ArcPy / `arcpy.mp`（ArcGIS Pro 3.6 API） |
| --- | --- | --- |
| 可用性、运行时与授权 | QGIS 为 GPL 开源软件；Python 可在 QGIS Console、Python 插件、启动脚本/`--code`，或独立程序中运行。独立脚本须指向已安装的 QGIS、构造 `QgsApplication` 并调用 `initQgis()`/`exitQgis()`，以载入投影资料和数据 provider；它不是脱离 QGIS 运行时的普通纯 Python wheel。[QGIS 许可](https://qgis.org/license/) [PyQGIS 运行模型](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/intro.html) | `arcpy.mp` 随 ArcGIS Pro 安装，对全部 Pro 许可级别可用；ArcPy 可安装到 conda 环境，但仍要求本机已安装 ArcGIS Pro，并在独立运行时具备有效授权。可在 Python 窗口、Notebook、脚本工具、IDE 或计划任务运行；应使用 Pro 的 conda/`propy` 环境，默认 `arcgispro-py3` 不宜直接修改。[安装 ArcPy](https://pro.arcgis.com/en/pro-app/3.6/arcpy/get-started/installing-arcpy.htm) [Pro Python/授权](https://pro.arcgis.com/en/pro-app/3.6/arcpy/get-started/installing-python-for-arcgis-pro.htm) [Pro 许可](https://pro.arcgis.com/en/pro-app/3.6/get-started/about-licensing.htm) |
| 工程与图层模型 | `QgsProject.instance()` 是项目单例，可 `read()`/`write()`；`QgsVectorLayer`/`QgsRasterLayer` 装载数据后加入 project，项目负责图层生命周期。矢量层通过 `renderer()`、`QgsSymbol` 和 `Qgs*Renderer` 管理单一、分类、分级等符号化。[加载项目](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/loadproject.html) [加载图层](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/loadlayer.html) [PyQGIS 符号化](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/vector.html) | 从 `arcpy.mp.ArcGISProject(path)` 进入 `.aprx`，用 `listMaps()`/`listLayouts()` 取得对象；`Map` 管理 `Layer`/Table，图层可改数据源、定义查询、可见性和 `symbology`。常规 renderer/colorizer 先取出、修改、再赋回图层；未暴露的符号属性须用 CIM 或准备好的 `.lyrx`，而非假定 GUI 的每一项都有高层 API。[ArcGISProject](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/arcgisproject.htm) [Symbology](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/symbology-class.htm) |
| 布局、图框与整饰元素 | 以 `QgsPrintLayout`/`QgsLayout` 为页面场景；创建后可注册到 `project.layoutManager()`，加入 `QgsLayoutItemMap`、label、legend、scale bar、shape 等元素，并显式控制毫米/厘米位置、尺寸和 map extent。也可将人工审定的 `.qpt` 模板参数化，避免把整张版面重新硬编码。[PyQGIS Layout](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/composer.html) | `Layout` 是 `.aprx` 中的单页布局；`listElements()` 可取文字、图例、图框、表格等元素，`createLayout`、`createMapFrame`、`createMapSurroundElement`、`createTextElement` 等 API 可自动生成页面。`MapFrame` 同时持有页面几何和 camera/extent，是脚本化主图、插图、比例尺和格网的枢纽。[Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [MapFrame](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapframe-class.htm) |
| 导出与地图册 | `QgsLayoutExporter` 对单页提供 `exportToPdf`、`exportToSvg`、`exportToImage`；返回结果码，支持 PDF/SVG/图像专用设置。`QgsPrintLayout.atlas()` 返回其 `QgsLayoutAtlas`，可设置 coverage layer、过滤、排序和文件名表达式，再作为 iterator 一次导出单个多页 PDF 或逐页文件。[QgsLayoutExporter](https://api.qgis.org/api/3.44/classQgsLayoutExporter.html) [QgsLayoutAtlas](https://api.qgis.org/api/3.44/classQgsLayoutAtlas.html) | 以 `arcpy.mp.CreateExportFormat()` 创建输出格式后调用 `layout.export()`；地图册由 `layout.createSpatialMapSeries()` 或 bookmark map series 创建，并由 `currentPageNumber`/`pageRow` 驱动逐页定制。`MapSeries.export()` 原生支持 JPEG、PDF、PNG、TIFF；若需其他格式，可循环设置当前页并导出 Layout。旧 `exportToPDF` 等方法在 Pro 3.4 起为 legacy，新增能力只进入 `export`。[Layout 导出](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) |

#### 脚本化制图的推荐工作流

1. **先由制图人员制作并验收一个工程和版面模板**：固定纸张、图框、格网/图廓、动态文字、字体、颜色、导出选项和命名规则；脚本只替换明确的输入数据、日期/标题、筛选条件、extent 与输出目录。这样保留布局工具的可视审稿能力，也避免把每一个排版坐标埋进业务代码。
2. **数据和符号随工程对象更新**：PyQGIS 读/写 `.qgs`/`.qgz`、装载图层并设置 renderer；ArcPy 打开 `.aprx`、更新连接、图层/符号和 `MapFrame.camera`。用于周期性专题图、行政区/网格分幅图册、同版式多地区报告、数据源迁移后的重出图和发布前 QA。`arcpy.mp` 官方也明确把项目清点、修复数据源、版面/地图册导出和逐页定制列为自动化场景。[QGIS 项目与图层](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/loadproject.html) [arcpy.mp 工作流](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm)
3. **用布局对象而非活动视图导出**：QGIS 使用 `QgsLayoutExporter(layout)`；ArcPy 优先使用 `Layout`/`MapFrame`，不要把运行中窗口截图当成可复现输出。Esri 明确指出，独立脚本无法可靠导出 `MapView`，而持久化了尺寸信息的 `MapFrame` 在应用内外结果一致。[QGIS 导出](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/composer.html) [MapFrame 导出边界](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapframe-class.htm)
4. **输出后做机器检查和视觉抽检**：检查导出返回值/文件是否存在、页数和命名，抽样比较 PDF/SVG/PNG，尤其覆盖长文字、空图例、极端 extent、跨 CRS、缺失字体和密集矢量。PyQGIS 可注册 layout validity check；`QgsLayoutExporter` 也能报告因透明度等高级效果导致的矢量输出栅格化风险。[PyQGIS 布局检查](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/composer.html) [QgsLayoutExporter](https://api.qgis.org/api/3.44/classQgsLayoutExporter.html)

#### 约束与版本陷阱

- **把运行时随模板锁定。**PyQGIS 必须匹配 QGIS 的库、provider、Python 与资源路径；项目中的外部数据若迁移会产生坏路径，虽可借 `QgsPathResolver` 预处理，但应先检查图层有效性。ArcPy 的独立任务既须在 Pro conda 环境运行，也须在运行节点获得 Pro 授权；`arcpy-base` 则不包含所有 Pro Python 功能。[PyQGIS 独立部署](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/intro.html) [QGIS 路径处理](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/loadproject.html) [ArcPy 环境](https://pro.arcgis.com/en/pro-app/3.6/arcpy/get-started/installing-arcpy.htm)
- **区分“当前工程”和磁盘工程”。**`ArcGISProject("CURRENT")` 只能在 Pro 内使用；以路径打开的工程可在外部脚本运行，但改动不会立即反映到当前 GUI。对同一 `.aprx` 的多个引用，只有第一个可直接保存，其他为只读；生产任务宜复制模板到独立工作目录再写回输出。[ArcGISProject](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/arcgisproject.htm)
- **按 API 主版本维护，而非复制旧示例。**ArcGIS Desktop 的 `arcpy.mapping` 已由 Pro 的 `arcpy.mp` 取代；Pro 3.4 以后应迁移到 `export`。`arcpy.mp` 的符号 API 只覆盖常用 renderer/colorizer，CIM 可补足但会受 2.x/3.x 的 `V2`/`V3` 模型演进影响，应为目标 Pro 版本做回归测试。[迁移到 arcpy.mp](https://pro.arcgis.com/en/pro-app/latest/arcpy/mapping/migratingfrom10xarcpymapping.htm) [Symbology/CIM](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/symbology-class.htm)
- **地图册要显式刷新和验收。**ArcPy 在修改索引图层要素、索引字段或图框范围后需 `mapSeries.refresh()`，否则保留旧设置；PyQGIS Atlas 的 filter、sort、page/filename expression 也应在输出前验证。不要仅凭“导出成功”判定每页范围、动态文本和图例正确。[ArcPy MapSeries refresh](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) [QgsLayoutAtlas](https://api.qgis.org/api/3.44/classQgsLayoutAtlas.html)

| 取舍 | PyQGIS | ArcPy / `arcpy.mp` |
| --- | --- | --- |
| 最合适的组织条件 | 开源/跨平台部署、已有 QGIS/ GDAL/GRASS 流程，愿意维护 QGIS 运行时与 `.qpt`/项目模板。 | 已有 ArcGIS Pro 许可、Portal/Enterprise、`.aprx`/`.lyrx` 样式资产与 Windows 运行节点。 |
| 批量版面能力 | `QgsLayout` + `QgsLayoutExporter` + `QgsLayoutAtlas`；PDF/SVG/图像和 Atlas iterator 是同一对象链。 | `ArcGISProject` + `Layout` + `MapFrame` + `MapSeries`；项目、布局、元素、输出格式和地图册是同一对象链。 |
| 符号化策略 | renderer/symbol 层级可直接由 QGIS Core API 配置；复杂模板通常先在 GUI 审定。 | 常规 renderer 直接改；不支持项以 CIM 或受控 `.lyrx` 样式资产处理。 |
| 部署风险 | 安装目录、动态库、GDAL/provider、字体和 QGIS API 版本一致性。 | Pro 安装/授权、conda 环境、扩展许可、Windows 依赖，以及 Pro 主版本 API 变化。 |
| 共同边界 | 两者自动化的是桌面 GIS 的工程与版面对象，不会替代面向终端用户的浏览器交互地图；仍应把 Web 地图和出版版面作为分别验收的交付物。 | 同左。 |

## 4. 桌面 GIS：QGIS、ArcGIS Pro 与 GRASS

| 工具 | 生态和部署 | 数据/分析能力 | 布局型版面地图能力 |
| --- | --- | --- | --- |
| **QGIS Desktop** | GPL 开源桌面 GIS，插件和 PyQGIS 扩展生态；可作为 OGC 服务客户端，且 QGIS Server 可发布 WMS/WCS/WFS/OAPIF 等服务。[许可/下载](https://www.qgis.org/download/) [QGIS 功能](https://docs.qgis.org/3.44/en/docs/about/features.html) [PyQGIS 控制台](https://docs.qgis.org/3.44/en/docs/user_manual/plugins/python_console.html) | 矢量、栅格、数据库管理、制图、编辑与 Processing；可调用 GDAL、SAGA、GRASS、OTB、R 等算法，适合桌面一体化工作流。[QGIS 分析功能](https://docs.qgis.org/3.44/en/docs/about/features.html) | **原生 Print Layout**：可放置 2D/3D 地图、文本、图像、图例、比例尺、箭头、表、HTML frame 等元素，支持多地图/多页、位置对齐旋转、模板、图片/PostScript/PDF/SVG 导出；Atlas 可按覆盖图层逐要素生成地图册。[Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [Atlas/输出](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html) |
| **ArcGIS Pro** | Esri 专业桌面 GIS；项目可与 ArcGIS Online/Enterprise 共享，产品与扩展受许可级别约束。[产品介绍](https://pro.arcgis.com/en/pro-app/3.0/get-started/get-started.htm) [许可](https://doc.esri.com/en/arcgis-pro/latest/get-started/licensing-arcgis-pro.html) | 2D/3D、可视化、分析、数据管理、地理处理和企业 ArcGIS 内容协同；适合已采用 Esri Portal、WebMap、Enterprise geodatabase 的组织。[ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.0/get-started/get-started.htm) | **原生 Layout**：虚拟页上可组织一个或多个 map frame、比例尺、指北针、题名、图例与 grids/graticules，所见即所得地打印/导出；Map Frame 是页面内独立 extent 的容器，可加边框、背景、阴影和格网。[Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) [Map frame](https://pro.arcgis.com/en/pro-app/3.5/help/layouts/add-and-modify-map-frames.htm) [Grid](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/add-a-grid.htm) |
| **GRASS GIS** | 免费开源的地学计算引擎，含 CLI、Python、Jupyter 和 Desktop GUI；可以与 QGIS Processing 集成。[GRASS](https://grass.osgeo.org/) [接口](https://grass.osgeo.org/grass-stable/manuals/interfaces_overview.html) | 强项为栅格、矢量、地形、水文、生态、影像、时间序列与大规模可复现分析；内建时间框架和 Python API。[GRASS 主页](https://grass.osgeo.org/) | 有 GUI 显示和可编程地图输出，但官方定位是计算/处理引擎，非 QGIS/ArcGIS Pro 同等级的多元素出版版面和地图集编辑器。实际项目常由 GRASS 计算、QGIS/Pro 最后成图。此为基于官方能力定位的选型建议。[GRASS 接口](https://grass.osgeo.org/grass-stable/manuals/interfaces_overview.html) |

### 4.1 为什么 QGIS/ArcGIS Pro 是布局制图的直接答案

“布局型版面地图”不是只画出一个地理视图，而是把地图视图嵌入一个**有固定物理页面和多个互相独立元素**的排版对象。QGIS 与 ArcGIS Pro 的官方模型均直接表达这件事：

| 版面要求 | QGIS | ArcGIS Pro |
| --- | --- | --- |
| 图框（地图项）及各自 extent | 一个 Layout 可有多个 map view，每个地图项有自己的 extent。[QGIS Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) | Map frame 是页面中地图的容器；同一 map 的每个 frame 具有独立 extent。[ArcGIS Map Frame](https://pro.arcgis.com/en/pro-app/3.5/help/layouts/add-and-modify-map-frames.htm) |
| 图例、比例尺、指北针、文字、表、插图 | 原生元素包括 legend、scale bar、arrows、labels、tables、HTML frame 等。[QGIS Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) | 官方 Layout 直接列出 scale bar、north arrow、title、text、legend、map frame 等。[ArcGIS Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) |
| 经纬网/坐标格网、图廓/整饰线 | Layout Map item 可配置栅格/坐标与图框相关设置；具体格网和注记须按项目 CRS、比例尺检查。[QGIS Map View](https://docs.qgis.org/3.44/en/docs/user_manual/map_views/map_view.html) | 可在 map frame 上添加 graticule、measured/MGRS/reference/custom grid；reference grid 提供 neatline 设置。[ArcGIS Grid](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/add-a-grid.htm) [ArcGIS Reference grid](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm) |
| 地图集/分幅图 | Atlas 以 coverage layer 的每个要素输出，支持过滤、排序、动态文本和单文件 PDF。[QGIS Atlas](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html) | Map Series 从一个含 map frame 的 layout 生成逐页 extent，含 spatial/bookmark/thematic 三类。[ArcGIS Map Series](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/map-series.htm) |
| 交付与质量 | 可出图像、SVG、PDF；可配置分辨率、矢量化、Geospatial PDF 等输出选项。[QGIS 输出](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html) | Layout 是为打印设计的虚拟页面，页面大小决定打印/导出结果；适于模板化地图册。[ArcGIS Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) |

**选 QGIS**：需开源、跨平台、可审计的项目文件/模板、插件和 PyQGIS 自动化，或团队已把 GDAL/GRASS/SAGA/OTB 作为处理底座。  
**选 ArcGIS Pro**：组织已有 ArcGIS Online/Enterprise、许可、Portal 内容与 Esri 专业工作流，需要减少产品间内容/权限/样式流转成本。  
两者均能读服务、做分析、出静态地图；差异主要在许可、组织平台与团队技能，而非“只有一个能做图框、图例或地图集”。

## 5. 横向功能矩阵

说明：**原生**=官方核心产品/文档直接提供该对象模型或能力；**组合/编程**=可与其他库或应用代码实现，但需要自行负责模板、测试与输出一致性；**非目标**=不是此工具的合理主职责。

| 工具族 | 动态 Web 交互 | 2D/3D 场景 | 空间分析/ETL | 多格式/服务互操作 | 正式固定版面、图框/图廓、图例/比例尺 | 地图集 | 自动化 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MapLibre / Mapbox GL JS | 原生 | GL JS 2D/3D 视觉表达原生 | 组合（前端计算/后端） | 瓦片、GeoJSON、供应商服务为主 | 组合；Mapbox 静态图不是 PDF/SVG 布局 | 组合 | JS/后端 |
| OpenLayers / Leaflet | 原生 | 以 2D 为主 | 组合 | OpenLayers 原生 OGC/多格式更强 | 组合 | 组合 | JS/后端 |
| CesiumJS / deck.gl | 原生 | 三维地球或大数据可视化原生 | 组合 | 取决于接入底图/服务 | 非目标 | 非目标 | JS/后端 |
| ArcGIS Maps SDK JS | 原生 | 2D/3D 原生 | 客户端分析 + ArcGIS 服务组合 | ArcGIS 生态原生 | 组合；版面交给 Pro | 组合 | JS + ArcGIS 服务 |
| GeoPandas/Shapely/Rasterio | 可经 Folium/HTML | 非核心 | 原生强项 | 强，实际受 GDAL/driver 影响 | Cartopy/Matplotlib 编程实现 | 编程 | Python/Notebook/CI |
| QGIS | 非其主交付物（可发布/插件/Server 配合） | 2D/3D 桌面视图 | 原生强项 | 原生强 | **原生强项** | **Atlas 原生** | Processing/PyQGIS |
| ArcGIS Pro | 非其主交付物（与 Online/Enterprise/JS 配合） | 2D/3D 桌面视图 | 原生强项 | ArcGIS 服务强 | **原生强项** | **Map Series 原生** | ArcPy/地理处理 |
| GRASS | 非核心 | 桌面显示/三维可视化 | **原生强项** | 强，常借 GDAL/QGIS | 非目标 | 非目标 | CLI/Python/Jupyter |

矩阵中 Web/Python/桌面行的能力判定分别可追溯到：MapLibre 的交互 WebGL 定位、Mapbox 的静态输出限制、OpenLayers 的 OGC/格式支持、Cesium/deck.gl 的三维/大数据定位、GeoPandas 的 Matplotlib/Folium 接口、QGIS Layout/Atlas、ArcGIS Pro Layout/Map Series 和 GRASS 官方接口说明。[MapLibre](https://maplibre.org/maplibre-gl-js/docs/) [Mapbox 印刷](https://docs.mapbox.com/help/dive-deeper/static-maps/) [OpenLayers](https://openlayers.org/) [Cesium](https://cesium.com/learn/cesiumjs-fundamentals/) [deck.gl](https://deck.gl/docs) [GeoPandas](https://docs.geopandas.org/en/latest/docs/user_guide/interactive_mapping.html) [QGIS](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) [GRASS](https://grass.osgeo.org/grass-stable/manuals/interfaces_overview.html)

## 6. 面向常见目标的推荐组合

| 目标 | 优先组合 | 原因与需避免的误区 |
| --- | --- | --- |
| 消费者/业务 Web 地图（查询、点选、筛选、实时状态） | MapLibre GL JS + 自有 tiles/GeoServer/PostGIS；或 Mapbox GL JS + Mapbox 服务 | Web runtime 才直接处理视图、事件与业务 UI；不要把 PDF 版面工具当在线地图渲染器。[MapLibre](https://maplibre.org/maplibre-gl-js/docs/) [Mapbox](https://docs.mapbox.com/mapbox-gl-js/) |
| 行业 OGC 服务、跨系统接入、非 Web Mercator/复杂服务兼容 | GeoServer + OpenLayers + QGIS | GeoServer 提供 WMS/WFS/WCS/WMTS，OpenLayers 支持 OGC 服务/多种格式，QGIS 是服务客户端和成图端。[GeoServer](https://docs.geoserver.org/main/en/user/services/index.html) [OpenLayers](https://openlayers.org/) [QGIS 功能](https://docs.qgis.org/3.44/en/docs/about/features.html) |
| 城市三维、地形、3D Tiles、时空数字孪生 | CesiumJS（必要时叠加 deck.gl）+ 适当的 terrain/3D Tiles 服务 | CesiumJS 直接面向 WebGL 三维地球、terrain 与 3D Tiles；deck.gl 强于大规模专题图层，不取代三维数据生产线。[Cesium](https://cesium.com/learn/cesiumjs-fundamentals/) [deck.gl](https://deck.gl/docs) |
| 数据清洗、空间连接、批处理、遥感栅格、可复现分析 | GeoPandas + Shapely + pyproj + Rasterio；重分析加入 GRASS；共享数据用 PostGIS | 分别覆盖表/几何/CRS/栅格/专业地学计算；注意平面计算和 driver/CRS 风险。[GeoPandas](https://geopandas.org/en/latest/docs/reference.html) [Rasterio](https://rasterio.readthedocs.io/en/stable/) [GRASS](https://grass.osgeo.org/) |
| 国标/行业规范纸图、报告 PDF、A0/A1 大版面、图框图廓、坐标格网、插图、地图册 | **QGIS Print Layout** 或 **ArcGIS Pro Layout**；Python 负责预处理与批量驱动 | 两者都把纸页、map frame、元素、格网、地图集作为原生对象。不要仅因 Web SDK 可截图，就把截图管线当出版制图系统。[QGIS Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [QGIS Atlas](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html) [ArcGIS Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) [ArcGIS Map Series](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/map-series.htm) |
| 已验收版面模板的夜间/周期性批量成图与地图册 | **QGIS + PyQGIS**，或 **ArcGIS Pro + ArcPy (`arcpy.mp`)**；工程模板与脚本一同版本控制 | 两套 API 都能直接驱动桌面工程中的布局、图框、动态元素和地图册，不必另造 Figure/Axes 排版引擎。把人工审定的 `.qpt`/`.qgz` 或 `.aprx`/`.lyrx` 作为模板，脚本只参数化数据、范围、文字和导出；每次版本升级仍须做渲染回归。[PyQGIS Layout/Atlas](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/composer.html) [arcpy.mp](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm) [ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) |
| 大批量、可版本控制的期刊/报告图 | GeoPandas + Cartopy + Matplotlib；对最终版面做渲染回归检查 | Cartopy 能处理投影、格网和标签，Matplotlib 能管理 Figure/Axes 和 PDF/SVG 输出；但团队要自己沉淀版式模板及质检规则。[Cartopy](https://cartopy.readthedocs.io/stable/) [Gridliner](https://cartopy.readthedocs.io/stable/matplotlib/gridliner.html) [Matplotlib](https://matplotlib.org/stable/Matplotlib.pdf) |
| 已投入 Esri 组织平台 | ArcGIS Pro + ArcGIS Online/Enterprise + ArcGIS Maps SDK JS | 桌面版面、Web 组件、门户内容和身份/服务可以在同一产品线管理；仍需按许可等级与扩展核对可用能力。[ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.0/get-started/get-started.htm) [ArcGIS JS](https://developers.arcgis.com/javascript/latest/get-started/) [Pro 许可](https://doc.esri.com/en/arcgis-pro/latest/get-started/licensing-arcgis-pro.html) |

## 7. 落地时必须独立验收的风险

1. **CRS、轴顺序与单位。**Web 视图、分析 CRS 与印刷 CRS 不应靠猜测衔接。`set_crs` 不转换坐标，GeoPandas 的通用操作为平面计算；面积、距离、比例尺和格网必须在明确 CRS 下验收。[GeoPandas CRS](https://geopandas.org/en/stable/docs/reference/api/geopandas.GeoDataFrame.set_crs.html) [GeoPandas 平面操作](https://geopandas.org/en/stable/docs/reference/api/geopandas.overlay.html)
2. **数据格式与服务不是无条件可互通。**GDAL/pyogrio 能力取决于实际打包的 driver；服务端使用 WMS/WMTS/OGC API 时，需对目标客户端、CRS、样式和性能作集成测试。[GDAL](https://gdal.org/en/stable/about.html) [pyogrio driver](https://pyogrio.readthedocs.io/en/latest/install.html) [GeoServer WMS](https://docs.geoserver.org/main/en/user/services/wms/reference/)
3. **符号和字体的跨端一致性。**GL Style、QGIS 项目样式、ArcGIS 样式与 PDF/SVG 不是同一渲染器；把“同源数据”与“像素级相同的符号”分开验收。对正式输出固定字体、DPI、色彩空间、透明度和 PDF/SVG 矢量化策略，并将导出样张纳入审稿。QGIS 官方文档也提示某些透明度会导致 PDF 栅格化。[QGIS PDF 输出](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html)
4. **Web 静态图的法律与产品限制。**Mapbox Static Images 使用时有 token、请求尺寸、归因和计费约束；移除归因仍有在页面/文档其他位置正确归因的义务。不要把“免费 JS 包”误认为“免费且可任意再分发的底图数据”。[Mapbox Static Images](https://docs.mapbox.com/api/maps/static-images/) [Mapbox 计费](https://docs.mapbox.com/accounts/guides/pricing/)
5. **地图集的动态元素。**QGIS Atlas 和 ArcGIS Map Series 会随覆盖要素/索引要素更新 extent 与文本等元素；实施前用边界要素、长文字、跨带 CRS、极小/极大范围和无法放置图例的页做抽样验收。[QGIS Atlas](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html) [ArcGIS Map Series](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/map-series.htm)

## 8. 尚需按具体项目决策的事项

以下是选型建议而非任何官方工具的承诺，必须由项目需求、样本数据和验收图来定：

1. **数据主库与交换格式**：事务编辑、多用户权限和空间 SQL 通常考虑 PostGIS；分析/湖仓可另用 GeoParquet/对象存储。要求生产与消费方对几何、CRS、空值和字段类型做契约测试。
2. **服务形态**：需要可编辑要素、预渲染地图还是矢量瓦片？前者可能需要 WFS/OGC API Features，后两者对应 WMS/WMTS/OGC API Tiles/自有瓦片服务；不要仅因前端能显示就假设可编辑或可分析。[GeoServer 服务](https://docs.geoserver.org/main/en/user/services/index.html) [OGC API Tiles](https://ogcapi.ogc.org/tiles/)
3. **版面规范**：纸张和出血、CMYK/RGB、比例尺精度、经纬网/坐标网、图廓线、审图号、保密标志、字体授权、数据来源和署名必须写成可测试的模板要求；它们不应留给临近交付时的手工记忆。
4. **交付边界**：若一个项目同时要“可交互 Web 地图”和“可出版 PDF 地图”，将二者列为两个交付物、共用数据与部分符号资产，分别验收。不要要求同一套浏览器截图同时承担高分辨率矢量版面、地图集和无障碍交互应用。

## 官方来源索引（按生态）

### Web

- [MapLibre GL JS 文档](https://maplibre.org/maplibre-gl-js/docs/)、[MapLibre Style Spec](https://maplibre.org/maplibre-style-spec/)、[MapLibre GL JS 许可证](https://github.com/maplibre/maplibre-gl-js/blob/main/LICENSE.txt)、[MapLibre 插件](https://maplibre.org/maplibre-gl-js/docs/plugins/)
- [Mapbox GL JS](https://docs.mapbox.com/mapbox-gl-js/)、[Mapbox Static Images API](https://docs.mapbox.com/api/maps/static-images/)、[Mapbox 静态与印刷地图](https://docs.mapbox.com/help/dive-deeper/static-maps/)、[Mapbox 账户/计费](https://docs.mapbox.com/accounts/)
- [OpenLayers](https://openlayers.org/)、[OpenLayers API](https://openlayers.org/en/latest/apidoc/)、[Leaflet](https://leafletjs.com/download.html)
- [CesiumJS Fundamentals](https://cesium.com/learn/cesiumjs-fundamentals/)、[CesiumJS 下载](https://cesium.com/downloads)、[deck.gl](https://deck.gl/docs)、[deck.gl Layers](https://deck.gl/docs/developer-guide/using-layers)
- [ArcGIS Maps SDK for JavaScript](https://developers.arcgis.com/javascript/latest/get-started/)、[ArcGIS JS References](https://developers.arcgis.com/javascript/latest/references/)

### Python 与底座

- [GeoPandas 数据结构](https://geopandas.org/en/latest/docs/user_guide/data_structures.html)、[GeoPandas API](https://geopandas.org/en/latest/docs/reference.html)、[GeoPandas 交互地图](https://docs.geopandas.org/en/latest/docs/user_guide/interactive_mapping.html)
- [Shapely](https://shapely.readthedocs.io/en/stable/index.html)、[pyproj](https://pyproj4.github.io/pyproj/stable/index.html)、[Rasterio](https://rasterio.readthedocs.io/en/stable/)、[pyogrio](https://pyogrio.readthedocs.io/)、[Cartopy](https://cartopy.readthedocs.io/stable/)、[Folium](https://folium.readthedocs.io/en/latest/)
- [PyQGIS Developer Cookbook](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/index.html)、[PyQGIS Layout/导出](https://docs.qgis.org/3.44/en/docs/pyqgis_developer_cookbook/composer.html)、[QgsLayoutExporter](https://api.qgis.org/api/3.44/classQgsLayoutExporter.html)、[QgsLayoutAtlas](https://api.qgis.org/api/3.44/classQgsLayoutAtlas.html)
- [ArcPy/Pro Python 环境](https://pro.arcgis.com/en/pro-app/3.6/arcpy/get-started/installing-python-for-arcgis-pro.htm)、[安装 ArcPy](https://pro.arcgis.com/en/pro-app/3.6/arcpy/get-started/installing-arcpy.htm)、[arcpy.mp](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm)、[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm)、[ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm)
- [GDAL](https://gdal.org/en/stable/about.html)、[PostGIS](https://postgis.net/docs/postgis-en.html)、[GeoServer](https://docs.geoserver.org/main/en/user/introduction/overview/)、[OGC API - Tiles](https://ogcapi.ogc.org/tiles/)

### 桌面 GIS

- [QGIS 功能](https://docs.qgis.org/3.44/en/docs/about/features.html)、[QGIS Print Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html)、[QGIS 输出和 Atlas](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html)、[PyQGIS Console](https://docs.qgis.org/3.44/en/docs/user_manual/plugins/python_console.html)
- [ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.0/get-started/get-started.htm)、[ArcGIS Pro Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm)、[Map Frame](https://pro.arcgis.com/en/pro-app/3.5/help/layouts/add-and-modify-map-frames.htm)、[Map Series](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/map-series.htm)
- [GRASS GIS](https://grass.osgeo.org/)、[GRASS 接口](https://grass.osgeo.org/grass-stable/manuals/interfaces_overview.html)
