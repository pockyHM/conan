package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type AgentUpdateTool struct {
	applier agentupdate.Applier
}

func NewAgentUpdateTool(applier agentupdate.Applier) *AgentUpdateTool {
	return &AgentUpdateTool{applier: applier}
}

func (a *AgentUpdateTool) Name() string {
	return "agent_update"
}

func (a *AgentUpdateTool) Description() string {
	return "Update conan-agent binary, config, and systemd unit on this node"
}

func (a *AgentUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"binary":{"type":"string","description":"Base64-encoded conan-agent binary for this node"},
			"binaries":{"type":"object","additionalProperties":{"type":"string"},"description":"Base64-encoded conan-agent binaries keyed by architecture"},
			"config":{"type":"string","description":"Rendered conan-agent config file"},
			"systemd_unit":{"type":"string","description":"Rendered conan-agent systemd unit file"},
			"remote_binary_path":{"type":"string","description":"Destination path for the conan-agent binary"},
			"remote_config_path":{"type":"string","description":"Destination path for the conan-agent config"},
			"systemd_unit_path":{"type":"string","description":"Destination path for the conan-agent systemd unit"}
		},
		"required":["config","systemd_unit","remote_binary_path","remote_config_path","systemd_unit_path"]
	}`)
}

func (a *AgentUpdateTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var req agentupdate.Request
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}

	result, err := a.applier.Apply(ctx, req)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())},
			IsError: true,
		}, nil
	}

	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf("updated conan-agent at %s for %s", result.BinaryPath, result.Arch))},
	}, nil
}
