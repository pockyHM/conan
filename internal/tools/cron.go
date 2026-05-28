package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// cron_list
type cronListTool struct{}

func (c *cronListTool) Name() string        { return "cron_list" }
func (c *cronListTool) Description() string { return "List crontab entries" }
func (c *cronListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"user":{"type":"string","description":"Crontab user (default: current)"}}}`)
}
func (c *cronListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		User string `json:"user"`
	}
	json.Unmarshal(input, &args)
	cmd := "crontab -l"
	if args.User != "" {
		cmd = fmt.Sprintf("crontab -l -u %s", args.User)
	}
	return runCommand(ctx, cmd)
}

// cron_add
type cronAddTool struct{}

func (c *cronAddTool) Name() string        { return "cron_add" }
func (c *cronAddTool) Description() string { return "Add crontab entry" }
func (c *cronAddTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"schedule":{"type":"string","description":"Cron schedule expression"},"command":{"type":"string","description":"Command to run"},"user":{"type":"string"}},"required":["schedule","command"]}`)
}
func (c *cronAddTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		User     string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	entry := fmt.Sprintf("%s %s", args.Schedule, args.Command)
	cmd := fmt.Sprintf(`(crontab -l%s 2>/dev/null; echo "%s") | crontab%s`, userFlag, entry, userFlag)
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("cron job added: " + entry)}}, nil
}

// cron_remove
type cronRemoveTool struct{}

func (c *cronRemoveTool) Name() string        { return "cron_remove" }
func (c *cronRemoveTool) Description() string { return "Remove crontab entry by line content" }
func (c *cronRemoveTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Pattern to match for removal"},"user":{"type":"string"}},"required":["pattern"]}`)
}
func (c *cronRemoveTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Pattern string `json:"pattern"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	cmd := fmt.Sprintf(`crontab -l%s 2>/dev/null | grep -v '%s' | crontab%s`, userFlag, args.Pattern, userFlag)
	return runCommand(ctx, cmd)
}

// cron_show
type cronShowTool struct{}

func (c *cronShowTool) Name() string        { return "cron_show" }
func (c *cronShowTool) Description() string { return "Show crontab entries matching pattern" }
func (c *cronShowTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Grep pattern"},"user":{"type":"string"}},"required":["pattern"]}`)
}
func (c *cronShowTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Pattern string `json:"pattern"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	cmd := fmt.Sprintf("crontab -l%s 2>/dev/null | grep '%s'", userFlag, args.Pattern)
	return runCommand(ctx, cmd)
}

func NewCronTools() []Tool {
	return []Tool{&cronListTool{}, &cronAddTool{}, &cronRemoveTool{}, &cronShowTool{}}
}
