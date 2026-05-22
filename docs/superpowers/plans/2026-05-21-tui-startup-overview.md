# TUI Startup Overview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the sparse initial TUI body with a compact overview of Conan, cluster, model, and node status.

**Architecture:** Keep the overview inside the existing `Model.renderBody()` empty-state branch. Add focused rendering helpers in `internal/tui/render.go` so the view logic remains readable and the overview can be tested through `Model.View()`.

**Tech Stack:** Go, Bubble Tea model tests, Lip Gloss styles.

---

### Task 1: Startup Overview Empty State

**Files:**
- Modify: `internal/tui/model_test.go`
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write the failing test**

Add `TestInitialModelViewRendersStartupOverview` in `internal/tui/model_test.go`:

```go
func TestInitialModelViewRendersStartupOverview(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
		{Name: "node-03", Host: "10.0.1.3", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", Nodes: nodes})
	model.selectedNodes = map[string]bool{"node-01": true, "node-03": true}

	view := model.View()

	for _, want := range []string{
		"██████╗ ██████╗ ███╗   ██╗ █████╗ ███╗   ██╗",
		"Cluster   production",
		"Model     claude-sonnet",
		"Nodes     2/3 selected, 2 online",
		"● node-01  10.0.1.1  Online   selected",
		"○ node-02  10.0.1.2  Offline  unselected",
		"● node-03  10.0.1.3  Online   selected",
		"Type a message or /help",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing startup overview part %q:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestInitialModelViewRendersStartupOverview -count=1`

Expected: FAIL because the current empty state only renders `No messages yet. Type a message or /help.`

- [ ] **Step 3: Implement startup overview rendering**

Add a `renderStartupOverview(cluster, model string, nodes []NodeInfo, selected map[string]bool) string` helper in `internal/tui/render.go`. It should build the wordmark, summary rows, up to five node rows, and the prompt.

Change the empty branch of `Model.renderBody()` in `internal/tui/model.go`:

```go
if body == "" {
	body = renderStartupOverview(m.cluster, m.model, m.nodes, m.selectedNodes)
}
```

- [ ] **Step 4: Run focused test**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestInitialModelViewRendersStartupOverview -count=1`

Expected: PASS.

- [ ] **Step 5: Run package tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -count=1`

Expected: PASS.
