# 采用 Tauri 2 与 Python sidecar 作为桌面运行时

> 状态：数据与 DuckDB 职责已由 [ADR-0003](0003-go-duckdb-geodata-serve.md) 取代；本文剩余的桌面宿主与 Agent 运行时选择尚未在本轮数据模块设计中重新决定。

本应用的主要体验是 MapLibre GL JS 数据工作台，而 Agent、DuckDB Spatial 与数据分析优先使用 Python。决定使用 Tauri 2 承载 TypeScript 前端，并将 Python 服务作为随 Windows x64 安装包分发的本机 sidecar：前者负责最小化的原生权限与进程生命周期，后者负责数据获取、分析与模型编排。

选择保留 Web 地图前端和 Python 分析栈，避免为桌面壳重写任一方；Electron 是在系统 WebView 无法满足真实地图负载时的备选，PySide6/Qt 是 Python 单进程成为强约束时的备选。代价是必须构建、验证和更新 Python sidecar 与 DuckDB 空间依赖，并维持窄的本机 IPC 接口。
