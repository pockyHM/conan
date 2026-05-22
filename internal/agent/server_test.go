package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func waitForServer(t *testing.T, base string) {
	t.Helper()
	for i := 0; i < 50; i++ {
		resp, err := http.Get(base + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not start in time")
}

func newTestServer(t *testing.T) string {
	t.Helper()
	cfg := configschema.DefaultAgentConfig()
	cfg.Token = ""
	r := tools.NewRegistry()
	r.Register(&echoTool{})
	srv := NewServer(cfg, r, "test")
	go srv.Start()
	t.Cleanup(func() { srv.Shutdown(t.Context()) })
	base := "http://" + cfg.Listen
	waitForServer(t, base)
	return base
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
	waitForServer(t, base)

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

func TestServerDownloadsFileViaHTTP(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/artifact.bin"
	if err := os.WriteFile(file, []byte("streamed bytes"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	base := newTestServer(t)

	resp, err := http.Get(base + "/files/download?path=" + url.QueryEscape(file))
	if err != nil {
		t.Fatalf("download request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(data) != "streamed bytes" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, data)
	}
}

func TestServerUploadsFileViaHTTP(t *testing.T) {
	dir := t.TempDir()
	dst := dir + "/nested/artifact.bin"
	base := newTestServer(t)

	req, err := http.NewRequest(http.MethodPut, base+"/files/upload?path="+url.QueryEscape(dst)+"&mkdirs=true", strings.NewReader("streamed upload"))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != "streamed upload" {
		t.Fatalf("uploaded data = %q", data)
	}
}
