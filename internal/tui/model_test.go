package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/pkg/mcpproto"
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
	for _, want := range []string{"Conan", "production", "claude-sonnet", "❯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInputRendersAsBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	model.input = "hello"

	view := model.View()

	for _, want := range []string{"╭", "│ ❯ hello", "╰"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing input box part %q:\n%s", want, view)
		}
	}
}

func TestAssistantMessageRendersDividerWithElapsed(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.messages = []chatMsg{
		{role: "user", content: "hello"},
		{role: "assistant", content: "hi", elapsed: 1200 * time.Millisecond},
	}

	view := model.View()

	if !strings.Contains(view, "1.2s") {
		t.Fatalf("view missing elapsed divider:\n%s", view)
	}
	if !strings.Contains(view, "──") {
		t.Fatalf("view missing divider line:\n%s", view)
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
	if !strings.Contains(view, "❯ hello") {
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

func TestTypingSpaceAddsInputSpace(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'i'}},
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'t'}},
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'r'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
	} {
		next, _ := model.Update(key)
		model = next.(Model)
	}

	if model.input != "hi there" {
		t.Fatalf("input = %q, want space preserved", model.input)
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
	if strings.Contains(view, "❯ hello") {
		t.Fatalf("clear did not remove message:\n%s", view)
	}
	if !strings.Contains(view, "Conversation cleared") {
		t.Fatalf("clear status missing:\n%s", view)
	}
}

func TestViewportScrollsMessagesAddedAfterWindowSize(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)

	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	if model.vp.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", model.vp.YOffset)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = next.(Model)

	if model.vp.YOffset == 0 {
		t.Fatal("PageUp did not scroll messages added after window sizing")
	}
}

func TestViewportScrollsWhileStreaming(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)
	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = next.(Model)

	if model.vp.YOffset == 0 {
		t.Fatal("PageUp should scroll while a response is streaming")
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

func TestVersionCheckWarningListsMismatchedAndUnreachableNodes(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.status = "Thinking..."

	next, _ := model.Update(versionCheckMsg{mismatches: []mcp.Mismatch{
		{Node: "node-a", Got: "1.2.2", Expected: "1.2.3"},
		{Node: "node-b", Got: "connection refused", Expected: "1.2.3", IsError: true},
	}})
	model = next.(Model)

	if model.status != "Thinking..." {
		t.Fatalf("status = %q, want original status preserved", model.status)
	}
	for _, want := range []string{"Version warning", "node-a: 1.2.2 (expected 1.2.3)", "node-b: unreachable (connection refused)"} {
		if !strings.Contains(model.versionWarning, want) {
			t.Fatalf("versionWarning missing %q: %q", want, model.versionWarning)
		}
	}

	view := model.View()
	for _, want := range []string{"Thinking...", "Version warning", "node-a: 1.2.2 (expected 1.2.3)", "node-b: unreachable (connection refused)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInitVersionCheckCommandRendersWarning(t *testing.T) {
	client := newInitializeVersionTestClient(t, "1.2.2")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "1.2.3", Clients: map[string]*mcp.Client{"node-a": client}})
	model.status = "Ready"

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, want version check command")
	}

	versionMsg := execVersionCheckFromBatch(t, cmd)
	if len(versionMsg.mismatches) != 1 {
		t.Fatalf("len(mismatches) = %d, want 1", len(versionMsg.mismatches))
	}

	next, _ := model.Update(versionMsg)
	model = next.(Model)
	if model.status != "Ready" {
		t.Fatalf("status = %q, want original status preserved", model.status)
	}
	view := model.View()
	for _, want := range []string{"Ready", "Version warning", "node-a: 1.2.2 (expected 1.2.3)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInitCommandUsesMCPInitializeResponseForVersionCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "conan-agent",
						"version": "1.2.2",
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]interface{}{},
			})
		}
	}))
	defer srv.Close()

	client := mcp.NewClient(mcp.Config{BaseURL: srv.URL})
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "1.2.3", Clients: map[string]*mcp.Client{"node-a": client}})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, want version check command")
	}

	versionMsg := execVersionCheckFromBatch(t, cmd)
	next, _ := model.Update(versionMsg)
	model = next.(Model)
	if model.status != "Ready" {
		t.Fatalf("status = %q, want Ready", model.status)
	}
	if !strings.Contains(model.View(), "Version warning") {
		t.Fatalf("view missing version warning:\n%s", model.View())
	}
}

