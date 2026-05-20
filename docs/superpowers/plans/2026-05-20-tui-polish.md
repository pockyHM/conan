# Phase 3F: TUI Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the rudimentary TUI into a polished, Claude Code-inspired interface with slash command autocomplete, markdown rendering, and proper styling.

**Architecture:** Add glamour for markdown rendering of assistant messages. Build a custom autocomplete overlay for slash commands (no extra dependencies). Refactor View() to render messages with proper styling — user messages in distinct color, assistant with markdown, tool calls as compact panels. Add scroll offset tracking for long conversations.

**Tech Stack:** charmbracelet/glamour (markdown rendering), existing lipgloss + bubbletea

---

### Task 1: Add Glamour + Create Render Helpers

**Files:**
- Create: `internal/tui/render.go`

- [ ] **Step 1: Add glamour dependency**

```bash
go get github.com/charmbracelet/glamour@latest
```

- [ ] **Step 2: Create render.go**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	toolSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	toolFailure = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	streamingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	headerKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)

	headerValStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	headerSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

var mdRenderer *glamour.TermRenderer

func init() {
	var err error
	mdRenderer, err = glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		mdRenderer, _ = glamour.NewTermRenderer(glamour.WithWordWrap(80))
	}
}

func renderMarkdown(text string) string {
	if mdRenderer == nil {
		return text
	}
	rendered, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSpace(rendered)
}

func renderUserMsg(content string) string {
	return userStyle.Render("❯ ") + content
}

func renderAssistantMsg(content string) string {
	rendered := renderMarkdown(content)
	return assistantStyle.Render(rendered)
}

func renderToolHeader(name string, nodeCount int) string {
	if nodeCount > 1 {
		return toolStyle.Render(fmt.Sprintf("⏚ %s on %d node(s)", name, nodeCount))
	}
	return toolStyle.Render(fmt.Sprintf("⏚ %s", name))
}

func renderToolNode(node string, success bool, output string) string {
	icon := toolSuccess.Render("✓")
	if !success {
		icon = toolFailure.Render("✗")
	}
	if idx := strings.Index(output, "\n"); idx != -1 {
		output = output[:idx]
	}
	if len(output) > 60 {
		output = output[:57] + "..."
	}
	return fmt.Sprintf("  %s %s  %s", icon, toolStyle.Render(node), output)
}
```

Note: `fmt` needs to be in the imports.

- [ ] **Step 3: Run `go build ./internal/tui/` to verify compilation**

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/tui/render.go
git commit -m "feat: add glamour markdown renderer and styled message helpers"
```

---

### Task 2: Slash Command Autocomplete

**Files:**
- Create: `internal/tui/autocomplete.go`
- Create: `internal/tui/autocomplete_test.go`

- [ ] **Step 1: Create autocomplete.go**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type commandInfo struct {
	Name        string
	Description string
	ArgHint     string
}

var commandRegistry = []commandInfo{
	{Name: "help", Description: "Show help information"},
	{Name: "clear", Description: "Clear conversation"},
	{Name: "exit", Description: "Exit Conan"},
	{Name: "cluster", Description: "Switch/display cluster", ArgHint: "[name]"},
	{Name: "model", Description: "Switch/display model", ArgHint: "[name]"},
	{Name: "nodes", Description: "Open node selector"},
	{Name: "memory", Description: "View memory summary"},
	{Name: "resume", Description: "Resume session", ArgHint: "[id]"},
}

type autocomplete struct {
	visible  bool
	selected int
	prefix   string
}

func newAutocomplete() autocomplete {
	return autocomplete{}
}

func (a autocomplete) update(input string) autocomplete {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		a.visible = false
		return a
	}
	a.visible = true
	a.prefix = strings.TrimPrefix(input, "/")
	if a.selected >= len(a.filtered()) {
		a.selected = 0
	}
	return a
}

func (a autocomplete) filtered() []commandInfo {
	if a.prefix == "" {
		return commandRegistry
	}
	var result []commandInfo
	for _, cmd := range commandRegistry {
		if strings.HasPrefix(cmd.Name, a.prefix) {
			result = append(result, cmd)
		}
	}
	return result
}

func (a autocomplete) moveUp() autocomplete {
	if a.selected > 0 {
		a.selected--
	}
	return a
}

func (a autocomplete) moveDown() autocomplete {
	filtered := a.filtered()
	if a.selected < len(filtered)-1 {
		a.selected++
	}
	return a
}

func (a autocomplete) completion() string {
	filtered := a.filtered()
	if a.selected < len(filtered) {
		return "/" + filtered[a.selected].Name + " "
	}
	return ""
}

