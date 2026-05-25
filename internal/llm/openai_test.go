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

func TestOpenAIChatUsesDirectEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"chatcmpl-test",
			"choices":[{
				"message":{"role":"assistant","content":"direct"},
				"finish_reason":"stop"
			}]
		}`)
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:              "test-key",
		Model:               "gpt-4.1",
		BaseURL:             server.URL + "/custom/openai",
		UseEndpointDirectly: true,
	})
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/custom/openai" {
		t.Fatalf("path = %q, want /custom/openai", gotPath)
	}
}

func TestOpenAIChatAppendsDefaultRoute(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"chatcmpl-test",
			"choices":[{
				"message":{"role":"assistant","content":"base"},
				"finish_reason":"stop"
			}]
		}`)
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "gpt-4.1",
		BaseURL: server.URL + "/v1",
	})
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
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

func TestOpenAIStreamUsesReasoningContentAsReasoningDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"reasoning_content":"Hello"},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"reasoning_content":" there"},"finish_reason":null}]}`)
		writeOpenAIChunk(w, flusher, `{"id":"chatcmpl-t","choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "glm-4.7",
		BaseURL: server.URL,
	})
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var reasoning string
	var stopReason string
	for event := range ch {
		switch e := event.(type) {
		case ReasoningDeltaEvent:
			reasoning += e.Delta
		case StopEvent:
			stopReason = e.Reason
		case ErrorEvent:
			t.Fatalf("error: %v", e.Err)
		}
	}
	if reasoning != "Hello there" {
		t.Fatalf("reasoning = %q", reasoning)
	}
	if stopReason != StopEndTurn {
		t.Fatalf("stopReason = %q", stopReason)
	}
}

func TestOpenAIBuildBodyIncludesThinkingSetting(t *testing.T) {
	disabled := false
	p := NewOpenAIProvider(OpenAIConfig{Model: "glm-4.7", Thinking: &disabled})

	body, err := p.buildBody(&ChatRequest{Messages: []models.Message{{Role: "user", Content: "hi"}}}, true)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	thinking, ok := decoded["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v, want disabled", decoded["thinking"])
	}

	enabled := true
	body, err = p.buildBody(&ChatRequest{Thinking: &enabled, Messages: []models.Message{{Role: "user", Content: "hi"}}}, true)
	if err != nil {
		t.Fatalf("buildBody override: %v", err)
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode override body: %v", err)
	}
	thinking, ok = decoded["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking override = %#v, want enabled", decoded["thinking"])
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
