# Runbook Draft 与 Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从 incident report 生成 runbook 草案，支持 runbook preview，并把 runbook 作为 Markdown 记忆复用。

**Architecture:** 新增 `internal/runbook` 包处理 Markdown/frontmatter、draft 和 preview；TUI 增加 `/runbook` 命令并把 preview/run 注入现有模型与工具执行流程；memory 层继续负责 Markdown 文件读写和搜索。

**Tech Stack:** Go, Markdown files under `~/.conan/memory/memory/runbooks`, existing `internal/memory` Markdown search/read, existing TUI slash command flow.

---

## File Structure

- Create `internal/runbook/runbook.go`: runbook model、frontmatter parser 和 renderer。
- Create `internal/runbook/runbook_test.go`: parse/render tests。
- Create `internal/runbook/draft.go`: 从 incident Markdown 生成草案。
- Create `internal/runbook/draft_test.go`: incident-to-runbook tests。
- Create `internal/runbook/preview.go`: preview 结构和风险步骤分类。
- Create `internal/runbook/preview_test.go`: read/confirm/destructive 分类测试。
- Modify `internal/tui/command.go`: 增加 `/runbook draft|preview|run`。
- Modify `internal/tui/command_test.go`: runbook 命令解析测试。
- Modify `internal/tui/model.go`: 执行 draft、preview 和 run 注入。
- Modify `internal/tui/model_test.go`: 行为测试。
- Modify `internal/memory/tools_test.go`: 确认 runbooks Markdown 可搜索和读取。

---

### Task 1: Runbook Markdown Model

**Files:**
- Create: `internal/runbook/runbook.go`
- Test: `internal/runbook/runbook_test.go`

- [ ] **Step 1: Write failing parse/render tests**

Test parsing this Markdown:

```markdown
---
title: Nginx 502 快速诊断
source_incident: incident-abc123
cluster: prod
tags: nginx, 502
created_at: 2026-05-23T10:00:00Z
---

# Nginx 502 快速诊断

## 适用场景

网关返回 502。

## 步骤

1. [read] 使用 `svc/status` 检查 nginx 状态。
2. [confirm] 使用 `svc/restart` 重启 nginx。
```

Expected: title, source incident, cluster, tags and two steps parse correctly.

- [ ] **Step 2: Implement model and parser**

Implement:

```go
type StepKind string

const (
	StepRead        StepKind = "read"
	StepConfirm     StepKind = "confirm"
	StepDestructive StepKind = "destructive"
)

type Step struct {
	Kind StepKind
	Text string
}

type Runbook struct {
	Title          string
	SourceIncident string
	Cluster        string
	Tags           []string
	CreatedAt      time.Time
	Scenario       string
	Prechecks      string
	Steps          []Step
	Verification   string
	Risks           string
}
```

Expose `ParseMarkdown`, `RenderMarkdown`, and `Slug`.

- [ ] **Step 3: Run parser tests**

Run:

```bash
go test ./internal/runbook -run 'TestParse|TestRender' -count=1
```

Expected: PASS.

### Task 2: Draft From Incident Report

**Files:**
- Create: `internal/runbook/draft.go`
- Test: `internal/runbook/draft_test.go`

- [ ] **Step 1: Write failing draft tests**

Use a fixture incident report with `## 摘要`、`## 影响范围`、`## 证据`、`## 执行动作`、`## 验证结果` and assert `DraftFromIncident` produces:

- title derived from incident title
- source incident id or path
- scenario from summary and impact
- read steps from evidence
- confirm steps from approved mutating actions
- verification from verification result

- [ ] **Step 2: Implement deterministic draft extraction**

Implement:

```go
func DraftFromIncident(path string, markdown string, now time.Time) (Runbook, error)
```

Extraction rules:

- Use first `# ` heading as title.
- Use `## 摘要` + `## 影响范围` for `Scenario`.
- Convert evidence lines containing read-only tool names into `[read]` steps.
- Convert approved action lines into `[confirm]` steps.
- Copy `## 验证结果` into `Verification`.
- Reject secret-like content using the same marker list as memory validation.

