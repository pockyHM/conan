package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

type CurlBuilder interface {
	CurlCommand(req *ChatRequest) string
}

type VisionProvider interface {
	DescribeImages(ctx context.Context, req *VisionRequest) (*VisionResponse, error)
	SupportsVision() bool
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
	Thinking     *bool
}

type ChatResponse struct {
	Message    models.Message
	ToolCalls  []ToolCall
	StopReason string
}

type VisionRequest struct {
	Prompt    string
	Images    []ImageInput
	MaxTokens int
}

type ImageInput struct {
	Name      string
	MediaType string
	Data      []byte
}

type VisionResponse struct {
	Summary string
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

type ReasoningDeltaEvent struct {
	Delta string
}

func (ReasoningDeltaEvent) chatEvent() {}

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

func sanitizeChatRequest(req *ChatRequest) *ChatRequest {
	msgs := make([]models.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = m
		if m.Role == "user" && m.ToolCallID == "" {
			msgs[i].Content = "hello"
		}
	}
	return &ChatRequest{
		SystemPrompt: req.SystemPrompt,
		Messages:     msgs,
		Tools:        req.Tools,
		MaxTokens:    req.MaxTokens,
		Thinking:     req.Thinking,
	}
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func buildCurl(endpoint string, headers map[string]string, body []byte) string {
	var sb strings.Builder
	sb.WriteString("curl -X POST ")
	sb.WriteString(shellEscape(endpoint))
	for k, v := range headers {
		sb.WriteString(" \\\n  -H ")
		sb.WriteString(shellEscape(k + ": " + v))
	}
	sb.WriteString(" \\\n  -d ")
	sb.WriteString(shellEscape(string(body)))
	return sb.String()
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
