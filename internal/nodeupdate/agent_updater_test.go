package nodeupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestMCPAgentUpdaterCallsAgentUpdateTool(t *testing.T) {
	handlerErrs := make(chan error, 4)
	sawTool := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				recordHandlerError(handlerErrs, fmt.Errorf("decode rpc request: %w", err))
				http.Error(w, "bad rpc request", http.StatusBadRequest)
				return
			}
			var params mcpproto.ToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				recordHandlerError(handlerErrs, fmt.Errorf("decode tool params: %w", err))
				writeRPCError(w, req.ID, "invalid tool params")
				return
			}
			if params.Name != "agent_update" {
				recordHandlerError(handlerErrs, fmt.Errorf("tool name = %q, want agent_update", params.Name))
				writeRPCError(w, req.ID, "unexpected tool name")
				return
			}
			if !strings.Contains(string(params.Arguments), "remote_binary_path") {
				recordHandlerError(handlerErrs, fmt.Errorf("arguments missing remote_binary_path: %s", string(params.Arguments)))
				writeRPCError(w, req.ID, "missing remote_binary_path")
				return
			}
			select {
			case sawTool <- struct{}{}:
			default:
			}
			if err := writeRPCResult(w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}}); err != nil {
				recordHandlerError(handlerErrs, err)
				http.Error(w, "write rpc response", http.StatusInternalServerError)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{
		BaseURL:      func(AgentTarget) string { return srv.URL },
		RestartDelay: -1,
	}
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
	assertNoHandlerError(t, handlerErrs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	select {
	case <-sawTool:
	default:
		t.Fatal("agent_update was not called")
	}
}

func TestMCPAgentUpdaterRetriesHealthAfterToolCall(t *testing.T) {
	handlerErrs := make(chan error, 4)
	var healthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			call := healthCalls.Add(1)
			if call < 3 {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				recordHandlerError(handlerErrs, fmt.Errorf("decode rpc request: %w", err))
				http.Error(w, "bad rpc request", http.StatusBadRequest)
				return
			}
			if err := writeRPCResult(w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}}); err != nil {
				recordHandlerError(handlerErrs, err)
				http.Error(w, "write rpc response", http.StatusInternalServerError)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{
		BaseURL:        func(AgentTarget) string { return srv.URL },
		HealthAttempts: 3,
		HealthDelay:    time.Millisecond,
		RestartDelay:   -1,
	}
	err := updater.Update(context.Background(), AgentTarget{Request: agentupdate.Request{
		Binary:           "Ymlu",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}})
	assertNoHandlerError(t, handlerErrs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := healthCalls.Load(); got != 3 {
		t.Fatalf("health calls = %d, want 3", got)
	}
}

func TestMCPAgentUpdaterWaitsRestartDelayBeforeHealthCheck(t *testing.T) {
	handlerErrs := make(chan error, 4)
	readyAt := time.Now().Add(20 * time.Millisecond)
	var healthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			healthCalls.Add(1)
			if time.Now().Before(readyAt) {
				http.Error(w, "old process still responding", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				recordHandlerError(handlerErrs, fmt.Errorf("decode rpc request: %w", err))
				http.Error(w, "bad rpc request", http.StatusBadRequest)
				return
			}
			if err := writeRPCResult(w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}}); err != nil {
				recordHandlerError(handlerErrs, err)
				http.Error(w, "write rpc response", http.StatusInternalServerError)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := MCPAgentUpdater{
		BaseURL:        func(AgentTarget) string { return srv.URL },
		HealthAttempts: 1,
		RestartDelay:   25 * time.Millisecond,
	}
	err := updater.Update(context.Background(), AgentTarget{Request: agentupdate.Request{
		Binary:           "Ymlu",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}})
	assertNoHandlerError(t, handlerErrs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := healthCalls.Load(); got != 1 {
		t.Fatalf("health calls = %d, want 1", got)
	}
}

func TestMCPAgentUpdaterDefaultRestartDelayRespectsContextCancellation(t *testing.T) {
	handlerErrs := make(chan error, 4)
	var healthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			healthCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				recordHandlerError(handlerErrs, fmt.Errorf("decode rpc request: %w", err))
				http.Error(w, "bad rpc request", http.StatusBadRequest)
				return
			}
			if err := writeRPCResult(w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}}); err != nil {
				recordHandlerError(handlerErrs, err)
				http.Error(w, "write rpc response", http.StatusInternalServerError)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	updater := MCPAgentUpdater{BaseURL: func(AgentTarget) string { return srv.URL }}
	err := updater.Update(ctx, AgentTarget{Request: agentupdate.Request{
		Binary:           "Ymlu",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}})
	assertNoHandlerError(t, handlerErrs)
	if err == nil || err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context deadline exceeded", err)
	}
	if got := healthCalls.Load(); got != 0 {
		t.Fatalf("health calls = %d, want 0 before restart delay elapses", got)
	}
}

func TestMCPAgentUpdaterReturnsToolErrorResult(t *testing.T) {
	handlerErrs := make(chan error, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			http.NotFound(w, r)
			return
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			recordHandlerError(handlerErrs, fmt.Errorf("decode rpc request: %w", err))
			http.Error(w, "bad rpc request", http.StatusBadRequest)
			return
		}
		if err := writeRPCResult(w, req.ID, mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent("install failed")},
			IsError: true,
		}); err != nil {
			recordHandlerError(handlerErrs, err)
			http.Error(w, "write rpc response", http.StatusInternalServerError)
			return
		}
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
	assertNoHandlerError(t, handlerErrs)
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("err = %v, want install failed", err)
	}
}

func recordHandlerError(errs chan<- error, err error) {
	select {
	case errs <- err:
	default:
	}
}

func assertNoHandlerError(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result mcpproto.ToolResult) error {
	raw, err := mustRaw(result)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(mcpproto.JSONRPCResponse{
		JSONRPC: mcpproto.JSONRPCVersion,
		ID:      id,
		Result:  raw,
	}); err != nil {
		return fmt.Errorf("write rpc response: %w", err)
	}
	return nil
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcpproto.JSONRPCResponse{
		JSONRPC: mcpproto.JSONRPCVersion,
		ID:      id,
		Error:   &mcpproto.JSONRPCError{Code: -32602, Message: message},
	})
}

func mustRaw(v interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal raw: %w", err)
	}
	return data, nil
}
