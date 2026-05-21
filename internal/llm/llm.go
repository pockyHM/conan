package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
)

type Provider interface {
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error)
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http %d", e.Status)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}

type ChatRequest struct {
	SystemPrompt string
	Messages     []models.Message
	Tools        []ToolDef
	MaxTokens    int
}

type ChatResponse struct {
	Message    models.Message
	ToolCalls  []ToolCall
	StopReason string
}

type ToolDef mcpproto.ToolDefinition

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ChatEvent interface {
	chatEvent()
}

type TextDeltaEvent struct {
	Delta string
}

func (TextDeltaEvent) chatEvent() {}

type ToolCallEvent struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

func (ToolCallEvent) chatEvent() {}

type StopEvent struct {
	Reason string
}

func (StopEvent) chatEvent() {}

type ErrorEvent struct {
	Err error
}

func (ErrorEvent) chatEvent() {}
