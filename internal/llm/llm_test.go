package llm

import (
	"context"
	"testing"

	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

type fakeProvider struct{}

func (f fakeProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Message: models.Message{Role: "assistant", Content: "hello"}, StopReason: StopEndTurn}, nil
}

func (f fakeProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	ch := make(chan ChatEvent, 2)
	ch <- TextDeltaEvent{Delta: "he"}
	ch <- TextDeltaEvent{Delta: "llo"}
	close(ch)
	return ch, nil
}

func TestProviderInterface(t *testing.T) {
	var provider Provider = fakeProvider{}
	resp, err := provider.Chat(t.Context(), &ChatRequest{SystemPrompt: "system"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
}

func TestStreamEvents(t *testing.T) {
	var provider Provider = fakeProvider{}
	stream, err := provider.ChatStream(t.Context(), &ChatRequest{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var text string
	for event := range stream {
		switch e := event.(type) {
		case TextDeltaEvent:
			text += e.Delta
		default:
			t.Fatalf("unexpected event %#v", event)
		}
	}
	if text != "hello" {
		t.Fatalf("text = %q", text)
	}
}

func TestToolDefAlias(t *testing.T) {
	tool := ToolDef{Name: "shell_run", Description: "run", InputSchema: []byte(`{"type":"object"}`)}
	mcpTool := mcpproto.ToolDefinition(tool)
	if mcpTool.Name != "shell_run" {
		t.Fatalf("name = %q", mcpTool.Name)
	}
}
