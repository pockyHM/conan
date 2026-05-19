package mcpproto

import (
	"encoding/json"
	"testing"
)

func TestToolResultText(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{TextContent("hello")},
	}
	if result.Content[0].Type != "text" {
		t.Errorf("type = %q, want text", result.Content[0].Type)
	}
	if result.Content[0].Text != "hello" {
		t.Errorf("text = %q, want hello", result.Content[0].Text)
	}
}

func TestToolResultIsError(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{TextContent("command failed")},
		IsError: true,
	}
	if !result.IsError {
		t.Error("IsError should be true")
	}
}

func TestToolCallParamsUnmarshal(t *testing.T) {
	raw := `{"name":"shell/run","arguments":{"command":"ls"}}`
	var params ToolCallParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if params.Name != "shell/run" {
		t.Errorf("name = %q, want shell/run", params.Name)
	}
}

func TestInitializeResult(t *testing.T) {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo{Name: "conan-agent", Version: "0.1.0"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" {
		t.Error("expected non-empty JSON output")
	}
}