- [ ] **Step 3: Run draft tests**

Run:

```bash
go test ./internal/runbook -run TestDraftFromIncident -count=1
```

Expected: PASS.

### Task 3: Preview Classification

**Files:**
- Create: `internal/runbook/preview.go`
- Test: `internal/runbook/preview_test.go`

- [ ] **Step 1: Write failing preview tests**

Verify preview returns:

- `ReadSteps` for `[read]`
- `ConfirmSteps` for `[confirm]`
- `DestructiveSteps` for `[destructive]`
- `Summary` containing title, cluster and counts

- [ ] **Step 2: Implement preview**

Implement:

```go
type Preview struct {
	Title            string
	Cluster          string
	ReadSteps        []Step
	ConfirmSteps     []Step
	DestructiveSteps []Step
	Summary          string
}

func BuildPreview(rb Runbook) Preview
func RenderPreview(p Preview) string
```

`RenderPreview` must not call tools or write files.

- [ ] **Step 3: Run preview tests**

Run:

```bash
go test ./internal/runbook -run TestPreview -count=1
```

Expected: PASS.

### Task 4: TUI Slash Commands

**Files:**
- Modify: `internal/tui/command.go`
- Test: `internal/tui/command_test.go`

- [ ] **Step 1: Write command tests**

Cover:

```text
/runbook draft
/runbook draft incidents/2026-05-23-api.md
/runbook preview runbooks/2026-05-23-nginx-502.md
/runbook run runbooks/2026-05-23-nginx-502.md
```

- [ ] **Step 2: Implement parser support**

Add command action and path argument. Unknown action should return a normal unknown-command message.

- [ ] **Step 3: Run command tests**

Run:

```bash
go test ./internal/tui -run 'TestRunbookCommand|TestParse.*Runbook' -count=1
```

Expected: PASS.

### Task 5: Wire Draft And Preview In TUI

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write behavior tests**

Test that:

- `/runbook draft <incident>` reads an incident Markdown file and writes a runbook Markdown file.
- `/runbook preview <runbook>` renders preview text and does not dispatch tool calls.
- `/runbook run <runbook>` injects runbook content into the next model request with an instruction to execute read steps first and confirm mutating steps.

- [ ] **Step 2: Implement draft handling**

Resolve paths relative to `~/.conan/memory/memory`. Only allow paths under `incidents/` for draft source and write output under `runbooks/`.

- [ ] **Step 3: Implement preview handling**

Read runbook using `memory.NewMarkdownStore`, parse with `runbook.ParseMarkdown`, render preview, and append it as an assistant-visible TUI message.

- [ ] **Step 4: Implement run injection**

For `/runbook run`, append a user message similar to:

```text
Execute this Conan runbook. First perform read-only evidence collection. For every [confirm] or [destructive] step, use Conan tools normally so the existing risk review and confirmation flow is enforced.

<runbook markdown>
```

Do not add a bypass path for confirmations.

- [ ] **Step 5: Run TUI behavior tests**

Run:

```bash
go test ./internal/tui -run 'TestRunbook' -count=1
```

Expected: PASS.

### Task 6: Memory Search Integration

**Files:**
- Modify: `internal/memory/tools_test.go`

- [ ] **Step 1: Add regression test**

Create a runbook Markdown file under `memory/runbooks/` and assert `memory_search` finds it and `memory_read` can read it.

- [ ] **Step 2: Fix memory integration only if test fails**

If current Markdown search already covers `runbooks/`, keep production code unchanged. If it misses runbooks, update the search allowlist in `internal/memory`.

- [ ] **Step 3: Run memory tests**

Run:

```bash
go test ./internal/memory -run 'TestMemorySearch.*Runbook|TestHandleMemoryRead' -count=1
```

Expected: PASS.

### Task 7: Verification

**Files:**
- No additional files.

- [ ] Run:

```bash
go test ./internal/runbook ./internal/tui ./internal/memory -count=1
```

Expected: PASS.

- [ ] Run:

```bash
go test ./... -count=1
```

Expected: PASS, unless unrelated dirty-worktree failures are already present; document unrelated failures with exact package and test name.
