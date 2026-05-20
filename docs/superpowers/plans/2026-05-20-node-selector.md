# Phase 3C: Node Selector & Multi-node Dispatch

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add interactive node selection UI and concurrent multi-node tool dispatch to the TUI.

**Architecture:** Add mode switching to the TUI model — `/nodes` switches to an interactive multi-select overlay. Track selected nodes in the model. When tool calls arrive during streaming, dispatch concurrently to all selected nodes and aggregate results in a tree-style visualization.

**Tech Stack:** Go, Bubble Tea, lipgloss

---

## File Structure

- `internal/tui/nodeselector.go` — Node selector overlay component (new)
- `internal/tui/nodeselector_test.go` — Selector unit tests (new)
- `internal/tui/model.go` — Mode switching, node state, multi-node dispatch, visualization (modify)
- `internal/tui/model_test.go` — Integration tests for node selection and dispatch (modify)
- `cmd/conan/main.go` — Build and pass node info to TUI (modify)

---

### Task 1: NodeInfo types and Model state

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `cmd/conan/main.go`

Adds `NodeInfo` type, selected node tracking, and wires node data from config into the TUI model. Replaces single-node `toolResultMsg` with multi-node `multiToolResultMsg`. Updates system prompt to include target nodes.

- [ ] **Step 1: Add NodeInfo type and update Model structs in `internal/tui/model.go`**

Add after the `chatMsg` struct:

```go
type NodeInfo struct {
	Name   string
	Host   string
	Online bool
}
```

Update `ModelConfig` — add `Nodes` field:

```go
type ModelConfig struct {
	Cluster  string
	Model    string
	Provider llm.Provider
	Conv     *conversation.Conversation
	Clients  map[string]*mcp.Client
	Tools    []llm.ToolDef
	Nodes    []NodeInfo
}
```

Update `Model` — add `nodes` and `selectedNodes` fields:

```go
type Model struct {
	cluster   string
	model     string
	provider  llm.Provider
	conv      *conversation.Conversation
	clients   map[string]*mcp.Client
	tools     []llm.ToolDef

	nodes         []NodeInfo
	selectedNodes map[string]bool

	input     string
	messages  []chatMsg
	status    string
	streaming bool
	streamBuf string
	streamCh  <-chan llm.ChatEvent

	width  int
	height int
}
```

Update `chatMsg` — add `nodeResults` field:

```go
type chatMsg struct {
	role        string
	content     string
	toolName    string
	toolInput   string
	toolOutput  string
	nodeResults []nodeToolResult
}
```

Add multi-node result types (replacing `toolResultMsg`):

```go
type nodeToolResult struct {
	Node    string
	Output  string
	Success bool
}

type multiToolResultMsg struct {
	Call    llm.ToolCall
	Results []nodeToolResult
}
```

Remove the old `toolResultMsg` struct entirely.

Update `NewModel` — initialize node state:

```go
func NewModel(cfg ModelConfig) Model {
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	selectedNodes := make(map[string]bool)
	for _, node := range cfg.Nodes {
		selectedNodes[node.Name] = true
	}
	return Model{
		cluster:       cfg.Cluster,
		model:         cfg.Model,
		provider:      cfg.Provider,
		conv:          cfg.Conv,
		clients:       cfg.Clients,
		tools:         cfg.Tools,
		nodes:         cfg.Nodes,
		selectedNodes: selectedNodes,
		status:        "Ready",
	}
}
```

Update header in `View()` to show node count:

```go
header := lipgloss.NewStyle().Bold(true).Render(
	fmt.Sprintf("Conan | Cluster: %s | Model: %s | Nodes: %d/%d", m.cluster, m.model, len(m.selectedNodes), len(m.nodes)),
)
```

Update `buildSystemPrompt` to include selected nodes (add `"sort"` to imports):

