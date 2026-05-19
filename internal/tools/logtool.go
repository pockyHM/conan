package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// log/read
type logReadTool struct{}

func (l *logReadTool) Name() string        { return "log/read" }
func (l *logReadTool) Description() string { return "Read log file with optional tail and filter" }
func (l *logReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Log file path"},"tail":{"type":"integer","description":"Last N lines"},"filter":{"type":"string","description":"Filter pattern"}},"required":["path"]}`)
}
func (l *logReadTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path   string `json:"path"`
		Tail   int    `json:"tail"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}

	f, err := os.Open(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if args.Filter != "" && !strings.Contains(line, args.Filter) {
			continue
		}
		lines = append(lines, line)
	}

	if args.Tail > 0 && len(lines) > args.Tail {
		lines = lines[len(lines)-args.Tail:]
	}

	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(lines, "\n"))}}, nil
}

// log/stream
type logStreamTool struct{}

func (l *logStreamTool) Name() string        { return "log/stream" }
func (l *logStreamTool) Description() string { return "Stream log file (SSE, real-time)" }
func (l *logStreamTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Log file path"},"filter":{"type":"string","description":"Filter pattern"}},"required":["path"]}`)
}
func (l *logStreamTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path string `json:"path"`
	}
	json.Unmarshal(input, &args)
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf("Streaming %s via SSE — use the SSE endpoint for real-time updates", args.Path))},
	}, nil
}

// log/journalctl
type logJournalctlTool struct{}

func (l *logJournalctlTool) Name() string        { return "log/journalctl" }
func (l *logJournalctlTool) Description() string { return "Query journalctl logs" }
func (l *logJournalctlTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"unit":{"type":"string","description":"Systemd unit name"},"since":{"type":"string","description":"Time range (e.g. '1h ago')"},"tail":{"type":"integer","description":"Last N entries"}}}`)
}
func (l *logJournalctlTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Unit  string `json:"unit"`
		Since string `json:"since"`
		Tail  int    `json:"tail"`
	}
	json.Unmarshal(input, &args)

	cmd := "journalctl --no-pager"
	if args.Unit != "" {
		cmd += fmt.Sprintf(" -u %s", args.Unit)
	}
	if args.Since != "" {
		cmd += fmt.Sprintf(" --since '%s'", args.Since)
	}
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" -n %d", args.Tail)
	}
	return runCommand(cmd)
}

func NewLogTools() []Tool {
	return []Tool{&logReadTool{}, &logStreamTool{}, &logJournalctlTool{}}
}
