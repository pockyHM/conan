package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/agentupdate"
)

func TestAgentUpdateToolSchemaAndMetadata(t *testing.T) {
	tool := NewAgentUpdateTool(agentupdate.Applier{})

	if tool.Name() != "agent_update" {
		t.Fatalf("Name() = %q, want agent_update", tool.Name())
	}
	if !strings.Contains(tool.Description(), "Update conan-agent") {
		t.Fatalf("Description() = %q, want it to describe updating conan-agent", tool.Description())
	}
	if !strings.Contains(string(tool.InputSchema()), "remote_binary_path") {
		t.Fatalf("InputSchema() missing remote_binary_path: %s", string(tool.InputSchema()))
	}

	meta, ok := MetadataFor("agent_update")
	if !ok {
		t.Fatal("MetadataFor(agent_update) missing")
	}
	if meta.Safety != SafetyDestructive {
		t.Fatalf("agent_update safety = %q, want %q", meta.Safety, SafetyDestructive)
	}
	if meta.Scope != ScopeNode {
		t.Fatalf("agent_update scope = %q, want %q", meta.Scope, ScopeNode)
	}
}

func TestAgentUpdateToolReturnsErrorResultForInvalidPayload(t *testing.T) {
	tool := NewAgentUpdateTool(agentupdate.Applier{})
	req := agentupdate.Request{
		Binary:           "not-base64",
		Config:           "token: test",
		SystemdUnit:      "[Unit]\nDescription=conan-agent\n",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}
	input, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned transport error: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if !result.IsError {
		t.Fatal("result.IsError = false, want true")
	}
	if len(result.Content) == 0 {
		t.Fatal("result content is empty")
	}
	if !strings.Contains(result.Content[0].Text, "decode binary") {
		t.Fatalf("result text = %q, want decode binary", result.Content[0].Text)
	}
}