```go
func buildSystemPrompt(cluster string, selectedNodes []string) string {
	nodes := strings.Join(selectedNodes, ", ")
	return fmt.Sprintf("You are Conan, an AI operations assistant. Cluster: %s. Target nodes: %s. Help the user manage their infrastructure.", cluster, nodes)
}
```

Update `startStream` to pass selected nodes:

```go
func (m Model) startStream() tea.Cmd {
	if m.provider == nil {
		return nil
	}
	provider := m.provider
	var selected []string
	for name := range m.selectedNodes {
		selected = append(selected, name)
	}
	sort.Strings(selected)
	req := &llm.ChatRequest{
		SystemPrompt: buildSystemPrompt(m.cluster, selected),
		Messages:     m.conv.Messages(),
		Tools:        m.tools,
	}
	return func() tea.Msg {
		ch, err := provider.ChatStream(context.Background(), req)
		return streamReadyMsg{ch: ch, err: err}
	}
}
```

- [ ] **Step 2: Update `cmd/conan/main.go` to pass node info**

In `tuiCmd`, build node info from cluster config. Add this after the `agentTools` block (before creating `conv`):

```go
var nodeInfos []tui.NodeInfo
if cluster != nil {
	for _, node := range cluster.Nodes {
		nodeInfos = append(nodeInfos, tui.NodeInfo{
			Name: node.Name,
			Host: node.Agent.Host,
		})
	}
}
```

Then add `Nodes` to the `ModelConfig`:

```go
model := tui.NewModel(tui.ModelConfig{
	Cluster:  selectedCluster,
	Model:    modelName,
	Provider: provider,
	Conv:     conv,
	Clients:  clients,
	Tools:    agentTools,
	Nodes:    nodeInfos,
})
```

Note: `cluster` variable needs to be accessible. Currently it's scoped inside the `if selectedCluster != ""` block. Lift the `cluster` variable declaration before the if block:

```go
var clients map[string]*mcp.Client
var agentTools []llm.ToolDef
var cluster *cfgloader.Cluster
if selectedCluster != "" {
	var err error
	cluster, err = loader.LoadCluster(selectedCluster)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load cluster %s: %v\n", selectedCluster, err)
	} else {
		// ... existing client/tool setup ...
	}
}
```

- [ ] **Step 3: Update existing tests in `internal/tui/model_test.go`**

Replace `toolResultMsg` usage in `TestToolResultMessage`:

```go
func TestToolResultMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
	}
	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("view missing tool name:\n%s", view)
	}
}
```

Remove the `"github.com/pockyHM/conan/pkg/mcpproto"` import if no longer used.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/... ./cmd/conan/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go cmd/conan/main.go
git commit -m "feat: add NodeInfo type and multi-node result tracking to TUI model"
```

---

### Task 2: Interactive node selector component

**Files:**
- Create: `internal/tui/nodeselector.go`
- Create: `internal/tui/nodeselector_test.go`

A standalone Bubble Tea sub-component for multi-select node picking. Delegates key handling, renders a styled list with online/offline indicators.

- [ ] **Step 1: Create `internal/tui/nodeselector.go`**

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type nodeSelector struct {
	nodes   []NodeInfo
	cursor  int
	checked map[string]bool
}

func newNodeSelector(nodes []NodeInfo, selected map[string]bool) nodeSelector {
	checked := make(map[string]bool)
	for name, ok := range selected {
		if ok {
			checked[name] = true
		}
	}
	return nodeSelector{
		nodes:   nodes,
		checked: checked,
	}
}

func (s nodeSelector) Update(msg tea.Msg) (nodeSelector, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if s.cursor > 0 {
				s.cursor--
			}
		case tea.KeyDown:
			if s.cursor < len(s.nodes)-1 {
				s.cursor++
			}
		case tea.KeySpace:
			if s.cursor < len(s.nodes) {
				node := s.nodes[s.cursor]
				if node.Online {
					if s.checked[node.Name] {
						delete(s.checked, node.Name)
					} else {
						s.checked[node.Name] = true
					}
				}
			}
		}
	}
	return s, nil
}

func (s nodeSelector) Selected() map[string]bool {
	return s.checked
}

func (s nodeSelector) SetNodes(nodes []NodeInfo) nodeSelector {
	s.nodes = nodes
	return s
}

func (s nodeSelector) View() string {
	if len(s.nodes) == 0 {
		return "No nodes configured for this cluster."
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Select Target Nodes"))
	b.WriteString("\n")

	for i, node := range s.nodes {
		cursor := " "
		if i == s.cursor {
			cursor = ">"
		}
		checked := "○"
		if s.checked[node.Name] {
			checked = "●"
		}

		status := "● Online"
		style := lipgloss.NewStyle()
		if !node.Online {
			status = "○ Offline"
			style = style.Foreground(lipgloss.Color("240"))
		}

		line := fmt.Sprintf(" %s %s  %-20s  %-15s  %s", cursor, checked, node.Name, node.Host, style.Render(status))
		b.WriteString(line)
		b.WriteString("\n")
	}

	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", 55))
	b.WriteString(sep)
	b.WriteString("\n")
	b.WriteString(" ↑↓ Move  Space Select  Enter Confirm  Esc Cancel")

	return b.String()
}
```

