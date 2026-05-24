# Conan 未来 Roadmap

日期：2026-05-23

## 定位

Conan 应该演进为一个面向多集群、多节点运维的本地 AI 控制面：它不是普通聊天 CLI，也不是单纯的远程命令执行器，而是把节点工具、风险审查、上下文记忆、文件传输、节点接入和多代理调查组织在一个可审计的运维工作台里。

未来路线的核心判断是：Conan 的差异化不在“能调用更多 shell 命令”，而在“让 AI 以安全、可证据化、可复用的方式完成运维工作”。这与 SRE 降低 toil、平台工程将能力产品化、OpenTelemetry 统一观测信号、MCP 标准化工具/资源/提示暴露的趋势一致。

## 当前基线

根据 `CLAUDE.md` 和现有 `docs/superpowers/specs/`、`docs/superpowers/plans/`，Conan 已具备以下基础：

- 双二进制架构：本地 `conan` CLI/TUI 和远端 `conan-agent`。
- 多节点 MCP 通信：HTTP/SSE JSON-RPC、工具发现、工具调用、健康检查和版本检查。
- TUI 运维会话：流式 LLM、slash commands、节点选择、工具调用、确认交互、会话恢复。
- 工具体系：shell、fs、sys、svc、log、net、k8s、pkg、cron、docker、web、文件传输、图片分析。
- 安全层：命令白名单、模型风险评估、确认流程、审计日志、本地文件写入防护。
- 记忆层：Markdown 行为规则、SQLite 记忆、隐式记忆设计和自动保存能力。
- 运营能力：模型管理、节点添加和 agent 部署、tool_search BM25、subagent 调查模式。

这个基线说明 Conan 已经越过“原型 CLI”阶段。下一阶段重点应从继续堆功能转向产品闭环、可靠性、治理和可扩展性。

## 产品原则

1. **证据优先**
   - 每个运维结论都应能追溯到工具输出、节点、时间、模型和风险决策。
   - AI 最终回答要表达判断，但底层系统要保留证据链。

2. **默认安全**
   - 读操作尽可能低摩擦。
   - 写操作、部署、重启、删除和权限变更必须经过可理解的确认和审计。
   - 子代理不应直接执行破坏性动作。

3. **工具优先于 shell**
   - shell 是兜底，不是主路径。
   - 专用工具应逐步覆盖高频运维动作，并给出结构化结果。

4. **记忆服务于复用**
   - 记忆不是聊天装饰，而是把拓扑、规则、runbook、incident 经验转化为下次可用的上下文。

5. **本地优先，云端可选**
   - 默认保留本地配置、凭据、审计和会话。
   - 团队协作、集中策略、远程同步作为后续增强，而不是第一阶段前提。

## Roadmap 概览

| 阶段 | 主题 | 目标 |
| --- | --- | --- |
| 0：稳定化 | 可依赖的单人运维工作台 | 把已有能力打磨到日常可用，减少误调用、误确认和上下文噪声 |
| 1：运维闭环 | Incident/Runbook 工作流 | 从“回答问题”升级为“调查、建议、执行、验证、沉淀” |
| 2：平台接入 | 观测、资产和工具生态 | 接入标准观测数据、外部 MCP 资源和组织级工具目录 |
| 3：团队化 | 治理、协作和发布 | 支持团队策略、共享记忆、审计检索和可发布部署 |

## 阶段 0：稳定化与产品闭环

时间建议：0-1 个月

目标：让 Conan 成为可以每天打开使用的单人运维工作台。当前已有很多能力，但 TUI、工具路由、配置、审计和文档还需要收敛成更稳定的体验。

重点工作：

- **TUI 工作流打磨**
  - 统一 `/help`、`/nodes`、`/memory`、`/subagents`、`/node` 的信息架构。
  - 给高风险工具提供更清晰的确认摘要，包括将要操作的节点、路径、服务名和影响面。
  - 让工具执行结果的成功、失败、部分成功在 UI 中更容易扫描。

- **工具路由质量**
  - 基于真实运维请求补充 tool_search 同义词、排名测试和失败样例。
  - 统计 exec fallback 使用率，把高频 shell 命令沉淀为专用工具。
  - 对文件传输、节点添加、web/fetch、图片分析等一等 meta tools 增加端到端场景测试。

- **配置和安装体验**
  - 提供 `conan init` 或等价初始化路径，生成最小可运行配置。
  - 明确 agent 二进制安装、升级、版本兼容和回滚建议。
  - 把 `configs/example/` 扩展为单机、本地网络、多集群三种样例。

- **质量门禁**
  - 固定 `go test ./...`、`go vet ./...`、race 测试和关键 TUI snapshot/行为测试。
  - 增加一份“发布前检查清单”，覆盖构建、配置迁移、agent 版本和安全默认值。

