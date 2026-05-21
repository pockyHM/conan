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
		return nil, &httpError{Status: httpResp.StatusCode, Body: strings.TrimSpace(string(data))}
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
		return nil, &httpError{Status: httpResp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	ch := make(chan ChatEvent, 20)
	go p.handleStream(httpResp.Body, ch)
	return ch, nil
}

func (p *AnthropicProvider) buildBody(req *ChatRequest, stream bool) ([]byte, error) {
	msgs := anthropicMessages(req.Messages)
	tools := anthropicTools(req.Tools)
	body := map[string]any{
		"model":      p.model,
		"max_tokens": anthropicMaxTokens(req.MaxTokens),
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
		Content []struct {
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
				Type  string   `json:"type"`
				Text  string   `json:"text"`
				ID    string   `json:"id"`
				Name  string   `json:"name"`
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
					if tc.id == "" {
						continue
					}
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

func anthropicMessages(msgs []models.Message) []json.RawMessage {
	var result []json.RawMessage
	i := 0
	for i < len(msgs) {
		switch msgs[i].Role {
		case "user":
			if msgs[i].ToolCallID == "" {
				result = append(result, anthropicJSONMarshal(map[string]any{
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
			result = append(result, anthropicJSONMarshal(map[string]any{
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
			result = append(result, anthropicJSONMarshal(map[string]any{
				"role":    "user",
				"content": content,
			}))

		default:
			i++
		}
	}
	return result
}

func anthropicTools(tools []ToolDef) []any {
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

func anthropicMaxTokens(n int) int {
	if n <= 0 {
		return 4096
	}
	return n
}

func anthropicJSONMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
