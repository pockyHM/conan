package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNodeAddToolOnlyAvailableWhenNodeToolsEnabled(t *testing.T) {
	model := NewModel(ModelConfig{})
	for _, tool := range model.availableToolDefs() {
		if tool.Name == metaToolNodeAdd {
			t.Fatal("node_add should not be exposed by default")
		}
	}

	model.nodeToolsEnabled = true
	found := false
	for _, tool := range model.availableToolDefs() {
		if tool.Name == metaToolNodeAdd {
			found = true
			if !strings.Contains(tool.Description, "deploys or updates") {
				t.Fatalf("node_add description should describe deployment risk: %s", tool.Description)
			}
			if !strings.Contains(string(tool.InputSchema), `"host"`) {
				t.Fatalf("node_add schema missing host: %s", string(tool.InputSchema))
			}
		}
	}
	if !found {
		t.Fatal("node_add should be exposed after /node")
	}
}

func TestSanitizeToolArgumentsRedactsNodeAddPassword(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.5","password":"secret","ssh_port":2222}`)

	sanitized := sanitizeToolArguments(metaToolNodeAdd, raw)

	var got map[string]any
	if err := json.Unmarshal(sanitized, &got); err != nil {
		t.Fatalf("sanitized arguments should be valid json: %v", err)
	}
	if got["host"] != "10.0.0.5" {
		t.Fatalf("host = %v, want preserved host", got["host"])
	}
	if got["password"] == "secret" {
		t.Fatal("password should be redacted")
	}
	if got["password"] != "[REDACTED]" {
		t.Fatalf("password = %v, want [REDACTED]", got["password"])
	}
}

func TestSanitizeToolArgumentsRejectsMalformedNodeAddArguments(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.5","password":"secret"`)

	sanitized := sanitizeToolArguments(metaToolNodeAdd, raw)

	if string(sanitized) == string(raw) {
		t.Fatal("malformed node_add arguments should not be returned raw")
	}
	if strings.Contains(string(sanitized), "secret") {
		t.Fatalf("sanitized arguments should not leak password: %s", string(sanitized))
	}
	if string(sanitized) != `{"error":"invalid node_add arguments"}` {
		t.Fatalf("sanitized arguments = %s, want invalid node_add payload", string(sanitized))
	}
}

func TestSanitizeToolArgumentsLeavesMalformedNonNodeToolArgumentsRaw(t *testing.T) {
	raw := json.RawMessage(`{"password":"secret"`)

	sanitized := sanitizeToolArguments(metaToolExec, raw)

	if string(sanitized) != string(raw) {
		t.Fatalf("non-node tool arguments should be unchanged: %s", string(sanitized))
	}
}
