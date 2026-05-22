# TUI Autocomplete Below Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render slash-command autocomplete below the TUI input box and align its width with the input box.

**Architecture:** Change `Model.View()` footer composition so autocomplete is appended after the input box. Update `autocomplete.View` to accept a width and apply the same bordered-width calculation used by the input box.

**Tech Stack:** Go, Bubble Tea model tests, Lip Gloss.

---

### Task 1: Autocomplete Below Input

**Files:**
- Modify: `internal/tui/model_test.go`
- Modify: `internal/tui/autocomplete.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write the failing test**

Add `TestAutocompleteRendersBelowInputBox` in `internal/tui/model_test.go`:

```go
func TestAutocompleteRendersBelowInputBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = next.(Model)
	for _, r := range "/cl" {
		next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}

	view := model.View()
	inputIndex := strings.Index(view, "│ ❯ /cl")
	acIndex := strings.Index(view, "▸ /clear")
	if inputIndex == -1 || acIndex == -1 {
		t.Fatalf("view missing input or autocomplete:\n%s", view)
	}
	if inputIndex > acIndex {
		t.Fatalf("autocomplete rendered above input:\n%s", view)
	}
	if !strings.Contains(view, "╭──────────────────────────────────────╮") {
		t.Fatalf("view missing full-width autocomplete border:\n%s", view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestAutocompleteRendersBelowInputBox -count=1`

Expected: FAIL because autocomplete currently renders above the input box.

- [ ] **Step 3: Implement footer order and autocomplete width**

Change `autocomplete.View()` in `internal/tui/autocomplete.go` to `View(width int)` and apply `style.Width(max(width-2, 1))` when width is positive.

Change `Model.View()` in `internal/tui/model.go` to call `m.ac.View(m.width)` and append autocomplete after the input box:

```go
acView := m.ac.View(m.width)
footer := statusView + "\n" + renderInputBox(m.input, m.width)
if m.mode == modeConfirm {
	footer = m.renderConfirmFooter()
}
if acView != "" && m.mode != modeConfirm {
	footer = footer + "\n" + acView
}
```

- [ ] **Step 4: Run focused test**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestAutocompleteRendersBelowInputBox -count=1`

Expected: PASS.

- [ ] **Step 5: Run package tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -count=1`

Expected: PASS.
