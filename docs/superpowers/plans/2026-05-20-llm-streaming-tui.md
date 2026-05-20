# LLM Integration & Streaming TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the TUI to real LLM providers (Anthropic + OpenAI-compatible) with streaming responses and an agentic tool-call dispatch loop.

**Architecture:** Two LLM provider implementations (`internal/llm/anthropic.go`, `internal/llm/openai.go`) satisfy the existing `Provider` interface. A shared SSE reader parses streaming events. The TUI model is refactored to accept a provider, conversation manager, MCP clients, and tool definitions. Streaming uses Bubble Tea's Cmd pattern: a goroutine starts the HTTP stream, then individual events are read one-at-a-time via successive Cmds. When the LLM returns tool_use, the TUI dispatches to an MCP agent and feeds results back into a new LLM call.

**Tech Stack:** Go 1.25, net/http, encoding/json, bufio, Bubble Tea, httptest

---

## Scope Boundary

This plan implements **Phase 3B-1: LLM Integration & Streaming**.

**Included:**

- SSE stream reader (shared by both providers)
- Anthropic Messages API provider (non-streaming + streaming)
- OpenAI Chat Completions API provider (non-streaming + streaming)
- Provider factory (model name → provider)
- Tool call ID support in `models.Message` and `conversation.Conversation`
- TUI refactor: streaming text display, tool-call dispatch loop, structured messages
- Wire everything into `conan tui` command

**Excluded (future plans):**

- Interactive `/nodes` multi-select UI and multi-node fan-out
- Security review (whitelist + model risk assessment)
- Memory system (SQLite + MEMORY.md)
- Session archive and `/resume`
- Markdown rendering with Glamour
- Collapsible tool-call blocks (expand on keypress)

---

## File Structure

### Created

```text
internal/llm/sse.go            # SSE stream parser
internal/llm/sse_test.go       # SSE tests
internal/llm/anthropic.go      # Anthropic Messages API provider
internal/llm/anthropic_test.go # Anthropic provider tests
internal/llm/openai.go         # OpenAI Chat Completions API provider
internal/llm/openai_test.go    # OpenAI provider tests
internal/llm/factory.go        # Provider factory
internal/llm/factory_test.go   # Factory tests
```

### Modified

```text
pkg/models/models.go                          # Add ToolCallID field
internal/conversation/conversation.go          # Add AddToolCall, AddToolResult methods
internal/conversation/conversation_test.go     # Update tests for new methods
internal/tui/model.go                          # Major refactor: streaming + tool dispatch
internal/tui/model_test.go                     # Update tests for new model structure
cmd/conan/main.go                             # Wire providers + MCP clients to TUI
CLAUDE.md                                     # Update progress
```

---

## Task 1: SSE Stream Reader

**Files:**

- Create: `internal/llm/sse.go`
- Create: `internal/llm/sse_test.go`

**Purpose:** Generic SSE parser used by both Anthropic and OpenAI providers. Reads `event:` and `data:` lines from an HTTP response body and emits structured events to a channel.

- [ ] **Step 1: Write failing SSE tests**

Create `internal/llm/sse_test.go`:

```go
package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadSSEParsesEventsWithData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	events := ReadSSE(resp.Body)
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[0].Event != "message_start" || collected[0].Data != `{"type":"message_start"}` {
		t.Fatalf("event[0] = %+v", collected[0])
	}
	if collected[1].Event != "content_block_delta" {
		t.Fatalf("event[1].Event = %q", collected[1].Event)
	}
}

func TestReadSSEHandlesDataOnlyLines(t *testing.T) {
	input := "data: hello\n\ndata: world\n\n"
	events := ReadSSE(strings.NewReader(input))
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[0].Data != "hello" {
		t.Fatalf("data[0] = %q", collected[0].Data)
	}
	if collected[1].Data != "world" {
		t.Fatalf("data[1] = %q", collected[1].Data)
	}
}

func TestReadSSESendsDoneSentinel(t *testing.T) {
	input := "data: first\n\ndata: [DONE]\n\n"
	events := ReadSSE(strings.NewReader(input))
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[1].Data != "[DONE]" {
		t.Fatalf("data[1] = %q", collected[1].Data)
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestReadSSE -v
```

Expected: FAIL with undefined `SSEEvent`, `ReadSSE`.

- [ ] **Step 3: Implement SSE reader**

Create `internal/llm/sse.go`:

