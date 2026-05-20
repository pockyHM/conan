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

// jsonMarshal marshals v to JSON, panicking on error.
// Safe for use with struct/map types that cannot fail to marshal.
func jsonMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
