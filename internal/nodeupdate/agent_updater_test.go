package nodeupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestMCPAgentUpdaterCallsAgentUpdateTool(t *testing.T) {
	var sawTool bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode rpc request: %v", err)
			}
			var params mcpproto.ToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode tool params: %v", err)
			}
			if params.Name != "agent_update" {
				t.Fatalf("tool name = %q, want agent_update", params.Name)
			}
			if !strings.Contains(string(params.Arguments), "remote_binary_path") {
				t.Fatalf("arguments missing remote_binary_path: %s", string(params.Arguments))
			}
			sawTool = true
			writeRPCResult(t, w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{BaseURL: func(AgentTarget) string { return srv.URL }}
	err := updater.Update(context.Background(), AgentTarget{
		Host:  "agent.example.com",
		Port:  9280,
		Token: "token",
		Request: agentupdate.Request{
			Binary:           "Ymlu",
			Config:           "config",
			SystemdUnit:      "unit",
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !sawTool {
		t.Fatal("agent_update was not called")
	}
}

func TestMCPAgentUpdaterRetriesHealthAfterToolCall(t *testing.T) {
	var healthCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			healthCalls++
			if healthCalls < 3 {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode rpc request: %v", err)
			}
			writeRPCResult(t, w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{
		BaseURL:        func(AgentTarget) string { return srv.URL },
		HealthAttempts: 3,
	}
	err := updater.Update(context.Background(), AgentTarget{Request: agentupdate.Request{
		Binary:           "Ymlu",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if healthCalls != 3 {
		t.Fatalf("health calls = %d, want 3", healthCalls)
	}
}

func TestMCPAgentUpdaterReturnsToolErrorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			http.NotFound(w, r)
			return
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		writeRPCResult(t, w, req.ID, mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent("install failed")},
			IsError: true,
		})
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{
		BaseURL:        func(AgentTarget) string { return srv.URL },
		HealthAttempts: 1,
	}
	err := updater.Update(context.Background(), AgentTarget{Request: agentupdate.Request{
		Binary:           "Ymlu",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}})
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("err = %v, want install failed", err)
	}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result mcpproto.ToolResult) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(mcpproto.JSONRPCResponse{
		JSONRPC: mcpproto.JSONRPCVersion,
		ID:      id,
		Result:  mustRaw(t, result),
	}); err != nil {
		t.Fatalf("write rpc response: %v", err)
	}
}

func mustRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	return data
}
