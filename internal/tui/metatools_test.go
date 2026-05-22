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