func (a autocomplete) View() string {
	if !a.visible {
		return ""
	}
	filtered := a.filtered()
	if len(filtered) == 0 {
		return ""
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var lines []string
	for i, cmd := range filtered {
		cursor := "  "
		nameStyle := normalStyle
		if i == a.selected {
			cursor = "▸ "
			nameStyle = selectedStyle
		}

		hint := ""
		if cmd.ArgHint != "" {
			hint = " " + cmd.ArgHint
		}
		line := fmt.Sprintf("%s%s%s  %s", cursor, nameStyle.Render("/"+cmd.Name+hint), "", descStyle.Render(cmd.Description))
		lines = append(lines, line)
	}

	panel := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(panel)
}
```

- [ ] **Step 2: Create autocomplete_test.go**

```go
package tui

import (
	"strings"
	"testing"
)

func TestAutocompleteShowsOnSlash(t *testing.T) {
	a := newAutocomplete().update("/")
	if !a.visible {
		t.Fatal("autocomplete should be visible after /")
	}
}

func TestAutocompleteHidesOnNonSlash(t *testing.T) {
	a := newAutocomplete().update("hello")
	if a.visible {
		t.Fatal("autocomplete should not be visible for non-slash input")
	}
}

func TestAutocompleteHidesAfterSpace(t *testing.T) {
	a := newAutocomplete().update("/help ")
	if a.visible {
		t.Fatal("autocomplete should hide after space")
	}
}

func TestAutocompleteFilters(t *testing.T) {
	a := newAutocomplete().update("/cl")
	filtered := a.filtered()
	if len(filtered) != 1 || filtered[0].Name != "cluster" {
		t.Fatalf("expected cluster, got %v", filtered)
	}
}

func TestAutocompleteNoMatch(t *testing.T) {
	a := newAutocomplete().update("/zzz")
	filtered := a.filtered()
	if len(filtered) != 0 {
		t.Fatalf("expected no matches, got %v", filtered)
	}
}

func TestAutocompleteNavigation(t *testing.T) {
	a := newAutocomplete().update("/")
	if a.selected != 0 {
		t.Fatal("should start at 0")
	}
	a = a.moveDown()
	if a.selected != 1 {
		t.Fatalf("after down: selected=%d, want 1", a.selected)
	}
	a = a.moveUp()
	if a.selected != 0 {
		t.Fatalf("after up: selected=%d, want 0", a.selected)
	}
}

func TestAutocompleteCompletion(t *testing.T) {
	a := newAutocomplete().update("/")
	a = a.moveDown() // select second command (clear)
	comp := a.completion()
	if !strings.HasPrefix(comp, "/") {
		t.Fatalf("completion = %q, should start with /", comp)
	}
}

func TestAutocompleteRenders(t *testing.T) {
	a := newAutocomplete().update("/")
	view := a.View()
	if view == "" {
		t.Fatal("autocomplete view should not be empty")
	}
	if !strings.Contains(view, "/help") {
		t.Fatalf("autocomplete view should contain /help:\n%s", view)
	}
}

func TestAutocompleteEmptyForNoMatch(t *testing.T) {
	a := newAutocomplete().update("/zzz")
	view := a.View()
	if view != "" {
		t.Fatalf("autocomplete should be empty for no match, got:\n%s", view)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/tui/ -v -run TestAutocomplete`

- [ ] **Step 4: Commit**

```bash
git add internal/tui/autocomplete.go internal/tui/autocomplete_test.go
git commit -m "feat: add slash command autocomplete with filtering and navigation"
```

---

### Task 3: Refactor View() and handleKey() for Polished UI

**Files:**
- Modify: `internal/tui/model.go`

This is the main integration task. Changes:

1. Add `autocomplete` and `scrollOffset` to Model
2. In `handleKey()`: handle Up/Down for autocomplete, Tab for completion, PageUp/PageDown for scroll
3. Refactor `View()` to use new render helpers, show autocomplete panel

- [ ] **Step 1: Add autocomplete and scroll to Model**

Add to Model struct after `sessionList sessionList`:
```go
autocomplete autocomplete
scrollOffset int
```

- [ ] **Step 2: Update handleKey for autocomplete and scroll**

In `handleKey()`, in the main `switch key.Type` block (after `tea.KeyCtrlC`, `tea.KeyCtrlL`, etc.), add handling:

For the autocomplete:
- When not streaming and input starts with `/` and no space:
  - Up/Down → navigate autocomplete
  - Tab → accept completion
- PageUp/PageDown → scroll message area

```go
case tea.KeyUp:
	if m.autocomplete.visible {
		m.autocomplete = m.autocomplete.moveUp()
		return m, nil
	}
case tea.KeyDown:
	if m.autocomplete.visible {
		m.autocomplete = m.autocomplete.moveDown()
		return m, nil
	}
case tea.KeyTab:
	if m.autocomplete.visible {
		comp := m.autocomplete.completion()
		if comp != "" {
			m.input = comp
			m.autocomplete.visible = false
		}
		return m, nil
	}
case tea.KeyPgUp:
	if m.scrollOffset < len(m.messages)-1 {
		m.scrollOffset++
	}
	return m, nil
case tea.KeyPgDown:
	if m.scrollOffset > 0 {
		m.scrollOffset--
	}
	return m, nil
```

Also, on every rune input and backspace, update autocomplete:
```go
case tea.KeyRunes:
	m.input += string(key.Runes)
	m.autocomplete = m.autocomplete.update(m.input)
	return m, nil
case tea.KeyBackspace:
	if len(m.input) > 0 {
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		m.autocomplete = m.autocomplete.update(m.input)
	}
	return m, nil
```

On Enter (submit), hide autocomplete:
```go
case tea.KeyEnter:
	m.autocomplete.visible = false
	return m.submit()
```

- [ ] **Step 3: Refactor View() to use new render helpers**

Replace the message rendering section with:
```go
var bodyParts []string
for _, msg := range m.messages {
	switch msg.role {
	case "user":
		bodyParts = append(bodyParts, renderUserMsg(msg.content))
	case "assistant":
		bodyParts = append(bodyParts, renderAssistantMsg(msg.content))
	case "tool":
		if len(msg.nodeResults) > 1 {
			h := renderToolHeader(msg.toolName, len(msg.nodeResults))
			if msg.toolOutput != "" {
				var lines []string
				for i, r := range msg.nodeResults {
					lines = append(lines, renderToolNode(r.Node, r.Success, r.Output))
					_ = i
				}
				h += "\n" + strings.Join(lines, "\n")
			} else {
				h += " (running...)"
			}
			bodyParts = append(bodyParts, h)
		} else {
			h := renderToolHeader(msg.toolName, 0)
			if len(msg.nodeResults) == 1 {
				h = toolStyle.Render(fmt.Sprintf("⏚ %s on %s", msg.toolName, msg.nodeResults[0].Node))
			}
			if msg.toolOutput != "" {
				h += "\n" + msg.toolOutput
			} else {
				h += " (running...)"
			}
			bodyParts = append(bodyParts, h)
		}
	}
}

if m.streaming && m.streamBuf != "" {
	bodyParts = append(bodyParts, renderAssistantMsg(m.streamBuf+"▌"))
}
```

Update the header:
```go
header := headerKeyStyle.Render("Conan") + headerSepStyle.Render(" │ ") +
	headerValStyle.Render(m.cluster) + headerSepStyle.Render(" │ ") +
	headerValStyle.Render(m.model) + headerSepStyle.Render(" │ ") +
	fmt.Sprintf("%d/%d nodes", len(m.selectedNodes), len(m.nodes))
```

Update the footer (input + status):
```go
inputLine := inputPromptStyle.Render("❯ ") + m.input
statusLine := statusStyle.Render(m.status)
footer := statusLine + "\n" + inputLine

// Show autocomplete overlay
acView := m.autocomplete.View()
if acView != "" {
	footer = acView + "\n" + footer
}

return header + "\n" + body + "\n\n" + footer
```

- [ ] **Step 4: Fix broken tests**

The View() output format changes will break some tests. Update test assertions to match new format. Key changes:
- "You:" → "❯" in user messages
- "Conan:" → rendered markdown (no "Conan:" prefix in rendered output)
- Header format changes

For test assertions, use `strings.Contains` with substrings that are format-independent where possible.

- [ ] **Step 5: Run all tests and fix**

Run: `go test ./... -v`

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: polish TUI with markdown rendering, autocomplete, and styled messages"
```

---

### Task 4: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add Phase 3F section**

```markdown
### Phase 3F: TUI Polish — DONE

Markdown rendering via glamour, slash command autocomplete, styled messages, scroll support.

- `internal/tui/render.go` — Glamour markdown renderer and styled message helpers
- `internal/tui/autocomplete.go` — Slash command autocomplete with filtering and navigation
- `internal/tui/model.go` — Refactored View() with polished styling, autocomplete integration, scroll offset

Plan: `docs/superpowers/plans/2026-05-20-tui-polish.md`
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update progress — Phase 3F TUI Polish complete"
```
