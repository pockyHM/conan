package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
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