```go
package llm

import (
	"bufio"
	"io"
	"strings"
)

type SSEEvent struct {
	Event string
	Data  string
}

func ReadSSE(reader io.Reader) <-chan SSEEvent {
	ch := make(chan SSEEvent, 10)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(reader)
		var event, data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if data.Len() > 0 {
					ch <- SSEEvent{Event: event.String(), Data: data.String()}
				}
				event.Reset()
				data.Reset()
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event.WriteString(strings.TrimSpace(line[6:]))
			} else if strings.HasPrefix(line, "data:") {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				val := line[5:]
				if len(val) > 0 && val[0] == ' ' {
					val = val[1:]
				}
				data.WriteString(val)
			}
		}
	}()
	return ch
}
```

- [ ] **Step 4: Run tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestReadSSE -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/sse.go internal/llm/sse_test.go
git commit -m "feat: add SSE stream reader for LLM providers"
```

---

## Task 2: Tool Call Support in Models & Conversation

**Files:**

- Modify: `pkg/models/models.go`
- Modify: `internal/conversation/conversation.go`
- Modify: `internal/conversation/conversation_test.go`

**Purpose:** Add `ToolCallID` to `models.Message` so providers can match tool results with tool calls. Add `AddToolCall` and `AddToolResult` methods to conversation for the agentic loop.

- [ ] **Step 1: Write failing conversation tests**

Add to `internal/conversation/conversation_test.go`:

```go
func TestConversationToolCallRoundTrip(t *testing.T) {
	c := New("prod", []string{"node-1"}, "claude-sonnet")
	c.AddUser("check disk")
	c.AddAssistant("I'll check disk usage.")
	c.AddToolCall("toolu_abc", "shell/run", `{"command":"df -h"}`)
	c.AddToolResult("toolu_abc", "Filesystem  Size  Used  Avail  Use%")

	msgs := c.Messages()
	if len(msgs) != 4 {
		t.Fatalf("len = %d, want 4", len(msgs))
	}

	toolCallMsg := msgs[2]
	if toolCallMsg.Role != RoleAssistant {
		t.Fatalf("tool call role = %q, want %q", toolCallMsg.Role, RoleAssistant)
	}
	if toolCallMsg.ToolCallID != "toolu_abc" {
		t.Fatalf("tool call id = %q", toolCallMsg.ToolCallID)
	}
	if toolCallMsg.ToolName != "shell/run" {
		t.Fatalf("tool name = %q", toolCallMsg.ToolName)
	}
	if toolCallMsg.ToolInput != `{"command":"df -h"}` {
		t.Fatalf("tool input = %q", toolCallMsg.ToolInput)
	}

	resultMsg := msgs[3]
	if resultMsg.Role != RoleTool {
		t.Fatalf("tool result role = %q", resultMsg.Role)
	}
	if resultMsg.ToolCallID != "toolu_abc" {
		t.Fatalf("tool result id = %q", resultMsg.ToolCallID)
	}
	if resultMsg.Content != "Filesystem  Size  Used  Avail  Use%" {
		t.Fatalf("tool result content = %q", resultMsg.Content)
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/conversation -run TestConversationToolCallRoundTrip -v
```

Expected: FAIL with undefined `AddToolCall`, `AddToolResult`, or `ToolCallID`.

- [ ] **Step 3: Implement model + conversation changes**

Add `ToolCallID` field to `pkg/models/models.go` in the `Message` struct after `Content`:

```go
type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolInput      string `json:"tool_input,omitempty"`
	ToolOutput     string `json:"tool_output,omitempty"`
	CreatedAt      string `json:"created_at"`
}
```

Update `internal/conversation/conversation.go` — change the `add` method signature to include `toolCallID` and add new methods:

```go
func (c *Conversation) AddToolCall(callID string, name string, input string) {
	c.add(RoleAssistant, "", callID, name, input, "")
}

func (c *Conversation) AddToolResult(callID string, output string) {
	c.add(RoleTool, output, callID, "", "", output)
}

func (c *Conversation) add(role string, content string, toolCallID string, toolName string, toolInput string, toolOutput string) {
	now := time.Now().UTC().Format(time.RFC3339)
	c.messages = append(c.messages, models.Message{
		ID:             models.NewID(),
		ConversationID: c.id,
		Role:           role,
		Content:        content,
		ToolCallID:     toolCallID,
		ToolName:       toolName,
		ToolInput:      toolInput,
		ToolOutput:     toolOutput,
		CreatedAt:      now,
	})
}
```

Update existing `AddTool` to pass empty string for `toolCallID`:

```go
func (c *Conversation) AddTool(name string, input string, output string) {
	c.add(RoleTool, output, "", name, input, output)
}
```

Update `AddUser` and `AddAssistant` similarly:

```go
func (c *Conversation) AddUser(content string) {
	c.add(RoleUser, content, "", "", "", "")
}

func (c *Conversation) AddAssistant(content string) {
	c.add(RoleAssistant, content, "", "", "", "")
}
```

- [ ] **Step 4: Run all conversation tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/conversation -v
```

Expected: PASS (all existing + new tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/models/models.go internal/conversation/conversation.go internal/conversation/conversation_test.go
git commit -m "feat: add tool call ID tracking to models and conversation"
```

---

## Task 3: Anthropic Provider

**Files:**

- Create: `internal/llm/anthropic.go`
- Create: `internal/llm/anthropic_test.go`

**Purpose:** Implement the `Provider` interface for the Anthropic Messages API, including non-streaming `Chat` and streaming `ChatStream` with tool-call support.

- [ ] **Step 1: Write failing Anthropic provider tests**

Create `internal/llm/anthropic_test.go`:

```go
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
		writeSSE(w, flusher, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_123","name":"shell/run","input":{}}}`)
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
	if toolCalls[0].ID != "toolu_123" || toolCalls[0].Name != "shell/run" {
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
```

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestAnthropic -v
```

Expected: FAIL with undefined `AnthropicConfig`, `NewAnthropicProvider`, etc.

- [ ] **Step 3: Implement Anthropic provider**

Create `internal/llm/anthropic.go`:

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pockyHM/conan/pkg/models"
)

type AnthropicConfig struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &AnthropicProvider{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := p.buildBody(req, false)
	if err != nil {
		return nil, err
	}
	httpResp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic api status %d: %s", httpResp.StatusCode, data)
	}
	return p.parseResponse(data)
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("anthropic api status %d: %s", httpResp.StatusCode, data)
	}
	ch := make(chan ChatEvent, 20)
	go p.handleStream(httpResp.Body, ch)
	return ch, nil
}

func (p *AnthropicProvider) buildBody(req *ChatRequest, stream bool) ([]byte, error) {
	msgs := messagesToAnthropic(req.Messages)
	tools := toolsToAnthropic(req.Tools)
	body := map[string]any{
		"model":      p.model,
		"max_tokens": maxTokensOrDefault(req.MaxTokens),
		"messages":   msgs,
	}
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	return json.Marshal(body)
}

func (p *AnthropicProvider) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	return p.client.Do(req)
}