func newInitializeVersionTestClient(t *testing.T, version string) *mcp.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo:      mcpproto.ServerInfo{Name: "conan-agent", Version: version},
			}))
		default:
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]interface{}{}))
		}
	}))
	t.Cleanup(srv.Close)
	return mcp.NewClient(mcp.Config{BaseURL: srv.URL})
}

func TestInitSkipsVersionCheckForDevVersion(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "dev"})

	if cmd := model.Init(); cmd != nil {
		t.Fatal("Init() returned command for dev version with no clients, want nil")
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
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "Hello "}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "world"}})
	model = next.(Model)
	if model.streamBuf != "Hello world" {
		t.Fatalf("streamBuf = %q", model.streamBuf)
	}

	next, _ = model.Update(streamDoneMsg{streamID: 1})
	model = next.(Model)
	if model.streaming {
		t.Fatal("should not be streaming after done")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Hello world") {
		t.Fatalf("view missing streamed text:\n%s", view)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Hello world" {
		t.Fatalf("conversation messages = %#v, want one assistant message with streamed content", msgs)
	}
	if !strings.Contains(model.status, "Stream ended") {
		t.Fatalf("status = %q, want stream ended status", model.status)
	}
}

func TestStreamErrorPreservesPartialContent(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "Partial response"
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream closed unexpectedly")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Partial response") {
		t.Fatalf("view missing preserved content:\n%s", view)
	}
	if !strings.Contains(model.status, "Stream error") || !strings.Contains(model.status, "preserved") {
		t.Fatalf("status = %q, want stream error with preserved content", model.status)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Partial response" {
		t.Fatalf("conversation messages = %#v, want preserved assistant message", msgs)
	}
}

func TestToolCallReturnsCommandThatContinuesStreamWaiting(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ch := make(chan llm.ChatEvent)
	model.streamCh = ch
	model.streamCtx = context.Background()

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "fs/read", Arguments: []byte(`{"path":"/tmp/a"}`),
	}})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("tool call returned nil command, want tool work batched with continued stream waiting")
	}
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("tool call command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want 2", len(batch))
	}

	go func() {
		ch <- llm.ToolCallEvent{ID: "tc2", Name: "fs/stat", Arguments: []byte(`{"path":"/tmp/b"}`)}
	}()
	continuedMsg := execCmd(t, batch[1])
	if _, ok := continuedMsg.(streamEventMsg); !ok {
		t.Fatalf("continued wait command returned %T, want streamEventMsg", continuedMsg)
	}
}

