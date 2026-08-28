# 主流 LLM Agent 框架调研与选型建议（2026）

> **调研日期：**2026-07-29（指标为该日抓取的快照）  
> **范围：**通用、代码优先的 LLM Agent 框架；不把模型 API、纯工作流自动化产品或单一托管平台单列为“框架”。  
> **一手来源：**项目官方文档、官方 GitHub 仓库、GitHub 官方 REST API / Dependency graph。外部宣传口径和第三方排行榜未作为证据。

## 结论先行

1. **需要一个默认的、跨模型且面向生产的通用底座：选 `LangGraph`，并按需配合 `LangChain`。**
   LangGraph 负责显式状态、循环、人工介入和可恢复的 agent runtime；LangChain 提供模型、工具、检索器等最广的集成面。它们可独立使用，但同属一个生态，不应把二者误当成两个互斥的选项。[官方生态说明](https://github.com/langchain-ai/langgraph#langgraph-ecosystem)
2. **知识库、文档解析、RAG 是成败关键：优先评估 `LlamaIndex`，必要时与 LangGraph 组合。**
   它的强项是数据连接器、索引、检索与 query engine，而不是替代通用工作流 runtime。[官方定位](https://github.com/run-llama/llama_index#overview)
3. **角色协作、较快搭建多 Agent 流程：选 `CrewAI`。**
   `Crews` 与确定性 `Flows` 的概念较直接，近期维护和发布很活跃；但“角色扮演”不应替代明确的状态机、评测和人工审批。[官方文档](https://docs.crewai.com/)
4. **已有厂商栈时，优先采用对应原生 SDK：**OpenAI 重度场景选 **OpenAI Agents SDK**；Gemini / Vertex AI 选 **Google ADK**；.NET / Azure 选 **Semantic Kernel**（并跟踪 Microsoft Agent Framework）。这些选择牺牲了一部分跨厂商中立性，换取原生工具、身份、部署和可观测性的集成。
5. **新项目不建议以 `AutoGen` 为首选。**它仍有很高历史热度，但官方 README 已标为 maintenance mode，并建议新项目考虑 Microsoft Agent Framework；除非是在延续既有代码。[官方说明](https://github.com/microsoft/autogen#why-autogen)

## 1. 口径与限制

这里把用户关心的三层拆开衡量：

| 层级 | 本文采用的证据 | 不代表什么 |
| --- | --- | --- |
| 生态 | 官方支持的模型/工具/检索/协议/部署/观测集成及语言覆盖 | 不能证明每个集成都稳定、适合生产 |
| 使用该框架项目的热门程度 | GitHub Dependency graph 的**公开仓库 dependents**（优先）及官方披露的公开采用者 | 不是活跃用户、生产部署或商业收入；私有仓库不在内，且 GitHub 明示计数为近似值 |
| 活跃度 | `pushed_at`、近期 commit、最新正式 release；必要时看维护状态 | `updated_at` 会被 Star、Issue 等事件改变，故不用作代码活跃度 |

框架自身的 Star/Fork 只衡量开发者关注度，**不替代**“下游项目热门程度”。不同包名、历史包名、monorepo、私有依赖会影响 GitHub 的 dependents 数，所以只适合分层而不适合精确市场份额比较。

## 2. 三层推荐

### 2.1 生态：LangChain/LangGraph 为通用首选，按约束选垂直生态

| 推荐序 | 框架 / 组合 | 生态判断 | 最合适的边界 |
| ---: | --- | --- | --- |
| 1 | **LangGraph + LangChain** | LangChain 官方列出第三方模型、embedding、vector store、tool、retriever 集成，并连接 LangGraph、LangSmith 与部署产品；LangGraph 可独立使用也可接入该生态。[官方 README](https://github.com/langchain-ai/langchain#langchain-ecosystem) | 跨模型、多工具、长流程、需 checkpoint / HITL / 观测的生产 agent |
| 2 | **LlamaIndex** | 核心 + 300 余个 integration packages，数据源、索引、检索和查询链能力很完整。[官方 README](https://github.com/run-llama/llama_index#llamaindex-) | 文档/知识库 agent；可把它当“数据层”接到 LangGraph 或其他 runtime |
| 3 | **CrewAI** | 官方支持多家 LLM、MCP 的三种 transport，并列出多种 observability 集成。[LLM](https://docs.crewai.com/en/concepts/llms) / [MCP](https://docs.crewai.com/en/mcp/overview) | 角色分工清晰、开发团队希望快速实现 Crews/Flows 的业务自动化 |
| 条件首选 | **OpenAI Agents SDK** | 官方原生覆盖 handoff、guardrail、HITL、session、tracing、realtime 与 sandbox；同时声明兼容 OpenAI API 及 100+ LLM。[官方 README](https://github.com/openai/openai-agents-python#openai-agents-sdk) | OpenAI 工具、语音/实时、Responses API 是主路径 |
| 条件首选 | **Google ADK** | Python/Java/Go 生态，官方提供 Agent、Workflow、A2A、MCP、Cloud Run/Vertex AI Agent Engine 等路径；虽为 Gemini 优化，官方称模型和部署可替换。[官方 README](https://github.com/google/adk-python#agent-development-kit-adk) | Gemini / Google Cloud 已是组织标准 |
| 条件首选 | **Semantic Kernel** | Microsoft 维护，适合将 agent 能力嵌入既有 C#/.NET/Azure 应用；不是 Python-first 团队的默认首选。[官方仓库](https://github.com/microsoft/semantic-kernel) | .NET、Azure 身份与企业治理优先 |

### 2.2 下游项目热门程度：LangChain 明显领先，LlamaIndex 与 CrewAI 是第二梯队

下表是 GitHub Dependency graph 中指定包的公开仓库近似数，直接衡量“采用该依赖的公开项目”，比框架 Star 更接近题目要求。只纳入能从 GitHub 页面复核的主包，不跨包名加总。

| 排名 | 主包 | 公开仓库 dependents | 解释与建议 |
| ---: | --- | ---: | --- |
| 1 | `langchain` | **284,081** repositories、5,536 packages | 远高于其他项目，意味着可复用代码、问答和招聘信号最强；同时也意味着历史 API 版本更多，选型时要锁定版本并看迁移指南。[GitHub Dependency graph](https://github.com/langchain-ai/langchain/network/dependents?dependent_type=REPOSITORY) |
| 2 | `llama-index` | **24,557** repositories、695 packages | 在数据/RAG 方向有很强的公开采用度，适合在该方向优先做 PoC。[GitHub Dependency graph](https://github.com/run-llama/llama_index/network/dependents?dependent_type=REPOSITORY) |
| 3 | `crewai` | **18,360** repositories、895 packages | 多 Agent 场景的公开采用度已形成规模；其官方 examples 仓库也有较高热度，但已归档，因此不能以 examples 的维护状态推断核心库状态。[GitHub Dependency graph](https://github.com/crewAIInc/crewAI/network/dependents?dependent_type=REPOSITORY) / [examples 归档信息](https://github.com/crewAIInc/crewAI-examples) |
| 不作横向名次 | LangGraph、OpenAI Agents SDK、Google ADK、Semantic Kernel | 包名、语言和生态边界不同，不能用一条未统一抓取的 dependents 数与前三者精确排序 | 看它们是否匹配既有云/模型/语言栈；不要因较新的项目公开依赖数较少就直接否决 |

作为补充的框架关注度快照：LangChain **142,852** stars / **23,779** forks，CrewAI **56,308** / **8,002**，LlamaIndex **51,188** / **7,826**。这些数支持其“主流”判断，但不替代上表的 adoption 指标。[LangChain API](https://api.github.com/repos/langchain-ai/langchain) / [CrewAI API](https://api.github.com/repos/crewAIInc/crewAI) / [LlamaIndex API](https://api.github.com/repos/run-llama/llama_index)

### 2.3 活跃度：近期代码与发布节奏优先于历史热度

| 组别 | 证据（2026-07-29 快照） | 结论 |
| --- | --- | --- |
| **高：LangChain / LangGraph / CrewAI / LlamaIndex** | LangChain pushed 7/29 且正式 release 7/28；LangGraph pushed 7/28；CrewAI pushed 7/29、最新正式 v1.15.8 于 7/28；LlamaIndex pushed 7/28、正式 v0.14.23 于 6/24。[LangChain](https://api.github.com/repos/langchain-ai/langchain) / [LangGraph](https://api.github.com/repos/langchain-ai/langgraph) / [CrewAI](https://api.github.com/repos/crewAIInc/crewAI) / [LlamaIndex](https://api.github.com/repos/run-llama/llama_index) | 四者都可列为持续演进的主流候选；活跃同时意味着 API 变化风险，生产环境应锁版本并保留回归评测。 |
| **高：OpenAI Agents SDK / Google ADK / Semantic Kernel** | 代码推送分别为 7/29、7/28、7/29；ADK README 说明稳定版约双周发布并已有 2.0 breaking changes。[OpenAI API](https://api.github.com/repos/openai/openai-agents-python) / [ADK API](https://api.github.com/repos/google/adk-python) / [Semantic Kernel API](https://api.github.com/repos/microsoft/semantic-kernel) / [ADK release cadence](https://github.com/google/adk-python#installation) | 有厂商长期投入，但升级节奏快；把模型和工具调用封装在自有适配层，避免业务代码紧贴 SDK。 |
| **不宜按活跃度推荐：AutoGen** | 仓库虽有 **60,071** stars / **9,047** forks，但 `pushed_at` 为 4/15，且官方写明 maintenance mode，仅接受 bug/security/docs 类型变更。[官方 API](https://api.github.com/repos/microsoft/autogen) / [维护声明](https://github.com/microsoft/autogen#why-autogen) | 既有项目继续维护即可；新项目应迁移评估 Microsoft Agent Framework 或上表的其他框架。 |

## 3. 候选框架的量化快照

| 框架主仓库 | Stars | Forks | 最近代码推送（UTC） | 定位 |
| --- | ---: | ---: | --- | --- |
| [LangChain](https://api.github.com/repos/langchain-ai/langchain) | 142,852 | 23,779 | 2026-07-29 | 通用组件与集成生态 |
| [LangGraph](https://api.github.com/repos/langchain-ai/langgraph) | 38,395 | 6,466 | 2026-07-28 | 有状态 agent / 工作流 runtime |
| [AutoGen](https://api.github.com/repos/microsoft/autogen) | 60,071 | 9,047 | 2026-04-15 | 历史主流；已进入维护模式 |
| [CrewAI](https://api.github.com/repos/crewAIInc/crewAI) | 56,308 | 8,002 | 2026-07-29 | Crews / Flows 多 agent 编排 |
| [LlamaIndex](https://api.github.com/repos/run-llama/llama_index) | 51,188 | 7,826 | 2026-07-28 | 数据、RAG 与文档 agent |
| [Semantic Kernel](https://api.github.com/repos/microsoft/semantic-kernel) | 28,386 | 4,698 | 2026-07-29 | .NET / Azure 企业集成 |
| [OpenAI Agents SDK](https://api.github.com/repos/openai/openai-agents-python) | 28,262 | 4,394 | 2026-07-29 | OpenAI 原生 agent 能力，仍可接多模型 |
| [Google ADK](https://api.github.com/repos/google/adk-python) | 20,930 | 3,770 | 2026-07-28 | Gemini / Google Cloud 原生路径 |

数值来自仓库 REST API 的 `stargazers_count`、`forks_count` 和 `pushed_at`。它们是时点快照，后续会变化；不得以它们衡量框架可靠性或安全性。

## 4. 面向项目的最终选择

| 项目约束 | 推荐 | 先验证什么 |
| --- | --- | --- |
| 不确定模型供应商；有长任务、分支、重试、人工审批、持久状态 | **LangGraph + 少量 LangChain 适配器** | 一条真实任务从失败恢复、HITL、审计 trace 和评测集回归 |
| 主要目标是企业知识库、PDF/网页/SQL 接入与检索质量 | **LlamaIndex**（runtime 需要时接 LangGraph） | 数据接入成功率、检索召回、引用可追溯性、成本与延迟 |
| 业务天然是“研究员—审核员—执行者”这类明确角色协作，且先追求实现速度 | **CrewAI** | 每个 Flow 的确定性、循环上限、工具权限、任务失败后的补偿逻辑 |
| 已深度采用 OpenAI Responses/Realtime/托管工具 | **OpenAI Agents SDK** | 供应商切换成本、trace 数据归属、工具/会话权限边界 |
| Gemini、Vertex AI、Cloud Run 是既定平台 | **Google ADK** | A2A/MCP 互操作、部署/身份、2.x 升级迁移与模型可替换性 |
| C#、Azure、Microsoft 身份和治理不可更改 | **Semantic Kernel**；同时做 **Microsoft Agent Framework** PoC | .NET 版本兼容、Azure 策略、官方迁移路径 |

无论选哪个框架，建议先做一个 1--2 周的垂直 PoC，而不是横向 demo：用同一批真实工具、任务、失败用例和预算，验收 **成功率、人工介入率、可恢复性、单任务成本、P95 延迟、可观测性与权限隔离**。框架热度只能缩小候选集，不能替代这一步。

## 官方来源索引

- [LangChain ecosystem](https://github.com/langchain-ai/langchain#langchain-ecosystem)；[LangGraph ecosystem](https://github.com/langchain-ai/langgraph#langgraph-ecosystem)
- [LlamaIndex 官方 README](https://github.com/run-llama/llama_index)；[CrewAI 官方文档](https://docs.crewai.com/)
- [OpenAI Agents SDK 官方 README](https://github.com/openai/openai-agents-python)；[Google ADK 官方 README](https://github.com/google/adk-python)；[Semantic Kernel 官方仓库](https://github.com/microsoft/semantic-kernel)
- [GitHub Dependency graph：LangChain](https://github.com/langchain-ai/langchain/network/dependents?dependent_type=REPOSITORY)、[LlamaIndex](https://github.com/run-llama/llama_index/network/dependents?dependent_type=REPOSITORY)、[CrewAI](https://github.com/crewAIInc/crewAI/network/dependents?dependent_type=REPOSITORY)
- [Microsoft AutoGen 官方维护状态](https://github.com/microsoft/autogen#why-autogen)