func (p *AnthropicProvider) parseResponse(data []byte) (*ChatResponse, error) {
	var resp struct {
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	var text string
	var toolCalls []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}
	return &ChatResponse{
		Message:    models.Message{Role: "assistant", Content: text},
		ToolCalls:  toolCalls,
		StopReason: anthropicStopReason(resp.StopReason),
	}, nil
}

func (p *AnthropicProvider) handleStream(reader io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer reader.Close()

	type toolCallBuilder struct {
		id   string
		name string
		buf  strings.Builder
	}
	var toolCalls []toolCallBuilder

	for sse := range ReadSSE(reader) {
		var ev struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input struct{} `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(sse.Data), &ev); err != nil {
			ch <- ErrorEvent{Err: fmt.Errorf("parse sse data: %w", err)}
			return
		}

		switch ev.Type {
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				ch <- TextDeltaEvent{Delta: ev.Delta.Text}
			case "input_json_delta":
				for len(toolCalls) <= ev.Index {
					toolCalls = append(toolCalls, toolCallBuilder{})
				}
				toolCalls[ev.Index].buf.WriteString(ev.Delta.PartialJSON)
			}

		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				for len(toolCalls) <= ev.Index {
					toolCalls = append(toolCalls, toolCallBuilder{})
				}
				toolCalls[ev.Index].id = ev.ContentBlock.ID
				toolCalls[ev.Index].name = ev.ContentBlock.Name
			}

		case "message_delta":
			if ev.Delta.StopReason != "" {
				for _, tc := range toolCalls {
					ch <- ToolCallEvent{
						ID:        tc.id,
						Name:      tc.name,
						Arguments: json.RawMessage(tc.buf.String()),
					}
				}
				ch <- StopEvent{Reason: anthropicStopReason(ev.Delta.StopReason)}
			}

		case "error":
			ch <- ErrorEvent{Err: fmt.Errorf("anthropic stream error: %s", sse.Data)}
			return
		}
	}
}

func messagesToAnthropic(msgs []models.Message) []json.RawMessage {
	var result []json.RawMessage
	i := 0
	for i < len(msgs) {
		switch msgs[i].Role {
		case "user":
			if msgs[i].ToolCallID == "" {
				result = append(result, jsonMarshal(map[string]any{
					"role":    "user",
					"content": msgs[i].Content,
				}))
			}
			i++

		case "assistant":
			var content []any
			for i < len(msgs) && msgs[i].Role == "assistant" {
				if msgs[i].Content != "" {
					content = append(content, map[string]any{
						"type": "text",
						"text": msgs[i].Content,
					})
				}
				if msgs[i].ToolCallID != "" {
					var input any
					if err := json.Unmarshal([]byte(msgs[i].ToolInput), &input); err != nil {
						input = map[string]any{}
					}
					content = append(content, map[string]any{
						"type":  "tool_use",
						"id":    msgs[i].ToolCallID,
						"name":  msgs[i].ToolName,
						"input": input,
					})
				}
				i++
			}
			result = append(result, jsonMarshal(map[string]any{
				"role":    "assistant",
				"content": content,
			}))

		case "tool":
			var content []any
			for i < len(msgs) && msgs[i].Role == "tool" {
				content = append(content, map[string]any{
					"type":        "tool_result",
					"tool_use_id": msgs[i].ToolCallID,
					"content":     msgs[i].Content,
				})
				i++
			}
			result = append(result, jsonMarshal(map[string]any{
				"role":    "user",
				"content": content,
			}))

		default:
			i++
		}
	}
	return result
}

func toolsToAnthropic(tools []ToolDef) []any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]any, len(tools))
	for i, t := range tools {
		result[i] = map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.InputSchema,
		}
	}
	return result
}

func anthropicStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return StopEndTurn
	case "tool_use":
		return StopToolUse
	case "max_tokens":
		return StopMaxTokens
	default:
		return reason
	}
}

func maxTokensOrDefault(n int) int {
	if n <= 0 {
		return 4096
	}
	return n
}

func jsonMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
```

- [ ] **Step 4: Run tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestAnthropic -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/anthropic.go internal/llm/anthropic_test.go
git commit -m "feat: add Anthropic Messages API provider with streaming"
```

---

## Task 4: OpenAI Provider

**Files:**

- Create: `internal/llm/openai.go`
- Create: `internal/llm/openai_test.go`

**Purpose:** Implement the `Provider` interface for the OpenAI Chat Completions API (also compatible with DeepSeek, Ollama, vLLM, etc.).

- [ ] **Step 1: Write failing OpenAI provider tests**

Create `internal/llm/openai_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestOpenAI -v
```

Expected: FAIL with undefined `OpenAIConfig`, `NewOpenAIProvider`.

- [ ] **Step 3: Implement OpenAI provider**

Create `internal/llm/openai.go`:

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pockyHM/conan/pkg/models"
)

type OpenAIConfig struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := p.buildBody(req, false)
	if err != nil {
		return nil, err
	}
	httpResp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai api status %d: %s", httpResp.StatusCode, data)
	}
	return p.parseResponse(data)
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("openai api status %d: %s", httpResp.StatusCode, data)
	}
	ch := make(chan ChatEvent, 20)
	go p.handleStream(httpResp.Body, ch)
	return ch, nil
}

func (p *OpenAIProvider) buildBody(req *ChatRequest, stream bool) ([]byte, error) {
	msgs := messagesToOpenAI(req.Messages)
	tools := toolsToOpenAI(req.Tools)
	body := map[string]any{
		"model":    p.model,
		"messages": msgs,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.SystemPrompt != "" {
		systemMsg := jsonMarshal(map[string]any{"role": "system", "content": req.SystemPrompt})
		msgs = append([]json.RawMessage{systemMsg}, msgs...)
		body["messages"] = msgs
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	return json.Marshal(body)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return p.client.Do(req)
}

func (p *OpenAIProvider) parseResponse(data []byte) (*ChatResponse, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai response has no choices")
	}
	choice := resp.Choices[0]
	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	return &ChatResponse{
		Message:    models.Message{Role: "assistant", Content: choice.Message.Content},
		ToolCalls:  toolCalls,
		StopReason: openaiStopReason(choice.FinishReason),
	}, nil
}

func (p *OpenAIProvider) handleStream(reader io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer reader.Close()

	type toolCallBuilder struct {
		id   string
		name string
		buf  strings.Builder
	}
	toolCalls := map[int]*toolCallBuilder{}

	for sse := range ReadSSE(reader) {
		if sse.Data == "[DONE]" {
			return
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
			ch <- ErrorEvent{Err: fmt.Errorf("parse openai chunk: %w", err)}
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			ch <- TextDeltaEvent{Delta: choice.Delta.Content}
		}
		for _, tc := range choice.Delta.ToolCalls {
			if toolCalls[tc.Index] == nil {
				toolCalls[tc.Index] = &toolCallBuilder{}
			}
			if tc.ID != "" {
				toolCalls[tc.Index].id = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[tc.Index].name = tc.Function.Name
			}
			toolCalls[tc.Index].buf.WriteString(tc.Function.Arguments)
		}
		if choice.FinishReason != nil {
			for i := 0; i < len(toolCalls); i++ {
				if tc, ok := toolCalls[i]; ok {
					ch <- ToolCallEvent{
						ID:        tc.id,
						Name:      tc.name,
						Arguments: json.RawMessage(tc.buf.String()),
					}
				}
			}
			ch <- StopEvent{Reason: openaiStopReason(*choice.FinishReason)}
		}
	}
}

func messagesToOpenAI(msgs []models.Message) []json.RawMessage {
	var result []json.RawMessage
	i := 0
	for i < len(msgs) {
		switch msgs[i].Role {
		case "user":
			result = append(result, jsonMarshal(map[string]any{
				"role":    "user",
				"content": msgs[i].Content,
			}))
			i++

		case "assistant":
			text := ""
			var toolCalls []any
			for i < len(msgs) && msgs[i].Role == "assistant" {
				if msgs[i].Content != "" {
					text = msgs[i].Content
				}
				if msgs[i].ToolCallID != "" {
					toolCalls = append(toolCalls, map[string]any{
						"id":   msgs[i].ToolCallID,
						"type": "function",
						"function": map[string]any{
							"name":      msgs[i].ToolName,
							"arguments": msgs[i].ToolInput,
						},
					})
				}
				i++
			}
			entry := map[string]any{"role": "assistant", "content": text}
			if len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
			}
			result = append(result, jsonMarshal(entry))

		case "tool":
			result = append(result, jsonMarshal(map[string]any{
				"role":         "tool",
				"tool_call_id": msgs[i].ToolCallID,
				"content":      msgs[i].Content,
			}))
			i++

		default:
			i++
		}
	}
	return result
}

func toolsToOpenAI(tools []ToolDef) []any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]any, len(tools))
	for i, t := range tools {
		result[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		}
	}
	return result
}

func openaiStopReason(reason string) string {
	switch reason {
	case "stop":
		return StopEndTurn
	case "tool_calls":
		return StopToolUse
	case "length":
		return StopMaxTokens
	default:
		return reason
	}
}
```

- [ ] **Step 4: Run tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestOpenAI -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/openai.go internal/llm/openai_test.go
git commit -m "feat: add OpenAI Chat Completions API provider with streaming"
```

---

## Task 5: Provider Factory

**Files:**

- Create: `internal/llm/factory.go`
- Create: `internal/llm/factory_test.go`

**Purpose:** Create a `Provider` from a model config by name, so `cmd/conan` can simply call `NewProvider(models, "claude-sonnet")`.

- [ ] **Step 1: Write failing factory tests**

Create `internal/llm/factory_test.go`:

```go
package llm

import (
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestNewProviderByModelName(t *testing.T) {
	configs := []configschema.ModelConfig{
		{Name: "claude", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-ant"},
		{Name: "gpt", Type: "openai", Model: "gpt-4.1", APIKey: "sk-oai"},
		{Name: "local", Type: "openai", Model: "qwen3:32b", Endpoint: "http://localhost:11434/v1"},
	}

	p, name, err := NewProvider(configs, "claude")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if name != "claude" {
		t.Fatalf("name = %q", name)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Fatalf("expected AnthropicProvider, got %T", p)
	}

	p, name, err = NewProvider(configs, "gpt")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Fatalf("expected OpenAIProvider, got %T", p)
	}

	p, name, err = NewProvider(configs, "local")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Fatalf("expected OpenAIProvider for local, got %T", p)
	}
}

func TestNewProviderReturnsErrorForUnknownModel(t *testing.T) {
	_, _, err := NewProvider([]configschema.ModelConfig{}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestNewProviderDefaultModel(t *testing.T) {
	configs := []configschema.ModelConfig{
		{Name: "default-model", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "key"},
	}
	p, name, err := NewProvider(configs, "")
	if err != nil {
		t.Fatalf("NewProvider with empty name: %v", err)
	}
	if name != "default-model" {
		t.Fatalf("name = %q", name)
	}
	if p == nil {
		t.Fatal("provider should not be nil")
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestNewProvider -v
```

Expected: FAIL with undefined `NewProvider`.

- [ ] **Step 3: Implement provider factory**

Create `internal/llm/factory.go`:

```go
package llm

import (
	"fmt"

	"github.com/pockyHM/conan/pkg/configschema"
)

func NewProvider(models []configschema.ModelConfig, name string) (Provider, string, error) {
	var cfg *configschema.ModelConfig
	for i := range models {
		if models[i].Name == name {
			cfg = &models[i]
			break
		}
	}
	if cfg == nil && len(models) > 0 {
		cfg = &models[0]
	}
	if cfg == nil {
		return nil, "", fmt.Errorf("no model configured")
	}
	switch cfg.Type {
	case "anthropic":
		return NewAnthropicProvider(AnthropicConfig{
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
			BaseURL: cfg.Endpoint,
		}), cfg.Name, nil
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
			BaseURL: cfg.Endpoint,
		}), cfg.Name, nil
	default:
		return nil, "", fmt.Errorf("unknown model type: %s", cfg.Type)
	}
}
```

- [ ] **Step 4: Run tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/llm -run TestNewProvider -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/factory.go internal/llm/factory_test.go
git commit -m "feat: add provider factory for model config lookup"
```

---

## Task 6: TUI Streaming & Tool Dispatch

**Files:**

- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

**Purpose:** Refactor the TUI model to accept a provider, conversation manager, MCP clients, and tool definitions. Implement streaming text display and the agentic tool-call dispatch loop using Bubble Tea Cmds.

- [ ] **Step 1: Write failing TUI streaming tests**

Replace `internal/tui/model_test.go` entirely:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

type fakeProvider struct {
	streamCh chan llm.ChatEvent
}

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
	result := &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("file1\nfile2")}}
	next, _ := model.Update(toolResultMsg{Call: call, Result: result})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("view missing tool name:\n%s", view)
	}
}
```

Add `context` to the test imports.

- [ ] **Step 2: Run tests to verify RED**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/tui -v
```