- [ ] **Step 2: Create `internal/tui/nodeselector_test.go`**

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNodeSelectorNavigation(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
		{Name: "node-03", Host: "10.0.1.3", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{"node-01": true})

	if s.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.cursor != 0 {
		t.Fatalf("cursor at top = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 2 {
		t.Fatalf("cursor at bottom = %d, want 2", s.cursor)
	}
}

func TestNodeSelectorToggle(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{})

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !s.checked["node-01"] {
		t.Fatal("node-01 should be checked after space")
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if s.checked["node-01"] {
		t.Fatal("node-01 should be unchecked after second space")
	}
}

func TestNodeSelectorOfflineUnselectable(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	s := newNodeSelector(nodes, map[string]bool{})

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if s.checked["node-02"] {
		t.Fatal("offline node should not be selectable")
	}
}

func TestNodeSelectorView(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	s := newNodeSelector(nodes, map[string]bool{})
	view := s.View()

	for _, want := range []string{"node-01", "10.0.1.1", "node-02", "10.0.1.2", "Online", "Offline", "Select Target Nodes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestNodeSelectorEmptyNodes(t *testing.T) {
	s := newNodeSelector(nil, nil)
	view := s.View()
	if !strings.Contains(view, "No nodes configured") {
		t.Fatalf("empty selector should show message:\n%s", view)
	}
}

func TestNodeSelectorSelected(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{"node-01": true, "node-02": true})

	selected := s.Selected()
	if len(selected) != 2 {
		t.Fatalf("len(selected) = %d, want 2", len(selected))
	}
	if !selected["node-01"] || !selected["node-02"] {
		t.Fatal("both nodes should be selected")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/tui/... -run TestNodeSelector -v`
Expected: All 6 tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/nodeselector.go internal/tui/nodeselector_test.go
git commit -m "feat: add interactive node selector component"
```

---

### Task 3: Selector integration, mode switching, and node ping

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

Integrates the node selector into the main TUI model. Adds mode field, handles `/nodes` command to open selector, dispatches async pings, renders overlay. Enter confirms selection, Esc cancels.

- [ ] **Step 1: Add mode type, selector, and ping message to Model**

Add to `internal/tui/model.go`:

```go
type tuiMode int

const (
	modeChat       tuiMode = iota
	modeNodeSelect
)
```

Add `pingResultMsg` (alongside the other message types):

```go
type pingResultMsg struct {
	node   string
	online bool
}
```

Add fields to `Model`:

```go
type Model struct {
	// ... existing fields ...

	mode         tuiMode
	nodeSelector nodeSelector
	prevSelected map[string]bool
}
```

Add `"time"` to imports.

- [ ] **Step 2: Update `handleKey` to check mode first**

```go
func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeNodeSelect {
		return m.handleNodeSelectKey(key)
	}
	if m.streaming {
		if key.Type == tea.KeyCtrlC {
			m.streaming = false
			m.streamCh = nil
			m.status = "Interrupted"
			return m, nil
		}
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyCtrlL:
		m.messages = nil
		m.status = "Conversation cleared"
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyRunes:
		m.input += string(key.Runes)
		return m, nil
	default:
		return m, nil
	}
}
```

Add `handleNodeSelectKey`:

```go
func (m Model) handleNodeSelectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		m.selectedNodes = m.nodeSelector.Selected()
		m.mode = modeChat
		m.status = fmt.Sprintf("Selected %d node(s)", len(m.selectedNodes))
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.selectedNodes = m.prevSelected
		m.mode = modeChat
		m.status = "Node selection cancelled"
		return m, nil
	default:
		var cmd tea.Cmd
		m.nodeSelector, cmd = m.nodeSelector.Update(key)
		return m, cmd
	}
}
```

- [ ] **Step 3: Handle `pingResultMsg` in `Update()`**

Add case in the `Update()` switch (before the `tea.KeyMsg` case):

```go
case pingResultMsg:
	for i := range m.nodes {
		if m.nodes[i].Name == msg.node {
			m.nodes[i].Online = msg.online
			break
		}
	}
	if m.mode == modeNodeSelect {
		m.nodeSelector = m.nodeSelector.SetNodes(m.nodes)
	}
	return m, nil
```

Also update the `tea.WindowSizeMsg` handler to propagate to selector:

```go
case tea.WindowSizeMsg:
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
```

(No change needed here — the selector doesn't use width/height.)

- [ ] **Step 4: Change `applyCommand` to return `(Model, tea.Cmd)`**

Update signature:

```go
func (m Model) applyCommand(cmd SlashCommand) (Model, tea.Cmd) {
```

Update all return statements. Most cases return `(m, nil)`. The `CommandNodes` case returns the ping Cmd:

```go
func (m Model) applyCommand(cmd SlashCommand) (Model, tea.Cmd) {
	switch cmd.Kind {
	case CommandHelp:
		m.messages = append(m.messages, chatMsg{
			role:    "assistant",
			content: "Conan: /help /clear /exit /cluster [name] /model [name] /nodes /memory /resume /config",
		})
		m.status = "Help shown"
	case CommandClear:
		m.messages = nil
		if m.conv != nil {
			m.conv.Clear()
		}
		m.status = "Conversation cleared"
	case CommandExit:
		m.status = "Exit requested"
	case CommandCluster:
		if cmd.Arg != "" {
			m.cluster = cmd.Arg
			m.status = "Cluster switched to " + cmd.Arg
		} else {
			m.status = "Current cluster: " + m.cluster
		}
	case CommandModel:
		if cmd.Arg != "" {
			m.model = cmd.Arg
			m.status = "Model switched to " + cmd.Arg
		} else {
			m.status = "Current model: " + m.model
		}
	case CommandNodes:
		if len(m.nodes) == 0 {
			m.status = "No nodes configured"
			return m, nil
		}
		m.mode = modeNodeSelect
		m.prevSelected = m.selectedNodes
		m.nodeSelector = newNodeSelector(m.nodes, m.selectedNodes)
		m.status = "Checking node status..."
		return m, m.pingNodes()
	default:
		m.status = "Unknown command: /" + cmd.Arg
	}
	return m, nil
}
```

Update `submit()` to propagate the Cmd:

```go
func (m Model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	if input == "" {
		return m, nil
	}
	if cmd, ok := ParseSlashCommand(input); ok {
		var c tea.Cmd
		m, c = m.applyCommand(cmd)
		if cmd.Kind == CommandExit {
			return m, tea.Quit
		}
		return m, c
	}
	if m.provider == nil {
		m.messages = append(m.messages, chatMsg{role: "user", content: input})
		m.status = "No LLM provider configured"
		return m, nil
	}
	m.messages = append(m.messages, chatMsg{role: "user", content: input})
	if m.conv != nil {
		m.conv.AddUser(input)
	}
	m.streaming = true
	m.streamBuf = ""
	m.status = "Thinking..."
	return m, m.startStream()
}
```

- [ ] **Step 5: Add `pingNodes` method**

```go
func (m Model) pingNodes() tea.Cmd {
	clients := m.clients
	var cmds []tea.Cmd
	for name, client := range clients {
		c := client
		n := name
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.Ping(ctx)
			return pingResultMsg{node: n, online: err == nil}
		})
	}
	return tea.Batch(cmds...)
}
```

- [ ] **Step 6: Update `View()` for overlay mode**

```go
func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Conan | Cluster: %s | Model: %s | Nodes: %d/%d", m.cluster, m.model, len(m.selectedNodes), len(m.nodes)),
	)

	if m.mode == modeNodeSelect {
		return fmt.Sprintf("%s\n\n%s\n\n%s", header, m.nodeSelector.View(), m.status)
	}

	var bodyParts []string
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			bodyParts = append(bodyParts, "You: "+msg.content)
		case "assistant":
			bodyParts = append(bodyParts, "Conan: "+msg.content)
		case "tool":
			header := fmt.Sprintf("-> %s", msg.toolName)
			if msg.toolOutput != "" {
				header += "\n" + msg.toolOutput
			} else {
				header += " (running...)"
			}
			bodyParts = append(bodyParts, header)
		}
	}

	if m.streaming && m.streamBuf != "" {
		bodyParts = append(bodyParts, "Conan: "+m.streamBuf+"...")
	}

	body := strings.Join(bodyParts, "\n\n")
	if body == "" {
		body = "No messages yet. Type a message or /help."
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n> %s", header, body, m.status, m.input)
}
```

- [ ] **Step 7: Add integration tests**

Add to `internal/tui/model_test.go`:

```go
func TestNodesCommandOpensSelector(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeNodeSelect {
		t.Fatal("should be in node select mode after /nodes")
	}
	view := model.View()
	if !strings.Contains(view, "Select Target Nodes") {
		t.Fatalf("view should show node selector:\n%s", view)
	}
}

func TestNodeSelectConfirm(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})
	if len(model.selectedNodes) != 2 {
		t.Fatalf("expected 2 default selected, got %d", len(model.selectedNodes))
	}

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatal("should be back in chat mode after confirm")
	}
	if model.selectedNodes["node-02"] {
		t.Fatal("node-02 should be deselected")
	}
	if !model.selectedNodes["node-01"] {
		t.Fatal("node-01 should still be selected")
	}
}

func TestNodeSelectCancel(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatal("should be back in chat mode after cancel")
	}
	if !model.selectedNodes["node-01"] {
		t.Fatal("cancel should restore original selection")
	}
}

func TestPingResultUpdatesNodeStatus(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: false},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	next, _ := model.Update(pingResultMsg{node: "node-01", online: true})
	model = next.(Model)

	if !model.nodes[0].Online {
		t.Fatal("node-01 should be online after ping")
	}
	if model.nodes[1].Online {
		t.Fatal("node-02 should still be offline")
	}
}

func TestNodesNoNodesConfigured(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatal("should stay in chat mode with no nodes")
	}
	if !strings.Contains(model.status, "No nodes") {
		t.Fatalf("status = %q, want no nodes message", model.status)
	}
}
```

- [ ] **Step 8: Run all tests**

Run: `go test ./internal/tui/... -v`
Expected: All tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: integrate node selector with mode switching and async ping"
```

---

### Task 4: Multi-node tool dispatch

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

Rewrites `dispatchTool` to fan out to all selected nodes concurrently. Replaces the old `toolResultMsg` handler with `multiToolResultMsg` handler that aggregates results and updates the conversation.

- [ ] **Step 1: Rewrite `dispatchTool` for concurrent multi-node dispatch**

Replace the existing `dispatchTool` method. Add `"sync"` to imports:

```go
func (m Model) dispatchTool(call llm.ToolCall) tea.Cmd {
	clients := m.clients
	selected := m.selectedNodes
	return func() tea.Msg {
		if len(selected) == 0 {
			return multiToolResultMsg{
				Call: call,
				Results: []nodeToolResult{
					{Node: "-", Output: "No nodes selected. Use /nodes to select target nodes.", Success: false},
				},
			}
		}

		type result struct {
			node    string
			output  string
			success bool
		}

		var wg sync.WaitGroup
		ch := make(chan result, len(selected))

		for name := range selected {
			client, exists := clients[name]
			if !exists {
				ch <- result{node: name, output: "no client configured for node", success: false}
				continue
			}
			wg.Add(1)
			go func(n string, c *mcp.Client) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				toolResult, err := c.CallTool(ctx, call.Name, call.Arguments)
				if err != nil {
					ch <- result{node: n, output: err.Error(), success: false}
					return
				}
				var output string
				for _, block := range toolResult.Content {
					output += block.Text
				}
				ch <- result{node: n, output: output, success: true}
			}(name, client)
		}

		wg.Wait()
		close(ch)

		var results []nodeToolResult
		for r := range ch {
			results = append(results, nodeToolResult{Node: r.node, Output: r.output, Success: r.success})
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Node < results[j].Node
		})

		return multiToolResultMsg{Call: call, Results: results}
	}
}
```

- [ ] **Step 2: Replace `toolResultMsg` handler with `multiToolResultMsg` handler in `Update()`**

Remove the old `case toolResultMsg:` block. Add:

```go
case multiToolResultMsg:
	var outputParts []string
	for _, r := range msg.Results {
		if r.Success {
			outputParts = append(outputParts, fmt.Sprintf("[%s] %s", r.Node, r.Output))
		} else {
			outputParts = append(outputParts, fmt.Sprintf("[%s] ERROR: %s", r.Node, r.Output))
		}
	}
	aggregatedOutput := strings.Join(outputParts, "\n")

	found := false
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "tool" && m.messages[i].toolOutput == "" {
			m.messages[i].toolOutput = aggregatedOutput
			m.messages[i].nodeResults = msg.Results
			found = true
			break
		}
	}
	if !found {
		m.messages = append(m.messages, chatMsg{
			role:        "tool",
			toolName:    msg.Call.Name,
			toolInput:   string(msg.Call.Arguments),
			toolOutput:  aggregatedOutput,
			nodeResults: msg.Results,
		})
	}
	if m.conv != nil {
		m.conv.AddToolResult(msg.Call.ID, aggregatedOutput)
	}
	return m, m.startStream()
```

- [ ] **Step 3: Add dispatch tests**

Add to `internal/tui/model_test.go`:

```go
func TestMultiNodeDispatch(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Conv:    conv,
		Nodes:   nodes,
	})

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "load average: 0.52", Success: true},
		{Node: "node-02", Output: "load average: 0.31", Success: true},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("view missing tool name:\n%s", view)
	}
	if !strings.Contains(view, "node-01") {
		t.Fatalf("view missing node-01:\n%s", view)
	}
	if !strings.Contains(view, "node-02") {
		t.Fatalf("view missing node-02:\n%s", view)
	}
}