验收信号：

- 新用户可以在 15 分钟内完成本地配置、接入一个测试节点并执行一次只读诊断。
- 常见诊断请求优先走专用工具，而不是直接 shell。
- 一次 mutating 操作的确认摘要和审计记录足以让用户复盘发生了什么。

## 阶段 1：Incident 与 Runbook 闭环

时间建议：1-3 个月

目标：让 Conan 从“会调用工具的聊天界面”升级为“能组织一次运维调查”的工作台。

重点工作：

- **Incident 模式**
  - 增加 `/incident start|status|close` 或轻量等价流程。
  - 将用户请求、节点范围、工具输出、子代理结论、确认动作和最终验证整理成 incident timeline。
  - 支持导出 Markdown 报告，包含症状、影响面、证据、根因假设、执行动作、验证结果和后续项。

- **Runbook 生成与执行**
  - 从成功 incident 中抽取可复用 runbook 草案。
  - 提供 runbook preview：只展示计划、读取项和风险点，不执行。
  - 执行时逐步确认 mutating steps，保留每步证据。

- **子代理产品化**
  - 将 subagent 用在多节点并行调查、审查结论、摘要压缩这三类稳定场景。
  - 限制子代理为只读工具面，主代理负责最终动作。
  - UI 显示子代理状态和结果摘要，详细 transcript 进入 debug/incident 证据。

- **记忆闭环**
  - incident close 后生成候选记忆：拓扑、runbook、已知问题、修复经验。
  - 把高价值记忆写入 `clusters/`、`runbooks/`、`incidents/` Markdown 层。
  - `/memory` 从原始列表升级为“最近沉淀、可复用知识、待确认候选”的管理视图。

验收信号：

- Conan 能对一次多节点故障排查产生结构化 incident 报告。
- 至少 3 类常见运维流程可以从 incident 沉淀为 runbook。
- 子代理输出被主代理正确整合，且不会绕过确认执行写操作。

## 阶段 2：观测与平台生态接入

时间建议：3-6 个月

目标：把 Conan 接入更真实的生产运维上下文，不只依赖远端节点命令。

重点工作：

- **Observability 接入**
  - 引入 OpenTelemetry 方向的抽象：metrics、logs、traces、events 作为 Conan 可检索上下文。
  - 先支持常见后端的只读查询适配器，例如 Prometheus/Loki/Tempo 或 OpenTelemetry Collector 前置接口。
  - 将观测查询结果和节点工具输出放进同一证据模型。

- **MCP 兼容层增强**
  - 现有实现以 tools 为主，后续应逐步支持 MCP resources 和 prompts。
  - resources 可用于暴露集群拓扑、runbook、配置片段、观测查询模板。
  - prompts 可用于用户显式选择的诊断模板或发布检查模板。

- **资产与拓扑模型**
  - 从静态 `nodes.yaml` 扩展到可查询资产模型：节点、服务、端口、Kubernetes namespace、关键依赖。
  - 增加拓扑摘要和变更检测，让模型知道“这次操作影响哪些服务”。

- **策略化工具目录**
  - 为工具增加标签：read-only、mutating、destructive、requires-root、k8s、network、file-write。
  - 工具搜索和风险评估使用这些标签，而不是只依赖名称和 prompt 约束。
  - 支持按 cluster 或 node profile 禁用/启用工具。

验收信号：

- Conan 可以把“指标异常 -> 相关日志 -> 节点状态 -> 建议动作”串成一次可审计调查。
- 外部 MCP resources 能作为上下文被发现和读取。
- 工具风险级别更多来自结构化元数据，而不是纯模型判断。

## 阶段 3：团队化与治理

时间建议：6-12 个月

目标：从个人本地工具扩展到小团队可共享、可治理、可发布的运维平台组件。

重点工作：

- **共享配置与策略**
  - 支持团队级安全策略、工具策略、模型策略和默认确认规则。
  - 本地配置继续可用，但可以从 Git 或内部配置源同步。
  - 对不同环境设置不同风险阈值：dev、staging、prod。

- **审计检索与合规**
  - 把审计日志从 append-only 文件升级为可查询索引。
  - 支持按时间、节点、用户、工具、风险级别和 incident 检索。
  - 形成“谁让模型做了什么、谁确认了什么、结果是什么”的闭环记录。

- **发布与升级**
  - 提供正式 release 流程：版本号、变更日志、二进制构建、校验和、agent 兼容矩阵。
  - 增加 agent rolling upgrade 或批量更新命令。
  - 支持版本不匹配时的降级提示和自动修复建议。

- **多人协作**
  - 共享 incident 报告和 runbook。
  - 支持导出给 issue/PR/工单系统。
  - 远期可考虑轻量服务端，但不应过早牺牲本地优先模型。

验收信号：

