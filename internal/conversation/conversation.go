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

func Restore(id string, cluster string, nodes []string, model string, messages []models.Message) *Conversation {
	if id == "" {
		id = models.NewID()
	}
	c := &Conversation{
		id:        id,
		cluster:   cluster,
		nodes:     append([]string(nil), nodes...),
		model:     model,
		createdAt: time.Now(),
	}
	c.ReplaceMessages(messages)
	return c
}

func NewWithMessages(id string, cluster string, nodes []string, model string, messages []models.Message) *Conversation {
	if id == "" {
		id = models.NewID()
	}
	c := New(cluster, nodes, model)
	c.id = id
	c.ReplaceMessages(messages)
	return c
}

func (c *Conversation) ID() string {
	return c.id
}

func (c *Conversation) AddUser(content string) {
	c.add(RoleUser, content, "", "", "", "")
}

func (c *Conversation) AddAssistant(content string) {
	c.add(RoleAssistant, content, "", "", "", "")
}

func (c *Conversation) AddTool(name string, input string, output string) {
	c.add(RoleTool, output, "", name, input, output)
}

func (c *Conversation) AddToolCall(callID string, name string, input string) {
	c.add(RoleAssistant, "", callID, name, input, "")
}

func (c *Conversation) AddToolResult(callID string, output string) {
	c.add(RoleTool, output, callID, "", "", output)
}

func (c *Conversation) Messages() []models.Message {
	return append([]models.Message(nil), c.messages...)
}

func (c *Conversation) ReplaceMessages(messages []models.Message) {
	c.messages = append([]models.Message(nil), messages...)
	for i := range c.messages {
		if c.messages[i].ConversationID == "" {
			c.messages[i].ConversationID = c.id
		}
	}
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
