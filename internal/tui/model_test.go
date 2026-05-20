package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/pkg/models"
)

type fakeProvider struct{}

func (f *fakeProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: "hello"},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (f *fakeProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent, 10)
	go func() {
		ch <- llm.TextDeltaEvent{Delta: "Hi"}
		ch <- llm.TextDeltaEvent{Delta: " there"}
		ch <- llm.StopEvent{Reason: llm.StopEndTurn}
		close(ch)
	}()
	return ch, nil
}

func TestInitialModelView(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	view := model.View()
	for _, want := range []string{"Conan", "production", "claude-sonnet", ">"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestTypingAndEnterAddsUserMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "You: hello") {
		t.Fatalf("view missing submitted message:\n%s", view)
	}
	if model.input != "" {
		t.Fatalf("input = %q, want empty", model.input)
	}
	if !model.streaming {
		t.Fatal("should be streaming after submit")
	}
	if cmd == nil {
		t.Fatal("expected a Cmd to be returned after submit")
	}
}

func TestClearCommandClearsMessages(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	for _, r := range "/clear" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "You: hello") {
		t.Fatalf("clear did not remove message:\n%s", view)
	}
	if !strings.Contains(view, "Conversation cleared") {
		t.Fatalf("clear status missing:\n%s", view)
	}
}

func TestExitCommandQuits(t *testing.T) {
	model := NewModel(ModelConfig{})
	for _, r := range "/exit" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("exit command did not return quit command")
	}
}

func TestNoProviderShowsStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !strings.Contains(model.status, "No LLM provider") {
		t.Fatalf("status = %q", model.status)
	}
}

func TestStreamingUpdatesAccumulate(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."

	next, _ := model.Update(streamEventMsg{Event: llm.TextDeltaEvent{Delta: "Hello "}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{Event: llm.TextDeltaEvent{Delta: "world"}})
	model = next.(Model)
	if model.streamBuf != "Hello world" {
		t.Fatalf("streamBuf = %q", model.streamBuf)
	}

	next, _ = model.Update(streamDoneMsg{})
	model = next.(Model)
	if model.streaming {
		t.Fatal("should not be streaming after done")
	}
	view := model.View()
	if !strings.Contains(view, "Conan: Hello world") {
		t.Fatalf("view missing streamed text:\n%s", view)
	}
}

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

// execCmd executes a tea.Cmd synchronously and returns its message.
func execCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

type stubRiskProvider struct {
	response string
}

func (s *stubRiskProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: s.response},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (s *stubRiskProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	return nil, nil
}

func TestToolCallDeniedBySecurity(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Whitelist: []string{},
		Provider:  &stubRiskProvider{response: `{"risk_level":"deny","reason":"Destructive"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	result, cmd := model.Update(streamEventMsg{Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	view := model.View()
	if !strings.Contains(view, "BLOCKED") {
		t.Fatalf("denied tool should show BLOCKED in view:\n%s", view)
	}
}

func TestToolCallNeedsConfirmation(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Whitelist: []string{},
		Provider:  &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Restarts service","suggestion":"Rolling restart"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	result, cmd := model.Update(streamEventMsg{Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Confirm?") {
		t.Fatalf("confirm mode should show prompt:\n%s", view)
	}
	if !strings.Contains(view, "Restarts service") {
		t.Fatalf("confirm mode should show risk reason:\n%s", view)
	}
}

func TestConfirmYesDispatchesTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Whitelist: []string{},
		Provider:  &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	result, cmd := model.Update(streamEventMsg{Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatal("should be in confirm mode")
	}

	for _, r := range "yes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after confirm", model.mode)
	}
	if cmd == nil {
		t.Fatal("confirming should dispatch the tool")
	}
}

func TestConfirmNoCancelsTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Whitelist: []string{},
		Provider:  &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	result, cmd := model.Update(streamEventMsg{Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	for _, r := range "no" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after cancel", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Cancelled") {
		t.Fatalf("cancelled tool should show cancelled:\n%s", view)
	}
}
