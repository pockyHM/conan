package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pockyHM/conan/pkg/models"
)

func TestOpenAIChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"chatcmpl-test",
			"choices":[{
				"message":{"role":"assistant","content":"Hi there!"},
				"finish_reason":"stop"
			}]
		}`)
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "gpt-4.1",
		BaseURL: server.URL,
	})
	resp, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "Hi there!" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if resp.StopReason != StopEndTurn {
		t.Fatalf("stopReason = %q", resp.StopReason)
	}
}

func TestOpenAIStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"content":" there"},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "gpt-4.1",
		BaseURL: server.URL,
	})
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
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
			t.Fatalf("error: %v", e.Err)
		}
	}
	if text != "Hi there" {
		t.Fatalf("text = %q", text)
	}
	if stopReason != StopEndTurn {
		t.Fatalf("stopReason = %q", stopReason)
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"content":"Let me check."},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"shell/run","arguments":""}}]},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"df -h\"}"}}]},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "gpt-4.1",
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
	if text != "Let me check." {
		t.Fatalf("text = %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "call_abc" || toolCalls[0].Name != "shell/run" {
		t.Fatalf("toolCall = %+v", toolCalls[0])
	}
}

func writeOpenAIChunk(w http.ResponseWriter, f http.Flusher, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	f.Flush()
}
