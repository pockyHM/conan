package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

func newTestServer(t *testing.T, requireAuth bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("rpc request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		if requireAuth && r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeRPC(t, w, req.ID, mcpproto.InitializeResult{ProtocolVersion: "2024-11-05", ServerInfo: mcpproto.ServerInfo{Name: "conan-agent", Version: "test"}})
		case "tools/list":
			writeRPC(t, w, req.ID, map[string]interface{}{"tools": []mcpproto.ToolDefinition{{Name: "shell/run", Description: "run", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
		case "tools/call":
			writeRPC(t, w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("called")}})
		case "bad/error":
			_ = json.NewEncoder(w).Encode(mcpproto.NewErrorResponse(req.ID, -32000, "agent error"))
		case "bad/error-with-data":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": mcpproto.JSONRPCVersion,
				"id":      req.ID,
				"error": map[string]interface{}{
					"code":    -32000,
					"message": "agent error with data",
					"data": map[string]interface{}{
						"node":    "node-a",
						"attempt": 2,
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(mcpproto.NewMethodNotFoundError(req.ID))
		}
	}))
}

func writeRPC(t *testing.T, w http.ResponseWriter, id json.RawMessage, result interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(id, result)); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestPing(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestInitialize(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	result, err := client.Initialize(t.Context())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if result.ServerInfo.Name != "conan-agent" {
		t.Fatalf("server name = %q", result.ServerInfo.Name)
	}
}

func TestListTools(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	tools, err := client.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "shell/run" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestCallTool(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	result, err := client.CallTool(t.Context(), "shell/run", json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.Content[0].Text != "called" {
		t.Fatalf("result = %#v", result)
	}
}

func TestAuthHeader(t *testing.T) {
	srv := newTestServer(t, true)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL, Token: "secret"})
	if _, err := client.Initialize(t.Context()); err != nil {
		t.Fatalf("Initialize with auth: %v", err)
	}
}

func TestRPCError(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.rpc(t.Context(), "bad/error", nil)
	if err == nil || !strings.Contains(err.Error(), "agent error") {
		t.Fatalf("err = %v", err)
	}
}

func TestRPCErrorPreservesJSONRPCErrorAndData(t *testing.T) {
	srv := newTestServer(t, false)
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})

	_, err := client.rpc(t.Context(), "bad/error-with-data", nil)
	if err == nil || !strings.Contains(err.Error(), "agent error with data") {
		t.Fatalf("err = %v", err)
	}
	var rpcErr *mcpproto.JSONRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err should be JSONRPCError, got %T", err)
	}
	if rpcErr.Code != -32000 {
		t.Fatalf("Code = %d, want -32000", rpcErr.Code)
	}
	data, ok := rpcErr.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Data = %#v, want map", rpcErr.Data)
	}
	if data["node"] != "node-a" || data["attempt"] != float64(2) {
		t.Fatalf("Data = %#v", data)
	}
}

func TestPingNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	err := client.Ping(t.Context())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
	var rpcErr *rpcError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err should be rpcError, got %T", err)
	}
	if rpcErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("HTTPStatus = %d, want %d", rpcErr.HTTPStatus, http.StatusServiceUnavailable)
	}
}

func TestRPCNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.rpc(t.Context(), "initialize", nil)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
	var rpcErr *rpcError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err should be rpcError, got %T", err)
	}
	if rpcErr.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("HTTPStatus = %d, want %d", rpcErr.HTTPStatus, http.StatusBadGateway)
	}
}

func TestRPCRejectsMismatchedID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(json.RawMessage(`999`), map[string]string{"ok": "true"}))
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.rpc(t.Context(), "initialize", nil)
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("err = %v", err)
	}
}

func TestRPCRejectsInvalidVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "1.0", "id": 1, "result": map[string]string{"ok": "true"}})
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.rpc(t.Context(), "initialize", nil)
	if err == nil || !strings.Contains(err.Error(), "jsonrpc") {
		t.Fatalf("err = %v", err)
	}
}

func TestURL(t *testing.T) {
	if got := URL("10.0.0.1", 9280, false); got != "http://10.0.0.1:9280" {
		t.Fatalf("url = %q", got)
	}
	if got := URL("node.local", 9443, true); got != "https://node.local:9443" {
		t.Fatalf("url = %q", got)
	}
}
