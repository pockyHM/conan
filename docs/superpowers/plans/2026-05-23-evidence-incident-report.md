# Evidence Model 与 Incident Report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Conan 增加最小证据模型、incident 生命周期命令和 Markdown 报告导出。

**Architecture:** 新增 `internal/evidence` 包负责事件模型、记录器和报告渲染；TUI 只负责命令路由和在现有工具/风险/消息流程中追加事件；报告写入现有 Markdown 记忆目录的 `incidents/` 层。

**Tech Stack:** Go, Bubble Tea, existing `internal/tui`, existing `internal/memory` Markdown rules, existing security audit/risk review flow.

---

## File Structure

- Create `internal/evidence/evidence.go`: `Event`、`Incident`、`Recorder` 和输入校验。
- Create `internal/evidence/evidence_test.go`: 事件追加、排序、摘要截断、secret-like 拒绝测试。
- Create `internal/evidence/report.go`: Markdown 报告渲染和导出路径生成。
- Create `internal/evidence/report_test.go`: golden 报告章节、时间线顺序和脱敏测试。
- Modify `internal/tui/command.go`: 增加 `/incident` 命令解析。
- Modify `internal/tui/command_test.go`: 覆盖 `/incident start|status|note|export|close` 解析。
- Modify `internal/tui/model.go`: 持有 incident recorder，并在用户输入、工具调用、风险结果、工具结果、subagent 结果和最终回复处记录证据。
- Modify `internal/tui/model_test.go`: 覆盖 incident lifecycle 和报告导出行为。
- Modify `cmd/conan/main.go`: 注入 incident 报告目录 `~/.conan/memory/memory/incidents`。

---

### Task 1: Evidence Core

**Files:**
- Create: `internal/evidence/evidence.go`
- Test: `internal/evidence/evidence_test.go`

- [ ] **Step 1: Write failing tests for recorder behavior**

Add tests that create a recorder, start an incident, append user/tool/risk events, verify chronological order, verify long summaries are truncated to 1200 runes, and verify secret-like text is rejected.

Run:

```bash
go test ./internal/evidence -run 'TestRecorder|TestEvent' -count=1
```

Expected: FAIL because `internal/evidence` does not exist.

- [ ] **Step 2: Implement core types and recorder**

Implement these public shapes in `internal/evidence/evidence.go`:

```go
package evidence

import (
	"encoding/json"
	"time"
)

type Source string

const (
	SourceUser      Source = "user"
	SourceAssistant Source = "assistant"
	SourceTool      Source = "tool"
	SourceSubagent  Source = "subagent"
	SourceMemory    Source = "memory"
	SourceRisk      Source = "risk"
)

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

type Event struct {
	ID          string            `json:"id"`
	IncidentID  string            `json:"incident_id"`
	Source      Source            `json:"source"`
	Cluster     string            `json:"cluster,omitempty"`
	Nodes       []string          `json:"nodes,omitempty"`
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

The recorder should expose `Start`, `Current`, `Append`, `Events`, `Note`, and `Close` methods. `Append` must no-op when no incident is open.

- [ ] **Step 3: Run focused tests**

Run:

```bash
go test ./internal/evidence -run 'TestRecorder|TestEvent' -count=1
```

Expected: PASS.

### Task 2: Markdown Report Export

**Files:**
- Modify: `internal/evidence/report.go`
- Test: `internal/evidence/report_test.go`

- [ ] **Step 1: Write failing golden report tests**

Test that `RenderMarkdown` includes these headings in order:

```text
# API latency incident
## 摘要
## 影响范围
## 时间线
## 证据
## 根因假设
## 执行动作
## 验证结果
## 后续项
```

Also test that risk events render their `RiskLevel` and `RiskOutcome`, and `ExportMarkdown` writes to `incidents/YYYY-MM-DD-api-latency-incident.md`.

- [ ] **Step 2: Implement report rendering**

Implement `RenderMarkdown(incident Incident, events []Event, model string) string` and `ExportMarkdown(root string, incident Incident, events []Event, model string) (string, error)`.

Report rendering rules:

- Sort events by `Timestamp`.
- Group tool events under `## 证据`.
- Put `SourceRisk` events under `## 执行动作` when `RiskOutcome` is `approved`, `cancelled`, `blocked`, or `dispatched`.
- Put assistant final summaries under `## 验证结果`.
- Limit each rendered event summary to 1200 runes.

- [ ] **Step 3: Run report tests**

Run:

```bash
go test ./internal/evidence -run 'TestRender|TestExport' -count=1
```

Expected: PASS.

### Task 3: TUI Incident Commands

**Files:**
- Modify: `internal/tui/command.go`
- Test: `internal/tui/command_test.go`

- [ ] **Step 1: Write command parsing tests**

Cover:

```text
/incident start API latency
/incident status
/incident note checked nginx logs
/incident export
/incident close
```

Expected parsed command should include action and trailing argument.

- [ ] **Step 2: Implement parser support**

Extend the slash command parser with a single `incident` command and an action field. Keep unknown actions as user-visible command errors.

- [ ] **Step 3: Run command tests**

Run:

```bash
go test ./internal/tui -run 'TestParse.*Incident|TestIncidentCommand' -count=1
```

Expected: PASS.

### Task 4: Wire Evidence Into TUI Flow

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`
- Modify: `cmd/conan/main.go`

- [ ] **Step 1: Write behavior tests**

Add tests for:

- `/incident start` creates an open incident and updates status.
- User prompt is recorded as `SourceUser`.
- A tool call and tool result are recorded as `SourceTool`.
- A risk confirmation records `SourceRisk` with outcome.
- `/incident export` writes a Markdown file and reports the path.
- `/incident close` closes the incident and prevents further automatic appends.

- [ ] **Step 2: Add recorder to model config**

Add fields to TUI config:

```go
IncidentDir string
```

Initialize an evidence recorder in `NewModel`. In `cmd/conan/main.go`, pass:

```go
IncidentDir: filepath.Join(loader.Home(), "memory", "memory", "incidents")
```

- [ ] **Step 3: Append evidence at existing flow points**

In `internal/tui/model.go`, append events at these points:

- After a normal user message is accepted.
- When a `llm.ToolCallEvent` is converted into a tool call message.
- When `riskAssessmentMsg` is processed.
- When `multiToolResultMsg` is processed.
- When a stream finishes with assistant text.
- When `subagents_run` returns.

Use existing sanitized tool arguments, selected cluster, and selected nodes.

- [ ] **Step 4: Run TUI tests**

Run:

```bash
go test ./internal/tui -run 'TestIncident|TestTool|TestRisk' -count=1
```

Expected: PASS.

### Task 5: Verification

**Files:**
- No additional files.

- [ ] Run:

```bash
go test ./internal/evidence ./internal/tui ./cmd/conan -count=1
```

Expected: PASS.

- [ ] Run:

```bash
go test ./... -count=1
```

Expected: PASS, unless unrelated dirty-worktree failures are already present; document unrelated failures with exact package and test name.