func TestMultiNodeDispatchWithFailure(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
		{Node: "node-02", Output: "Connection timeout", Success: false},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "node-01") {
		t.Fatalf("view missing node-01:\n%s", view)
	}
	if !strings.Contains(view, "node-02") {
		t.Fatalf("view missing node-02:\n%s", view)
	}
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/tui/... -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: concurrent multi-node tool dispatch with result aggregation"
```

---

### Task 5: Multi-node result visualization

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

Updates `View()` to render multi-node tool results in a tree format with per-node success/failure indicators. Single-node results (one entry in `nodeResults`) are shown with the node name. Updates the test to verify the tree rendering.

- [ ] **Step 1: Update tool message rendering in `View()`**

Replace the `"tool"` case in the body rendering loop:

```go
case "tool":
	if len(msg.nodeResults) > 1 {
		header := fmt.Sprintf("-> %s on %d node(s)", msg.toolName, len(msg.nodeResults))
		if msg.toolOutput != "" {
			var lines []string
			for i, r := range msg.nodeResults {
				prefix := "├──"
				if i == len(msg.nodeResults)-1 {
					prefix = "└──"
				}
				icon := "✓"
				if !r.Success {
					icon = "✗"
				}
				output := r.Output
				if idx := strings.Index(output, "\n"); idx != -1 {
					output = output[:idx]
				}
				if len(output) > 60 {
					output = output[:57] + "..."
				}
				lines = append(lines, fmt.Sprintf("%s %s %s  %s", prefix, r.Node, icon, output))
			}
			header += "\n" + strings.Join(lines, "\n")
		} else {
			header += " (running...)"
		}
		bodyParts = append(bodyParts, header)
	} else {
		header := fmt.Sprintf("-> %s", msg.toolName)
		if len(msg.nodeResults) == 1 {
			header = fmt.Sprintf("-> %s on %s", msg.toolName, msg.nodeResults[0].Node)
		}
		if msg.toolOutput != "" {
			header += "\n" + msg.toolOutput
		} else {
			header += " (running...)"
		}
		bodyParts = append(bodyParts, header)
	}
