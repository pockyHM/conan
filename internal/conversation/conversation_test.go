package conversation

import (
	"testing"

	"github.com/pockyHM/conan/pkg/models"
)

func TestConversationAppendsMessagesWithRolesAndToolFields(t *testing.T) {
	c := New("cluster-a", []string{"node-1", "node-2"}, "gpt-4.1")

	if c.ID() == "" {
		t.Fatal("conversation id should not be empty")
	}

	c.AddUser("hello")
	c.AddAssistant("hi there")
	c.AddTool("shell", "ls", "file1\nfile2")

	msgs := c.Messages()
	if got, want := len(msgs), 3; got != want {
		t.Fatalf("len(messages) = %d, want %d", got, want)
	}

	assertMessage := func(t *testing.T, msg models.Message, role, content, toolName, toolInput, toolOutput string) {
		t.Helper()
		if msg.Role != role {
			t.Fatalf("role = %q, want %q", msg.Role, role)
		}
		if msg.Content != content {
			t.Fatalf("content = %q, want %q", msg.Content, content)
		}
		if msg.ToolName != toolName {
			t.Fatalf("tool name = %q, want %q", msg.ToolName, toolName)
		}
		if msg.ToolInput != toolInput {
			t.Fatalf("tool input = %q, want %q", msg.ToolInput, toolInput)
		}
		if msg.ToolOutput != toolOutput {
			t.Fatalf("tool output = %q, want %q", msg.ToolOutput, toolOutput)
		}
	}

	assertMessage(t, msgs[0], RoleUser, "hello", "", "", "")
	assertMessage(t, msgs[1], RoleAssistant, "hi there", "", "", "")
	assertMessage(t, msgs[2], RoleTool, "file1\nfile2", "shell", "ls", "file1\nfile2")
}

func TestConversationContextKeepsNewestMessagesWithinBudget(t *testing.T) {
	c := New("cluster-a", []string{"node-1"}, "gpt-4.1")

	c.AddUser("old user message")
	c.AddAssistant("old assistant reply")
	c.AddUser("middle user message")
	c.AddAssistant("middle assistant reply")
	c.AddUser("new user message")
	c.AddAssistant("new assistant reply")

	ctx := c.Context(40)
	if len(ctx) == 0 {
		t.Fatal("context should not be empty")
	}

	for _, msg := range ctx {
		if msg.Content == "old user message" || msg.Content == "old assistant reply" {
			t.Fatalf("context included old message %q", msg.Content)
		}
	}

	if got := ctx[len(ctx)-1].Content; got != "new assistant reply" {
		t.Fatalf("last context message = %q, want %q", got, "new assistant reply")
	}

	if got := ctx[0].Content; got != "new user message" {
		t.Fatalf("first context message = %q, want new user message", got)
	}
}

func TestContextUsesCharacterBudget(t *testing.T) {
	c := New("prod", nil, "claude-sonnet")
	c.AddUser("旧")
	c.AddAssistant("新")

	ctx := c.Context(2)
	if len(ctx) != 2 {
		t.Fatalf("context length = %d, messages = %#v", len(ctx), ctx)
	}
	if ctx[0].Content != "旧" || ctx[1].Content != "新" {
		t.Fatalf("context = %#v", ctx)
	}
}

func TestConversationMessagesReturnsCopyAndClearEmptiesMessages(t *testing.T) {
	c := New("cluster-a", []string{"node-1"}, "gpt-4.1")
	c.AddUser("hello")
	c.AddAssistant("hi")

	msgs := c.Messages()
	msgs[0].Content = "mutated"

	fresh := c.Messages()
	if fresh[0].Content != "hello" {
		t.Fatalf("conversation messages were mutated through copy, got %q", fresh[0].Content)
	}

	c.Clear()
	if got := len(c.Messages()); got != 0 {
		t.Fatalf("len(messages) after clear = %d, want 0", got)
	}
}
