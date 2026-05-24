package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// docker/ps
type dockerPsTool struct{}

func (d *dockerPsTool) Name() string        { return "docker/ps" }
func (d *dockerPsTool) Description() string { return "List Docker containers" }
func (d *dockerPsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"all":{"type":"boolean","description":"Show all containers (default running only)"},"filter":{"type":"string","description":"Filter expression"}}}`)
}
func (d *dockerPsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		All    bool   `json:"all"`
		Filter string `json:"filter"`
	}
	json.Unmarshal(input, &args)
	cmd := "docker ps"
	if args.All {
		cmd += " -a"
	}
	if args.Filter != "" {
		cmd += fmt.Sprintf(" --filter '%s'", args.Filter)
	}
	return runCommand(ctx, cmd)
}

// docker/images
type dockerImagesTool struct{}

func (d *dockerImagesTool) Name() string        { return "docker/images" }
func (d *dockerImagesTool) Description() string { return "List Docker images" }
func (d *dockerImagesTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string","description":"Filter expression"}}}`)
}
func (d *dockerImagesTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Filter string `json:"filter"`
	}
	json.Unmarshal(input, &args)
	cmd := "docker images"
	if args.Filter != "" {
		cmd += fmt.Sprintf(" --filter '%s'", args.Filter)
	}
	return runCommand(ctx, cmd)
}

// docker/logs
type dockerLogsTool struct{}

func (d *dockerLogsTool) Name() string        { return "docker/logs" }
func (d *dockerLogsTool) Description() string { return "Get container logs" }
func (d *dockerLogsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"tail":{"type":"integer","description":"Last N lines"}},"required":["container"]}`)
}
func (d *dockerLogsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Container string `json:"container"`
		Tail      int    `json:"tail"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("docker logs %s", args.Container)
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" --tail %d", args.Tail)
	}
	return runCommand(ctx, cmd)
}

// docker/exec
type dockerExecTool struct{}

func (d *dockerExecTool) Name() string        { return "docker/exec" }
func (d *dockerExecTool) Description() string { return "Execute command in container" }
func (d *dockerExecTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"command":{"type":"string","description":"Command to execute"}},"required":["container","command"]}`)
}
func (d *dockerExecTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Container string `json:"container"`
		Command   string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("docker exec %s %s", args.Container, args.Command)
	return runCommand(ctx, cmd)
}

// docker/run
type dockerRunTool struct{}

func (d *dockerRunTool) Name() string        { return "docker/run" }
func (d *dockerRunTool) Description() string { return "Run a Docker container" }
func (d *dockerRunTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"image":{"type":"string"},"name":{"type":"string"},"ports":{"type":"array","items":{"type":"string"}},"detach":{"type":"boolean"},"env":{"type":"object","additionalProperties":{"type":"string"}}},"required":["image"]}`)
}
func (d *dockerRunTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Image  string            `json:"image"`
		Name   string            `json:"name"`
		Ports  []string          `json:"ports"`
		Detach bool              `json:"detach"`
		Env    map[string]string `json:"env"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := "docker run"
	if args.Detach {
		cmd += " -d"
	}
	if args.Name != "" {
		cmd += fmt.Sprintf(" --name %s", args.Name)
	}
	for _, p := range args.Ports {
		cmd += fmt.Sprintf(" -p %s", p)
	}
	for k, v := range args.Env {
		cmd += fmt.Sprintf(" -e %s=%s", k, v)
	}
	cmd += fmt.Sprintf(" %s", args.Image)
	return runCommand(ctx, cmd)
}

// docker/compose
type dockerComposeTool struct{}

func (d *dockerComposeTool) Name() string        { return "docker/compose" }
func (d *dockerComposeTool) Description() string { return "Run docker compose command" }
func (d *dockerComposeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","description":"Compose action (up, down, ps, logs)"},"file":{"type":"string","description":"Compose file path"}},"required":["action"]}`)
}
func (d *dockerComposeTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Action string `json:"action"`
		File   string `json:"file"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := "docker compose"
	if args.File != "" {
		cmd += fmt.Sprintf(" -f %s", args.File)
	}
	cmd += " " + args.Action
	return runCommand(ctx, cmd)
}

func NewDockerTools() []Tool {
	return []Tool{&dockerPsTool{}, &dockerImagesTool{}, &dockerLogsTool{}, &dockerExecTool{}, &dockerRunTool{}, &dockerComposeTool{}}
}
