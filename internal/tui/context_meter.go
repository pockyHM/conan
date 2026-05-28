package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

const autoCompactContextPercent = 90

var modelContextLimits = map[string]int{
	"gpt-5.5":                 1050000,
	"gpt-5.5-2026-04-23":      1050000,
	"gpt-5.5-pro":             1050000,
	"gpt-5.5-pro-2026-04-23":  1050000,
	"gpt-5.4":                 1050000,
	"gpt-5.4-2026-03-05":      1050000,
	"gpt-5.4-pro":             1050000,
	"gpt-5.4-mini":            400000,
	"gpt-5.4-mini-2026-03-17": 400000,
	"gpt-5.4-nano":            400000,
	"gpt-5.2":                 400000,
	"gpt-5.1":                 400000,
	"gpt-5.1-2025-11-13":      400000,
	"gpt-5.1-codex":           400000,
	"gpt-5.1-codex-max":       400000,
	"gpt-5-codex":             400000,
	"gpt-5":                   400000,
	"gpt-5-2025-08-07":        400000,
	"gpt-5-mini":              400000,
	"gpt-5-nano":              400000,
	"chat-latest":             400000,
	"gpt-4.1":                 1047576,
	"gpt-4.1-2025-04-14":      1047576,
	"gpt-4.1-mini":            1047576,
	"gpt-4.1-nano":            1047576,
	"gpt-4o":                  128000,
	"gpt-4o-mini":             128000,
	"o3":                      200000,
	"o3-mini":                 200000,
	"o4-mini":                 200000,

	"claude-sonnet-4-6":          200000,
	"claude-sonnet-4":            200000,
	"claude-sonnet-4-20250514":   200000,
	"claude-opus-4":              200000,
	"claude-opus-4-20250514":     200000,
	"claude-opus-4-1":            200000,
	"claude-opus-4-1-20250805":   200000,
	"claude-3-5-sonnet":          200000,
	"claude-3-7-sonnet":          200000,
	"claude-3-7-sonnet-20250219": 200000,
	"claude-3-7-sonnet-latest":   200000,
	"claude-3-5-haiku":           200000,
	"claude-3-5-haiku-20241022":  200000,
	"claude-3-5-haiku-latest":    200000,

	"qwen3.7-max":              1000000,
	"qwen3.7-max-2026-05-20":   1000000,
	"qwen3.7-max-preview":      1000000,
	"qwen3.6-plus":             1000000,
	"qwen3.6-plus-2026-04-02":  1000000,
	"qwen3.6-flash":            1000000,
	"qwen3.6-flash-2026-04-16": 1000000,
	"qwen3.6-max-preview":      256000,
	"qwen3.5-plus":             1000000,
	"qwen3.5-flash":            1000000,
	"qwen3.5-397b-a17b":        256000,
	"qwen3.5-122b-a10b":        256000,
	"qwen3.5-27b":              256000,
	"qwen-max":                 128000,
	"qwen-plus":                1000000,
	"qwen-flash":               1000000,
	"qwen-turbo":               1000000,
	"qwq-plus":                 128000,

	"kimi-k2.6":                 256000,
	"kimi-k2.5":                 256000,
	"kimi-k2-0905":              256000,
	"kimi-k2-0905-preview":      256000,
	"kimi-k2-turbo-preview":     256000,
	"kimi-k2-thinking":          256000,
	"kimi-k2-thinking-turbo":    256000,
	"moonshot-kimi-k2-instruct": 256000,

	"deepseek-v4-pro":   1000000,
	"deepseek-v4-flash": 1000000,
	"deepseek-chat":     1000000,
	"deepseek-reasoner": 1000000,
	"deepseek-v3.2":     64000,

	"glm-4.5":     128000,
	"glm-4.5-air": 128000,
}

type contextMeter struct {
	Used  int
	Limit int
	Known bool
}

func lookupModelContextLimit(model string) (int, bool) {
	limit, ok := modelContextLimits[strings.ToLower(strings.TrimSpace(model))]
	return limit, ok
}

func (m Model) currentModelID() string {
	for _, cfg := range m.modelConfigs {
		if cfg.Name == m.model {
			if strings.TrimSpace(cfg.Model) != "" {
				return cfg.Model
			}
			return cfg.Name
		}
	}
	return m.model
}

func (m Model) currentContextMeter() contextMeter {
	used := m.estimatedCurrentContextTokens()
	limit, known := lookupModelContextLimit(m.currentModelID())
	return contextMeter{Used: used, Limit: limit, Known: known}
}

func (m Model) shouldAutoCompactNow() bool {
	meter := m.currentContextMeter()
	if !meter.Known || meter.Limit <= 0 {
		return false
	}
	if m.conv == nil || len(m.conv.Messages()) <= compactTailMessages {
		return false
	}
	return meter.Used*100 >= meter.Limit*autoCompactContextPercent
}

func (m Model) canAutoCompactForContextLimit() bool {
	return m.provider != nil && m.conv != nil && len(m.conv.Messages()) > compactTailMessages && !m.autoCompactRetried
}

func (m Model) estimatedCurrentContextTokens() int {
	var total int
	total += estimateTextTokens(m.buildSystemPromptWithMemory())
	if m.conv != nil {
		total += estimateMessagesTokens(m.conv.Messages())
	}
	if strings.TrimSpace(m.input) != "" {
		total += estimateTextTokens(m.input) + 4
	}
	for _, tool := range m.availableToolDefs() {
		if data, err := json.Marshal(tool); err == nil {
			total += estimateTextTokens(string(data))
		} else {
			total += estimateTextTokens(tool.Name + " " + tool.Description)
		}
	}
	return total
}

func estimateMessagesTokens(messages []models.Message) int {
	total := 0
	for _, msg := range messages {
		total += 4
		total += estimateTextTokens(msg.Role)
		total += estimateTextTokens(msg.Content)
		total += estimateTextTokens(msg.ToolCallID)
		total += estimateTextTokens(msg.ToolName)
		total += estimateTextTokens(msg.ToolInput)
		total += estimateTextTokens(msg.ToolOutput)
	}
	return total
}

func estimateChatRequestTokens(req *llm.ChatRequest) int {
	if req == nil {
		return 0
	}
	total := estimateTextTokens(req.SystemPrompt) + estimateMessagesTokens(req.Messages)
	for _, tool := range req.Tools {
		if data, err := json.Marshal(tool); err == nil {
			total += estimateTextTokens(string(data))
		}
	}
	return total
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	asciiChars := 0
	nonASCII := 0
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if r <= 127 {
			asciiChars++
		} else {
			nonASCII++
		}
	}
	return (asciiChars+3)/4 + nonASCII
}

func contextMeterPercent(meter contextMeter) int {
	if !meter.Known || meter.Limit <= 0 {
		return 0
	}
	percent := meter.Used * 100 / meter.Limit
	if percent > 100 {
		return 100
	}
	if percent < 0 {
		return 0
	}
	return percent
}

func formatContextCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

func isContextLimitError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	needles := []string{
		"context length",
		"context_length",
		"context limit",
		"maximum context",
		"max context",
		"token limit",
		"too many tokens",
		"input is too long",
		"prompt is too long",
		"maximum number of tokens",
	}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
