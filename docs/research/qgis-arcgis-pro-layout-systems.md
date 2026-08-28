# QGIS Print Layout 与 ArcGIS Pro Layout：布局制图系统深度调研

> **范围与版本。**本文只比较用于正式纸图、PDF/SVG、图册和模板化制图的布局系统，不把 Web 地图截图当作等价能力。资料截点为 **2026-07-29**：QGIS 使用当时最新的 **4.2**（QGIS 官方发布于 2026-07-03，且 4.2 文档建议优先使用此版）；ArcGIS Pro 使用 Esri 标注为 released version 的 **3.6**。所有事实均链接到 QGIS 或 Esri 官方文档/API；“建议”“待验证”是本文的工程判断，而非厂商承诺。[QGIS 4.2 发布说明](https://qgis.org/project/visual-changelogs/visualchangelog42/) [QGIS 4.2 文档前言](https://docs.qgis.org/4.2/en/docs/about/preamble.html) [ArcGIS Pro 3.6 介绍](https://pro.arcgis.com/en/pro-app/3.6/get-started/get-started.htm)
>
> **部署版本说明。**已有总览报告以 **QGIS 3.44** 为基线；它是 3.x 的最终/最后一个 LTR，而 QGIS 官方计划让 4.2 于 2026-10 进入 LTR 仓库。故本文以最新 4.2 验证当前功能，同时保留 3.44 为生产部署兼容基线：文中涉及的核心 Print Layout/Atlas 对象在 3.44 官方文档同样存在，但上线到 3.44 时仍应在目标版本运行模板和脚本回归，不把 4.2 的新增/行为差异静默回填给 3.44。[QGIS 4.x/LTR 计划](https://blog.qgis.org/2025/10/07/update-on-qgis-4-0-release-schedule-and-ltr-plans/) [QGIS 3.44 Print Layout](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) [QGIS 3.44 Atlas/输出](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/create_output.html)

## 结论先行

1. **两者都是原生出版制图系统。**QGIS Print Layout 与 ArcGIS Pro Layout 都以“纸页 + 地图容器 + 整饰元素 + 导出/打印”为一等对象，适合图框、图廓/整饰线、经纬网或坐标格网、图例、比例尺、指北针、插图和分幅图册；它们不是仅输出一张渲染图片的 API。[QGIS Print Layout](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) [ArcGIS Pro Layouts](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm)
2. **最重要的模型差异是“多页布局”与“单页布局+地图集”。**QGIS 的一个 Print Layout 可以包含多页；ArcPy 的 `Layout` 是 `.aprx` 内的单页对象，ArcGIS Pro 的多页地图册由一个 layout 的 **Map Series** 生成。因此，QGIS 更自然地表达“封面—主图—属性表”这种异页文档；Pro 的标准多页交付更应建模为 Map Series，而不是复制许多单页 Layout。[QGIS 页面属性](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [ArcGIS Pro Map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/map-series.htm)
3. **插图的图层独立性是实际选型点。**QGIS 的每个 Map Item 可锁定可见图层和样式，或跟随 map theme，因此同页主图/插图可以用不同图层组合；ArcGIS Pro 的不同 Map Frame 有独立 extent，但若指向同一个 Map，其图层可见性和图层属性在各 view 间共享。要让 Pro 主图与插图显示不同图层组合，通常应使用不同 Map（或在版面规范允许时接受共享图层状态）。[QGIS Map Item—Layers](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) [ArcGIS Pro Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm)
4. **QGIS Atlas 与 ArcGIS Map Series 都能做空间索引图册，但 ArcGIS 的系列类别更明确。**Atlas 以 coverage layer 的表/矢量要素逐个出页；ArcGIS Pro GUI 提供 spatial（索引要素）、bookmark（书签视图）和 thematic（radio group 子图层）三种系列。`arcpy.mp` 的高层对象当前明确覆盖 spatial 与 bookmark；thematic 的批量脚本接口边界应先以样例工程验证，不应假定它等同前两者。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) [Spatial map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/spatial-map-series.htm) [Bookmark map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) [Thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/introduction-to-a-thematic-map-series.htm) [ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm)

## 1. 概念边界与对象模型

### 1.1 两套布局系统实际保存的是什么

| 层次 | QGIS Print Layout | ArcGIS Pro Layout | 对制图工程的含义 |
| --- | --- | --- | --- |
| 工作空间 | `QgsProject` 管理项目内的布局；布局管理器可创建、复制、重命名、删除并打开布局/报告。布局可随 project 保存，也可保存为模板。[QGIS Layout Manager](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | `.aprx` 项目可含多个 Layout；`ArcGISProject.listLayouts()` 返回 Layout 列表，布局宜使用唯一名称。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 两者都应将“项目文件 + 数据连接 + 模板/样式资产”作为可交付的制图源，而不是只保存最终 PDF。 |
| 页面 | 一个 Print Layout 可含**多个页面**，页可分别设尺寸、背景和是否导出；QGIS 还可将布局裁成覆盖所有元素的一张实页。[QGIS 页面与布局属性](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | `Layout` 是项目内的**单页**布局对象；可设置 page width/height、units、RGB/CMYK 色彩模型。地图册页面是 Map Series 的输出页，而不是 `Layout` 内多个纸页。[ArcPy Layout properties](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 需要一个多页说明书/附表时，QGIS 可直接排页；Pro 中应明确是“单页版面”还是“Map Series”，封面、目录等非地图集页需要单独组织与合并输出。 |
| 版面元素 | `QgsLayout` 是核心场景，元素继承 `QgsLayoutItem`；典型有 `QgsLayoutItemMap`、label、legend、scale bar、picture、shape、table、HTML、chart 等。[PyQGIS Layout 模型](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html) | `Layout.listElements()` 可访问 Graphic、Group、Legend、MapFrame、MapSurround、Picture、TableFrame、Text 等元素；页面中也有 grids 与 extent indicators。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [Layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm) | 两者都应给元素稳定、语义化的名称/ID，供模板维护与脚本检索，避免仅凭“第 N 个元素”的顺序操作。**这是工程建议。** |
| 地图容器 | Map Item 是当前主画布的一份地图渲染；每个 Map Item 有自身 extent、比例尺、旋转、CRS、图层和 grid 设置，Layout 可设一个 reference map。[QGIS Map Item](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) [QGIS reference map](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | Map Frame 是指向项目中 Map 或 Scene 的容器；其 extent 与打开的 map view 独立，但同一 Map 的图层内容/选择/可见性并不按 frame 隔离。[Map on a layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-a-map-on-a-layout.htm) [Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) | 不要把“多个图框”误解成“多个完全隔离的地图”。这决定插图、专题比较图和地图集时项目 Map 的拆分方式。 |
| 图册驱动 | `QgsLayoutAtlas` 迭代 coverage layer 的要素；Map Item/标签/表格/HTML 可引用当前 atlas feature 和变量。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | 一个 Layout 可有一个 Map Series；spatial 由 index layer、bookmark 由书签、thematic 由 radio group layer 驱动。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [Map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/map-series.htm) | Atlas/Map Series 是“重复渲染同一版式”的数据模型，不是多张手工复制 layout 的别名。 |

### 1.2 页面、单位、辅助线和版面纪律

**事实。**QGIS 版面有水平/垂直 guides、规则 grid、吸附阈值和 smart guides；grid 与 guide 仅是对齐辅助，不是地图坐标网。页面属性支持尺寸、背景、多个页面及排除某页导出。QGIS 把默认字体、grid/guide 的样式与模板搜索路径放在 Layout Options 中。[QGIS guides、grid 与 pages](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html)

**事实。**ArcGIS Pro 将 ruler units、page units 和元素的位置/尺寸单位联动；guides 不会被打印或导出，可吸附到 guides、元素智能辅助线、纸页边界和打印边距。Layout Properties 还集中管理 Page Setup、Map Series 和 Color Management；打印机边距会随所选打印机变化。[ArcGIS Pro page setup](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/page-setup.htm)

**建议。**先锁定成图规范中的页尺寸、单位（制图通常统一 mm 或 inch）、安全边距、主图框/图例/署名的 guide 坐标，再开始放元素。切勿把 screen DPI 当成纸图比例尺：比例尺由地图容器的 extent、地图单位和纸上尺寸共同决定；DPI 只影响栅格化输出密度。

## 2. 地图框、主图/插图和范围联动

### 2.1 独立 extent 与不同图层组合

**QGIS。**同一布局可有多个 map view，且每个 Map Item 有独立 extent；Map Item 可以手动设比例尺、旋转和 CRS，锁定图层、锁定图层样式或跟随 map theme。由此可用一个项目画布派生主图、全国定位插图和局部放大图，并让它们拥有不同 visible layer set/样式。[QGIS overview](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) [QGIS Map Item properties](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html)

**ArcGIS Pro。**一个 layout 可有多个 Map Frame，每个 frame 可指向 Map、Scene 或暂不指向地图；frame extent 与对应的 map view 独立。它提供 fixed extent、fixed center、fixed center and scale、fixed scale 等约束，以及由另一个 frame 的 extent/center/scale 驱动的单向链接约束。3D Scene 没有 fixed constraints。[Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) [Work with a map on a layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-a-map-on-a-layout.htm)

**差异与建议。**Pro 的 Map Frame 可以独立缩放和平移，却不会为同一 Map 提供独立图层可见性；若插图必须隐藏主图专题层、或要用不同 definition query/显示范围，应建立一个明确命名的“inset map”并将其连接到插图 frame。QGIS 则优先以 map theme + Map Item 的锁定机制达成。前者是官方行为，后者是据此给出的工程建模建议。[ArcGIS Pro Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) [QGIS Map Item layers](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html)

### 2.2 插图定位框、overview/extent indicator 与标签避让

**QGIS。**Map Item 的 **Overviews** 使一个地图项显示另一个地图项的 extent，适用于在全国插图中框出主图范围；Map Item 还允许把比例尺、指北针、插图等 layout item 标为 label blocking items，避免标签压在整饰元素下。地图也可以裁剪到 atlas feature 或版面上的 shape/polygon item。[QGIS Overviews and Map Item](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html)

**ArcGIS Pro。**Layout 元素包含 extent indicator，Map Frame 可以设为 linked map frame extent、center and scale 或 scale；这些能力可将插图与主图/地图集索引 frame 联动。ArcGIS Pro 还允许在 spatial map series 中将其他 frame 设为 linked map series shape/center，以索引要素驱动插图范围。[Layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm) [Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm)

**建议。**正式模板至少应有一条“主图范围—插图定位框”连接和一个极端范围测试：最小、最大、跨投影带/反经线、主图范围接近插图边界。这样能在输出前暴露 extent indicator、overview、格网标注或标签的越界问题。

## 3. 坐标格网、经纬网、图廓与坐标注记

这里的“版面辅助网格”与“地图中的坐标格网”必须分开：前者只辅助摆放元素，不应出现在交付图；后者是 Map Item/Map Frame 的制图元素，必须随投影、比例尺和纸图规范验收。

| 需求 | QGIS Print Layout | ArcGIS Pro Layout | 结论 |
| --- | --- | --- | --- |
| 多 CRS 经纬网/坐标网 | 单个 Map Item 可加多个 grid，每个 grid 可用 map CRS 或另一 CRS；格网可叠放排序。[QGIS grids](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | 每个 Map Frame 可添加 graticule、measured、MGRS、reference、custom 五种 grid，并可组合显示多个坐标体系。[ArcGIS Pro add a grid](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-a-grid.htm) | 两者都能在同图框内叠加经纬度与投影坐标，不能仅以“有/无格网”区分。 |
| 格网间隔 | QGIS 可按地图单位，或按纸上 mm/cm 设间隔；也可按当前 extent 选“pretty”间隔。[QGIS grid interval](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | Pro 的 graticule/measured/MGRS/reference/custom grid 各按其类型配置；custom grid 由 map frame 内与边缘相交的线/面图层和字段/Arcade 标注驱动。[ArcGIS Pro grid types](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/grids-and-graticules.htm) [Custom grid](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-a-grid.htm) | “纸上固定间距”与“坐标单位固定间距”会产生不同视觉与坐标含义，必须写入模板要求。 |
| 图廓/整饰线 | QGIS grid frame 有 `No Frame`、Zebra、航海 Zebra、内/外 tick、line border 等，并可控制四边显隐、尺寸、边距、线/填充色。[QGIS grid frame](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | Pro 的 Reference Grid 有 neatline（地理数据 extent 的边界符号），另有 labels、tabs、gridlines、ticks 和 intersection points 组件；reference grid 仅适用于矩形 frame。[ArcGIS Pro Reference grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm) | 规范中的“图框”应具体到 map item/frame 的外框、grid frame/neatline、还是装饰性 graphic，三者不应混为同一元素。 |
| 坐标注记 | QGIS 可按边设置经/纬或 X/Y、内外放置、水平/垂直/随边方向、DMS/十进制度/自定义表达式、精度和字体；旋转/重投影时可跟随 grid rotation。[QGIS grid coordinates](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | Pro grid 的 labels、tabs、ticks 等都是可配置组件；reference grid 的行列标签可用内置或自定义 scheme，支持文本符号、偏移和竖排。[ArcGIS Pro Reference grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm) | 格网标签是最需要在实际投影、旋转和极端页上检查的元素。 |

**建议。**图廓要求通常来自行业/出版规范，而不是 GIS 软件的默认样式；先把投影、坐标表示法、注记精度、四边显示规则、tick/线宽、是否允许双格网写成验收项，再在两套工具中落模板。若要求高度非标准的 neatline/tab 排版，ArcGIS Pro 官方也提示动态 grid 组件存在可修改边界，必要时可转为 graphics；代价是失去与地图范围的动态联系。[ArcGIS Pro Reference grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm)

## 4. 图例、比例尺、指北针、文字、表格和图形

### 4.1 常用元素的能力与联动对象

| 元素 | QGIS | ArcGIS Pro | 需要注意的约束 |
| --- | --- | --- | --- |
| 图例 | Legend 默认绑定 Map Item；可改标题、列、符号、换行、自动更新，或筛选 linked map/当前 atlas feature 内的条目，标题和符号标签可 data-defined。[QGIS Legend](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_legend.html) | Legend 仅能包含**一个** Map Frame 的项目；可设 fitting strategy、列、换行、边框/背景/阴影，转 graphics 后失去与 Map 的动态连接。[ArcGIS Pro Legend](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-a-legend.htm) | 多主图各要独立 legend；最终转图形应视作“锁版”操作，后续符号更新不会反映。 |
| 比例尺和指北针 | Scale bar 需关联 Map Item；north arrow 是默认同步 Map Item 的 picture item，可根据 grid north/true north 和偏角旋转。[QGIS Scale bar](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_scale_bar.html) [QGIS North arrow](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_image.html) | 两者都是 map-surround 类型；在 map series 中会随 map frame 变化而更新。[ArcGIS Pro layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm) [Spatial series dynamic elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/use-dynamic-text-with-map-series.htm) | 不能把主图的比例尺/指北针错误绑定到插图 frame；应纳入导出前检查。 |
| 动态/静态文字 | 标签支持表达式 `[% … %]`、HTML 和 layout/item/atlas variables；大多数通用属性可 data-defined。[QGIS Label and item options](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_label.html) [QGIS common item properties](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_items_options.html) | Dynamic Text 用标签生成项目、地图、地图框和地图集信息；文本长度会因页变化，Esri 建议用 rectangle text 与 fitting strategy 避免截断。[ArcGIS Pro dynamic text](https://pro.arcgis.com/en/pro-app/latest/help/layouts/add-and-modify-dynamic-text.htm) [Spatial series dynamic elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/use-dynamic-text-with-map-series.htm) | 不能只看第一页标题；最长地名、最长页码/日期必须有测试样本。 |
| 属性表/表框 | QGIS 有 attribute table 和 fixed table；attribute table 可展示项目图层属性，并可在 atlas 中显示当前 coverage feature 或关联对象。[QGIS Table items](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_tables.html) | Table Frame 可显示全部行、map frame 可见行，或 spatial series 的 index feature 行；支持 query、filter、行数上限、字段和 fitting strategy。[ArcGIS Pro Table frames](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-and-modify-table-frames.htm) | 表格不是静态截图；分页、空表、列宽和最长字符串应随每页动态数据验收。 |
| 图片、logo、图形 | QGIS Picture 可用 raster/SVG/URL，SVG 参数和 source 可 data-defined；shape、marker、arrow、HTML、chart 和 elevation profile 也是原生项。[QGIS picture/north arrow](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_image.html) [QGIS Layout items](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | Pro 有 picture、graphic、text、dynamic text、chart/table frame 等；普通 picture 导入后存入 project 并与源文件脱钩，spatial map series 才可使用基于 index layer 的 dynamic picture。[ArcGIS Pro pictures](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-pictures.htm) [ArcGIS Pro layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm) | 机构 logo 更新策略要明确：QGIS 需确认外部路径/嵌入策略；Pro 普通图片不会因替换源文件而更新。 |

### 4.2 样式、条件显示与数据驱动

**QGIS 事实。**通用元素支持位置/尺寸/旋转/frame/background/rendering/item variables；多数属性可由表达式和变量 data-defined。渲染可设 opacity、blending、排除导出；item 还可指定 GeoPDF group。Map Item 可以锁图层/样式，或跟随 map theme；Atlas 情境下可 data-defined 页面方向、元素尺寸/位置、标题、图片和图例列数。[QGIS common item properties](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_items_options.html) [QGIS Map Item layers](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) [QGIS Atlas data-defined example](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)

**ArcGIS Pro 事实。**Layout elements 分为 dynamic 与 static；Map Series 中 map frame、dynamic text、legend、table/chart frame、picture、extent indicator 等可按约束或数据更新。标准元素共同有 border/background/shadow 等属性，并可存为 style；布局可在 Layout Properties 使用 RGB 或 CMYK 及 color profile。对于 Map Series，页面级变化必须由当前 series page、动态元素、map surround constraint 或脚本改变元素来表达。[ArcGIS Pro layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm) [Spatial series dynamic elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/use-dynamic-text-with-map-series.htm) [ArcGIS Pro page setup](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/page-setup.htm)

**建议。**把“动态”分三类管理并分别测试：

1. 地图渲染动态（extent、比例尺、地图集索引/书签、可见图层）；
2. 版面文本/表格/图片动态（字段、表达式、路径、页码）；
3. 版式几何动态（纸张方向、元素位置和尺寸）。

QGIS 对第 3 类有显式的 data-defined 布局属性示例；ArcGIS Pro 若要做逐页非标准版式，先确认动态元素能否满足，再用 `arcpy.mp` 或 CIM 做有限且可回归测试的定制。不要将“GUI 可以手工调一页”直接推论为“可稳定批量调所有页”。

## 5. Atlas 与 Map Series：图册能力、约束和逐页行为

### 5.1 功能对照

| 维度 | QGIS Atlas | ArcGIS Pro Spatial Map Series | ArcGIS Pro Bookmark Map Series | ArcGIS Pro Thematic Map Series |
| --- | --- | --- | --- |
| 逐页驱动 | coverage **table 或 vector layer** 的每个 atlas feature；支持 filter、sort、page name 与 filename expression。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | index layer 的每个 feature；每页 frame extent 取索引要素，可按字段驱动 rotation/空间参考。[Spatial map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/spatial-map-series.htm) | 选定 bookmarks 的每个视图；地图/场景 extent 为书签 extent，书签可保留 time/range。[Bookmark map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) | radio group layer 的每个子图层；每页只显示一个子图层，map extent 不变。[Create thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/create-a-thematic-map-series.htm) |
| 主图范围策略 | controlled Map Item 可按 feature margin、predefined best-fit scale 或 fixed scale；也可固定范围而让 temporal content 随 coverage 表变动。[QGIS Map Item—Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | map series constraint 自动作用于承载 index layer 的 frame，不能移除；可用 index feature extent、linked map series shape/center 等约束驱动其他 frame。[Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) | 由 bookmark 的 camera/extent 驱动；适合不规则层级、2D/3D、time/range 书签。[Bookmark map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) | 设计目标是同范围、多专题显隐，不是逐对象缩放。[Thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/introduction-to-a-thematic-map-series.htm) |
| 逐页元素 | map、label/HTML、table 等可引用当前 atlas feature、关系和变量；图例可筛掉当前 atlas feature 外项目。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) [QGIS Legend](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_legend.html) | dynamic text、legend、table/chart frame、picture、extent indicator 可随页更新；动态文字应预留自适应空间。[Spatial series dynamic elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/use-dynamic-text-with-map-series.htm) | map frame、table frame、legend、scale bar 等按书签页更新。[Bookmark map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) | static 元素保持；map frame、legend、dynamic text 等按当前 radio sublayer 更新。[Thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/introduction-to-a-thematic-map-series.htm) |
| GUI 输出 | PDF 可单文件；图片/SVG 可逐 feature 输出，文件名用 expression。[QGIS Atlas output](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | 支持 JPEG/PDF/PNG/TIFF，页范围/选中 index features/命名/单个多页 PDF。[ArcGIS Pro map series export](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/export-options.htm) | 同左；没有 index layer，不能使用“selected index features”。[ArcGIS Pro map series export](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/export-options.htm) | 同左的输出面板能力；主题系列本身按 radio group。 [Create thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/create-a-thematic-map-series.htm) |

### 5.2 更新和约束的陷阱

**事实。**ArcGIS Pro 的 Spatial Map Series 修改 index feature、名称/排序/范围等驱动字段后可能需要 Refresh；Map Series 由单个 layout 生成，改变任一 layout element 会应用到所有页。QGIS Atlas 同样是同一 Layout 的迭代：每个 feature 会按所有页面及项目的 export 设置处理。[ArcGIS Pro refresh map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/refresh-map-series.htm) [QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)

**事实。**ArcPy 3.6 `Layout.mapSeries` 在启用 spatial series 时返回 `MapSeries`、启用 bookmark series 时返回 `BookmarkMapSeries`；这两个对象可设当前页并导出。官方 high-level `MapSeries` 文档说明其 `export()` 支持 JPEG/PDF/PNG/TIFF，其他格式可以循环当前页并导出 Layout。另一方面，Pro GUI 的 Thematic Map Series 已是独立产品功能，因此若需求是 thematic + 无人值守导出，应先用目标版本的 `.aprx` 做 POC，确认可访问对象和最终渲染，不把 spatial/bookmark 脚本示例套用过去。[ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) [ArcPy BookmarkMapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/bookmarkmapseries-class.htm) [Thematic Map Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/create-a-thematic-map-series.htm)

**建议。**以输出页类型选择驱动模型：行政区/网格/线路分幅选 spatial/Atlas；场景层级、时间范围或异质视角选 bookmark；同范围的方案/指标/主题切换选 thematic。若 QGIS 项目需要书签或专题式图册，不应声称 Atlas “原生等价”于它们；可建立覆盖表/网格并以表达式、map theme、时间范围等实现，但这是需要项目 POC 的组合方案。

## 6. 导出、印刷和交付质量

### 6.1 输出能力

| 项目 | QGIS 4.2 | ArcGIS Pro 3.6 | 工程含义 |
| --- | --- | --- | --- |
| 常规 Layout 输出 | Print/PostScript、图片（PNG/BMP/TIF/JPG 等）、SVG、PDF；多页 PDF 为单文件，图片/SVG 按页输出。[QGIS output](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | Layout 的 `export()`/GUI export 生成所选格式；ArcPy Layout 与 MapFrame 都有导出接口。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 最终版面不要依赖操作系统截图。 |
| 图册输出 | Atlas 可打印、PDF、图片、SVG；PDF 能单文件，文件名可表达式化。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | GUI 的 map series 支持 JPEG/PDF/PNG/TIFF，单 PDF 或逐页文件；其他全系列格式通过 Python 按页导出。[ArcGIS Pro map series export](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/export-options.htm) [ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) | 文件名、页排序和合并策略也应进入验收要求。 |
| 矢量与栅格化 | QGIS 可“always export as vectors”，也可全页 raster；高级 effects 可能要求项目局部 raster 化。SVG/PDF 可控制文字为 text/outline。[QGIS export settings](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) [QGIS PDF/SVG export](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | `PDFFormat` 可压缩 vector streams、选择 marker 作为字体或 polygon、嵌 color profile；PDF 可保留地理参考、annotation、label 和图层属性，亦能 rasterize 内容减小文件。[ArcPy PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm) | “导出 PDF”并不保证全矢量、可编辑或视觉一致，需检查透明度、effects、符号和字体。 |
| 地理参考 PDF/图像 | PDF/TIFF 默认可地理参考；其他格式可生成 world file。GeoPDF 可含 map themes、矢量要素信息和逻辑 groups。[QGIS output](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | PDF 输出可包含 georeference info、layers/attributes；PDFFormat 可选择 profile、font markers 等行为。[ArcPy PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm) | GeoPDF 是附加交付能力，不能替代普通 PDF 版面审查。 |
| 颜色 | QGIS 文档在布局导出层强调输出分辨率、vector/raster、压缩和 metadata；颜色管理/印厂 profile 的最终可用性应以目标运行环境和样张验证。[QGIS export settings](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | Layout 支持 RGB/CMYK 色彩模型和 color profile；PDF 可嵌入 profile，Esri 明确说明未嵌入或非色彩管理查看器可能造成显著色差。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [ArcPy PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm) | 印刷交付应在目标印厂 profile、打样设备和 PDF viewer 上签样，不能只看 GIS 预览。 |

### 6.2 已知渲染/印刷风险

- **QGIS 事实：**矢量图层或其符号 opacity 小于 100% 会强制该对象在 PDF 中栅格化，可能显著增大文件；tile-based raster export 为节省内存可能出现 seam，关闭 tiled export 可修复但会增加内存；强制全矢量可能与 Layout 预览不同。[QGIS PDF/SVG export](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)
- **QGIS 事实：**SVG 输出仍受 Qt 底层限制；将文字输出为 outline 可以规避目标机器缺字体的显示问题，但会损失可编辑文字/搜索性。这是“输出 text 还是 outline”选项的直接后果。[QGIS SVG export](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)
- **ArcGIS Pro 事实：**嵌入 color profile 有助于跨设备颜色一致；若字体 marker 不能嵌入，`convertMarkers=True` 可转为 polygon，但只作用于基于字体字符的 marker，不作用于普通文本。[ArcPy PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm)
- **建议：**对每次正式发布保存四份证据：导出设置清单、打印级 PDF、100% 放大检查截图、与基准样张的差异记录。选择性抽查最复杂页（透明栅格、长图例、双格网、最大要素数、最长文本、深色底图）比只看第一页更有效。

## 7. 脚本化制图：只说明布局接口的分工

本节不重复既有 PyQGIS/ArcPy 总览，而只说明布局系统的自动化切入点。

| 任务 | PyQGIS（QGIS 4.2） | `arcpy.mp`（ArcGIS Pro 3.6） | 建议 |
| --- | --- | --- | --- |
| 访问/创建布局 | `QgsPrintLayout` 可临时创建，注册至 `project.layoutManager()` 后随 project 保存；所有版面项目继承 `QgsLayoutItem`。[PyQGIS Layout](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html) | `ArcGISProject.listLayouts()` 访问现有 layout；`createLayout`、`createMapFrame`、`createMapSurroundElement`、`createTableFrameElement` 等可创建版面及关键元素。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 生产环境优先加载人工签核模板，仅脚本化数据、范围、文字、文件名和导出，不要无需求地用代码重建全部美术版式。 |
| 参数化元素 | 用 item ID/name 定位，表达式/variables/data-defined 覆盖元素属性；`QgsLayoutExporter` 输出 PDF/SVG/图像，`QgsAbstractValidityCheck` 可加版面自定义检查。[QGIS item properties](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_items_options.html) [PyQGIS validation/export](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html) | `listElements()` 访问元素；MapSeries current page 与 page row 可做逐页定制。Pro 3.4 起 `export()` 是当前接口，旧 `exportTo*` 仅保留为 legacy。[ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) [ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) | 元素名称、字段名与输出断言属于模板契约，脚本启动时应明确检查并失败退出。 |
| 自动化边界 | QGIS cookbook 区分快速 `QgsMapRendererJob` 渲染与精细 `QgsLayout` 输出；后者用于纸图元素组合。[PyQGIS Map rendering and printing](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html) | `arcpy.mp` 支持自动导出 Layout、MapView、MapSeries、Report；但布局工作应由 persistent Layout/MapFrame 负责，而非从 GUI 活动 view 推断版面状态。[arcpy.mp introduction](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm) [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 脚本的正确单元是“确定的项目模板 + 数据快照 + 参数 + 导出配置”，不是桌面窗口。 |

## 8. 布局制图资产目录：原生文件边界、团队结构与交付

本节的一个前提是：**软件产生的文件并不等于团队应采用的目录结构**。以下先说明每种产品原生支持的文件及其边界；随后给出可审计的团队目录。除 `QGS/QGZ` 与 `.atbx` 的官方公开格式外，不把文件内部结构当作可编辑 API；尤其不要凭扩展名推断 `.aprx`、`.pagx`、`.stylx` 或包文件的未文档化内部目录。

### 8.1 QGIS：项目、布局、图层、样式与数据资产

| 文件/目录 | 原生作用与已公开结构 | 布局复用时的边界与例子 |
| --- | --- | --- |
| `atlas.qgs` | QGIS 项目主文件，保存为 XML；项目状态包括图层、图层样式、地图视图、Print Layout、布局元素和 Atlas 设置，但**不是数据副本**。[QGIS Project Files](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/project_files.html) | 可用作可读的项目审查对象，例如 `qgis/projects/atlas.qgs`。若使用 auxiliary storage，必须连同同目录的 `atlas.qgd` 传递。项目文件格式会随版本演进，XML 可读不等于应手工批量改写或可跨版本自动合并。 |
| `atlas.qgz` | 默认的压缩项目格式，是 ZIP 容器；官方说明其中嵌有 `.qgs` 和关联的 SQLite 辅助数据库 `.qgd`，可解压查看这两类文件。[QGIS Project Files](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/project_files.html) | `qgis/projects/atlas.qgz` 是较不易遗漏 auxiliary data 的工程入口；它**不**自动纳入 GeoPackage 以外的数据、普通 SVG/图片、字体或远程服务。不要把 ZIP 内除 `.qgs/.qgd` 外的内容当作稳定清单。 |
| `atlas.qgd` | auxiliary storage 的 SQLite 数据库。使用 `.qgs` 时它在同目录；使用 `.qgz` 时被嵌入包内。它可保存不写回原始数据源的辅助字段/属性；删除 auxiliary layer 会失去对应自定义属性。[Auxiliary storage](https://docs.qgis.org/3.44/en/docs/user_manual/working_with_vector/vector_properties.html) | 它不是独立工程或通用数据包。示例：只读道路数据的手工标注位置保存在 `atlas.qgd`；应将它与同名 `.qgs` 成对管理。 |
| `A3_landscape.qpt` | Print Layout 模板。布局可“Save as Template”，其他布局可“Add Items from Template”；也可从 Browser/文件管理器拖入布局创建或追加其 items。[QGIS Layout templates](https://docs.qgis.org/3.44/en/docs/user_manual/print_layout/overview_layout.html) | `qgis/templates/A3_landscape.qpt` 可放图框、图例、比例尺和标题占位符。它不是完整项目或数据包；其中引用的外部资源仍须可解析。 |
| `admin.qlr` | Layer Definition File，包含图层**数据源引用**及其样式；拖入即可把带样式的图层加回项目。[QGIS layer definition](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/general_tools.html) | 适合 `qgis/layers/admin.qlr`，例如引用 `../data/base.gpkg` 内的行政区层并附符号。它不携带数据，换机后仍依赖可用路径或服务。 |
| `roads.qml`、`roads.sld` | `.qml` 是 QGIS layer style；`.sld` 是互操作样式格式，QGIS 的矢量层可导出它。将分类/分级渲染器导入 SLD 后会转为 rule-based，因此要保留 QGIS 原渲染语义时应以 QML 为权威资产。[QGIS style formats](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/general_tools.html) | `qgis/styles/roads.qml` 是团队内复用的样式源；`roads.sld` 仅在需向 SLD 系统交付时生成。二者均不含数据；若样式引用外部 SVG，SVG 也要随工程交付。 |
| `svg/`、`images/`、`fonts/` | QGIS 可配置 SVG 搜索路径；图片/SVG 可以从本地路径或 URL 读取，也可嵌入当前项目、样式数据库或布局模板。Font marker 使用已安装字体渲染。[QGIS SVG paths and embedded files](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/general_tools.html) [QGIS font marker](https://docs.qgis.org/3.44/en/docs/user_manual/style_library/symbol_selector.html) | 例如 `qgis/svg/north-arrow.svg`、`shared/logos/logo.svg`。对必须自包含的 logo 可选择 Embed File；常用 SVG 源文件仍放受控目录并相对引用。`fonts/` 只可保存获许可的字体安装包与安装说明，实际渲染机仍必须安装它，不能假定 QGIS 会从该目录自动加载字体。 |
| `atlas.gpkg` | GeoPackage 是单一 SQLite 容器，可存矢量、栅格瓦片、非空间表和扩展；QGIS 也可把项目存进 GeoPackage，且 Package layers 工具可把矢量层写入 GeoPackage 并可保存 layer styles。[QGIS GeoPackage](https://docs.qgis.org/3.44/en/docs/user_manual/managing_data_source/supported_data.html) [Package layers](https://docs.qgis.org/3.44/en/docs/user_manual/processing_algs/qgis/database.html) | `qgis/data/atlas.gpkg` 可含 `admin`、`roads`、Atlas coverage 等数据（以及可选样式）。单文件利于传递，但不代表自动收集布局图片、字体、外部服务、非 GPKG 数据或模板。 |

**QGIS 的路径和代码风险。**项目可把文件/图层路径设成绝对路径或相对项目文件的路径，且可在项目层覆盖默认值；团队工程应统一采用后者，并将项目、数据和外置资源置于同一可移动根目录。[QGIS Configuration](https://docs.qgis.org/4.2/en/docs/user_manual/introduction/qgis_configuration.html) `.qgs/.qgz` 还能带有宏、自定义表达式函数、actions 等嵌入 Python；QGIS 的 Project Trust 正是为这类代码的打开行为提供控制。因此外部项目应按不受信任代码处理，源码库中也不应存放服务令牌、数据库密码或私有 URL。[QGIS Project Trust](https://docs.qgis.org/4.2/en/docs/user_manual/introduction/qgis_configuration.html)

### 8.2 ArcGIS Pro：项目、布局、地图、样式、工具、连接与包

| 文件/目录 | 原生作用与已公开结构 | 布局复用时的边界与例子 |
| --- | --- | --- |
| `Atlas.aprx` | ArcGIS Pro 项目文件，是 maps、scenes、layouts、charts、reports 及到数据/资源的连接的组织入口；项目的 home folder 通常也容纳默认 geodatabase 和默认 toolbox。[Projects in ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.6/help/projects/what-is-a-project.htm) [Project terminology](https://pro.arcgis.com/en/pro-app/3.6/help/projects/terminology-for-working-with-projects.htm) | `arcgis-pro/project/Atlas.aprx` 是编辑入口而非数据副本。不要凭经验解包或编辑其内部；官方也警告不要用文件系统命令移动/改名项目，而应另存或打 project package。[Project settings](https://pro.arcgis.com/en/pro-app/latest/help/projects/change-a-project-s-settings.htm) |
| `A3_landscape.pagx` | 跨项目共享的 Layout file。与 map/layer file 一样保存 item 属性和数据引用而非所引用数据；它可作为空 Map Frame 模板，导入后再关联本项目的 Map。[Layout files](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/layout-files.htm) [Projects in ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.6/help/projects/what-is-a-project.htm) | `arcgis-pro/layouts/A3_landscape.pagx` 可含图框、经纬网、图例、指北针和动态文字，但对方仍必须能访问数据。它适合版面复用，不是离线交付包。 |
| `Reference.mapx` | 独立 Map/Scene file，保存地图名称、书签、坐标系、图层及其属性；不含图层引用的数据。[Projects in ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.6/help/projects/what-is-a-project.htm) | `arcgis-pro/maps/Reference.mapx` 用来复用主图/插图的地图定义。若连数据一起交付，应改用 `.mpkx` map package。 |
| `Roads.lyrx` | Layer file 可含一个或多个图层、表和 group layers；保存图层属性（如符号化、标注、pop-up）及数据引用，而数据和属性一体的替代物是 `.lpkx` layer package。[LayerFile API](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layerfile-class.htm) [Layer file vs package](https://pro.arcgis.com/en/pro-app/3.6/help/mapping/layer-properties/add-layers-to-a-map.htm) | `arcgis-pro/layers/Roads.lyrx` 适合复用制图规则；同事须能访问源数据。`Save To Layer File` 可选择存为相对路径，使移动时按相对位置重定位。[Save To Layer File](https://pro.arcgis.com/en/pro-app/3.6/tool-reference/data-management/save-to-layer-file.htm) |
| `Organization.stylx` | ArcGIS Pro style 是本地或 portal 上的单文件数据库，存 symbols、colors、color schemes、label placements 和 layout items 等共享资产。[ArcGIS Pro styles](https://pro.arcgis.com/en/pro-app/3.6/help/projects/styles.htm) | `arcgis-pro/styles/Organization.stylx` 可作为组织级北箭头、比例尺、标题文字样式和颜色的权威库。它可被项目引用，但不应把系统 style、个人 Favorites 或在线 style 误认为随项目自动共享的自定义资产。 |
| `Atlas.atbx`、`layout_tools.pyt` | `.atbx` 是 JSON/open-specification 的 ArcGIS toolbox，可存 tools、script tools、models；`.pyt` 是 Python toolbox，工具及属性由 Python 代码定义。[ArcGIS Pro toolboxes](https://pro.arcgis.com/en/pro-app/3.6/help/projects/connect-to-a-toolbox.htm) | `arcgis-pro/tools/Atlas.atbx` 可放 Export PDF、布局验收的模型；`layout_tools.pyt` 可将同一流程代码化。`atbx` 的 JSON 可供审查，但仍应通过 Pro 修改/测试，而非靠手改未测试的 JSON。 |
| `Atlas.gdb/` | File geodatabase 是以 `.gdb` 结尾的**多文件目录**，保存地理数据、属性、索引、lock 等内部文件；内部文件名刻意不可读，不能按单个 feature class 去复制、移动或删除。[File geodatabases and File Explorer](https://pro.arcgis.com/en/pro-app/3.4/help/data/geodatabases/manage-file-gdb/file-geodatabases-and-windows-explorer.htm) | `arcgis-pro/data/Atlas.gdb/` 可保存索引图层、行政区和专题要素。把它视作一个整体，优先通过 Pro/地理处理工具复制和维护，绝不在文件管理器内挑选其子文件。 |
| `production.sde` | 数据库/enterprise geodatabase 连接文件，而非数据库数据；目标数据库、网络、驱动与身份验证仍须可用。[Database connections](https://pro.arcgis.com/en/pro-app/latest/help/projects/connect-to-a-database.htm) | 放在 `connections/production.sde` 仅作为本机或安全部署资产。`.sde` 可选择保存用户名/密码；这会带来凭据暴露风险，不应无审查提交或外发。[Create Database Connection](https://pro.arcgis.com/en/pro-app/3.3/tool-reference/data-management/create-database-connection.htm) |
| `printing.ags` | ArcGIS Server 连接文件，常保存在项目 home folder；可用来访问或发布 Server 内容。[Connect to a GIS server](https://pro.arcgis.com/en/pro-app/3.6/help/projects/connect-to-a-gis-server.htm) | 同样只作本机连接资产。官方将凭据写入 `.ags` 视为最低安全性选项；脚本/CI 应使用安全凭据存储或安全注入，不把 `.ags` 加入版本库。[CreateAGSServerConnection](https://pro.arcgis.com/en/pro-app/latest/arcpy/functions/createagsserverconnection.htm) |
| `Atlas.ppkx` | Project package 是 ArcGIS Pro 的便携单文件包，可收集 project、maps、data/layers、toolboxes、history、styles、layouts、attachments 和适当的 connections。对外共享时会 consolidate/copy 网络/企业库/服务数据、复制 styles 并移除 connections；内部包可保留 UNC/企业库引用和 connections。[Share a project package](https://pro.arcgis.com/en/pro-app/3.3/help/sharing/overview/project-package.htm) | `arcgis-pro/packages/Atlas_2026-07.ppkx` 是正式归档/交付的首选，不是裸 `.aprx` 的别名。创建时必须显式选择 internal/external 并查看 Analyze 消息；包只能由 ArcGIS Pro 创建和打开。[Package Project](https://pro.arcgis.com/en/pro-app/3.3/tool-reference/data-management/package-project.htm) |

### 8.3 推荐的团队资产树（约定，非产品默认目录）

下面是将可编辑源、可复用组件、敏感连接和派生交付物分开的最小结构。这里的每一项都是团队约定；QGIS 和 ArcGIS Pro 都不会自动生成这一完整树。

```text
cartographic-kit/
├─ README.md                         # 软件精确版本、打开/打包步骤、数据许可和交付清单
├─ specification/
│  └─ layout-contract.md             # 元素名称、CRS、页尺寸、字体、导出设置、验收规则
├─ shared/
│  ├─ logos/                         # PNG/SVG 的业务源资产，不等同于软件内嵌副本
│  ├─ fonts/                         # 许可允许的字体安装包及安装说明；不自动随 GIS 加载
│  └─ docs/                          # 图例规则、数据字典、品牌与版式说明
├─ qgis/
│  ├─ projects/                      # *.qgz，或成对的 *.qgs + *.qgd
│  ├─ templates/                     # *.qpt：可复用的 Print Layout
│  ├─ layers/                        # *.qlr：带数据指针的图层定义
│  ├─ styles/                        # *.qml 为权威样式；*.sld 为互操作副本
│  ├─ svg/                           # 未嵌入项目的 SVG 符号、北箭头等
│  └─ data/                          # *.gpkg、栅格或其他可分发数据
├─ arcgis-pro/
│  ├─ project/                       # *.aprx：编辑入口；默认 GDB/工具可与其配套
│  ├─ layouts/                       # *.pagx：可复用 layout，不含数据
│  ├─ maps/                          # *.mapx：地图定义，不含数据
│  ├─ layers/                        # *.lyrx：图层定义，不含数据
│  ├─ styles/                        # *.stylx：组织符号、色带、布局元素
│  ├─ tools/                         # *.atbx、*.pyt 和相邻的 Python 源码/依赖说明
│  ├─ data/                          # *.gdb/ 必须整目录；也可放 *.gpkg 等交换数据
│  └─ packages/                      # *.ppkx：可交付/可归档的封装快照
├─ connections-local/                # *.sde、*.ags、.env；默认 .gitignore，不交付
├─ scripts/                          # PyQGIS/ArcPy 参数化导出与质量检查脚本
├─ baselines/                        # 已签核 PDF/PNG 视觉回归样张和其哈希/说明
└─ deliverables/                     # 发布 PDF/SVG/PNG；仅派生结果，不回写编辑资产
```

每份 `.qpt/.pagx/.qgz/.aprx` 旁建议加一个简短 `README` 或 manifest，列出软件精确版本、所需字体、所有外部路径/URL、SVG 是否嵌入、数据许可、最后成功导出的 PDF 哈希和视觉样张。它把“模板能打开”与“能在干净机器上复现相同成图”清楚地区分开。

### 8.4 两套可落地的目录示例

**QGIS 示例：一个 A3 行政区 Atlas。**

```text
qgis-atlas/
├─ projects/
│  └─ city_atlas.qgz                # 日常编辑入口；布局/Atlas 随项目保存
├─ templates/
│  └─ A3_landscape.qpt              # 图框、图例、比例尺、标题占位符
├─ layers/
│  └─ boundaries.qlr                # 指向 ../data/city.gpkg 的带样式图层定义
├─ styles/
│  ├─ roads.qml                     # QGIS 权威样式
│  └─ roads.sld                     # 仅供外部 SLD 工作流
├─ svg/
│  └─ north-arrow.svg               # 项目以相对路径引用；企业 logo 则可选择嵌入 QPT
└─ data/
   └─ city.gpkg                     # admin、roads、atlas_coverage 及可选 layer styles
```

`city_atlas.qgz` 的**已文档化示意**只有“ZIP 内的 `.qgs` 与关联 `.qgd`”；它不是上述整个目录的压缩包。故将此树复制到另一台机器后，应先以清洁 QGIS profile 打开、确认相对路径、字体、SVG 和 Atlas 页面均正常，再交付。布局和 Atlas 由项目文件保存的事实、以及 QGZ 的 `.qgs/.qgd` 结构均由 QGIS 文档明确说明。[QGIS Project Files](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/project_files.html)

**ArcGIS Pro 示例：同一 A3 制图套件。**

```text
arcgis-pro-atlas/
├─ project/
│  ├─ Atlas.aprx                    # 项目、Map、Layout 的编辑入口
│  ├─ Atlas.gdb/                     # 默认/项目数据；多文件目录，整体管理
│  └─ Atlas.atbx                     # 默认工具箱；ExportPDF、CheckLayout 等模型
├─ layouts/
│  └─ A3_landscape.pagx             # 供其他 APRX 导入的布局模板
├─ maps/
│  └─ Reference.mapx                # 主图或插图地图定义
├─ layers/
│  └─ Roads.lyrx                    # 符号/标注/数据引用
├─ styles/
│  └─ Organization.stylx            # 标准色带、文字、指北针、比例尺资产
├─ tools/
│  └─ layout_tools.pyt              # PyToolbox；与脚本依赖说明一同版本控制
├─ data/
│  └─ Atlas.gdb/                     # 若将项目数据与默认 GDB 分开，仍整体管理
├─ connections-local/
│  ├─ production.sde                # 忽略，不提交
│  └─ printing.ags                  # 忽略，不提交
└─ packages/
   └─ Atlas_2026-07.ppkx            # 对外/归档前 Analyze 后生成的快照
```

创建新 Pro 项目时，home folder 默认会创建同名 `.gdb` 和 `.atbx`；上例刻意把它们与 `.aprx` 放在同一 `project/` 下，以保留这层关系。若把数据改放 `data/`，应通过 Pro 修改连接和用实际打包验证，不能直接在 Explorer 中移动 `.gdb` 内部文件或随意移动项目文件。[Project terminology](https://pro.arcgis.com/en/pro-app/3.6/help/projects/terminology-for-working-with-projects.htm) [File geodatabases and File Explorer](https://pro.arcgis.com/en/pro-app/3.4/help/data/geodatabases/manage-file-gdb/file-geodatabases-and-windows-explorer.htm)

### 8.5 复用、可移植性、打包、版本控制与安全检查

| 目标 | QGIS | ArcGIS Pro | 团队验收动作 |
| --- | --- | --- | --- |
| 复用“版式” | 用 `.qpt` 复用布局 items；外置数据/资源另行管理。 | 用 `.pagx` 复用 layout；空 Map Frame 模板导入后关联目标 Map。 | 不把 `.qpt/.pagx` 当数据包；逐项核查图例、比例尺、图片和动态文字绑定。 |
| 复用“地图/图层样式” | `.qlr` 是“数据引用 + 样式”，`.qml` 是样式本体，数据另交付。 | `.mapx/.lyrx` 保存属性和数据引用，`.mpkx/.lpkx` 才收集相应数据。 | 给每个定义文件写明相对根和数据许可证；在无原工作站路径的机器打开。 |
| 可移动路径 | 项目层统一存相对项目路径，外置 SVG、图片、GPKG 与项目处于同一根目录。 | `.lyrx` 导出时可显式存相对路径；项目整体移动/跨机共享优先 project package，而不是复制裸 `.aprx`。 | CI/干净 VM 从根目录外的另一路径打开，不能出现 broken layer。 |
| 包与归档 | `.qgz` 仅把 `.qgs/.qgd` 组合；它不是全量数据包。可用 GPKG 承载数据/可选样式，但仍需资源清单。 | `.ppkx` 根据 internal/external 策略决定引用还是复制、并如何处理 connections。 | 对外包应在无内部网络访问的机器验收；记录 package 选项和 Analyze 结果。 |
| Git 与合并 | `.qgs` 是 XML，可做文本审查；`.qgz` 为 ZIP。二者均应通过 QGIS 打开和导出回归验证，而不是只看 diff。 | `.atbx` 是 JSON；`.aprx/.stylx/.gdb` 及 packages 应按厂商工具打开、以二进制锁或单编辑者策略处理。 | 源模板、脚本、规范和样张进入 Git；大型数据/包用 LFS 或制品仓库；禁止多人同时手改同一二进制资产。 |
| 密钥与字体 | 外来项目的嵌入 Python 先按不受信任代码处理；不提交服务 token/密码。字体须有可部署许可且在渲染机安装。 | `.sde/.ags` 默认本机忽略；不把保存凭据的连接文件或发布权限交给版本库。字体在导入 style 前也须安装。 | `.gitignore` 覆盖 `connections-local/`、`.env`、`*.sde`、`*.ags`；用 secret manager/部署步骤创建连接。 |

上述 Git 和目录策略是工程建议，不是厂家对合并、二进制 diff 或跨主版本兼容性的承诺。尤其 QGIS 文档明确项目格式会更新，ArcGIS Pro 的 item/package 也有版本兼容边界；每次 QGIS/Pro 大版本升级都应运行代表性 Atlas/Map Series，并将导出的 PDF/PNG 与 `baselines/` 比较。[QGIS Project Files](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/project_files.html) [ArcGIS project package compatibility](https://pro.arcgis.com/en/pro-app/3.6/help/projects/terminology-for-working-with-projects.htm)

## 9. 特性矩阵（事实与工程判断分开）

说明：**原生**表示官方文档直接提供对象/工作流；**组合实现**表示可借表达式、脚本、额外 Map/模板实现，但需项目自行验证；“推荐”仅是工程建议。

| 能力 | QGIS Print Layout | ArcGIS Pro Layout | 选型含义 |
| --- | --- | --- | --- |
| 正式固定纸页与 WYSIWYG 输出 | 原生，多页 layout、打印/图片/PostScript/PDF/SVG。[QGIS Layout](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | 原生，虚拟页、打印/导出；`Layout` 在 API 中为单页。[ArcGIS Layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm) [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 两者皆适合图框型静态地图。 |
| 单 layout 内多页普通文档 | **原生**。[QGIS pages](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | **不以 Layout 对象表达**；应使用多个 Layout 或 Map Series/外部 PDF 组织。 [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) | 这是区别，不代表任何一方“不能做多页 PDF”。 |
| 一页多个独立地图范围 | 原生，多个 Map Item 各有 extent。[QGIS overview](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | 原生，多个 Map Frame、独立 extent/camera。[ArcGIS Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) | 主图+插图都可做。 |
| 同页图框不同图层/样式 | 原生，lock layers/styles 或 map theme。[QGIS Map Item](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | **组合实现**，不同 frame 指同一 Map 时共享图层状态；使用不同 Map 更稳妥。[ArcGIS Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm) | 插图专题差异大时，QGIS 模型更直接；Pro 需更明确的 Map 资产设计。 |
| 格网、经纬网、坐标标签、图廓 | 原生，多 grid、CRS、frame、tick、coordinates。[QGIS grids](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) | 原生，五类 grid；reference grid 有 neatline 和 components。[ArcGIS add a grid](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-a-grid.htm) [Reference grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm) | 都能满足一般行业制图；非标准规范需 POC。 |
| 数据驱动版式 | 原生的 expressions、variables 与 data-defined property；Atlas 可动态页面方向/元素几何。[QGIS item options](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_items_options.html) [QGIS Atlas example](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | 原生动态文本/地图集/地图整饰元素，复杂页级非标准几何通常需脚本/CIM 验证。[ArcGIS dynamic elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/use-dynamic-text-with-map-series.htm) [ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm) | 数据驱动要求越强，越要先列出每一属性的来源与最大长度。 |
| 空间索引图册 | 原生 Atlas，coverage feature + margin/predefined/fixed scale。[QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | 原生 Spatial Map Series，index layer + page constraint。[Spatial Map Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/spatial-map-series.htm) | 功能重叠很大。 |
| 书签/场景/时间范围图册 | 组合实现，需自建驱动表/表达式方案并验证。 | 原生 Bookmark Map Series，官方说明支持 scene 与 time/range。[Bookmark Map Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) | 这是 Pro 的明显优势场景。 |
| 同范围主题翻页 | 组合实现，需项目化建模。 | 原生 Thematic Map Series（radio group layer）。[Thematic Map Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/create-a-thematic-map-series.htm) | 同区域多方案、多指标的版面交付优先评估 Pro。 |
| GeoPDF 与可编辑矢量输出 | 原生 PDF/SVG/GeoPDF，可选 text/outline、map theme、vector feature info。[QGIS output](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) | 原生 PDF，支持 georef、layers/attributes、color profile、markers 选项。[ArcPy PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm) | 功能有重叠；透明度、字体、审图软件兼容性必须实测。 |
| 组织模板复用 | `.qpt` 和模板搜索路径。[QGIS templates](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) | `.pagx`、import gallery、reuse existing maps、styles。[ArcGIS import layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-a-layout-to-your-project.htm) | 两者都能沉淀模板；现有 GIS 平台、许可证和协作资产通常是最终决定因素。 |

## 10. 按场景的工作流建议与验收清单

### 10.1 推荐工作流

| 场景 | 推荐 | 原因与前置条件 |
| --- | --- | --- |
| A0/A1 行业纸图、坐标格网、图框图廓、主图+插图、PDF/SVG | QGIS 或 ArcGIS Pro 均可；先以目标 CRS、字体、透明度和印厂 PDF profile 做模板 POC。 | 两者都有原生布局、格网和整饰元素；实际风险在规范细节与输出链路，而非“能否画图框”。[QGIS Map Item](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html) [ArcGIS grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/grids-and-graticules.htm) |
| 多页报告：封面、主图、属性附表、网页/HTML 内容 | 优先 QGIS 多页 Print Layout；Pro 方案应明确哪些是 Layout、哪些是 Map Series、如何合并 PDF。 | QGIS 官方支持同一 layout 的多页和异类 item；ArcPy `Layout` 是单页对象。 [QGIS pages](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html) [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm) |
| 行政区/规则网格/管线走廊地图册 | QGIS Atlas 或 Pro Spatial Map Series；由团队生态、许可和模板资产决定。 | 两者都有按 feature 的空间分页，动态文本/表格/图例。 [QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) [Pro Spatial series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/spatial-map-series.htm) |
| 多层级书签、场景 2D/3D、时间/范围地图册 | 优先 ArcGIS Pro Bookmark Map Series。 | 官方明确 bookmark series 支持 spatial series 不支持的 scene、time、range 情境。 [Bookmark Map Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm) |
| 同空间范围下多主题/方案逐页对比 | 优先 ArcGIS Pro Thematic Map Series；若 QGIS 已是主平台，先制作 coverage/map-theme POC。 | Pro 原生以 radio group layer 逐子图层成页；QGIS Atlas 的官方核心模型是 coverage features。 [Pro Thematic Series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/create-a-thematic-map-series.htm) [QGIS Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html) |
| 夜间批量、周期制图 | 已签核 `.qpt/.qgz` + PyQGIS，或 `.pagx/.aprx` + ArcPy；脚本只填充合同参数、校验、导出和归档。 | 两者都提供 layout/atlas/map series 导出 API；无论使用哪一个，都要对渲染做视觉回归。 [PyQGIS export](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html) [arcpy.mp](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/introduction-to-arcpy-mp.htm) |

### 10.2 发布前检查清单（建议）

**数据与地图框**

- [ ] 主图、插图、overview/extent indicator 的图层、extent、scale、CRS、rotation 和关联关系已核对。
- [ ] Pro 中每个需要不同可见图层组合的 Map Frame 都有明确的 Map 资产；QGIS 中每个 Map Item 的 lock/map theme 策略明确。
- [ ] 比例尺、指北针、图例和表格绑定到正确地图容器；QGIS 的导出预检会检查比例尺与 overview 连接，但这不是完整规范审查。[QGIS output checks](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)

**格网与版式**

- [ ] 纸张尺寸、单位、边距、guides、图框/图廓定义及是否出血已签核。
- [ ] 经/纬网或坐标格网的 CRS、间隔单位、标注格式、精度、四边、tick/neatline、旋转策略符合图规。
- [ ] 最长标题、最长图例、最大表行数和动态图片缺失值已经在代表性页测试。

**图册**

- [ ] coverage/index/bookmark/radio group 的筛选、排序、页名、页码、输出文件名和单/多文件策略已固定。
- [ ] 已抽查首/末页、最小/最大范围、边界要素、空数据页、跨 CRS 页和最长动态文本页。
- [ ] Pro 对 index/bookmark 改动后必要时执行 Refresh；QGIS Atlas preview 中逐页检查动态元素。[ArcGIS refresh map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/refresh-map-series.htm) [QGIS Atlas preview](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)

**输出与归档**

- [ ] 明确 PDF/SVG/PNG/TIFF、DPI、文字 text/outline、vector/raster、透明度、压缩、地理参考和 color profile 设置。
- [ ] 在目标 PDF viewer/印刷流程中检查字体、透明度、格网、细线、栅格 seam、颜色与页边距。
- [ ] 归档源项目、模板、样式、SVG/logo、脚本、数据版本、导出设置、最终 PDF 和视觉基准；不要只归档导出图。

## 11. 仍需项目确认的开放问题

以下不是软件事实，必须由具体项目的图规、样张与运行环境决策：

1. **规范匹配：**图框/图廓的线型、坐标标注、保密标识、审图号、出血、CMYK/profile、字体授权和 PDF/A/无障碍要求分别是什么？两套软件虽可放置/配置元素，但不自动证明符合某一国家、行业或印厂规范。
2. **动态复杂度：**是否允许每页横竖向切换、图例列数切换、图片路径切换、表格溢出或完全不同的版式？QGIS 对 data-defined 几何更直接；Pro 的非标准页级布局应先证明 Map Series/dynamic element/API 能稳定覆盖。
3. **Pro 主题系列自动化：**本调研确认 Thematic Map Series 的 GUI 能力与 spatial/bookmark 的 `arcpy.mp` 对象文档，但未找到与前两者同等的 3.6 高层 ThematicMapSeries API 文档。若要无人值守生产，应以真实 `.aprx` 和期望格式跑通创建、逐页取名、导出、升级回归后再承诺 SLA。
4. **协作和可复现：**`.qpt`、`.qgz`、`.pagx`、`.aprx` 的版本管理、二进制合并、相对路径、外部样式/字体和授权如何落地？应在选型前让两名制图员完成一次“从模板到 CI/批量导出”的演练。

## 官方资料索引

### QGIS 4.2

- [QGIS 4.2 发布说明](https://qgis.org/project/visual-changelogs/visualchangelog42/)、[Print Layout 总览](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/overview_layout.html)、[Map Item](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_map.html)、[通用 Item 属性](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_items_options.html)
- [Legend](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_legend.html)、[Scale bar](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_scale_bar.html)、[Picture/North arrow](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_image.html)、[Table items](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/layout_items/layout_tables.html)
- [导出与 Atlas](https://docs.qgis.org/4.2/en/docs/user_manual/print_layout/create_output.html)、[PyQGIS Layout/输出](https://docs.qgis.org/4.2/en/docs/pyqgis_developer_cookbook/composer.html)
- **资产目录补充（QGIS 3.44/4.2）：**[Project Files（QGS/QGZ/QGD、项目内容与 GeoPackage）](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/project_files.html)、[Auxiliary storage](https://docs.qgis.org/3.44/en/docs/user_manual/working_with_vector/vector_properties.html)、[QLR/QML/SLD 与嵌入文件](https://docs.qgis.org/3.44/en/docs/user_manual/introduction/general_tools.html)、[GeoPackage](https://docs.qgis.org/3.44/en/docs/user_manual/managing_data_source/supported_data.html)、[Project Trust 与相对路径](https://docs.qgis.org/4.2/en/docs/user_manual/introduction/qgis_configuration.html)

### ArcGIS Pro 3.6

- [Layouts in ArcGIS Pro](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/layouts-in-arcgis-pro.htm)、[Set up a layout](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/page-setup.htm)、[Work with layout elements](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-layout-elements.htm)、[Map frame constraints](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/customizing-your-map-extent.htm)
- [Grids and graticules](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/grids-and-graticules.htm)、[Add a grid](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/add-a-grid.htm)、[Reference grids](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/reference-grids.htm)、[Legend](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/work-with-a-legend.htm)
- [Spatial](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/spatial-map-series.htm)、[Bookmark](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/bookmark-map-series.htm)、[Thematic map series](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/introduction-to-a-thematic-map-series.htm)、[Map Series 导出](https://pro.arcgis.com/en/pro-app/3.6/help/layouts/export-options.htm)
- [ArcPy Layout](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/layout-class.htm)、[ArcPy MapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/mapseries-class.htm)、[BookmarkMapSeries](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/bookmarkmapseries-class.htm)、[PDFFormat](https://pro.arcgis.com/en/pro-app/3.6/arcpy/mapping/pdfformat-class.htm)
- **资产目录补充：**[Projects and item files](https://pro.arcgis.com/en/pro-app/3.6/help/projects/what-is-a-project.htm)、[Layout files](https://pro.arcgis.com/en/pro-app/3.3/help/layouts/layout-files.htm)、[Styles](https://pro.arcgis.com/en/pro-app/3.6/help/projects/styles.htm)、[Toolboxes](https://pro.arcgis.com/en/pro-app/3.6/help/projects/connect-to-a-toolbox.htm)、[File geodatabases](https://pro.arcgis.com/en/pro-app/3.4/help/data/geodatabases/manage-file-gdb/file-geodatabases-and-windows-explorer.htm)、[Project packages](https://pro.arcgis.com/en/pro-app/3.3/help/sharing/overview/project-package.htm)
