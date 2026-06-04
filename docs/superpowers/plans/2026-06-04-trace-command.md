# Trace Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `/trace` to Conan's TUI so users can inspect the current-session execution chain, including live assistant, tool, and subagent nodes.

**Architecture:** Add a presentation-only trace state to `internal/tui.Model`, plus a new `modeTrace` full-window view. Existing event paths append or update trace nodes when user messages, stream deltas, tool calls, tool results, and subagent results arrive; saved conversation history remains unchanged.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, existing Conan TUI model/update tests.

---

## File Structure

- Modify `internal/tui/command.go`: add `CommandTrace` and `/trace` parser branch.
- Modify `internal/tui/command_test.go`: add parse coverage.
- Modify `internal/tui/model.go`: add trace fields, `modeTrace`, command handling, key routing, data-flow hooks, clear/resume integration.
- Create `internal/tui/trace.go`: trace types, mutation helpers, summary helpers, rebuild-from-conversation helpers.
- Create `internal/tui/tracepage.go`: render timeline/detail pages and handle trace-mode keys.
- Create `internal/tui/tracepage_test.go`: focused tests for rendering, navigation, mutation helpers, and model integration.
- Modify `internal/tui/subagentpage.go` only if a small exported or shared helper is needed; prefer not to change it unless implementation shows duplication is excessive.

## Task 1: Slash Command and Trace Mode Shell

**Files:**
- Modify: `internal/tui/command.go`
- Modify: `internal/tui/command_test.go`
- Modify: `internal/tui/model.go`
- Create: `internal/tui/tracepage.go`
- Test: `internal/tui/tracepage_test.go`

- [ ] **Step 1: Write the failing command parser test**

In `internal/tui/command_test.go`, add this row to `TestParseSlashCommand`:

```go
{input: "/trace", kind: CommandTrace},
```

- [ ] **Step 2: Write the failing mode-open test**

Create `internal/tui/tracepage_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTraceCommandOpensEmptyTracePage(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.applyCommand(SlashCommand{Kind: CommandTrace})
	model = next

	if model.mode != modeTrace {
		t.Fatalf("mode = %v, want modeTrace", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Trace", "No trace nodes yet"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trace view missing %q:\n%s", want, view)
		}
	}
}

func TestTracePageEscReturnsToChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}
```

- [ ] **Step 3: Run tests and verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestParseSlashCommand|TestTraceCommandOpensEmptyTracePage|TestTracePageEscReturnsToChat' -count=1
```

Expected: FAIL because `CommandTrace` and `modeTrace` are undefined.

- [ ] **Step 4: Add the minimal command and mode implementation**

In `internal/tui/command.go`, add the constant:

```go
CommandTrace CommandKind = "trace"
```

Add parser branch:

```go
case "trace":
	return SlashCommand{Kind: CommandTrace, Arg: arg}, true
```

In `internal/tui/model.go`, add `modeTrace` to the `tuiMode` const block after `modeSubagentList`:

```go
modeTrace
```

In `handleKey`, route trace mode before confirm/node modes:

```go
if m.mode == modeTrace {
	return m.handleTraceKey(key)
}
```

In `View`, render trace mode before the chat body:

```go
if m.mode == modeTrace {
	return header + "\n\n" + m.renderTracePage() + "\n\n" + statusView
}
```

In `applyCommand`, add:

```go
case CommandTrace:
	m.mode = modeTrace
	m.status = m.uiLanguage.tr("Trace opened", "已打开链路")
	return m, nil
```

Update the help string to include `/trace` in both English and Chinese branches.

Create `internal/tui/tracepage.go` with the minimal view and key handler:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tracePageBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)

	tracePageTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				Padding(0, 1)

	tracePageHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true)
)

func (m Model) renderTracePage() string {
	title := tracePageTitleStyle.Render(m.uiLanguage.tr("Trace", "链路追踪"))
	empty := m.uiLanguage.tr(
		"No trace nodes yet.\n\nSend a message or run a tool to populate the current-session trace.",
		"还没有链路节点。\n\n发送消息或运行工具后，当前会话链路会显示在这里。",
	)
	help := tracePageHelpStyle.Render(m.uiLanguage.tr("Esc close", "Esc 关闭"))
	return tracePageBoxStyle.Render(strings.Join([]string{title, "", empty, "", help}, "\n"))
}

func (m Model) handleTraceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEsc {
		m.mode = modeChat
		m.traceDetailVisible = false
		m.status = m.uiLanguage.tr("Trace closed", "已关闭链路")
		return m, nil
	}
	return m, nil
}
```

