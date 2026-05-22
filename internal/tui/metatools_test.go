package tui

import (
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
