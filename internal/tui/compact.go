package tui

import (
	"strings"
	"time"

	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/pkg/models"
)

const compactTailMessages = 4

type compactArchive struct {
	ConversationID string           `json:"conversation_id"`
	Cluster        string           `json:"cluster"`
	Model          string           `json:"model"`
	CreatedAt      string           `json:"created_at"`
	Messages       []models.Message `json:"messages"`
}

func compactSystemPrompt(focus string) string {
	var b strings.Builder
	b.WriteString(strings.Join([]string{
		"You are compacting an agentic coding and operations conversation for future continuation.",
		"Do not merely shorten the transcript. Produce a structured handoff summary that preserves working state.",
		"Include only facts supported by the transcript.",
		"",
		"Preserve these sections when applicable:",
		"- Current user goal and explicit constraints",
		"- Decisions made and alternatives rejected",
		"- Files, commands, nodes, clusters, tools, and configuration mentioned",
		"- Code changes already made and tests or verification already run",
		"- Tool results, failures, diagnostics, and unresolved risks",
		"- Pending tasks and the next concrete step",
		"",
		"Keep secrets and credentials out of the summary. If a secret appeared, say only that a secret was present and redacted.",
	}, "\n"))
	if strings.TrimSpace(focus) != "" {
		b.WriteString("\n\nUser focus instructions:\n")
		b.WriteString(strings.TrimSpace(focus))
	}
	return b.String()
}

func buildCompactedMessages(convID string, summary string, oldMessages []models.Message) []models.Message {
	now := time.Now().UTC().Format(time.RFC3339)
	result := []models.Message{{
		ID:             models.NewID(),
		ConversationID: convID,
		Role:           conversation.RoleUser,
		Content:        "Previous conversation compacted. Continue from this state instead of asking for the omitted transcript.\n\nSummary:\n" + strings.TrimSpace(summary),
		CreatedAt:      now,
	}}
	result = append(result, compactTail(oldMessages, compactTailMessages)...)
	for i := range result {
		if result[i].ConversationID == "" {
			result[i].ConversationID = convID
		}
		if result[i].CreatedAt == "" {
			result[i].CreatedAt = now
		}
	}
	return result
}

func compactTail(messages []models.Message, limit int) []models.Message {
	if limit <= 0 {
		return nil
	}
	tail := make([]models.Message, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(tail) < limit; i-- {
		msg := messages[i]
		if msg.ToolCallID != "" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Role != conversation.RoleUser && msg.Role != conversation.RoleAssistant {
			continue
		}
		tail = append(tail, msg)
	}
	for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 {
		tail[i], tail[j] = tail[j], tail[i]
	}
	return tail
}