- [ ] **Step 5: Run tests and verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestParseSlashCommand|TestTraceCommandOpensEmptyTracePage|TestTracePageEscReturnsToChat' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/command.go internal/tui/command_test.go internal/tui/model.go internal/tui/tracepage.go internal/tui/tracepage_test.go
git commit -m "feat(tui): add trace command shell"
```

## Task 2: Trace State and Timeline Rendering

**Files:**
- Create: `internal/tui/trace.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/tracepage.go`
- Test: `internal/tui/tracepage_test.go`

- [ ] **Step 1: Write failing tests for append, render markers, and navigation**

Append these tests to `internal/tui/tracepage_test.go`:

```go
func TestTracePageRendersArrowTimelineMarkers(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace
	model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", "check nginx", "check nginx"))
	model = model.appendTraceNode(traceNode{ID: "tool-1", Kind: traceToolCall, Status: traceRunning, Title: "tool call", Summary: "shell_run {\"command\":\"uptime\"}", Detail: "shell_run"})
	model = model.appendTraceNode(traceNode{ID: "result-1", Kind: traceToolResult, Status: traceDone, Title: "tool result", Summary: "local · ok", Detail: "ok"})
	model = model.appendTraceNode(traceNode{ID: "subagent-1", Kind: traceSubagent, Status: traceRunning, Title: "subagent", Summary: "investigator[node-01] running", Detail: "turn 1"})

	view := model.View()
	for _, want := range []string{"●", "│", "↓", "▶", "✓", "◇", "check nginx", "shell_run", "local · ok"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trace view missing %q:\n%s", want, view)
		}
	}
}

func TestTracePageNavigationAndDetail(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace
	model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", "first", "first detail"))
	model = model.appendTraceNode(newTraceNode(traceAssistant, traceDone, "assistant", "second", "second detail"))

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.traceCursor != 1 {
		t.Fatalf("traceCursor = %d, want 1", model.traceCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !model.traceDetailVisible {
		t.Fatal("trace detail should be visible after Enter")
	}
	if view := model.View(); !strings.Contains(view, "second detail") {
		t.Fatalf("detail missing selected node detail:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.traceDetailVisible {
		t.Fatal("Esc from detail should return to timeline")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestTracePageRendersArrowTimelineMarkers|TestTracePageNavigationAndDetail' -count=1
```

Expected: FAIL because trace types and render logic do not exist.

- [ ] **Step 3: Add trace state fields to Model**

In `internal/tui/model.go`, add fields to `Model` near other TUI state fields:

```go
traceNodes             []traceNode
traceCursor            int
traceDetailVisible     bool
activeTraceAssistantID string
```

- [ ] **Step 4: Create trace types and helpers**

Create `internal/tui/trace.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/pkg/models"
)

type traceKind string

const (
	traceUser       traceKind = "user"
	traceAssistant  traceKind = "assistant"
	traceToolCall   traceKind = "tool_call"
	traceToolResult traceKind = "tool_result"
	traceSubagent   traceKind = "subagent"
)

type traceStatus string

const (
	tracePending traceStatus = "pending"
	traceRunning traceStatus = "running"
	traceDone    traceStatus = "done"
	traceFailed  traceStatus = "failed"
	traceBlocked traceStatus = "blocked"
)

type traceNode struct {
	ID        string
	ParentID  string
	Kind      traceKind
	Status    traceStatus
	Title     string
	Summary   string
	Detail    string
	StartedAt time.Time
	EndedAt   time.Time

	ToolCallID string
	ToolName   string
	SubagentID string
}

func newTraceNode(kind traceKind, status traceStatus, title, summary, detail string) traceNode {
	now := time.Now()
	return traceNode{
		ID:        models.NewID(),
		Kind:      kind,
		Status:    status,
		Title:     title,
		Summary:   strings.TrimSpace(summary),
		Detail:    strings.TrimSpace(detail),
		StartedAt: now,
	}
}

func (m Model) appendTraceNode(node traceNode) Model {
	if node.ID == "" {
		node.ID = models.NewID()
	}
	if node.StartedAt.IsZero() {
		node.StartedAt = time.Now()
	}
	m.traceNodes = append(m.traceNodes, node)
	if m.traceCursor < 0 || m.traceCursor >= len(m.traceNodes) {
		m.traceCursor = len(m.traceNodes) - 1
	}
	return m
}

func (m Model) updateTraceNode(id string, fn func(*traceNode)) Model {
	if id == "" || fn == nil {
		return m
	}
	for i := range m.traceNodes {
		if m.traceNodes[i].ID == id {
			fn(&m.traceNodes[i])
			return m
		}
	}
	return m
}

func (m Model) findTraceByToolCallID(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].ToolCallID == id {
			return i
		}
	}
	return -1
}

func (m Model) findTraceBySubagentID(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].SubagentID == id {
			return i
		}
	}
	return -1
}

func traceKindLabel(kind traceKind, lang uiLanguage) string {
	switch kind {
	case traceUser:
		return lang.tr("user", "用户")
	case traceAssistant:
		return lang.tr("assistant", "助手")
	case traceToolCall:
		return lang.tr("tool call", "工具调用")
	case traceToolResult:
		return lang.tr("tool result", "工具结果")
	case traceSubagent:
		return lang.tr("subagent", "子智能体")
	default:
		return string(kind)
	}
}

