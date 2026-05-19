package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// sys/cpu
type sysCPUTool struct{}

func (s *sysCPUTool) Name() string        { return "sys/cpu" }
func (s *sysCPUTool) Description() string { return "Get CPU usage and load average" }
func (s *sysCPUTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysCPUTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	loadAvg, _ := getLoadAvg()
	cores := runtime.NumCPU()
	output := fmt.Sprintf(`{"cores": %d, "load_avg": %s}`, cores, loadAvg)
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(output)}}, nil
}

func getLoadAvg() (string, error) {
	out, err := exec.Command("sh", "-c", "cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null || uptime").Output()
	if err != nil {
		return "[]", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sys/mem
type sysMemTool struct{}

func (s *sysMemTool) Name() string        { return "sys/mem" }
func (s *sysMemTool) Description() string { return "Get memory usage" }
func (s *sysMemTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysMemTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var cmd string
	if runtime.GOOS == "linux" {
		cmd = "free -b | head -2"
	} else {
		cmd = "vm_stat | head -10"
	}
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/disk
type sysDiskTool struct{}

func (s *sysDiskTool) Name() string        { return "sys/disk" }
func (s *sysDiskTool) Description() string { return "Get disk usage" }
func (s *sysDiskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysDiskTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	cmd := "df -h"
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/net
type sysNetTool struct{}

func (s *sysNetTool) Name() string        { return "sys/net" }
func (s *sysNetTool) Description() string { return "Get network interface stats" }
func (s *sysNetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysNetTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	cmd := "ip -s link 2>/dev/null || netstat -I -n 2>/dev/null || ifconfig"
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/processes
type sysProcessesTool struct{}

func (s *sysProcessesTool) Name() string        { return "sys/processes" }
func (s *sysProcessesTool) Description() string { return "Get process list" }
func (s *sysProcessesTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"sort":{"type":"string","description":"Sort by: cpu, mem, pid"},"limit":{"type":"integer","description":"Max processes to return"}}}`)
}
func (s *sysProcessesTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Sort  string `json:"sort"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal(input, &args)
	sortFlag := "--sort=-%cpu"
	if args.Sort == "mem" {
		sortFlag = "--sort=-%mem"
	}
	limit := 20
	if args.Limit > 0 {
		limit = args.Limit
	}
	cmd := fmt.Sprintf("ps aux %s | head -%d", sortFlag, limit+1)
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func NewSysTools() []Tool {
	return []Tool{&sysCPUTool{}, &sysMemTool{}, &sysDiskTool{}, &sysNetTool{}, &sysProcessesTool{}}
}