func TestToolCallPreservesPrecedingAssistantText(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "Before the tool."}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`),
	}})
	model = next.(Model)

	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty after tool call", model.streamBuf)
	}
	if len(model.messages) < 2 {
		t.Fatalf("messages = %#v, want assistant text followed by tool call", model.messages)
	}
	if model.messages[0].role != "assistant" || model.messages[0].content != "Before the tool." {
		t.Fatalf("first message = %#v, want preserved assistant text", model.messages[0])
	}
	if model.messages[1].role != "tool" || model.messages[1].toolName != "shell/run" {
		t.Fatalf("second message = %#v, want tool call placeholder", model.messages[1])
	}
	view := model.View()
	if !strings.Contains(view, "Before the tool.") {
		t.Fatalf("view missing preserved assistant text:\n%s", view)
	}
	msgs := conv.Messages()
	if len(msgs) != 2 {
		t.Fatalf("conversation messages = %#v, want assistant text and tool call", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "Before the tool." {
		t.Fatalf("conversation first message = %#v, want preserved assistant text", msgs[0])
	}
	if msgs[1].ToolCallID != "tc1" || msgs[1].ToolName != "shell/run" {
		t.Fatalf("conversation second message = %#v, want tool call", msgs[1])
	}
}

func TestStreamErrorWithEmptyBufferDoesNotAddAssistantMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream failed")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	if !strings.Contains(model.status, "Stream error") {
		t.Fatalf("status = %q, want stream error status", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none", got)
	}
}

func TestInterruptedStreamIgnoresLateEvents(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "partial"
	model.streamCh = make(chan llm.ChatEvent)
	cancelled := false
	model.streamCancel = func() { cancelled = true }
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	if !cancelled {
		t.Fatal("interrupt did not call stream cancel")
	}

	if model.streaming {
		t.Fatal("should not be streaming after interrupt")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	if model.streamCh != nil {
		t.Fatalf("streamCh = %#v, want nil", model.streamCh)
	}
	if model.streamCancel != nil {
		t.Fatalf("streamCancel = %#v, want nil", model.streamCancel)
	}
	if model.activeStreamID != 0 {
		t.Fatalf("activeStreamID = %d, want 0", model.activeStreamID)
	}
	if !strings.Contains(model.status, "Interrupted") {
		t.Fatalf("status = %q, want interrupted status", model.status)
	}

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "late text"}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("stale text delta returned a command")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty after stale text", model.streamBuf)
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale text", conv.Messages())
	}

	next, cmd = model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("stale stop returned a command")
	}
	if model.streaming {
		t.Fatal("stale stop should not resume streaming")
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale stop", conv.Messages())
	}
}

func TestStreamErrorWithPartialBufferPreservesContent(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "Partial response"
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream closed unexpectedly")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Partial response") {
		t.Fatalf("view missing preserved content:\n%s", view)
	}
	if !strings.Contains(model.status, "Stream error") || !strings.Contains(model.status, "preserved") {
		t.Fatalf("status = %q, want stream error with preserved content", model.status)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Partial response" {
		t.Fatalf("conversation messages = %#v, want preserved assistant message", msgs)
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

func TestLateRiskAssessmentAfterInterruptIsIgnored(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell/run", toolInput: `{"command":"rm -rf /"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`)}
	next, cmd := model.Update(riskAssessmentMsg{
		streamID:   1,
		call:       call,
		assessment: security.RiskAssessment{Level: security.RiskDeny, Reason: "Destructive"},
	})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("late risk assessment returned command, want ignored")
	}
	if model.streaming {
		t.Fatal("late risk assessment should not restart streaming")
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale risk assessment", got)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages = %#v, want stale risk assessment not to mutate tool output", model.messages)
	}
}

func TestCtrlCCancelsInFlightRiskAssessment(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{
			started: started,
			block:   make(chan struct{}),
			done:    done,
		},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ctx, cancel := context.WithCancel(context.Background())
	model.streamCtx = ctx
	model.streamCancel = cancel

	cmd := model.assessToolRisk(1, llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`)})
	cmdDone := make(chan tea.Msg, 1)
	go func() { cmdDone <- execCmd(t, cmd) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("risk assessment did not start")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("risk assessment did not observe stream context cancellation")
	}
	select {
	case msg := <-cmdDone:
		riskMsg, ok := msg.(riskAssessmentMsg)
		if !ok {
			t.Fatalf("command returned %T, want riskAssessmentMsg", msg)
		}
		if riskMsg.err == nil {
			t.Fatal("risk assessment message err is nil, want context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("risk assessment command did not return after cancellation")
	}
}

func TestCtrlCCancelsInFlightToolDispatch(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	client := mcp.NewClient(mcp.Config{BaseURL: "http://node-01", Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		close(done)
		return nil, req.Context().Err()
	})}})
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Nodes:   []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
		Clients: map[string]*mcp.Client{"node-01": client},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ctx, cancel := context.WithCancel(context.Background())
	model.streamCtx = ctx
	model.streamCancel = cancel

	cmd := model.dispatchTool(1, llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)})
	cmdDone := make(chan tea.Msg, 1)
	go func() { cmdDone <- execCmd(t, cmd) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool dispatch did not start")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tool dispatch did not observe stream context cancellation")
	}
	select {
	case msg := <-cmdDone:
		if _, ok := msg.(multiToolResultMsg); !ok {
			t.Fatalf("command returned %T, want multiToolResultMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("tool dispatch command did not return after cancellation")
	}
}

func TestLateToolResultAfterInterruptIsIgnored(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell/run", toolInput: `{"command":"uptime"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}
	next, cmd := model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     call,
		Results: []nodeToolResult{
			{Node: "node-01", Output: "late output", Success: true},
		},
	})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("late tool result returned command, want ignored")
	}
	if model.streaming {
		t.Fatal("late tool result should not restart streaming")
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale tool result", got)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages = %#v, want stale tool result not to mutate tool output", model.messages)
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

