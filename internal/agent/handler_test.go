package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type echoTool struct{}

func (e *echoTool) Name() string            { return "test/echo" }
func (e *echoTool) Description() string      { return "Echo tool" }
func (e *echoTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
}
func (e *echoTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Msg string `json:"msg"` }
	json.Unmarshal(input, &args)
	result := mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(args.Msg)},
	}
	return &result, nil
}

func setupTestHandler() *Handler {
	r := tools.NewRegistry()
	r.Register(&echoTool{})
	return NewHandler(r, "0.1.0")
}

func TestHandleInitialize(t *testing.T) {
	h := setupTestHandler()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var rpcResp mcpproto.JSONRPCResponse
	data, _ := io.ReadAll(resp.Body)
	json.Unmarshal(data, &rpcResp)
	resultMap, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}
	if resultMap["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", resultMap["protocolVersion"])
	}
}

func TestHandleToolsList(t *testing.T) {
	h := setupTestHandler()
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	data, _ := io.ReadAll(resp.Body)
	var rpcResp mcpproto.JSONRPCResponse
	json.Unmarshal(data, &rpcResp)
	resultMap, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}
	toolsArr, ok := resultMap["tools"].([]interface{})
	if !ok {
		t.Fatal("tools is not an array")
	}
	if len(toolsArr) != 1 {
		t.Errorf("tools length = %d, want 1", len(toolsArr))
	}
}

func TestHandleToolsCall(t *testing.T) {
	h := setupTestHandler()
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"test/echo","arguments":{"msg":"hello"}}}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	data, _ := io.ReadAll(resp.Body)
	var rpcResp mcpproto.JSONRPCResponse
	json.Unmarshal(data, &rpcResp)
	resultMap, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %v", rpcResp.Result)
	}
	content, _ := resultMap["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	firstBlock, _ := content[0].(map[string]interface{})
	if firstBlock["text"] != "hello" {
		t.Errorf("text = %v, want hello", firstBlock["text"])
	}
}

func TestHandleMethodNotFound(t *testing.T) {
	h := setupTestHandler()
	body := `{"jsonrpc":"2.0","id":4,"method":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	data, _ := io.ReadAll(resp.Body)
	var rpcResp mcpproto.JSONRPCResponse
	json.Unmarshal(data, &rpcResp)
	if rpcResp.Error == nil {
		t.Fatal("expected error response")
	}
	if rpcResp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", rpcResp.Error.Code)
	}
}
