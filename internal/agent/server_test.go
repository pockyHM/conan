package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func newTestServer(t *testing.T) string {
	t.Helper()
	cfg := configschema.DefaultAgentConfig()
	cfg.Token = "" // disable auth for tests
	r := tools.NewRegistry()
	r.Register(&echoTool{})
	srv := NewServer(cfg, r, "test")
	go srv.Start()
	t.Cleanup(func() { srv.Shutdown(t.Context()) })
	return "http://" + cfg.Listen
}

func TestServerHealth(t *testing.T) {
	base := newTestServer(t)
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServerAuth(t *testing.T) {
	cfg := configschema.DefaultAgentConfig()
	cfg.Listen = "0.0.0.0:9201"
	cfg.Token = "secret-token"
	r := tools.NewRegistry()
	r.Register(&echoTool{})
	srv := NewServer(cfg, r, "test")
	go srv.Start()
	t.Cleanup(func() { srv.Shutdown(t.Context()) })
	base := "http://" + cfg.Listen

	// No auth
	resp, err := http.Post(base+"/rpc", "application/json", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
	))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// With auth
	req, _ := http.NewRequest(http.MethodPost, base+"/rpc", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auth request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200, body: %s", resp.StatusCode, body)
	}
}

func TestServerToolCall(t *testing.T) {
	base := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, base+"/rpc", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"test/echo","arguments":{"msg":"integration"}}}`,
	))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	var rpcResp mcpproto.JSONRPCResponse
	json.Unmarshal(data, &rpcResp)
	resultMap := rpcResp.Result.(map[string]interface{})
	content := resultMap["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["text"] != "integration" {
		t.Errorf("text = %v, want integration", first["text"])
	}
}
