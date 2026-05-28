package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/localtools"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/skills"
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

func TestExposedToolNamesMatchOpenAIPattern(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	var tools []llm.ToolDef
	tools = append(tools, metaToolDefs...)
	tools = append(tools, nodeManagementToolDefs...)
	tools = append(tools, imageToolDefs...)
	tools = append(tools, localtools.ToolDefs()...)
	tools = append(tools, skills.ToolDefs()...)
	for _, def := range memory.ToolDefs() {
		raw, err := json.Marshal(def)
		if err != nil {
			t.Fatalf("marshal memory tool: %v", err)
		}
		var tool llm.ToolDef
		if err := json.Unmarshal(raw, &tool); err != nil {
			t.Fatalf("unmarshal memory tool: %v", err)
		}
		tools = append(tools, tool)
	}

	for _, tool := range tools {
		if !pattern.MatchString(tool.Name) {
			t.Fatalf("tool name %q does not match OpenAI tool pattern", tool.Name)
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
			Name:        "docker_logs",
			Description: "Get container logs",
			InputSchema: []byte(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"tail":{"type":"integer","description":"Last N lines"}}}`),
		},
	})

	results := cache.Search("tail", []string{"node-a"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	if results[0].Name != "docker_logs" {
		t.Fatalf("result name = %q, want docker_logs", results[0].Name)
	}
}

func TestToolCacheSearchRanksNameMatchAboveDescriptionOnlyMatch(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{
		{
			Name:        "log_read",
			Description: "Read a file from disk",
			InputSchema: []byte(`{"type":"object"}`),
		},
		{
			Name:        "fs_read",
			Description: "Read log file contents",
			InputSchema: []byte(`{"type":"object"}`),
		},
	})

	results := cache.Search("log", []string{"node-a"})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2: %#v", len(results), results)
	}
	if results[0].Name != "log_read" {
		t.Fatalf("top result = %q, want log_read: %#v", results[0].Name, results)
	}
}

func TestToolCacheSearchMergesDuplicateToolsAcrossNodes(t *testing.T) {
	cache := newToolCache()
	tool := mcpproto.ToolDefinition{
		Name:        "k8s_logs",
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

func TestToolCacheSearchResultsIncludeMetadata(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{{
		Name:        "svc_status",
		Description: "Show service status",
		InputSchema: []byte(`{"type":"object"}`),
	}})

	results := cache.Search("service status", []string{"node-a"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	if results[0].Safety != "read-only" {
		t.Fatalf("safety = %q, want read-only", results[0].Safety)
	}
	if results[0].Scope != "node" {
		t.Fatalf("scope = %q, want node", results[0].Scope)
	}
	if !reflect.DeepEqual(results[0].Capability, []string{"service"}) {
		t.Fatalf("capability = %#v, want service", results[0].Capability)
	}
}

func TestToolCacheSearchRanksCapabilityMetadata(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{
		{Name: "exec", Description: "Run a service command", InputSchema: []byte(`{"type":"object"}`)},
		{Name: "svc_status", Description: "Show daemon state", InputSchema: []byte(`{"type":"object"}`)},
	})

	results := cache.Search("service read only status", []string{"node-a"})

	if len(results) < 2 {
		t.Fatalf("results = %#v, want at least 2", results)
	}
	if results[0].Name != "svc_status" {
		t.Fatalf("top result = %q, want svc_status: %#v", results[0].Name, results)
	}
}

func TestToolCacheSearchFileTransferMetadata(t *testing.T) {
	cache := newToolCache()
	cache.Set("node-a", []mcpproto.ToolDefinition{{
		Name:        "file_put",
		Description: "Upload a local file",
		InputSchema: []byte(`{"type":"object"}`),
	}})

	results := cache.Search("upload file", []string{"node-a"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(results), results)
	}
	if results[0].Name != "file_put" || results[0].Safety != "mutating" {
		t.Fatalf("result = %#v, want mutating file_put", results[0])
	}
}

func TestSubagentToolExecutorBlocksNonReadOnlyNodeTool(t *testing.T) {
	executor := subagentToolExecutor{model: NewModel(ModelConfig{})}
	output, ok := executor.ExecuteSubagentTool(context.Background(), llm.ToolCall{
		Name:      metaToolCallTool,
		Arguments: json.RawMessage(`{"node":"node-a","tool":"docker_exec","arguments":{"container":"api","command":"restart"}}`),
	})

	if ok {
		t.Fatalf("docker_exec should be blocked, output=%s", output)
	}
	if !strings.Contains(output, "blocked") {
		t.Fatalf("output = %q, want blocked message", output)
	}
}

func TestSubagentToolExecutorAllowsDirectReadOnlyNodeTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.Method {
		case "tools/call":
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("nginx active")}}))
		default:
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]any{}))
		}
	}))
	defer server.Close()
	client := mcp.NewClient(mcp.Config{BaseURL: server.URL})
	model := NewModel(ModelConfig{
		Clients: map[string]*mcp.Client{"node-a": client},
		Nodes:   []NodeInfo{{Name: "node-a", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-a": true}
	executor := subagentToolExecutor{model: model}

	output, ok := executor.ExecuteSubagentTool(context.Background(), llm.ToolCall{
		Name:      "svc_status",
		Arguments: json.RawMessage(`{"name":"nginx"}`),
	})

	if !ok {
		t.Fatalf("svc_status should be allowed, output=%s", output)
	}
	if !strings.Contains(output, "nginx active") {
		t.Fatalf("output = %q", output)
	}
}
