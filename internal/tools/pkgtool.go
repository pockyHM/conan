package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// pkg/list
type pkgListTool struct{}

func (p *pkgListTool) Name() string        { return "pkg/list" }
func (p *pkgListTool) Description() string { return "List installed packages" }
func (p *pkgListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Filter by package name"}}}`)
}
func (p *pkgListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Name string `json:"name"` }
	json.Unmarshal(input, &args)
	cmd := "dpkg -l 2>/dev/null || rpm -qa"
	if args.Name != "" {
		cmd = fmt.Sprintf("dpkg -l %s 2>/dev/null || rpm -q %s", args.Name, args.Name)
	}
	return runCommand(cmd)
}

// pkg/install
type pkgInstallTool struct{}

func (p *pkgInstallTool) Name() string        { return "pkg/install" }
func (p *pkgInstallTool) Description() string { return "Install package" }
func (p *pkgInstallTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Package name"},"update_cache":{"type":"boolean","description":"Update package cache first"}},"required":["name"]}`)
}
func (p *pkgInstallTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name        string `json:"name"`
		UpdateCache bool   `json:"update_cache"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	var cmd string
	if args.UpdateCache {
		cmd = fmt.Sprintf("apt-get update && apt-get install -y %s 2>/dev/null || yum install -y %s", args.Name, args.Name)
	} else {
		cmd = fmt.Sprintf("apt-get install -y %s 2>/dev/null || yum install -y %s", args.Name, args.Name)
	}
	return runCommand(cmd)
}

// pkg/update
type pkgUpdateTool struct{}

func (p *pkgUpdateTool) Name() string        { return "pkg/update" }
func (p *pkgUpdateTool) Description() string { return "Update package" }
func (p *pkgUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Package name (empty = update all)"}}}`)
}
func (p *pkgUpdateTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Name string `json:"name"` }
	json.Unmarshal(input, &args)
	var cmd string
	if args.Name != "" {
		cmd = fmt.Sprintf("apt-get install --only-upgrade -y %s 2>/dev/null || yum update -y %s", args.Name, args.Name)
	} else {
		cmd = "apt-get upgrade -y 2>/dev/null || yum update -y"
	}
	return runCommand(cmd)
}

// pkg/search
type pkgSearchTool struct{}

func (p *pkgSearchTool) Name() string        { return "pkg/search" }
func (p *pkgSearchTool) Description() string { return "Search packages" }
func (p *pkgSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`)
}
func (p *pkgSearchTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Query string `json:"query"` }
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("apt-cache search %s 2>/dev/null || yum search %s", args.Query, args.Query)
	return runCommand(cmd)
}

func NewPkgTools() []Tool {
	return []Tool{&pkgListTool{}, &pkgInstallTool{}, &pkgUpdateTool{}, &pkgSearchTool{}}
}
