---
status: accepted
---

# 采用 Go + DuckDB 的本地 geodata-serve

项目的数据获取、保存和空间分析由 `services/geodata-serve/` 中的 Go 常驻进程统一执行：每个地理项目对应一个进程和一个持久化 DuckDB 数据库，Agent 通过仅监听 loopback 的 HTTP interface 提交原始 SQL。选择 Go 是为了让多个 Agent 与子 Agent 共用同一 DuckDB 写进程，并集中处理并发、取消、写前备份和流式结果；代价是需要维护 CGO/Windows GCC 工具链和本地进程生命周期。本决定取代 ADR-0001 中“Python sidecar 直接拥有 DuckDB 与数据分析”的部分，不决定桌面宿主、前端、制图或 Agent skill 的最终技术栈。