func traceStatusLabel(status traceStatus, lang uiLanguage) string {
	switch status {
	case tracePending:
		return lang.tr("pending", "等待")
	case traceRunning:
		return lang.tr("running", "运行中")
	case traceDone:
		return lang.tr("done", "完成")
	case traceFailed:
		return lang.tr("failed", "失败")
	case traceBlocked:
		return lang.tr("blocked", "已阻止")
	default:
		return string(status)
	}
}

func (m Model) rebuildTraceFromMessages(messages []models.Message) Model {
	m.traceNodes = nil
	m.traceCursor = 0
	m.traceDetailVisible = false
	m.activeTraceAssistantID = ""
	for _, msg := range messages {
		switch msg.Role {
		case conversation.RoleUser:
			m = m.appendTraceNode(newTraceNode(traceUser, traceDone, "user", msg.Content, msg.Content))
		case conversation.RoleAssistant:
			if msg.ToolCallID != "" {
				node := newTraceNode(traceToolCall, traceDone, msg.ToolName, traceToolSummary(msg.ToolName, msg.ToolInput), msg.ToolInput)
				node.ToolCallID = msg.ToolCallID
				node.ToolName = msg.ToolName
				m = m.appendTraceNode(node)
			} else if strings.TrimSpace(msg.Content) != "" {
				m = m.appendTraceNode(newTraceNode(traceAssistant, traceDone, "assistant", firstTraceLine(msg.Content), msg.Content))
			}
		case conversation.RoleTool:
			node := newTraceNode(traceToolResult, traceDone, "tool result", traceToolResultSummary([]nodeToolResult{{Node: "local", Output: msg.Content, Success: true}}), msg.Content)
			node.ToolCallID = msg.ToolCallID
			m = m.appendTraceNode(node)
		}
	}
	return m
}

func firstTraceLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := strings.Split(text, "\n")[0]
	return truncateWithEllipsis(strings.Join(strings.Fields(line), " "), 120)
}

func traceToolSummary(name, args string) string {
	args = strings.Join(strings.Fields(args), " ")
	if args == "" {
		return name
	}
	return truncateWithEllipsis(fmt.Sprintf("%s %s", name, args), 140)
}

func traceToolResultSummary(results []nodeToolResult) string {
	if len(results) == 0 {
		return "0 nodes"
	}
	okCount := 0
	failCount := 0
	for _, r := range results {
		if r.Success {
			okCount++
		} else {
			failCount++
		}
	}
	if len(results) == 1 {
		status := "failed"
		if okCount == 1 {
			status = "ok"
		}
		return fmt.Sprintf("%s · %s", results[0].Node, status)
	}
	return fmt.Sprintf("%d nodes · %d ok · %d failed", len(results), okCount, failCount)
}
```

- [ ] **Step 5: Replace tracepage rendering with timeline/detail rendering**

In `internal/tui/tracepage.go`, keep the package/imports and style variables, then add:

```go
func (m Model) renderTracePage() string {
	if m.traceDetailVisible {
		return m.renderTraceDetailPage()
	}
	return m.renderTraceTimelinePage()
}

func (m Model) renderTraceTimelinePage() string {
	title := tracePageTitleStyle.Render(fmt.Sprintf("%s · %d %s",
		m.uiLanguage.tr("Trace", "链路追踪"),
		len(m.traceNodes),
		m.uiLanguage.tr("nodes", "节点"),
	))
	if len(m.traceNodes) == 0 {
		empty := m.uiLanguage.tr(
			"No trace nodes yet.\n\nSend a message or run a tool to populate the current-session trace.",
			"还没有链路节点。\n\n发送消息或运行工具后，当前会话链路会显示在这里。",
		)
		help := tracePageHelpStyle.Render(m.uiLanguage.tr("Esc close", "Esc 关闭"))
		return tracePageBoxStyle.Render(strings.Join([]string{title, "", empty, "", help}, "\n"))
	}
	lines := []string{title, ""}
	for i, node := range m.traceNodes {
		lines = append(lines, m.renderTraceTimelineNode(i, node, i == len(m.traceNodes)-1))
	}
	lines = append(lines, "", tracePageHelpStyle.Render(m.uiLanguage.tr(
		"↑↓/jk select · Enter detail · Esc close",
		"↑↓/jk 选择 · Enter 详情 · Esc 关闭",
	)))
	return tracePageBoxStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) renderTraceTimelineNode(index int, node traceNode, last bool) string {
	marker := traceMarker(node)
	rail := traceRailStyle.Render("│")
	arrow := traceRailStyle.Render("↓")
	if last {
		rail = " "
		arrow = " "
	}
	cursor := "  "
	rowStyle := traceRowStyle(node)
	if index == m.traceCursor {
		cursor = subagentPageCursorStyle.Render("▶ ")
		rowStyle = subagentPageSelectedStyle
	}
	line := fmt.Sprintf("%02d %-11s %-8s %s",
		index+1,
		traceKindLabel(node.Kind, m.uiLanguage),
		traceStatusLabel(node.Status, m.uiLanguage),
		truncateWithEllipsis(node.Summary, max(m.width-34, 40)),
	)
	return strings.Join([]string{
		cursor + traceMarkerStyle(node).Render(marker),
		"  " + rail,
		"  " + arrow + " " + rowStyle.Render(line),
	}, "\n")
}