func TestNodeCommandEnablesNodeToolsForNextResponse(t *testing.T) {
	model := NewModel(ModelConfig{})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode})

	if !next.nodeToolsEnabled {
		t.Fatal("/node should enable node tools")
	}
	if next.status != "Node management enabled for next model response" {
		t.Fatalf("status = %q", next.status)
	}
}

func TestNodeCommandOffDisablesNodeTools(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true

	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode, Arg: "off"})

	if next.nodeToolsEnabled {
		t.Fatal("/node off should disable node tools")
	}
	if next.status != "Node management disabled" {
		t.Fatalf("status = %q", next.status)
	}
}

func TestNodeToolExposureDispatchesNodeAddLocally(t *testing.T) {
	model := NewModel(ModelConfig{})
	rawArgs := json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`)
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: rawArgs}

	msg := execCmd(t, model.dispatchTool(7, call))

	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if result.streamID != 7 {
		t.Fatalf("streamID = %d, want 7", result.streamID)
	}
	if string(result.Call.Arguments) != string(rawArgs) {
		t.Fatalf("dispatch should preserve raw arguments, got %s", string(result.Call.Arguments))
	}
	if len(result.Results) != 1 || result.Results[0].Node != "local" {
		t.Fatalf("results = %#v, want one local result", result.Results)
	}
	if result.Results[0].Success {
		t.Fatal("node_add dispatch should fail when node tools are disabled")
	}
	if !strings.Contains(result.Results[0].Output, "node_add is not enabled") {
		t.Fatalf("output = %q, want authorization error", result.Results[0].Output)
	}
}

func TestNodeToolExposureClearedOnFinishStream(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true

	model.finishStream(false)

	if model.nodeToolsEnabled {
		t.Fatal("finishStream should clear node tool exposure")
	}
}

func TestNodeAddAuditLogsRedactPassword(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()

	model := NewModel(ModelConfig{AuditLogger: auditLog})
	call := llm.ToolCall{
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`),
	}

	model.logAuditDecision(call, security.RiskAssessment{Level: security.RiskConfirm, Reason: "node add"}, "denied")
	model.logAuditExecution(call, []nodeToolResult{{Node: "local", Output: "not implemented", Success: false}})

	if err := auditLog.Close(); err != nil {
		t.Fatalf("close audit log: %v", err)
	}
	contents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	text := string(contents)
	if strings.Contains(text, "secret") {
		t.Fatalf("audit log should not contain raw password: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("audit log should contain redacted password marker: %s", text)
	}
	if !strings.Contains(text, "10.0.0.5") {
		t.Fatalf("audit log should preserve host: %s", text)
	}
}

