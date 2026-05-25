# Evidence Model 与 Incident Report 设计

## 目标

建立 Conan 的最小证据模型，并在 TUI 中支持 incident 生命周期与 Markdown 报告导出。这个设计对应 roadmap 的最高优先级：让一次运维调查从“聊天 + 零散工具输出”变成可追溯、可导出、可复盘的工作流。

## 非目标

- 不引入服务端控制平面。
- 不做多人协作、审计检索索引或合规查询 UI。
- 不改变远端 `conan-agent` 的职责，远端仍然只执行工具。
- 不让子代理执行 mutating 操作。

## 用户体验

TUI 增加 incident 命令：

```text
/incident start <title>       开始一次事件调查
/incident status              查看当前 incident 摘要
/incident note <content>      追加人工备注
/incident export              导出 Markdown 报告
/incident close               关闭 incident 并导出报告
```

当 incident 开启后，Conan 自动记录：

- 用户请求
- 目标 cluster 和节点范围
- 工具调用与工具结果摘要
- 风险评估、确认、取消、拒绝和执行结果
- 子代理任务、结论和工具证据摘要
- 最终助手回复
- 人工 note

导出的报告默认写入：

```text
~/.conan/memory/memory/incidents/YYYY-MM-DD-<slug>.md
```

关闭 incident 后，TUI 显示报告路径，并把报告作为候选记忆的来源之一。

## Evidence Model

新增 `internal/evidence` 包，定义统一证据结构。证据只保存可审计摘要和原始输出引用，不把大段原始工具输出重复写入每个事件。

```go
type Source string

const (
	SourceUser          Source = "user"
	SourceAssistant     Source = "assistant"
	SourceTool          Source = "tool"
	SourceObservability Source = "observability"
	SourceSubagent      Source = "subagent"
	SourceMemory        Source = "memory"
	SourceRisk          Source = "risk"
)

type Event struct {
	ID          string            `json:"id"`
	IncidentID  string            `json:"incident_id"`
	Source      Source            `json:"source"`
	Cluster     string            `json:"cluster,omitempty"`
	Nodes       []string          `json:"nodes,omitempty"`
	Service     string            `json:"service,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	ToolName    string            `json:"tool_name,omitempty"`
	Arguments   json.RawMessage   `json:"arguments,omitempty"`
	Summary     string            `json:"summary"`
	RawRef      string            `json:"raw_ref,omitempty"`
	RiskLevel   string            `json:"risk_level,omitempty"`
	RiskOutcome string            `json:"risk_outcome,omitempty"`
	Success     *bool             `json:"success,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
```

最小实现中，`RawRef` 使用会话消息 ID 或本地 raw 文件路径；后续审计检索可以把它升级为索引键。

## Incident Model

```go
type Incident struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Cluster   string    `json:"cluster,omitempty"`
	Nodes     []string  `json:"nodes,omitempty"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	ClosedAt  time.Time `json:"closed_at,omitempty"`
	Report    string    `json:"report,omitempty"`
}
```

状态只支持：

- `open`
- `closed`

同一 TUI 会话同一时间只允许一个 open incident。再次执行 `/incident start` 时，如果已有 open incident，TUI 返回当前 incident 信息而不是创建新事件。

## 报告格式

Markdown 报告包含固定章节：

```markdown
# <Incident Title>

## 摘要

## 影响范围

## 时间线

## 证据

## 根因假设

## 执行动作

## 验证结果

## 后续项
```

`时间线` 按事件时间排序，`证据` 按 source 与工具分组。报告中必须包含 cluster、nodes、开始时间、关闭时间、模型名称、风险决策摘要和导出时间。

## 安全与隐私

- 复用现有 `sanitizeToolArguments` 逻辑，报告不得出现 `node_add.password`。
- 对 token、password、authorization、private key 等 secret-like 文本复用 memory 层的拒绝规则。
- 单条工具输出摘要限制在 1200 字符；报告生成时不展开完整原始输出。
- mutating 工具必须记录风险等级、确认结果和目标节点。

## 测试策略

- `internal/evidence` 单元测试覆盖事件追加、排序、摘要截断、secret 拒绝和 Markdown 导出。
- `internal/tui` 行为测试覆盖 `/incident start|status|note|export|close`。
- 使用 fake tool result 验证工具调用、风险确认和最终回复进入 incident timeline。
- 使用 golden Markdown 验证报告章节、时间线顺序和脱敏行为。

## 验收标准

- 用户能在 TUI 内开始、查看、导出和关闭 incident。
- 一次包含工具调用、风险确认和最终回复的调查能生成结构化 Markdown 报告。
- 报告足以回答“谁让模型做了什么、在哪些节点、风险决策是什么、结果是什么”。
- 报告不会泄露已知 secret-like 内容。