Expected: FAIL with undefined types like `streamEventMsg`, `streamDoneMsg`, `toolResultMsg`, or field `streaming`.

- [ ] **Step 3: Rewrite TUI model**

Replace `internal/tui/model.go` entirely:

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

type ModelConfig struct {
	Cluster  string
	Model    string
	Provider llm.Provider
	Conv     *conversation.Conversation
	Clients  map[string]*mcp.Client
	Tools    []llm.ToolDef
}

type chatMsg struct {
	role       string
	content    string
	toolName   string
	toolInput  string
	toolOutput string
}

type Model struct {
	cluster    string
	model      string
	provider   llm.Provider
	conv       *conversation.Conversation
	clients    map[string]*mcp.Client
	tools      []llm.ToolDef

	input      string
	messages   []chatMsg
	status     string
	streaming  bool
	streamBuf  string
	streamCh   <-chan llm.ChatEvent

	width      int
	height     int
}

func NewModel(cfg ModelConfig) Model {
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	return Model{
		cluster:  cfg.Cluster,
		model:    cfg.Model,
		provider: cfg.Provider,
		conv:     cfg.Conv,
		clients:  cfg.Clients,
		tools:    cfg.Tools,
		status:   "Ready",
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

// Bubble Tea internal messages for streaming

type streamReadyMsg struct {
	ch  <-chan llm.ChatEvent
	err error
}

type streamEventMsg struct {
	Event llm.ChatEvent
}

type streamDoneMsg struct{}

type toolResultMsg struct {
	Call   llm.ToolCall
	Result *mcpproto.ToolResult
	Err    error
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case streamReadyMsg:
		if msg.err != nil {
			m.streaming = false
			m.status = "Error: " + msg.err.Error()
			return m, nil
		}
		m.streamCh = msg.ch
		return m, m.waitForEvent()

	case streamEventMsg:
		switch e := msg.Event.(type) {
		case llm.TextDeltaEvent:
			m.streamBuf += e.Delta
		case llm.ToolCallEvent:
			m.messages = append(m.messages, chatMsg{
				role:      "tool",
				toolName:  e.Name,
				toolInput: string(e.Arguments),
			})
			if m.conv != nil {
				m.conv.AddToolCall(e.ID, e.Name, string(e.Arguments))
			}
			return m, m.dispatchTool(llm.ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments})
		case llm.StopEvent:
			if m.conv != nil {
				m.conv.AddAssistant(m.streamBuf)
			}
			if m.streamBuf != "" {
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf})
			}
			m.streamBuf = ""
			if e.Reason == llm.StopToolUse {
				m.status = "Running tool..."
				return m, m.waitForEvent()
			}
			m.streaming = false
			m.status = "Ready"
			return m, nil
		case llm.ErrorEvent:
			m.streaming = false
			m.status = "Stream error: " + e.Err.Error()
			return m, nil
		}
		return m, m.waitForEvent()

	case streamDoneMsg:
		m.streaming = false
		m.status = "Stream ended"
		return m, nil

	case toolResultMsg:
		var output string
		if msg.Err != nil {
			output = "Error: " + msg.Err.Error()
		} else {
			for _, block := range msg.Result.Content {
				output += block.Text
			}
		}
		// Update last tool message with output
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == "tool" && m.messages[i].toolOutput == "" {
				m.messages[i].toolOutput = output
				break
			}
		}
		if m.conv != nil {
			m.conv.AddToolResult(msg.Call.ID, output)
		}
		// Continue agentic loop
		return m, m.startStream()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Conan | Cluster: %s | Model: %s", m.cluster, m.model),
	)

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

	// Show streaming buffer
	if m.streaming && m.streamBuf != "" {
		bodyParts = append(bodyParts, "Conan: "+m.streamBuf+"...")
	}

	body := strings.Join(bodyParts, "\n\n")
	if body == "" {
		body = "No messages yet. Type a message or /help."
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n> %s", header, body, m.status, m.input)
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	if input == "" {
		return m, nil
	}
	if cmd, ok := ParseSlashCommand(input); ok {
		m = m.applyCommand(cmd)
		if cmd.Kind == CommandExit {
			return m, tea.Quit
		}
		return m, nil
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

func (m Model) applyCommand(cmd SlashCommand) Model {
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
		m.status = "Interactive node selection is not implemented yet"
	default:
		m.status = "Unknown command: /" + cmd.Arg
	}
	return m
}

// Bubble Tea Cmd functions

func (m Model) startStream() tea.Cmd {
	if m.provider == nil {
		return nil
	}
	provider := m.provider
	req := &llm.ChatRequest{
		SystemPrompt: buildSystemPrompt(m.cluster),
		Messages:     m.conv.Messages(),
		Tools:        m.tools,
	}
	return func() tea.Msg {
		ch, err := provider.ChatStream(context.Background(), req)
		return streamReadyMsg{ch: ch, err: err}
	}
}

func (m Model) waitForEvent() tea.Cmd {
	ch := m.streamCh
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return streamEventMsg{Event: event}
	}
}

