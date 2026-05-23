package tui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestMetaToolDescriptionsPreferSearchBeforeExec(t *testing.T) {
	descriptions := make(map[string]string)
	for _, tool := range metaToolDefs {
		descriptions[tool.Name] = strings.ToLower(tool.Description)
	}

	for _, want := range []string{"last-resort", "tool_search", "specialized", "file transfer"} {
		if !strings.Contains(descriptions[metaToolExec], want) {
			t.Fatalf("exec description missing %q: %s", want, descriptions[metaToolExec])
		}
	}
	for _, want := range []string{"primary", "except first-class file transfer", "before exec"} {
		if !strings.Contains(descriptions[metaToolToolSearch], want) {
			t.Fatalf("tool_search description missing %q: %s", want, descriptions[metaToolToolSearch])
		}
	}
	for _, want := range []string{"specialized", "tool_search", "risk review"} {
		if !strings.Contains(descriptions[metaToolCallTool], want) {
			t.Fatalf("call_tool description missing %q: %s", want, descriptions[metaToolCallTool])
		}
	}
	for _, want := range []string{"upload", "managed file transfer", "do not use tool_search", "scp", "text file", "binary and image"} {
		if !strings.Contains(descriptions[metaToolFilePut], want) {
			t.Fatalf("file_put description missing %q: %s", want, descriptions[metaToolFilePut])
		}
	}
	for _, want := range []string{"download", "managed file transfer", "do not use tool_search", "scp", "text file", "binary and image"} {
		if !strings.Contains(descriptions[metaToolFileGet], want) {
			t.Fatalf("file_get description missing %q: %s", want, descriptions[metaToolFileGet])
		}
	}
}

func TestNodeAddToolOnlyAvailableWhenNodeToolsEnabled(t *testing.T) {
	model := NewModel(ModelConfig{})
	firstClassTools := map[string]bool{}
	for _, tool := range model.availableToolDefs() {
		if tool.Name == metaToolNodeAdd {
			t.Fatal("node_add should not be exposed by default")
		}
		firstClassTools[tool.Name] = true
	}
	for _, toolName := range []string{metaToolFilePut, metaToolFileGet} {
		if !firstClassTools[toolName] {
			t.Fatalf("%s should be exposed as a first-class tool", toolName)
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

func TestToolCacheSearchMatchesInputSchema(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{
		{
			Name:        "docker/logs",
			Description: "Get container logs",
			InputSchema: []byte(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"tail":{"type":"integer","description":"Last N lines"}}}`),
		},
	})

	results := cache.Search("tail", []string{"node-a"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	if results[0].Name != "docker/logs" {
		t.Fatalf("result name = %q, want docker/logs", results[0].Name)
	}
}

func TestToolCacheSearchRanksNameMatchAboveDescriptionOnlyMatch(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{
		{
			Name:        "log/read",
			Description: "Read a file from disk",
			InputSchema: []byte(`{"type":"object"}`),
		},
		{
			Name:        "fs/read",
			Description: "Read log file contents",
			InputSchema: []byte(`{"type":"object"}`),
		},
	})

	results := cache.Search("log", []string{"node-a"})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2: %#v", len(results), results)
	}
	if results[0].Name != "log/read" {
		t.Fatalf("top result = %q, want log/read: %#v", results[0].Name, results)
	}
}

func TestToolCacheSearchMergesDuplicateToolsAcrossNodes(t *testing.T) {
	cache := newToolCache()
	tool := mcpproto.ToolDefinition{
		Name:        "k8s/logs",
		Description: "Get pod logs",
		InputSchema: []byte(`{"type":"object"}`),
	}
	cache.Set("node-b", []mcpproto.ToolDefinition{tool})
	cache.Set("node-a", []mcpproto.ToolDefinition{tool})

	results := cache.Search("logs", []string{"node-a", "node-b"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	if !reflect.DeepEqual(results[0].Nodes, []string{"node-a", "node-b"}) {
		t.Fatalf("nodes = %#v, want node-a and node-b", results[0].Nodes)
	}
}

func TestSubagentToolExecutorBlocksNonReadOnlyNodeTool(t *testing.T) {
	executor := subagentToolExecutor{model: NewModel(ModelConfig{})}
	output, ok := executor.ExecuteSubagentTool(context.Background(), llm.ToolCall{
		Name:      metaToolCallTool,
		Arguments: json.RawMessage(`{"node":"node-a","tool":"docker/exec","arguments":{"container":"api","command":"restart"}}`),
	})

	if ok {
		t.Fatalf("docker/exec should be blocked, output=%s", output)
	}
	if !strings.Contains(output, "blocked") {
		t.Fatalf("output = %q, want blocked message", output)
	}
}
