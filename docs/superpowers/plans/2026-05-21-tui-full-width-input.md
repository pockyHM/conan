# TUI Full-Width Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI chat input box fill the terminal width while preserving current input behavior.

**Architecture:** Thread the Bubble Tea window width from `Model.View()` into the input renderer. Let `renderInputBox` keep its existing compact behavior when width is unavailable, and apply a fixed lipgloss width when width is known.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, existing `internal/tui` package tests.

---

### Task 1: Full-Width Input Box

**Files:**
- Modify: `internal/tui/model_test.go`
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write the failing test**

Update `TestInputRendersAsBox` in `internal/tui/model_test.go` so it sends a window size before rendering and expects a full-width border:

```go
func TestInputRendersAsBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)
	model.input = "hello"

	view := model.View()

	for _, want := range []string{"╭──────────────────────────────────────╮", "│ ❯ hello", "╰──────────────────────────────────────╯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing input box part %q:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run TestInputRendersAsBox -count=1`

Expected: FAIL because the input renderer still creates a content-sized border.

- [ ] **Step 3: Write minimal implementation**

Change `renderInputBox` in `internal/tui/render.go` to accept a width. If width is positive, set the content width to `width - 2` because Lip Gloss border width adds two columns.

```go
func renderInputBox(input string, width int) string {
	style := inputBoxStyle
	if width > 0 {
		style = style.Width(max(width-2, 1))
	}
	return style.Render(inputPromptStyle.Render("❯ ") + input)
}
```

Change `Model.View()` in `internal/tui/model.go`:

```go
footer := statusView + "\n" + renderInputBox(m.input, m.width)
```

- [ ] **Step 4: Run focused test**

Run: `go test ./internal/tui -run TestInputRendersAsBox -count=1`

Expected: PASS.

- [ ] **Step 5: Run package tests**

Run: `go test ./internal/tui -count=1`

Expected: PASS.