func (m Model) dispatchTool(call llm.ToolCall) tea.Cmd {
	clients := m.clients
	return func() tea.Msg {
		for _, client := range clients {
			result, err := client.CallTool(context.Background(), call.Name, call.Arguments)
			return toolResultMsg{Call: call, Result: result, Err: err}
		}
		return toolResultMsg{Call: call, Err: fmt.Errorf("no agent available")}
	}
}

func buildSystemPrompt(cluster string) string {
	return fmt.Sprintf("You are Conan, an AI operations assistant. Cluster: %s. Help the user manage their infrastructure.", cluster)
}
```

- [ ] **Step 4: Run tests to verify GREEN**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./internal/tui -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: refactor TUI model with streaming and tool dispatch loop"
```

---

## Task 7: Wire CLI Command

**Files:**

- Modify: `cmd/conan/main.go`

**Purpose:** Update the `conan tui` command to create a provider from config, load MCP clients for cluster nodes, fetch agent tools, and pass everything to the TUI model.

- [ ] **Step 1: Run existing CLI tests to verify baseline**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./cmd/conan -v
```

Expected: PASS (existing tests should still work).

- [ ] **Step 2: Update `conan tui` command in `cmd/conan/main.go`**

Add imports at the top of `main.go`:

```go
"github.com/pockyHM/conan/internal/conversation"
"github.com/pockyHM/conan/internal/llm"
```

Replace the `tuiCmd` definition with:

```go
tuiCmd := &cobra.Command{
	Use:   "tui",
	Short: "Start the interactive TUI",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		loader := cfgloader.NewLoader(home)
		global, err := loader.LoadGlobal()
		if err != nil {
			return err
		}
		selectedCluster := clusterName
		if selectedCluster == "" {
			selectedCluster = global.DefaultCluster
		}

		provider, modelName, err := llm.NewProvider(global.Models, global.DefaultModel)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
		}

		var clients map[string]*mcp.Client
		var agentTools []llm.ToolDef
		if selectedCluster != "" {
			cluster, err := loader.LoadCluster(selectedCluster)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load cluster %s: %v\n", selectedCluster, err)
			} else {
				clients = make(map[string]*mcp.Client)
				for _, node := range cluster.Nodes {
					url := mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS)
					clients[node.Name] = mcp.NewClient(mcp.Config{
						BaseURL: url,
						Token:   node.Agent.Token,
					})
				}
				// Fetch tools from first available agent
				for _, client := range clients {
					tools, err := client.ListTools(cmd.Context())
					if err == nil {
						for _, t := range tools {
							agentTools = append(agentTools, llm.ToolDef(t))
						}
					}
					break
				}
			}
		}

		conv := conversation.New(selectedCluster, nil, modelName)
		model := tui.NewModel(tui.ModelConfig{
			Cluster:  selectedCluster,
			Model:    modelName,
			Provider: provider,
			Conv:     conv,
			Clients:  clients,
			Tools:    agentTools,
		})
		return runTeaProgram(model, cmd.InOrStdin(), cmd.OutOrStdout())
	},
}
```

- [ ] **Step 3: Run CLI tests**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./cmd/conan -v
```

