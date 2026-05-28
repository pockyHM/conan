package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pockyHM/conan/pkg/models"
)

func TestAnthropicChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != nil {
			t.Errorf("non-streaming request should not have stream field")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"msg_test","role":"assistant",
			"content":[{"type":"text","text":"Hello!"}],
			"model":"claude-sonnet-4-6","stop_reason":"end_turn"
		}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(AnthropicConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: server.URL,
	})
	resp, err := p.Chat(context.Background(), &ChatRequest{
		SystemPrompt: "You are helpful.",
		Messages:     []models.Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "Hello!" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if resp.StopReason != StopEndTurn {
		t.Fatalf("stopReason = %q", resp.StopReason)
	}
}

func TestAnthropicChatUsesDirectEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"direct"}],
			"stop_reason":"end_turn"
		}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(AnthropicConfig{
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-6",
		BaseURL:             server.URL + "/custom/messages",
		UseEndpointDirectly: true,
	})
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/custom/messages" {
		t.Fatalf("path = %q, want /custom/messages", gotPath)
	}
}

func TestAnthropicChatAppendsDefaultRoute(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"base"}],
			"stop_reason":"end_turn"
		}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(AnthropicConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: server.URL,
	})
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
}

func TestAnthropicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		writeSSE(w, flusher, "message_start", `{"type":"message_start","message":{"id":"msg_t","role":"assistant"}}`)
		writeSSE(w, flusher, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeSSE(w, flusher, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
		writeSSE(w, flusher, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`)
		writeSSE(w, flusher, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSE(w, flusher, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
		writeSSE(w, flusher, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(AnthropicConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: server.URL,
	})
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var text string
	var stopReason string
	for event := range ch {
		switch e := event.(type) {
		case TextDeltaEvent:
			text += e.Delta
		case StopEvent:
			stopReason = e.Reason
		case ErrorEvent:
			t.Fatalf("error event: %v", e.Err)
		}
	}
	if text != "Hello world" {
		t.Fatalf("text = %q", text)
	}
	if stopReason != StopEndTurn {
		t.Fatalf("stopReason = %q", stopReason)
	}
}

func TestAnthropicStreamToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		writeSSE(w, flusher, "message_start", `{"type":"message_start","message":{"id":"msg_t","role":"assistant"}}`)
		writeSSE(w, flusher, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeSSE(w, flusher, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking."}}`)
		writeSSE(w, flusher, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSE(w, flusher, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_123","name":"shell_run","input":{}}}`)
		writeSSE(w, flusher, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"df -h\"}"}}`)
		writeSSE(w, flusher, "content_block_stop", `{"type":"content_block_stop","index":1}`)
		writeSSE(w, flusher, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`)
		writeSSE(w, flusher, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(AnthropicConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: server.URL,
	})
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "check disk"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var text string
	var toolCalls []ToolCall
	for event := range ch {
		switch e := event.(type) {
		case TextDeltaEvent:
			text += e.Delta
		case ToolCallEvent:
			toolCalls = append(toolCalls, ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments})
		case StopEvent:
			if e.Reason != StopToolUse {
				t.Fatalf("stopReason = %q, want tool_use", e.Reason)
			}
		}
	}
	if text != "Checking." {
		t.Fatalf("text = %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "toolu_123" || toolCalls[0].Name != "shell_run" {
		t.Fatalf("toolCall = %+v", toolCalls[0])
	}
	var args map[string]string
	json.Unmarshal(toolCalls[0].Arguments, &args)
	if args["command"] != "df -h" {
		t.Fatalf("args = %q", args["command"])
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	f.Flush()
}