func (m Model) renderTraceDetailPage() string {
	if len(m.traceNodes) == 0 {
		return m.renderTraceTimelinePage()
	}
	if m.traceCursor < 0 {
		m.traceCursor = 0
	}
	if m.traceCursor >= len(m.traceNodes) {
		m.traceCursor = len(m.traceNodes) - 1
	}
	node := m.traceNodes[m.traceCursor]
	title := tracePageTitleStyle.Render(m.uiLanguage.tr("Trace detail", "链路详情"))
	lines := []string{
		title,
		"",
		subagentPageLabelStyle.Render("ID: ") + node.ID,
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Type: ", "类型: ")) + traceKindLabel(node.Kind, m.uiLanguage),
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Status: ", "状态: ")) + traceStatusLabel(node.Status, m.uiLanguage),
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Summary: ", "摘要: ")) + node.Summary,
		"",
		subagentPageSectionStyle.Render(m.uiLanguage.tr("Detail", "详情")),
	}
	detail := strings.TrimSpace(node.Detail)
	if detail == "" {
		detail = m.uiLanguage.tr("(no detail)", "（无详情）")
	}
	for _, line := range strings.Split(truncateWithEllipsis(detail, 2400), "\n") {
		lines = append(lines, subagentAssistantStyle.Render("  "+line))
	}
	lines = append(lines, "", tracePageHelpStyle.Render(m.uiLanguage.tr("Esc back", "Esc 返回")))
	return tracePageBoxStyle.Render(strings.Join(lines, "\n"))
}
```

Add helper styles/functions to `tracepage.go`:

```go
var traceRailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

func traceMarker(node traceNode) string {
	switch node.Kind {
	case traceUser:
		return "●"
	case traceAssistant:
		return "◆"
	case traceToolCall:
		return "▶"
	case traceToolResult:
		if node.Status == traceFailed {
			return "✗"
		}
		return "✓"
	case traceSubagent:
		return "◇"
	default:
		return "■"
	}
}

func traceMarkerStyle(node traceNode) lipgloss.Style {
	color := "244"
	switch node.Kind {
	case traceUser:
		color = "39"
	case traceAssistant:
		color = "141"
	case traceToolCall:
		color = "220"
	case traceToolResult:
		if node.Status == traceFailed {
			color = "196"
		} else {
			color = "82"
		}
	case traceSubagent:
		color = "14"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
}

func traceRowStyle(node traceNode) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(traceMarkerStyle(node).GetForeground())
}
```

Update imports in `tracepage.go` to include `fmt`.

- [ ] **Step 6: Implement trace key navigation**

Replace `handleTraceKey` in `tracepage.go` with:

```go
func (m Model) handleTraceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.traceDetailVisible {
		if key.Type == tea.KeyEsc {
			m.traceDetailVisible = false
			return m, nil
		}
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc:
		m.mode = modeChat
		m.status = m.uiLanguage.tr("Trace closed", "已关闭链路")
	case tea.KeyUp:
		if m.traceCursor > 0 {
			m.traceCursor--
		}
	case tea.KeyDown:
		if m.traceCursor < len(m.traceNodes)-1 {
			m.traceCursor++
		}
	case tea.KeyEnter:
		if len(m.traceNodes) > 0 {
			m.traceDetailVisible = true
		}
	case tea.KeyRunes:
		if len(key.Runes) == 1 {
			switch key.Runes[0] {
			case 'k':
				if m.traceCursor > 0 {
					m.traceCursor--
				}
			case 'j':
				if m.traceCursor < len(m.traceNodes)-1 {
					m.traceCursor++
				}
			}
		}
	}
	return m, nil
}
```

- [ ] **Step 7: Run tests and verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestTracePageRendersArrowTimelineMarkers|TestTracePageNavigationAndDetail|TestTraceCommandOpensEmptyTracePage' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/trace.go internal/tui/tracepage.go internal/tui/tracepage_test.go
git commit -m "feat(tui): render trace timeline"
```

## Task 3: User and Assistant Trace Recording

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/trace.go`
- Test: `internal/tui/tracepage_test.go`

- [ ] **Step 1: Write failing tests for user submit and assistant streaming**

Append to `internal/tui/tracepage_test.go`:

```go
func TestTraceRecordsUserMessage(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.startSubmittedMessage("visible user", "llm user", nil)
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("trace nodes = %d, want 1", len(model.traceNodes))
	}
	node := model.traceNodes[0]
	if node.Kind != traceUser || node.Status != traceDone || node.Summary != "visible user" || node.Detail != "llm user" {
		t.Fatalf("user trace node = %#v", node)
	}
}