```

- [ ] **Step 2: Update dispatch test to verify tree rendering**

Update `TestMultiNodeDispatch`:

```go
func TestMultiNodeDispatch(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Conv:    conv,
		Nodes:   nodes,
	})

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "load average: 0.52", Success: true},
		{Node: "node-02", Output: "load average: 0.31", Success: true},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run on 2 node(s)") {
		t.Fatalf("view missing multi-node header:\n%s", view)
	}
	if !strings.Contains(view, "├── node-01 ✓") {
		t.Fatalf("view missing first node tree line:\n%s", view)
	}
	if !strings.Contains(view, "└── node-02 ✓") {
		t.Fatalf("view missing last node tree line:\n%s", view)
	}
}
```

Update `TestMultiNodeDispatchWithFailure`:

```go
func TestMultiNodeDispatchWithFailure(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
		{Node: "node-02", Output: "Connection timeout", Success: false},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "node-01 ✓") {
		t.Fatalf("view missing success node:\n%s", view)
	}
	if !strings.Contains(view, "node-02 ✗") {
		t.Fatalf("view missing failure node:\n%s", view)
	}
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/tui/... -v`
Expected: All tests PASS

- [ ] **Step 4: Run full build**

Run: `make build`
Expected: Both binaries build successfully

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: tree-style visualization for multi-node tool results"
```

---

### Task 6: Update CLAUDE.md and verify

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update implementation progress**

In `CLAUDE.md`, mark Phase 3C as DONE and update the description:

```markdown
### Phase 3C: Node Selector & Multi-node — DONE

Interactive `/nodes` multi-select, concurrent multi-node tool dispatch, tree-style result visualization.

- `internal/tui/nodeselector.go` — Interactive multi-select node overlay component
- `internal/tui/model.go` — Mode switching, node state tracking, async ping, multi-node dispatch
- `cmd/conan/main.go` — Node info wiring from cluster config to TUI

Plan: `docs/superpowers/plans/2026-05-20-node-selector.md`

### Phase 3D: Security Review — NEXT

Whitelist pre-check, model risk assessment, confirmation prompts.
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All packages PASS

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update Phase 3C node selector progress"
```