- 团队可以统一配置生产环境的风险策略。
- 审计记录足以支撑一次操作复盘或合规检查。
- agent 升级不再依赖手工复制二进制和逐节点处理。

## 横向技术主题

### 1. Evidence Model

建议引入统一证据模型，贯穿工具输出、观测查询、子代理结果、确认动作和最终报告。

候选字段：

- source：tool、observability、subagent、memory、user
- node / cluster / service
- timestamp
- command/tool name
- arguments redacted
- output summary
- raw output reference
- risk decision
- success/failure

这会支撑 incident timeline、审计检索、报告导出和模型上下文压缩。

### 2. Tool Metadata

当前工具主要靠名称、描述和 schema 被搜索和评估。未来应为每个工具增加结构化元数据：

- capability：logs、service、filesystem、network、kubernetes、package、deploy
- safety：read-only、mutating、destructive
- scope：local、node、cluster
- required privileges：user、root、kubeconfig
- output shape：text、json、table、stream

这能减少 prompt 约束的脆弱性，让安全评估和工具选择更可测试。

### 3. Memory Promotion

记忆应分层处理：

- 短期会话摘要：帮助继续对话。
- SQLite 事实索引：帮助检索历史经验。
- Markdown runbook/incident/topology：帮助复用和人工审查。

Roadmap 的关键不是“保存更多”，而是“把值得复用的事实提升到正确层级”。

### 4. Provider Abstraction

模型管理已经支持多 provider。后续需要继续加强：

- 按任务选择模型：主会话、风险评估、记忆提取、子代理、图片分析。
- 记录模型选择和成本估算。
- 支持 provider 能力检测，例如 tool calling、vision、reasoning、streaming。

### 5. Test Harness

TUI 和 agentic flows 需要更高层的测试夹具：

- fake LLM script：按步骤返回 tool calls、文本、错误。
- fake node agent：模拟工具列表、延迟、失败和部分成功。
- golden incident report：验证证据链和最终输出。
- security regression：验证密码、token、私密路径不会进入日志、审计或会话。

## 明确不做或暂缓

- 不把远端 `conan-agent` 变成 LLM 进程；远端保持工具执行面。
- 不默认启用破坏性自治；主代理始终负责高风险动作确认。
- 不在早期引入重型服务端控制平面；先把本地优先体验做扎实。
- 不追求支持所有云厂商 API；先通过 MCP、OpenTelemetry 和专用适配器形成扩展边界。
- 不把 shell 能力无限放大；高频路径应沉淀为专用工具和 runbook。

## 优先级建议

最高优先级：

1. 阶段 0 的 TUI 工作流、确认摘要、工具路由质量。
2. Evidence Model 的最小版本。
3. Incident report 导出。

第二优先级：

1. Runbook 草案和执行预览。
2. 子代理调查模式产品化。
3. Tool Metadata 和风险策略结构化。

第三优先级：

1. OpenTelemetry/Prometheus/Loki 等观测接入。
2. MCP resources/prompts 支持。
3. 团队策略和审计检索。

## 参考信号

- CNCF Platform Engineering maturity model：平台应作为产品提供能力，覆盖 people、process、policy、technology，并驱动业务结果。https://www.cncf.io/blog/2023/11/20/announcing-the-platform-engineering-maturity-model/
- CNCF 2026 Technology Radar：平台工程工具正在成熟，组织开始为 AI-driven infrastructure 调整平台能力。https://www.cncf.io/announcements/2026/03/24/cncf-and-slashdata-report-finds-platform-engineering-tools-maturing-as-organizations-prepare-for-ai-driven-infrastructure/
- Google SRE Eliminating Toil：自动化是降低重复、可预测运维工作的核心路径，但需要度量和渐进推进。https://sre.google/workbook/eliminating-toil/
- OpenTelemetry：将 traces、metrics、logs 等信号作为统一观测框架，适合作为 Conan 未来证据层的外部标准。https://opentelemetry.io/docs/what-is-opentelemetry/
- MCP Resources/Prompts：MCP 不只包含 tools，也提供 resources 和 prompts，适合 Conan 后续扩展上下文与诊断模板。https://modelcontextprotocol.io/docs/concepts/resources 和 https://modelcontextprotocol.io/docs/concepts/prompts

## 下一步建议

下一步不建议直接开做大阶段。更稳妥的路径是先写三份可执行 spec：

1. `Evidence Model + Incident Report`：定义证据结构、timeline、Markdown 导出和测试夹具。
2. `Tool Metadata + Risk Policy`：定义工具元数据、风险标签、搜索与审查如何消费这些字段。
3. `Runbook Draft + Preview`：定义 runbook 生成、预览、逐步确认和记忆沉淀。

这三项完成后，Conan 的未来功能会有统一骨架，而不是继续在 TUI model 中追加零散流程。