func TestNodeAddRiskReviewRedactsPasswordAndPreservesRawCall(t *testing.T) {
	provider := &stubRiskProvider{response: `{"risk_level":"confirm","reason":"node add"}`}
	reviewer := security.NewReviewer(security.ReviewerConfig{Provider: provider})
	model := NewModel(ModelConfig{Reviewer: reviewer})
	rawArgs := json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`)
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: rawArgs}

	msg := execCmd(t, model.assessToolRisk(7, call))
	result, ok := msg.(riskAssessmentMsg)
	if !ok {
		t.Fatalf("assessToolRisk returned %T, want riskAssessmentMsg", msg)
	}
	if result.err != nil {
		t.Fatalf("risk assessment error: %v", result.err)
	}
	if provider.req == nil {
		t.Fatal("risk provider was not called")
	}
	if strings.Contains(provider.req.SystemPrompt, "secret") {
		t.Fatalf("risk prompt should not contain raw password: %s", provider.req.SystemPrompt)
	}
	if !strings.Contains(provider.req.SystemPrompt, "[REDACTED]") {
		t.Fatalf("risk prompt should contain redacted password marker: %s", provider.req.SystemPrompt)
	}
	if string(result.call.Arguments) != string(rawArgs) {
		t.Fatalf("risk assessment call args = %s, want raw args", result.call.Arguments)
	}
}

func TestDebugLogStreamEventRedactsNodeAddPassword(t *testing.T) {
	var buf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	model := NewModel(ModelConfig{})
	model.debugLogStreamEvent(llm.ToolCallEvent{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`),
	})

	logText := buf.String()
	if strings.Contains(logText, "secret") {
		t.Fatalf("debug log should not contain raw password: %s", logText)
	}
	if !strings.Contains(logText, "[REDACTED]") {
		t.Fatalf("debug log should contain redacted password marker: %s", logText)
	}
	if !strings.Contains(logText, "10.0.0.5") {
		t.Fatalf("debug log should preserve host: %s", logText)
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
	if !strings.Contains(view, "node-01") || !strings.Contains(view, "node-02") {
		t.Fatalf("view missing node names:\n%s", view)
	}
}

func TestToolOutputCollapsesAndTogglesLastToolWithCtrlO(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	firstCall := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"seq 1 6"}`)}
	firstOutput := "first 1\nfirst 2\nfirst 3\nfirst 4\nfirst 5\nfirst 6"

	next, _ := model.Update(multiToolResultMsg{Call: firstCall, Results: []nodeToolResult{{Node: "node-01", Output: firstOutput, Success: true}}})
	model = next.(Model)

	secondCall := llm.ToolCall{ID: "c2", Name: "shell/run", Arguments: []byte(`{"command":"seq 1 6"}`)}
	secondOutput := "second 1\nsecond 2\nsecond 3\nsecond 4\nsecond 5\nsecond 6"
	next, _ = model.Update(multiToolResultMsg{Call: secondCall, Results: []nodeToolResult{{Node: "node-01", Output: secondOutput, Success: true}}})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"first 4", "second 4", "2 more line(s)", "Ctrl+O"} {
		if !strings.Contains(view, want) {
			t.Fatalf("collapsed view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "first 6") || strings.Contains(view, "second 6") {
		t.Fatalf("collapsed view showed hidden lines:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = next.(Model)
	view = model.View()
	if strings.Contains(view, "first 6") {
		t.Fatalf("Ctrl+O expanded an older tool message:\n%s", view)
	}
	if !strings.Contains(view, "second 6") {
		t.Fatalf("expanded view missing last tool final line:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = next.(Model)
	view = model.View()
	if strings.Contains(view, "second 6") || !strings.Contains(view, "2 more line(s)") {
		t.Fatalf("re-collapsed view did not hide final lines:\n%s", view)
	}
}

func TestShellRunOutputShowsStatusWithoutStdoutStderrLabels(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"pwd"}`)}
	output := "exit_code: 0\nstdout:\n/home/app\nstderr:\n"

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: []nodeToolResult{{Node: "node-01", Output: output, Success: true}}})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"status: 0", "/home/app"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"stdout:", "stderr:"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view contains %q:\n%s", unwanted, view)
		}
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
		t.Fatalf("view missing success node:\n%s", view)
	}
	if !strings.Contains(view, "node-02") {
		t.Fatalf("view missing failure node:\n%s", view)
	}
}

