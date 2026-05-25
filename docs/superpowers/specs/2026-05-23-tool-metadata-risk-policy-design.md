# Tool Metadata 与 Risk Policy 设计

## 目标

为 Conan 工具体系增加结构化元数据，让工具搜索、风险评估、确认摘要和子代理工具限制更多依赖可测试的数据，而不是只依赖工具名称、描述和 prompt 约束。

## 非目标

- 不重写 MCP 协议；第一阶段通过 Conan 本地映射补齐 metadata。
- 不一次性覆盖外部 MCP server 的全部工具。
- 不删除现有 LLM 风险评估；结构化 policy 是前置判定和 prompt 输入增强。
- 不引入集中策略服务。

## Metadata Schema

新增 `internal/tools/metadata.go` 定义：

```go
type Safety string
type Scope string
type Privilege string
type OutputShape string

type Metadata struct {
	Name       string
	Capability []string
	Safety     Safety
	Scope      Scope
	Privileges []Privilege
	Output     OutputShape
	Tags       []string
}
```

枚举值：

```text
Safety: read-only, mutating, destructive
Scope: local, node, cluster
Privilege: user, root, kubeconfig
OutputShape: text, json, table, stream
```

`Safety` 是风险策略的主输入。`Capability` 和 `Tags` 是 tool_search 的排名输入。

## 默认元数据

第一阶段为现有内建工具和 TUI meta tools 覆盖元数据：

- read-only：`fs/read`、`fs/list`、`fs/stat`、`sys/*`、`svc/list`、`svc/status`、`log/*`、`net/ping`、`net/traceroute`、`net/portcheck`、`web/search`、`web/fetch`、`docker/ps`、`docker/images`、`docker/logs`、`k8s/pods`、`k8s/logs`、`k8s/events`、`k8s/describe`、`pkg/list`、`pkg/search`、`cron/list`、`cron/show`、`tool_search`、`memory_search`、`memory_read`
- mutating：`file_put`、`file_get`、`memory_patch`、`memory_write_note`、`memory_promote`、`node_add`
- destructive：`shell/run`、`exec` 作为兜底能力，具体风险继续由 whitelist、blacklist 和模型评估细分
- delegated：`call_tool` 使用 inner tool 的 metadata；`subagents_run` 只允许 read-only 子工具

## Risk Policy

新增 `internal/security/policy.go`：

```go
type PolicyDecision struct {
	Level  RiskLevel
	Reason string
}

type Policy struct {
	Metadata map[string]tools.Metadata
}

func (p Policy) Evaluate(toolName string, toolInput string, targetNodes []string) PolicyDecision
```

策略顺序：

1. `read-only` 默认 `allow`。
2. `mutating` 默认 `confirm`。
3. `destructive` 默认进入现有 reviewer 的 whitelist/blacklist/LLM 流程。
4. `call_tool` 解出 inner tool 后按 inner metadata 评估。
5. 未知工具默认 `confirm`，原因写明 `missing tool metadata`。

现有 `Reviewer.Review` 保留，但先调用 policy。policy 可以直接给出 `allow` 或 `confirm`；`destructive` 和 shell 类调用继续走现有模型风险评估。

## Tool Search Ranking

`tool_search` 的文档构建增加 metadata 字段：

- capability tokens 权重大于描述 tokens。
- safety tokens 用于用户明确要求“只读”“不要改动”“重启”“删除”等场景。
- 输出结果展示 `safety`、`scope` 和 `capability`，让模型选择工具时看到结构化信息。

搜索结果示例：

```json
{
  "name": "svc/status",
  "nodes": ["web-1"],
  "safety": "read-only",
  "scope": "node",
  "capability": ["service"],
  "description": "Show service status"
}
```

## 确认摘要

高风险确认 UI 应优先从 metadata 和参数中提取摘要：

- tool
- safety
- target nodes
- path、service、namespace、package、command 等关键参数
- risk reason

确认摘要不应要求用户阅读完整 JSON 才知道影响面。

## 配置扩展

后续可在 config 中增加按 cluster/node profile 禁用工具的配置。第一阶段只做 metadata 与 policy 的代码结构，不实现复杂策略同步。

```yaml
tools:
  disabled:
    - docker/*
  policy:
    prod:
      node_add: confirm
      exec: confirm
```

## 测试策略

- `internal/tools` 测试覆盖每个内建工具都有 metadata。
- `internal/security` 测试覆盖 read-only allow、mutating confirm、destructive shell 进入 reviewer、unknown confirm、call_tool 使用 inner metadata。
- `internal/tui` 测试覆盖 tool_search 输出包含 metadata，确认摘要包含安全级别和目标。
- 子代理测试覆盖只读工具过滤不再依赖硬编码工具名列表。

## 验收标准

- 常见只读诊断工具不再依赖硬编码 `readOnlyTools` map 才能通过风险审查。
- mutating 工具稳定要求确认，并在确认 UI 中显示结构化影响摘要。
- `tool_search` 能输出工具 safety、scope 和 capability。
- 新增内建工具时，如果没有 metadata，测试失败。