func TestTraceUpdatesSingleAssistantNodeDuringStreaming(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.streaming = true
	model.activeStreamID = 1
	model.streamStartedAt = time.Now()
	model.streamCh = make(chan llm.ChatEvent)

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "hello"}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: " world"}})
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("trace nodes = %d, want 1", len(model.traceNodes))
	}
	if model.traceNodes[0].Kind != traceAssistant || model.traceNodes[0].Status != traceRunning {
		t.Fatalf("assistant trace node = %#v", model.traceNodes[0])
	}
	if model.traceNodes[0].Detail != "hello world" {
		t.Fatalf("assistant detail = %q, want hello world", model.traceNodes[0].Detail)
	}

	model.appendAssistantStreamContent()
	model.finishStream(false)
	model = model.finishActiveTraceAssistant(traceDone, "hello world")

	if len(model.traceNodes) != 1 {
		t.Fatalf("trace nodes after finish = %d, want 1", len(model.traceNodes))
	}
	if model.traceNodes[0].Status != traceDone {
		t.Fatalf("assistant status = %s, want done", model.traceNodes[0].Status)
	}
}
```

Add imports to `tracepage_test.go`:

```go
import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/llm"
)
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestTraceRecordsUserMessage|TestTraceUpdatesSingleAssistantNodeDuringStreaming' -count=1
```

Expected: FAIL because user/assistant tracing helpers are not wired.

- [ ] **Step 3: Add assistant trace helpers**

In `internal/tui/trace.go`, add:

```go
func (m Model) recordUserTrace(visibleInput, llmInput string) Model {
	detail := llmInput
	if strings.TrimSpace(detail) == "" {
		detail = visibleInput
	}
	return m.appendTraceNode(newTraceNode(traceUser, traceDone, "user", visibleInput, detail))
}

func (m Model) ensureActiveTraceAssistant() Model {
	if m.activeTraceAssistantID != "" && m.findTraceByID(m.activeTraceAssistantID) >= 0 {
		return m
	}
	node := newTraceNode(traceAssistant, traceRunning, "assistant", "streaming...", "")
	m.activeTraceAssistantID = node.ID
	return m.appendTraceNode(node)
}

func (m Model) updateActiveTraceAssistant(content string) Model {
	m = m.ensureActiveTraceAssistant()
	content = strings.TrimSpace(content)
	if content == "" {
		return m
	}
	return m.updateTraceNode(m.activeTraceAssistantID, func(node *traceNode) {
		node.Status = traceRunning
		node.Detail = content
		node.Summary = firstTraceLine(content)
	})
}

func (m Model) finishActiveTraceAssistant(status traceStatus, content string) Model {
	if m.activeTraceAssistantID == "" {
		return m
	}
	id := m.activeTraceAssistantID
	m = m.updateTraceNode(id, func(node *traceNode) {
		node.Status = status
		node.EndedAt = time.Now()
		if strings.TrimSpace(content) != "" {
			node.Detail = strings.TrimSpace(content)
			node.Summary = firstTraceLine(content)
		}
	})
	m.activeTraceAssistantID = ""
	return m
}