func TestDispatchExecCallsShellRunTool(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method != "tools/call" {
			t.Fatalf("method = %q, want tools/call", req.Method)
		}
		var params mcpproto.ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.Name != "shell/run" {
			t.Fatalf("tool name = %q, want shell/run", params.Name)
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			t.Fatalf("decode arguments: %v", err)
		}
		if args.Command != "uptime" {
			t.Fatalf("command = %q, want uptime", args.Command)
		}
		called = true
		_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("ok")}}))
	}))
	t.Cleanup(srv.Close)

	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Clients: map[string]*mcp.Client{"node-01": mcp.NewClient(mcp.Config{BaseURL: srv.URL})},
	})
	call := llm.ToolCall{ID: "c1", Name: "exec", Arguments: []byte(`{"node":"node-01","command":"uptime"}`)}

	msg := execCmd(t, model.dispatchTool(0, call))
	resultMsg, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if !called {
		t.Fatal("agent tools/call was not called")
	}
	if len(resultMsg.Results) != 1 || !resultMsg.Results[0].Success {
		t.Fatalf("results = %#v, want one successful result", resultMsg.Results)
	}
}

func TestDispatchToolConnectionLostMarksOnlyFailedNodeOffline(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	okClient := mcp.NewClient(mcp.Config{BaseURL: "http://node-01", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`)),
			Header:     make(http.Header),
		}, nil
	})}})
	connectionLostClient := mcp.NewClient(mcp.Config{BaseURL: "http://node-02", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("read tcp 10.0.0.2:443->10.0.0.1:54832: read: connection reset by peer")
	})}})
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Nodes:   nodes,
		Clients: map[string]*mcp.Client{
			"node-01": okClient,
			"node-02": connectionLostClient,
		},
	})
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}

	msg := execCmd(t, model.dispatchTool(0, call))
	resultMsg, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	next, _ := model.Update(resultMsg)
	model = next.(Model)

	if !model.nodes[0].Online {
		t.Fatal("node-01 should remain online")
	}
	if model.nodes[1].Online {
		t.Fatal("node-02 should be marked offline after connection loss")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// execCmd executes a tea.Cmd synchronously and returns its message.
func execCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func execVersionCheckFromBatch(t *testing.T, cmd tea.Cmd) versionCheckMsg {
	t.Helper()
	msg := execCmd(t, cmd)
	if vm, ok := msg.(versionCheckMsg); ok {
		return vm
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want versionCheckMsg or tea.BatchMsg", msg)
	}
	for _, c := range batch {
		inner := execCmd(t, c)
		if vm, ok := inner.(versionCheckMsg); ok {
			return vm
		}
	}
	t.Fatal("no versionCheckMsg in batch")
	return versionCheckMsg{}
}

type stubRiskProvider struct {
	response string
	err      error
	req      *llm.ChatRequest
	started  chan struct{}
	block    chan struct{}
	done     chan struct{}
}

func (s *stubRiskProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	s.req = req
	if s.started != nil {
		close(s.started)
	}
	if s.block != nil {
		select {
		case <-ctx.Done():
			if s.done != nil {
				close(s.done)
			}
			return nil, ctx.Err()
		case <-s.block:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: s.response},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (s *stubRiskProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	return nil, nil
}

type countingStreamProvider struct {
	calls int
}

func (p *countingStreamProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}

func (p *countingStreamProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.calls++
	ch := make(chan llm.ChatEvent)
	close(ch)
	return ch, nil
}

func TestRiskDenyFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"deny","reason":"Destructive"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages after tool call = %#v, want one empty placeholder", model.messages)
	}

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want denial to fill existing placeholder only", model.messages)
	}
	if model.messages[0].toolOutput == "" || !strings.Contains(model.messages[0].toolOutput, "BLOCKED") {
		t.Fatalf("placeholder output = %q, want BLOCKED", model.messages[0].toolOutput)
	}
}

func TestToolCallDeniedBySecurity(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"deny","reason":"Destructive"}`},
	})
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()
	model := NewModel(ModelConfig{
		Cluster:     "test",
		Model:       "m",
		Conv:        conv,
		Reviewer:    reviewer,
		AuditLogger: auditLog,
		Nodes:       []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
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
	auditContents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditContents), "[DENY]") || !strings.Contains(string(auditContents), "node-01") {
		t.Fatalf("audit log missing denied decision: %s", auditContents)
	}
}

func TestRiskAssessmentErrorFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{err: errors.New("review unavailable")},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want error to fill existing placeholder only", model.messages)
	}
	if !strings.Contains(model.messages[0].toolOutput, "Risk assessment error") {
		t.Fatalf("placeholder output = %q, want risk assessment error", model.messages[0].toolOutput)
	}
}

func TestRiskAssessmentErrorRecordsConversationToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{err: errors.New("review unavailable")},
	})
	provider := &countingStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Provider: provider,
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, cmd = model.Update(msg)
	model = result.(Model)

	if cmd == nil {
		t.Fatal("risk assessment error should resume after recording tool result")
	}
	if provider.calls != 0 {
		t.Fatalf("ChatStream called before command execution: %d", provider.calls)
	}
	msgs := conv.Messages()
	if len(msgs) != 2 || msgs[0].ToolCallID != "tc1" || msgs[1].Role != conversation.RoleTool || msgs[1].ToolCallID != "tc1" {
		t.Fatalf("conversation messages = %#v, want tool call followed by matching tool result", msgs)
	}
	if !strings.Contains(msgs[1].Content, "Risk assessment error") {
		t.Fatalf("tool result content = %q, want risk assessment error", msgs[1].Content)
	}
}

func TestMultipleToolCallsResumeOnlyAfterAllResults(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	provider := &countingStreamProvider{}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: provider, Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	for _, call := range []llm.ToolCallEvent{
		{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)},
		{ID: "tc2", Name: "shell/run", Arguments: []byte(`{"command":"date"}`)},
	} {
		result, _ := model.Update(streamEventMsg{streamID: 1, Event: call})
		model = result.(Model)
	}
	result, _ := model.Update(streamDoneMsg{streamID: 1})
	model = result.(Model)

	result, cmd := model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "up", Success: true}}})
	model = result.(Model)
	if cmd != nil {
		t.Fatal("first tool result resumed stream before second result")
	}
	if provider.calls != 0 {
		t.Fatalf("ChatStream calls = %d, want none before all tool results", provider.calls)
	}

	result, cmd = model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc2", Name: "shell/run", Arguments: []byte(`{"command":"date"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "today", Success: true}}})
	model = result.(Model)
	if cmd == nil {
		t.Fatal("second tool result should resume stream")
	}
	execCmd(t, cmd)
	if provider.calls != 1 {
		t.Fatalf("ChatStream calls = %d, want one resume after all tool results", provider.calls)
	}
}

func TestIdenticalToolCallsFillPlaceholderByID(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	args := []byte(`{"command":"uptime"}`)
	model.messages = []chatMsg{
		{role: "tool", toolCallID: "tc1", toolName: "shell/run", toolInput: string(args)},
		{role: "tool", toolCallID: "tc2", toolName: "shell/run", toolInput: string(args)},
	}

	model.fillToolPlaceholder(llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: args}, "first", nil)

	if model.messages[0].toolOutput != "first" {
		t.Fatalf("first output = %q, want first", model.messages[0].toolOutput)
	}
	if model.messages[1].toolOutput != "" {
		t.Fatalf("second output = %q, want empty", model.messages[1].toolOutput)
	}
}

func TestToolCallNeedsConfirmation(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Restarts service","suggestion":"Rolling restart"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
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
	if !strings.Contains(view, "Security Review") {
		t.Fatalf("confirm mode should show security review:\n%s", view)
	}
	if !strings.Contains(view, "▶ Allow") || !strings.Contains(view, "Deny") {
		t.Fatalf("confirm mode should show selectable allow/deny options:\n%s", view)
	}
	if strings.Contains(view, "▶ Allow    Deny") {
		t.Fatalf("confirm options should be stacked vertically:\n%s", view)
	}
	if strings.Contains(view, "yes") || strings.Contains(view, "Confirm?") {
		t.Fatalf("confirm mode should not require typed confirmation:\n%s", view)
	}
	if !strings.Contains(view, "Restarts service") {
		t.Fatalf("confirm mode should show risk reason:\n%s", view)
	}
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("confirm mode should keep the tool placeholder visible:\n%s", view)
	}
	if !strings.Contains(view, "Command") || !strings.Contains(view, "systemctl restart nginx") {
		t.Fatalf("confirm mode should show the command being reviewed:\n%s", view)
	}
	if strings.Contains(view, "╭") || strings.Contains(view, "╰") {
		t.Fatalf("confirm mode should render inline at the bottom, not as a separate panel:\n%s", view)
	}
}