Expected: PASS.

- [ ] **Step 4: Run full test suite**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./... 2>&1 | grep -v "SKIP"
```

Expected: PASS for all packages.

- [ ] **Step 5: Build and commit**

```bash
make build && git add cmd/conan/main.go go.mod go.sum && git commit -m "feat: wire LLM providers and MCP clients into TUI"
```

---

## Task 8: Update Documentation

**Files:**

- Modify: `CLAUDE.md`

**Purpose:** Update the implementation progress section to reflect the completed LLM integration and note what remains.

- [ ] **Step 1: Update implementation progress**

In `CLAUDE.md`, replace the Phase 3B section:

```markdown
### Phase 3B: LLM Integration & Streaming — DONE

Anthropic + OpenAI providers, streaming text in TUI, agentic tool-call dispatch loop.

- `internal/llm/anthropic.go` — Anthropic Messages API provider (Chat + ChatStream)
- `internal/llm/openai.go` — OpenAI Chat Completions API provider (Chat + ChatStream)
- `internal/llm/sse.go` — Shared SSE stream reader
- `internal/llm/factory.go` — Provider factory (model name → Provider)
- `pkg/models/models.go` — Added ToolCallID to Message
- `internal/conversation/conversation.go` — Added AddToolCall, AddToolResult methods
- `internal/tui/model.go` — Refactored with streaming display + tool dispatch loop

