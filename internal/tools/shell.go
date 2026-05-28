package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type shellInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	User    string `json:"user,omitempty"`
}

type ShellTool struct{}

func (s *ShellTool) Name() string        { return "shell_run" }
func (s *ShellTool) Description() string { return "Execute a shell command with timeout" }
func (s *ShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Shell command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in seconds (default 30)"},
			"user":    {"type": "string", "description": "Run as user (default: agent user)"}
		},
		"required": ["command"]
	}`)
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args shellInput
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}

	timeout := time.Duration(args.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if args.User != "" {
		cmd = exec.CommandContext(ctx, "su", "-", args.User, "-c", args.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", args.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	timedOut := ctx.Err() == context.DeadlineExceeded

	if timedOut {
		output := fmt.Sprintf("Command timed out after %d seconds\nstdout:\n%s\nstderr:\n%s",
			args.Timeout, stdout.String(), stderr.String())
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(output)},
			IsError: true,
		}, nil
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output := fmt.Sprintf("exit_code: %d\nstdout:\n%s\nstderr:\n%s",
		exitCode, stdout.String(), stderr.String())

	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(output)},
		IsError: false,
	}, nil
}