func TestNodeAddRequiresConfirmationEvenWhenRiskAllows(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"allow","reason":"allowed"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
	})
	model.nodeToolsEnabled = true
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: []byte(`{"host":"10.0.0.12","user":"deploy","password":"secret"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", model.mode)
	}
	if model.pendingRisk == nil || model.pendingRisk.Level != security.RiskConfirm {
		t.Fatalf("pendingRisk = %#v, want forced confirmation", model.pendingRisk)
	}
	if !strings.Contains(model.View(), "node_add requires confirmation") {
		t.Fatalf("view missing forced confirmation reason:\n%s", model.View())
	}
}

func TestConfirmEnterOnAllowDispatchesTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()
	model := NewModel(ModelConfig{
		Cluster:     "test",
		Model:       "m",
		Conv:        conv,
		Reviewer:    reviewer,
		AuditLogger: auditLog,
		Nodes:       []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
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

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after confirm", model.mode)
	}
	if cmd == nil {
		t.Fatal("confirming should dispatch the tool")
	}
	auditContents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditContents), "[CONFIRM]") || !strings.Contains(string(auditContents), `outcome="approved"`) {
		t.Fatalf("audit log missing approved confirmation: %s", auditContents)
	}
}

func TestConfirmNoFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages after tool call = %#v, want one empty placeholder", model.messages)
	}

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = result.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want deny selected", model.confirmChoice)
	}
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want cancellation to fill existing placeholder only", model.messages)
	}
	if model.messages[0].toolOutput != "Cancelled by user" {
		t.Fatalf("placeholder output = %q, want cancellation", model.messages[0].toolOutput)
	}
}

func TestConfirmAllowAndAddWritesNodeAllowlist(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "test", "cluster.yaml"), `name: test
`)
	writeTestFile(t, filepath.Join(home, "clusters", "test", "nodes.yaml"), `nodes:
  - name: node-01
    host: 10.0.0.11
`)
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:    "test",
		Model:      "m",
		Conv:       conv,
		Reviewer:   reviewer,
		ConfigHome: home,
		Nodes:      []NodeInfo{{Name: "node-01", Host: "10.0.0.11", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "exec", Arguments: []byte(`{"command":"systemctl restart nginx","node":"node-01"}`),
	}})
	model = result.(Model)
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	view := model.View()
	if !strings.Contains(view, "Allow and add to allowlist") {
		t.Fatalf("confirm view missing allowlist option:\n%s", view)
	}
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = result.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want allow and add selected", model.confirmChoice)
	}
	result, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = result.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want chat", model.mode)
	}
	if cmd == nil {
		t.Fatal("allow and add should dispatch the tool")
	}
	data, err := os.ReadFile(filepath.Join(home, "clusters", "test", "nodes.yaml"))
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	if !strings.Contains(string(data), "systemctl restart nginx") {
		t.Fatalf("nodes.yaml missing command allowlist:\n%s", data)
	}

	assessment, err := reviewer.Review(context.Background(), "exec", `{"command":"systemctl restart nginx","node":"node-01"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Level != security.RiskAllow {
		t.Fatalf("updated reviewer should allow command, got %#v", assessment)
	}
}

func TestConfirmEscCancelsTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = result.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after esc", model.mode)
	}
	if model.messages[0].toolOutput != "Cancelled by user" {
		t.Fatalf("placeholder output = %q, want cancellation", model.messages[0].toolOutput)
	}
}

func TestConfirmNoCancelsTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want deny selected", model.confirmChoice)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after cancel", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Cancelled") {
		t.Fatalf("cancelled tool should show cancelled:\n%s", view)
	}
}
