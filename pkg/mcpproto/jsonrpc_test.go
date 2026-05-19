package mcpproto

import (
	"encoding/json"
	"testing"
)

func TestJSONRPCRequestUnmarshal(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"shell/run","arguments":{"command":"echo hi"}}}`
	var req JSONRPCRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", req.JSONRPC)
	}
	if req.Method != "tools/call" {
		t.Errorf("method = %q, want tools/call", req.Method)
	}
}

func TestJSONRPCResponseMarshal(t *testing.T) {
	resp := NewSuccessResponse(json.RawMessage(`1`), map[string]string{"status": "ok"})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" {
		t.Error("expected non-empty JSON output")
	}
}

func TestJSONRPCError(t *testing.T) {
	err := NewMethodNotFoundError(json.RawMessage(`42`))
	if err.Error.Code != -32601 {
		t.Errorf("code = %d, want -32601", err.Error.Code)
	}
}