func (m Model) findTraceByID(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].ID == id {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Wire user submit tracing**

In `startSubmittedMessage`, after each `m.messages = append(m.messages, chatMsg{role: "user", content: visibleInput})`, add:

```go
m = m.recordUserTrace(visibleInput, llmInput)
```

There are two branches: provider nil and provider configured. Add it to both.

- [ ] **Step 5: Wire streaming trace updates**

In `Update` under `llm.TextDeltaEvent`, after `m.streamBuf += e.Delta`, add:

```go
m = m.updateActiveTraceAssistant(m.streamBuf)
```

In the `llm.ToolCallEvent` branch, before clearing `m.streamBuf`, capture content and mark the current assistant node done:

```go
assistantContent := m.streamBuf
if m.streamBuf != "" {
	...
	m = m.finishActiveTraceAssistant(traceDone, assistantContent)
	m.streamBuf = ""
}
```

In `appendAssistantStreamContent`, after appending assistant content and before clearing `m.streamBuf`, add:

```go
*m = m.finishActiveTraceAssistant(traceDone, content)
```

In `finishStream`, before clearing state, add:

```go
if cancel {
	*m = m.finishActiveTraceAssistant(traceBlocked, m.streamBuf)
}
```

In `llm.ErrorEvent`, before each `m.finishStream(false)` call, add:

```go
m = m.finishActiveTraceAssistant(traceFailed, m.streamBuf)
```

This must run before `finishStream(false)` because `finishStream` clears the
active stream buffers.

- [ ] **Step 6: Run tests and verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestTraceRecordsUserMessage|TestTraceUpdatesSingleAssistantNodeDuringStreaming|TestTracePageRendersArrowTimelineMarkers' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/trace.go internal/tui/tracepage_test.go
git commit -m "feat(tui): record user and assistant trace nodes"
```

## Task 4: Tool and Subagent Trace Recording

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/trace.go`
- Modify: `internal/tui/tracepage.go`
- Test: `internal/tui/tracepage_test.go`

- [ ] **Step 1: Write failing tests for tool result and subagent trace updates**

Append to `internal/tui/tracepage_test.go`:

```go
func TestTraceRecordsToolCallAndResult(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.streaming = true
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)

	call := llm.ToolCallEvent{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)}
	next, _ := model.Update(streamEventMsg{streamID: 1, Event: call})
	model = next.(Model)

	if idx := model.findTraceByToolCallID("tc1"); idx < 0 {
		t.Fatalf("tool call trace not found: %#v", model.traceNodes)
	}

	next, _ = model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)},
		Results:  []nodeToolResult{{Node: "node-01", Output: "up", Success: true}, {Node: "node-02", Output: "down", Success: false}},
	})
	model = next.(Model)

	if len(model.traceNodes) < 2 {
		t.Fatalf("trace nodes = %d, want at least 2", len(model.traceNodes))
	}
	view := model.renderTracePage()
	for _, want := range []string{"▶", "✗", "2 nodes · 1 ok · 1 failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trace page missing %q:\n%s", want, view)
		}
	}
}

func TestTraceRecordsSubagentRunEvents(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	req := model.newSubagentRequest(subagent.RoleInvestigator, "check disk", []string{"node-01"})
	model.addSubagentRun(req)

	if idx := model.findTraceBySubagentID(req.ID); idx < 0 {
		t.Fatalf("subagent trace not found after addSubagentRun: %#v", model.traceNodes)
	}

	events := map[string][]subagent.Event{
		req.ID: {
			{Kind: subagent.EventTurnStart, Turn: 1},
			{Kind: subagent.EventToolCall, Turn: 1, Tool: "k8s_pods"},
		},
	}
	model.applySubagentEventsFromMessage(events)

	idx := model.findTraceBySubagentID(req.ID)
	if idx < 0 {
		t.Fatal("subagent trace disappeared")
	}
	if !strings.Contains(model.traceNodes[idx].Summary, "k8s_pods") {
		t.Fatalf("subagent trace summary = %q, want tool name", model.traceNodes[idx].Summary)
	}
}
```

Add import:

```go
"github.com/pockyHM/conan/internal/subagent"
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestTraceRecordsToolCallAndResult|TestTraceRecordsSubagentRunEvents' -count=1
```

Expected: FAIL because tool/subagent trace hooks are not implemented.

- [ ] **Step 3: Add tool trace helpers**

In `internal/tui/trace.go`, add:

```go
func (m Model) recordToolCallTrace(call llm.ToolCall) Model {
	args := sanitizeToolArguments(call.Name, call.Arguments)
	node := newTraceNode(traceToolCall, traceRunning, call.Name, traceToolSummary(call.Name, string(args)), string(args))
	node.ToolCallID = call.ID
	node.ToolName = call.Name
	return m.appendTraceNode(node)
}

func (m Model) markToolCallTrace(callID string, status traceStatus, detail string) Model {
	idx := m.findTraceByToolCallID(callID)
	if idx < 0 {
		return m
	}
	id := m.traceNodes[idx].ID
	return m.updateTraceNode(id, func(node *traceNode) {
		node.Status = status
		if status == traceDone || status == traceFailed || status == traceBlocked {
			node.EndedAt = time.Now()
		}
		if strings.TrimSpace(detail) != "" {
			node.Detail = strings.TrimSpace(node.Detail + "\n\n" + detail)
		}
	})
}

func (m Model) recordToolResultTrace(call llm.ToolCall, results []nodeToolResult) Model {
	success := true
	var lines []string
	for _, result := range results {
		if !result.Success {
			success = false
		}
		prefix := result.Node
		if prefix == "" {
			prefix = "local"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", prefix, result.Output))
	}
	status := traceDone
	if !success {
		status = traceFailed
	}
	m = m.markToolCallTrace(call.ID, status, strings.Join(lines, "\n"))
	node := newTraceNode(traceToolResult, status, "tool result", traceToolResultSummary(results), strings.Join(lines, "\n"))
	node.ToolCallID = call.ID
	node.ToolName = call.Name
	return m.appendTraceNode(node)
}
```

Add `github.com/pockyHM/conan/internal/llm` to `trace.go` imports.

- [ ] **Step 4: Wire tool call/result tracing**

In `Update` under `llm.ToolCallEvent`, after constructing `call := llm.ToolCall{...}`, add:

```go
m = m.recordToolCallTrace(call)
```

In `riskAssessmentMsg`:

- For `RiskDeny`, before `completeToolAndResume`, add:

```go
m = m.markToolCallTrace(msg.call.ID, traceBlocked, msg.assessment.Reason)
```

- For `RiskConfirm` and the explicit-confirm branch, add:

```go
m = m.markToolCallTrace(msg.call.ID, tracePending, msg.assessment.Reason)
```

In `multiToolResultMsg`, after `aggregatedOutput := strings.Join(outputParts, "\n")`, add:

```go
m = m.recordToolResultTrace(msg.Call, msg.Results)
```

- [ ] **Step 5: Add subagent trace helpers**

In `internal/tui/trace.go`, add:

```go
func (m Model) recordSubagentTrace(run subagentRunView) Model {
	summary := fmt.Sprintf("%s", run.Role)
	if len(run.Nodes) > 0 {
		summary = fmt.Sprintf("%s[%s]", run.Role, strings.Join(run.Nodes, ","))
	}
	if strings.TrimSpace(run.Task) != "" {
		summary += " · " + firstTraceLine(run.Task)
	}
	node := newTraceNode(traceSubagent, traceRunning, "subagent", summary, run.Prompt)
	node.SubagentID = run.ID
	return m.appendTraceNode(node)
}

func (m Model) updateSubagentTrace(run subagentRunView) Model {
	idx := m.findTraceBySubagentID(run.ID)
	if idx < 0 {
		return m.recordSubagentTrace(run)
	}
	id := m.traceNodes[idx].ID
	return m.updateTraceNode(id, func(node *traceNode) {
		node.Status = traceStatusFromSubagentStatus(run.Status)
		node.Summary = subagentTraceSummary(run)
		node.Detail = subagentTraceDetail(run)
		if node.Status == traceDone || node.Status == traceFailed || node.Status == traceBlocked {
			node.EndedAt = time.Now()
		}
	})
}

func traceStatusFromSubagentStatus(status string) traceStatus {
	switch status {
	case "completed":
		return traceDone
	case "failed":
		return traceFailed
	case "cancelled":
		return traceBlocked
	default:
		return traceRunning
	}
}

func subagentTraceSummary(run subagentRunView) string {
	if len(run.Events) > 0 {
		for i := len(run.Events) - 1; i >= 0; i-- {
			ev := run.Events[i]
			if ev.Tool != "" {
				return fmt.Sprintf("%s · turn %d · %s", run.Role, ev.Turn, ev.Tool)
			}
			if ev.Turn > 0 {
				return fmt.Sprintf("%s · turn %d", run.Role, ev.Turn)
			}
		}
	}
	if strings.TrimSpace(run.Summary) != "" {
		return firstTraceLine(run.Summary)
	}
	return firstTraceLine(run.Task)
}

func subagentTraceDetail(run subagentRunView) string {
	var lines []string
	if run.Prompt != "" {
		lines = append(lines, run.Prompt)
	}
	for _, ev := range run.Events {
		line := fmt.Sprintf("%s turn=%d elapsed=%s", ev.Kind, ev.Turn, ev.Elapsed)
		if ev.Tool != "" {
			line += " tool=" + ev.Tool
		}
		if ev.Content != "" {
			line += " content=" + firstTraceLine(ev.Content)
		}
		if ev.Out != "" {
			line += " output=" + firstTraceLine(ev.Out)
		}
		lines = append(lines, line)
	}
	if run.Summary != "" {
		lines = append(lines, "summary: "+run.Summary)
	}
	if run.Err != "" {
		lines = append(lines, "error: "+run.Err)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 6: Wire subagent tracing**

Change `addSubagentRun` in `internal/tui/model.go` after appending to `m.subagentRuns`:

```go
mAsValue := Model(*m)
mAsValue = mAsValue.recordSubagentTrace(m.subagentRuns[len(m.subagentRuns)-1])
m.traceNodes = mAsValue.traceNodes
m.traceCursor = mAsValue.traceCursor
```

In `updateSubagentRunResult`, after mutating the matching run and before return:

```go
mAsValue := Model(*m)
mAsValue = mAsValue.updateSubagentTrace(m.subagentRuns[i])
m.traceNodes = mAsValue.traceNodes
m.traceCursor = mAsValue.traceCursor
```

In `updateSubagentRunResultsFromToolOutput`, after setting each receiving run status/summary/err:

```go
mAsValue := Model(*m)
mAsValue = mAsValue.updateSubagentTrace(m.subagentRuns[i])
m.traceNodes = mAsValue.traceNodes
m.traceCursor = mAsValue.traceCursor
```

In `applySubagentEventsFromMessage`, after assigning events:

```go
mAsValue := Model(*m)
mAsValue = mAsValue.updateSubagentTrace(m.subagentRuns[i])
m.traceNodes = mAsValue.traceNodes
m.traceCursor = mAsValue.traceCursor
```

- [ ] **Step 7: Run tests and verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestTraceRecordsToolCallAndResult|TestTraceRecordsSubagentRunEvents|TestTracePageRendersArrowTimelineMarkers' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/trace.go internal/tui/tracepage.go internal/tui/tracepage_test.go
git commit -m "feat(tui): record tool and subagent trace nodes"
```

## Task 5: Clear, Resume, Polish, and Verification

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/trace.go`
- Modify: `internal/tui/tracepage.go`
- Test: `internal/tui/tracepage_test.go`
- Test: existing `internal/tui/model_test.go` if resume helper coverage fits better there

- [ ] **Step 1: Write failing tests for clear and resume rebuild**

Append to `internal/tui/tracepage_test.go`:

```go
func TestTraceClearClearsTraceNodes(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", "hello", "hello"))

	model, _ = model.applyCommand(SlashCommand{Kind: CommandClear})

	if len(model.traceNodes) != 0 {
		t.Fatalf("trace nodes after clear = %d, want 0", len(model.traceNodes))
	}
}

func TestTraceRebuildsFromConversationMessages(t *testing.T) {
	conv := conversation.New("prod", []string{"node-01"}, "claude")
	conv.AddUser("check disk")
	conv.AddAssistant("I will inspect disk")
	conv.AddToolCall("tc1", "shell_run", `{"command":"df -h"}`)
	conv.AddToolResult("tc1", "Filesystem ok")
	model := NewModel(ModelConfig{Cluster: "prod", Model: "claude", ConfigHome: t.TempDir(), Conv: conv})

	model = model.rebuildTraceFromMessages(conv.Messages())

	if len(model.traceNodes) != 4 {
		t.Fatalf("trace nodes = %d, want 4", len(model.traceNodes))
	}
	kinds := []traceKind{traceUser, traceAssistant, traceToolCall, traceToolResult}
	for i, want := range kinds {
		if model.traceNodes[i].Kind != want {
			t.Fatalf("trace node %d kind = %s, want %s", i, model.traceNodes[i].Kind, want)
		}
	}
}
```

Add import:

```go
"github.com/pockyHM/conan/internal/conversation"
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestTraceClearClearsTraceNodes|TestTraceRebuildsFromConversationMessages' -count=1
```

Expected: FAIL because clear and resume wiring are incomplete.

- [ ] **Step 3: Wire clear**

In `applyCommand` under `CommandClear`, after clearing messages and conversation:

```go
m.traceNodes = nil
m.traceCursor = 0
m.traceDetailVisible = false
m.activeTraceAssistantID = ""
```

- [ ] **Step 4: Wire resume rebuild**

In `loadSession` or the message handler that assigns restored conversation to `m.conv`, after:

```go
m.conv = conversation.Restore(rec.ID, rec.Cluster, nodes, rec.Model, messages)
```

add:

```go
m = m.rebuildTraceFromMessages(messages)
```

If the assignment is inside a pointer method or a narrow scope that cannot use
the returned value directly, copy the fields explicitly:

```go
rebuilt := m.rebuildTraceFromMessages(messages)
m.traceNodes = rebuilt.traceNodes
m.traceCursor = rebuilt.traceCursor
m.traceDetailVisible = rebuilt.traceDetailVisible
m.activeTraceAssistantID = rebuilt.activeTraceAssistantID
```

- [ ] **Step 5: Polish trace details**

In `renderTraceDetailPage`, add optional related identifiers after status:

```go
if node.ToolCallID != "" {
	lines = append(lines, subagentPageLabelStyle.Render("Tool call ID: ")+node.ToolCallID)
}
if node.ToolName != "" {
	lines = append(lines, subagentPageLabelStyle.Render("Tool: ")+node.ToolName)
}
if node.SubagentID != "" {
	lines = append(lines, subagentPageLabelStyle.Render("Subagent ID: ")+node.SubagentID)
}
```

Add elapsed rendering:

```go
if !node.StartedAt.IsZero() {
	end := node.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	if elapsed := end.Sub(node.StartedAt); elapsed > 0 {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Elapsed: ", "耗时: "))+elapsed.Round(100*time.Millisecond).String())
	}
}
```

Update `tracepage.go` imports to include `time`.

- [ ] **Step 6: Run focused tests**

Run:

```bash
go test ./internal/tui -run 'TestTrace|TestParseSlashCommand' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run full TUI tests**

Run:

```bash
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 8: Run full repository tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/trace.go internal/tui/tracepage.go internal/tui/tracepage_test.go
git commit -m "feat(tui): complete trace command integration"
```

## Final Verification

- [ ] **Step 1: Inspect git history**

Run:

```bash
git log --oneline -5
```

Expected: recent commits include the trace command shell, timeline rendering, trace recording, and final integration commits.

- [ ] **Step 2: Inspect final diff**

Run:

```bash
git diff --stat HEAD~4..HEAD
```

Expected: changes are limited to `internal/tui` trace implementation/tests and this plan/spec documentation.

- [ ] **Step 3: Manual TUI smoke test**

Run:

```bash
go run ./cmd/conan --home "$(mktemp -d)"
```

In the TUI:

1. Enter `/trace`.
2. Verify the empty trace page opens.
3. Press `Esc`.
4. Send a normal message if a provider is configured, or use unit tests as the verification source if no provider is available.

Expected: `/trace` opens and closes without panic; if a provider is configured, live nodes appear as the model streams and tools run.
