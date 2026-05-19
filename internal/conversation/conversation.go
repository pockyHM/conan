package conversation

import (
	"time"
	"unicode/utf8"

	"github.com/pockyHM/conan/pkg/models"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type Conversation struct {
	id        string
	cluster   string
	nodes     []string
	model     string
	messages  []models.Message
	createdAt time.Time
}

func New(cluster string, nodes []string, model string) *Conversation {
	return &Conversation{
		id:        models.NewID(),
		cluster:   cluster,
		nodes:     append([]string(nil), nodes...),
		model:     model,
		createdAt: time.Now(),
	}
}

func (c *Conversation) ID() string {
	return c.id
}

func (c *Conversation) AddUser(content string) {
	c.add(RoleUser, content, "", "", "")
}

func (c *Conversation) AddAssistant(content string) {
	c.add(RoleAssistant, content, "", "", "")
}

func (c *Conversation) AddTool(name string, input string, output string) {
	c.add(RoleTool, output, name, input, output)
}

func (c *Conversation) Messages() []models.Message {
	return append([]models.Message(nil), c.messages...)
}

func (c *Conversation) Context(maxChars int) []models.Message {
	if maxChars <= 0 {
		return c.Messages()
	}
	total := 0
	start := len(c.messages)
	for i := len(c.messages) - 1; i >= 0; i-- {
		cost := utf8.RuneCountInString(c.messages[i].Content) + utf8.RuneCountInString(c.messages[i].ToolInput) + utf8.RuneCountInString(c.messages[i].ToolName)
		if total+cost > maxChars && start != len(c.messages) {
			break
		}
		total += cost
		start = i
	}
	return append([]models.Message(nil), c.messages[start:]...)
}

func (c *Conversation) Clear() {
	c.messages = nil
}

func (c *Conversation) add(role string, content string, toolName string, toolInput string, toolOutput string) {
	now := time.Now().UTC().Format(time.RFC3339)
	c.messages = append(c.messages, models.Message{
		ID:             models.NewID(),
		ConversationID: c.id,
		Role:           role,
		Content:        content,
		ToolName:       toolName,
		ToolInput:      toolInput,
		ToolOutput:     toolOutput,
		CreatedAt:      now,
	})
}
