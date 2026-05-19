package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// net/ping
type netPingTool struct{}

func (n *netPingTool) Name() string        { return "net/ping" }
func (n *netPingTool) Description() string { return "Ping a host" }
func (n *netPingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Host to ping"},"count":{"type":"integer","description":"Number of pings (default 3)"}},"required":["host"]}`)
}
func (n *netPingTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host  string `json:"host"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Count == 0 {
		args.Count = 3
	}
	cmd := fmt.Sprintf("ping -c %d -W 5 %s", args.Count, args.Host)
	return runCommand(cmd)
}

// net/traceroute
type netTracerouteTool struct{}

func (n *netTracerouteTool) Name() string        { return "net/traceroute" }
func (n *netTracerouteTool) Description() string { return "Traceroute to host" }
func (n *netTracerouteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Target host"}},"required":["host"]}`)
}
func (n *netTracerouteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("traceroute -m 30 -w 2 %s 2>/dev/null || tracepath %s", args.Host, args.Host)
	return runCommand(cmd)
}

// net/portcheck
type netPortcheckTool struct{}

func (n *netPortcheckTool) Name() string        { return "net/portcheck" }
func (n *netPortcheckTool) Description() string { return "Check if a port is open" }
func (n *netPortcheckTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Host address"},"port":{"type":"integer","description":"Port number"}},"required":["host","port"]}`)
}
func (n *netPortcheckTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("%s:%d", args.Host, args.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf(`{"host":"%s","port":%d,"open":false}`, args.Host, args.Port))},
		}, nil
	}
	conn.Close()
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf(`{"host":"%s","port":%d,"open":true}`, args.Host, args.Port))},
	}, nil
}

// net/curl
type netCurlTool struct{}

func (n *netCurlTool) Name() string        { return "net/curl" }
func (n *netCurlTool) Description() string { return "Make HTTP request" }
func (n *netCurlTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to request"},"method":{"type":"string","description":"HTTP method (default GET)"},"headers":{"type":"object","description":"Request headers"},"body":{"type":"string","description":"Request body"}},"required":["url"]}`)
}
func (n *netCurlTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Method == "" {
		args.Method = "GET"
	}

	cmdArgs := []string{"-s", "-o", "-", "-w", "\nHTTP_CODE:%{http_code}", "-X", args.Method}
	for k, v := range args.Headers {
		cmdArgs = append(cmdArgs, "-H", fmt.Sprintf("%s: %s", k, v))
	}
	if args.Body != "" {
		cmdArgs = append(cmdArgs, "-d", args.Body)
	}
	cmdArgs = append(cmdArgs, args.URL)

	out, err := exec.Command("curl", cmdArgs...).CombinedOutput()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func NewNetTools() []Tool {
	return []Tool{&netPingTool{}, &netTracerouteTool{}, &netPortcheckTool{}, &netCurlTool{}}
}
