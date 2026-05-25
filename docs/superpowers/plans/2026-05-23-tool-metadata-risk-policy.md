# Tool Metadata 与 Risk Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用结构化 tool metadata 驱动工具搜索、风险前置判定、确认摘要和子代理只读工具过滤。

**Architecture:** `internal/tools` 提供内建工具 metadata registry；`internal/security` 新增 policy 层并被 `Reviewer` 调用；TUI 的 `tool_search` 和确认 UI 从 metadata 获取 safety/scope/capability；subagent 复用同一套 metadata 过滤只读工具。

**Tech Stack:** Go, existing MCP tool definitions, existing `internal/security` reviewer, existing `internal/tui` tool cache and BM25 search.

---

## File Structure

- Create `internal/tools/metadata.go`: metadata types、默认 registry、lookup helpers。
- Create `internal/tools/metadata_test.go`: 覆盖所有 agent 内建工具和 TUI meta tools 的 metadata。
- Create `internal/security/policy.go`: policy evaluator。
- Create `internal/security/policy_test.go`: risk policy 决策测试。
- Modify `internal/security/reviewer.go`: 先调用 policy，再保留 shell whitelist/blacklist/LLM 审查。
- Modify `internal/security/reviewer_test.go`: 调整只读、mutating 和 unknown 工具期望。
- Modify `internal/tui/metatools.go`: 为 meta tools 提供 metadata，并在 tool search doc 中加入 metadata tokens。
- Modify `internal/tui/metatools_test.go`: 验证 search 输出包含 metadata 且 ranking 使用 capability。
- Modify `internal/tui/model.go`: 确认摘要显示 safety、scope、关键参数和目标节点。
- Modify `internal/subagent/runner.go`: 用 metadata 判断 read-only 工具。
- Modify `internal/subagent/runner_test.go`: 验证 mutating 工具被过滤。

---

### Task 1: Tool Metadata Registry

**Files:**
- Create: `internal/tools/metadata.go`
- Test: `internal/tools/metadata_test.go`

- [ ] **Step 1: Write failing metadata coverage tests**

Create tests that:

- Assert `MetadataFor("svc/status")` returns `SafetyReadOnly` and capability `service`.
- Assert `MetadataFor("node_add")` returns `SafetyMutating`.
- Assert `MetadataFor("exec")` returns `SafetyDestructive`.
- Iterate through `Registry.List()` from `registerAllTools` equivalent fixtures and fail if any built-in tool lacks metadata.

Run:

```bash
go test ./internal/tools ./cmd/conan-agent -run 'Test.*Metadata|TestRegisterAllTools' -count=1
```

Expected: FAIL because metadata registry does not exist.

- [ ] **Step 2: Implement metadata types**

Create `internal/tools/metadata.go` with:

```go
type Safety string

const (
	SafetyReadOnly    Safety = "read-only"
	SafetyMutating    Safety = "mutating"
	SafetyDestructive Safety = "destructive"
)

type Scope string

const (
	ScopeLocal   Scope = "local"
	ScopeNode    Scope = "node"
	ScopeCluster Scope = "cluster"
)

type Metadata struct {
	Name        string
	Capability  []string
	Safety      Safety
	Scope       Scope
	Privileges  []string
	OutputShape string
	Tags        []string
}
```

Add `DefaultMetadata() map[string]Metadata`, `MetadataFor(name string) (Metadata, bool)`, and `IsReadOnly(name string) bool`.

- [ ] **Step 3: Run metadata tests**

Run:

```bash
go test ./internal/tools -run Metadata -count=1
```

Expected: PASS.

### Task 2: Risk Policy Evaluator

**Files:**
- Create: `internal/security/policy.go`
- Test: `internal/security/policy_test.go`
- Modify: `internal/security/reviewer.go`
- Test: `internal/security/reviewer_test.go`

- [ ] **Step 1: Write failing policy tests**

Test these cases:

- `svc/status` returns `RiskAllow` with reason `read-only tool metadata`.
- `file_put` returns `RiskConfirm`.
- `call_tool` with inner `svc/status` returns allow.
- `call_tool` with inner `svc/restart` returns confirm because metadata is missing or mutating.
- `exec` returns an indeterminate/destructive decision so reviewer continues to whitelist/model flow.
- unknown tool returns confirm with reason `missing tool metadata`.

- [ ] **Step 2: Implement policy**

Implement:

```go
type PolicyDecision struct {
	Level       RiskLevel
	Reason      string
	ContinueLLM bool
}

type Policy struct {
	Metadata map[string]tools.Metadata
}
```

`Evaluate` should parse `call_tool` arguments:

```json
{"node":"web-1","tool":"svc/status","arguments":{"service":"nginx"}}
```

and evaluate the inner tool name.

- [ ] **Step 3: Wire reviewer**

In `Reviewer.Review`, call policy before the hard-coded read-only map. Remove or shrink `readOnlyTools` after tests prove metadata coverage. Preserve current behavior for shell whitelist, blacklist, local file allowlist and LLM fallback.

- [ ] **Step 4: Run security tests**

Run:

```bash
go test ./internal/security -run 'TestPolicy|TestReviewer' -count=1
```

Expected: PASS.

### Task 3: Tool Search Metadata

**Files:**
- Modify: `internal/tui/metatools.go`
- Test: `internal/tui/metatools_test.go`

- [ ] **Step 1: Write failing search tests**

Add tests that:

- `tool_search` result for `svc/status` includes `safety`, `scope`, and `capability`.
- Query `service read only status` ranks `svc/status` above `exec`.
- Query `upload file` still returns first-class `file_put` metadata as mutating.

- [ ] **Step 2: Extend search docs and result JSON**

Add metadata fields to `toolSearchDoc` and `toolSearchResult`. Tokenize `Capability`, `Safety`, `Scope`, and `Tags` as part of BM25 input, with capability terms repeated to increase weight.

- [ ] **Step 3: Run TUI metadata search tests**

Run:

```bash
go test ./internal/tui -run 'TestToolSearch.*Metadata|TestToolSearch.*Ranking' -count=1
```

Expected: PASS.

### Task 4: Confirmation Summary

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write confirmation rendering tests**

For pending `file_put`, verify the confirmation view includes:

```text
Safety: mutating
Node: web-1
local_path: README.md
remote_path: /tmp/README.md
```

For `svc/restart` through `call_tool`, verify it includes inner tool name and service name.

- [ ] **Step 2: Implement summary builder**

Add a helper near confirmation rendering:

```go
func confirmationSummary(call llm.ToolCall, nodes []string) []string
```

It should use metadata plus selected argument keys: `command`, `path`, `local_path`, `remote_path`, `service`, `namespace`, `package`, `name`, and `host`.

- [ ] **Step 3: Run confirmation tests**

Run:

```bash
go test ./internal/tui -run 'TestConfirmation.*Summary' -count=1
```

Expected: PASS.

### Task 5: Subagent Read-Only Filtering

**Files:**
- Modify: `internal/subagent/runner.go`
- Test: `internal/subagent/runner_test.go`

- [ ] **Step 1: Write filtering tests**

Verify investigator role receives `svc/status`, `log/read`, `memory_search`, `memory_read`, and `tool_search`, but does not receive `file_put`, `node_add`, `memory_patch`, or `exec`.

- [ ] **Step 2: Use metadata in `allowedTools`**

Replace hard-coded read-only decisions with `tools.IsReadOnly`, while keeping explicit allowance for model meta tools that have metadata.

- [ ] **Step 3: Run subagent tests**

Run:

```bash
go test ./internal/subagent -run TestRunner -count=1
```

Expected: PASS.

### Task 6: Verification

**Files:**
- No additional files.

- [ ] Run:

```bash
go test ./internal/tools ./internal/security ./internal/tui ./internal/subagent ./cmd/conan-agent -count=1
```

Expected: PASS.

- [ ] Run:

```bash
go test ./... -count=1
```

Expected: PASS, unless unrelated dirty-worktree failures are already present; document unrelated failures with exact package and test name.
