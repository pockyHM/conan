package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// svc/list
type svcListTool struct{}

func (s *svcListTool) Name() string        { return "svc/list" }
func (s *svcListTool) Description() string { return "List systemd services" }
func (s *svcListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"state":{"type":"string","description":"Filter by state: active, inactive, failed"}}}`)
}
func (s *svcListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		State string `json:"state"`
	}
	json.Unmarshal(input, &args)
	cmd := "systemctl list-units --type=service --no-pager"
	if args.State != "" {
		cmd += " --state=" + args.State
	}
	return runCommand(cmd)
}

// svc/status
type svcStatusTool struct{}

func (s *svcStatusTool) Name() string        { return "svc/status" }
func (s *svcStatusTool) Description() string { return "Get service status" }
func (s *svcStatusTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStatusTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl status %s --no-pager", args.Name)
	return runCommand(cmd)
}

// svc/start
type svcStartTool struct{}

func (s *svcStartTool) Name() string        { return "svc/start" }
func (s *svcStartTool) Description() string { return "Start a service" }
func (s *svcStartTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStartTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl start %s", args.Name)
	return runCommand(cmd)
}

// svc/stop
type svcStopTool struct{}

func (s *svcStopTool) Name() string        { return "svc/stop" }
func (s *svcStopTool) Description() string { return "Stop a service" }
func (s *svcStopTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStopTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl stop %s", args.Name)
	return runCommand(cmd)
}

// svc/restart
type svcRestartTool struct{}

func (s *svcRestartTool) Name() string        { return "svc/restart" }
func (s *svcRestartTool) Description() string { return "Restart a service" }
func (s *svcRestartTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcRestartTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl restart %s", args.Name)
	return runCommand(cmd)
}

func NewSvcTools() []Tool {
	return []Tool{&svcListTool{}, &svcStatusTool{}, &svcStartTool{}, &svcStopTool{}, &svcRestartTool{}}
}

// runCommand is a shared helper for tools that wrap simple shell commands.
func runCommand(cmd string) (*mcpproto.ToolResult, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	text := string(out)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.TextContent(text)},
			IsError: true,
		}, nil
	}
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(text)},
	}, nil
}