### Phase 3C: Node Selector & Multi-node — NEXT

Interactive `/nodes` multi-select, concurrent multi-node tool dispatch, collapsed tool-call visualization.

### Phase 3D: Security Review — TODO

Whitelist pre-check, model risk assessment, confirmation prompts.

### Phase 3E: Memory & Session — TODO

SQLite memory store, MEMORY.md rules, session archive and `/resume`.
```

Keep the Phase 1, 2, 3A DONE sections unchanged.

- [ ] **Step 2: Verify markdown**

```bash
python3 -c "
from pathlib import Path
text = Path('CLAUDE.md').read_text()
assert 'Phase 1: Foundation & Agent — DONE' in text
assert 'Phase 2: CLI Core — DONE' in text
assert 'Phase 3A: TUI Shell & Slash Commands — DONE' in text
assert 'Phase 3B: LLM Integration & Streaming — DONE' in text
assert 'Phase 3C: Node Selector & Multi-node — NEXT' in text
print('OK')
"
```

Expected: `OK`.

- [ ] **Step 3: Run final verification**

```bash
GOROOT=/opt/homebrew/opt/go/libexec go test ./... && GOROOT=/opt/homebrew/opt/go/libexec make build
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update Phase 3B LLM integration progress"
```

---

## Self-Review

**Spec coverage:**

- LLM providers (Anthropic + OpenAI) → Tasks 3, 4, 5
- Streaming integration → Task 6
- Tool call dispatch → Task 6
- Provider factory → Task 5
- Models/conversation for tool calls → Task 2
- SSE reader → Task 1
- Wire in CLI → Task 7

No spec requirements from Phase 3B-1 are missing from this plan. Node selector, security, memory, and session resume are explicitly scoped out for subsequent plans.

**Placeholder scan:** No TBD, TODO, or "implement later" placeholders. All steps contain complete code.

**Type consistency:**

- `llm.ToolCall{ID, Name, Arguments}` used consistently across providers, TUI, and MCP dispatch
- `streamEventMsg`, `streamDoneMsg`, `toolResultMsg`, `streamReadyMsg` defined in Task 6 and used in test and implementation
- `chatMsg{role, content, toolName, toolInput, toolOutput}` defined and used consistently in View
- `conversation.AddToolCall(callID, name, input)` and `conversation.AddToolResult(callID, output)` match the call sites
- `models.Message.ToolCallID` added in Task 2, used by providers in Tasks 3/4 and conversation in Task 6
- `llm.NewProvider` returns `(Provider, string, error)` matching the factory test and cmd/conan usage
