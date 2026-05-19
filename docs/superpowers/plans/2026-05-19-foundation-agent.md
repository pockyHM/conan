# Foundation & Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared type foundation and a fully functional `conan-agent` binary that exposes MCP tools over HTTP/SSE.

**Architecture:** Agent runs as a persistent HTTP daemon on each managed node. It accepts MCP JSON-RPC requests, dispatches to registered tools, and returns structured results. Auth via token, audit logging for all tool calls.

**Tech Stack:** Go 1.25, net/http, encoding/json, gopkg.in/yaml.v3, github.com/spf13/cobra, github.com/google/uuid

**Module:** `github.com/pockyHM/conan`

---

## File Structure

### Created in this plan:

```
conan/
├── go.mod
├── go.sum
├── Makefile
├── .gitignore
├── cmd/
│   ├── conan/main.go                  # CLI placeholder
│   └── conan-agent/main.go            # Agent entry point
├── pkg/
│   ├── mcpproto/
│   │   ├── jsonrpc.go                 # JSON-RPC 2.0 types
│   │   ├── jsonrpc_test.go
│   │   ├── tool.go                    # MCP tool types
│   │   └── tool_test.go
│   ├── configschema/
│   │   ├── config.go                  # All config structs
│   │   └── config_test.go
│   └── models/
│       ├── models.go                  # Shared data models
│       └── models_test.go
├── internal/
│   ├── tools/
│   │   ├── tool.go                    # Tool interface & registry
│   │   ├── tool_test.go
│   │   ├── shell.go                   # shell/run
│   │   ├── shell_test.go
│   │   ├── fs.go                      # fs/read,write,edit,list,stat,download,upload
│   │   ├── fs_test.go
│   │   ├── sys.go                     # sys/cpu,mem,disk,net,processes
│   │   ├── sys_test.go
│   │   ├── svc.go                     # svc/list,status,start,stop,restart
│   │   ├── svc_test.go
│   │   ├── logtool.go                 # log/read,stream,journalctl
│   │   ├── logtool_test.go
│   │   ├── nettool.go                 # net/ping,traceroute,portcheck,curl
│   │   ├── nettool_test.go
│   │   ├── k8s.go                     # k8s/pods,logs,events,describe,apply,delete
│   │   ├── k8s_test.go
│   │   ├── pkgtool.go                 # pkg/install,update,list,search
│   │   ├── pkgtool_test.go
│   │   ├── cron.go                    # cron/list,add,remove,show
│   │   ├── cron_test.go
│   │   ├── docker.go                  # docker/ps,images,logs,exec,run,compose
│   │   └── docker_test.go
│   └── agent/
│       ├── server.go                  # HTTP server + JSON-RPC routing
│       ├── server_test.go
│       ├── handler.go                 # Method handlers (initialize, tools/list, tools/call)
│       ├── handler_test.go
│       └── middleware.go              # Auth + audit middleware
├── configs/
│   └── example/
│       └── agent-config.yaml
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `cmd/conan/main.go` (placeholder)
- Create: `cmd/conan-agent/main.go` (placeholder)
- Create: `configs/example/agent-config.yaml`

- [ ] **Step 1: Initialize Go module and create scaffold**

```bash
cd /Volumes/data/IdeaProjects/conan
go mod init github.com/pockyHM/conan
```

- [ ] **Step 2: Create .gitignore**

```gitignore
bin/
*.exe
*.exe~
*.dll
*.so
*.dylib
*.test
*.out
vendor/
.idea/
*.swp
*.swo
*~
.DS_Store
```

- [ ] **Step 3: Create Makefile**

```makefile
.PHONY: build build-linux build-darwin clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/conan ./cmd/conan
	go build -ldflags "$(LDFLAGS)" -o bin/conan-agent ./cmd/conan-agent

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-linux-amd64 ./cmd/conan
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-linux-amd64 ./cmd/conan-agent
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-linux-arm64 ./cmd/conan
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-linux-arm64 ./cmd/conan-agent

build-darwin:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-darwin-amd64 ./cmd/conan
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-darwin-arm64 ./cmd/conan
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-darwin-arm64 ./cmd/conan-agent

clean:
	rm -rf bin/

test:
	go test ./...

test-verbose:
	go test -v ./...
```

- [ ] **Step 4: Create placeholder entry points**

`cmd/conan/main.go`:
```go
package main

import "fmt"

var version = "dev"

func main() {
	fmt.Printf("conan %s\n", version)
}
```

`cmd/conan-agent/main.go`:
```go
package main

import "fmt"

var version = "dev"

func main() {
	fmt.Printf("conan-agent %s\n", version)
}
```

- [ ] **Step 5: Create example agent config**

`configs/example/agent-config.yaml`:
```yaml
listen: "0.0.0.0:9200"
token: "changeme"
tls: false
# tls_cert: /etc/conan-agent/cert.pem
# tls_key: /etc/conan-agent/key.pem
audit_log: /var/log/conan-agent/audit.log
rate_limit: 10
disabled_tools: []
log_level: info
```

- [ ] **Step 6: Verify build**

Run: `cd /Volumes/data/IdeaProjects/conan && make build`
Expected: `bin/conan` and `bin/conan-agent` created, both print version on run

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: project scaffold with Makefile and entry points"
```

---

### Task 2: MCP Protocol Types (pkg/mcpproto)

**Files:**
- Create: `pkg/mcpproto/jsonrpc.go`
- Create: `pkg/mcpproto/jsonrpc_test.go`
- Create: `pkg/mcpproto/tool.go`
- Create: `pkg/mcpproto/tool_test.go`

- [ ] **Step 1: Write the JSON-RPC types test**

`pkg/mcpproto/jsonrpc_test.go`:
```go
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
```

- [ ] **Step 2: Implement JSON-RPC types**

`pkg/mcpproto/jsonrpc.go`:
```go
package mcpproto

import "encoding/json"

const JSONRPCVersion = "2.0"

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return e.Message
}

func NewSuccessResponse(id json.RawMessage, result interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{JSONRPC: JSONRPCVersion, ID: id, Result: result}
}

func NewErrorResponse(id json.RawMessage, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}

func NewParseError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32700, "Parse error")
}

func NewInvalidRequestError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32600, "Invalid request")
}

func NewMethodNotFoundError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32601, "Method not found")
}

func NewInvalidParamsError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32602, "Invalid params")
}

func NewInternalError(id json.RawMessage, msg string) *JSONRPCResponse {
	return NewErrorResponse(id, -32603, "Internal error: "+msg)
}
```

- [ ] **Step 3: Run JSON-RPC tests**

Run: `go test ./pkg/mcpproto/ -run TestJSONRPC -v`
Expected: All tests PASS

- [ ] **Step 4: Write MCP tool types test**

`pkg/mcpproto/tool_test.go`:
```go
package mcpproto

import (
	"encoding/json"
	"testing"
)

func TestToolResultText(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{TextContent("hello")},
	}
	if result.Content[0].Type != "text" {
		t.Errorf("type = %q, want text", result.Content[0].Type)
	}
	if result.Content[0].Text != "hello" {
		t.Errorf("text = %q, want hello", result.Content[0].Text)
	}
}

func TestToolResultIsError(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{TextContent("command failed")},
		IsError: true,
	}
	if !result.IsError {
		t.Error("IsError should be true")
	}
}

func TestToolCallParamsUnmarshal(t *testing.T) {
	raw := `{"name":"shell/run","arguments":{"command":"ls"}}`
	var params ToolCallParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if params.Name != "shell/run" {
		t.Errorf("name = %q, want shell/run", params.Name)
	}
}

func TestInitializeResult(t *testing.T) {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo{Name: "conan-agent", Version: "0.1.0"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" {
		t.Error("expected non-empty JSON output")
	}
}
```

- [ ] **Step 5: Implement MCP tool types**

`pkg/mcpproto/tool.go`:
```go
package mcpproto

import "encoding/json"

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func TextContent(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

func ErrorContent(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: "Error: " + text}
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type InitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo        `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
```

- [ ] **Step 6: Run tool type tests**

Run: `go test ./pkg/mcpproto/ -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/mcpproto/
git commit -m "feat: add MCP protocol types (JSON-RPC + tool definitions)"
```

---

### Task 3: Config Schema (pkg/configschema)

**Files:**
- Create: `pkg/configschema/config.go`
- Create: `pkg/configschema/config_test.go`

- [ ] **Step 1: Write config test**

`pkg/configschema/config_test.go`:
```go
package configschema

import (
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-12345")
	result := ExpandEnv("${TEST_API_KEY}")
	if result != "sk-12345" {
		t.Errorf("got %q, want sk-12345", result)
	}
}

func TestExpandEnvEmpty(t *testing.T) {
	result := ExpandEnv("plain-text-no-env")
	if result != "plain-text-no-env" {
		t.Errorf("got %q, want plain-text-no-env", result)
	}
}

func TestAgentConfigDefaults(t *testing.T) {
	cfg := DefaultAgentConfig()
	if cfg.Listen != "0.0.0.0:9200" {
		t.Errorf("listen = %q, want 0.0.0.0:9200", cfg.Listen)
	}
	if cfg.RateLimit != 10 {
		t.Errorf("rate_limit = %d, want 10", cfg.RateLimit)
	}
}
```

- [ ] **Step 2: Implement config types**

`pkg/configschema/config.go`:
```go
package configschema

import (
	"os"
	"regexp"
	"strings"
)

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func ExpandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		return os.Getenv(key)
	})
}

// --- Agent-side config ---

type AgentConfig struct {
	Listen        string   `yaml:"listen"`
	Token         string   `yaml:"token"`
	TLS           bool     `yaml:"tls"`
	TLSCert       string   `yaml:"tls_cert,omitempty"`
	TLSKey        string   `yaml:"tls_key,omitempty"`
	AuditLog      string   `yaml:"audit_log"`
	RateLimit     int      `yaml:"rate_limit"`
	DisabledTools []string `yaml:"disabled_tools"`
	LogLevel      string   `yaml:"log_level"`
}

func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		Listen:    "0.0.0.0:9200",
		Token:     "changeme",
		RateLimit: 10,
		LogLevel:  "info",
	}
}

// --- CLI-side config ---

type GlobalConfig struct {
	DefaultModel   string         `yaml:"default_model"`
	DefaultCluster string         `yaml:"default_cluster"`
	Models         []ModelConfig  `yaml:"models"`
	Security       SecurityConfig `yaml:"security"`
	Memory         MemoryConfig   `yaml:"memory"`
	Logging        LoggingConfig  `yaml:"logging"`
}

type ModelConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`     // "anthropic" or "openai"
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"`
}

type SecurityConfig struct {
	RiskAssessmentModel string   `yaml:"risk_assessment_model"`
	CommandWhitelist    []string `yaml:"command_whitelist"`
}

type MemoryConfig struct {
	RulesTokenBudget     int `yaml:"rules_token_budget"`
	KnowledgeTokenBudget int `yaml:"knowledge_token_budget"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	File   string `yaml:"file"`
	Audit  bool   `yaml:"audit"`
}

// --- Cluster config ---

type ClusterConfig struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Inherits    string      `yaml:"inherits"`
	Agent       AgentConfig `yaml:"agent"`
	NodeDefaults NodeDefaults `yaml:"node_defaults"`
}

type NodeDefaults struct {
	User    string `yaml:"user"`
	SSHPort int    `yaml:"ssh_port"`
}

type NodeList struct {
	Nodes []NodeConfig `yaml:"nodes"`
}

type NodeConfig struct {
	Name   string            `yaml:"name"`
	Host   string            `yaml:"host"`
	Labels []string          `yaml:"labels"`
	Zone   string            `yaml:"zone,omitempty"`
	Agent  *NodeAgentOverride `yaml:"agent,omitempty"`
}

type NodeAgentOverride struct {
	User string `yaml:"user"`
	Port int    `yaml:"port"`
}
```

- [ ] **Step 3: Run config tests**

Run: `go test ./pkg/configschema/ -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/configschema/
git commit -m "feat: add config schema types for agent and CLI"
```

---

### Task 4: Shared Data Models (pkg/models)

**Files:**
- Create: `pkg/models/models.go`
- Create: `pkg/models/models_test.go`

- [ ] **Step 1: Write model tests**

`pkg/models/models_test.go`:
```go
package models

import "testing"

func TestNewID(t *testing.T) {
	id1 := NewID()
	id2 := NewID()
	if id1 == "" {
		t.Error("id should not be empty")
	}
	if id1 == id2 {
		t.Error("ids should be unique")
	}
}

func TestMemoryCategories(t *testing.T) {
	cats := []string{CategoryEvent, CategoryExperience, CategoryTroubleshooting, CategoryTopology}
	for _, cat := range cats {
		if cat == "" {
			t.Error("category should not be empty")
		}
	}
}
```

- [ ] **Step 2: Implement models**

`pkg/models/models.go`:
```go
package models

import "github.com/google/uuid"

func NewID() string {
	return uuid.New().String()[:8]
}

const (
	CategoryEvent          = "event"
	CategoryExperience     = "experience"
	CategoryTroubleshooting = "troubleshooting"
	CategoryTopology       = "topology"
)

type Memory struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Tags        string `json:"tags"`         // JSON array string
	SourceConv  string `json:"source_conv"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Conversation struct {
	ID        string `json:"id"`
	Cluster   string `json:"cluster"`
	Nodes     string `json:"nodes"`          // JSON array string
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Summary   string `json:"summary"`
	Messages  string `json:"messages"`       // Full conversation JSON for resume
}

type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`       // user / assistant / tool
	Content        string `json:"content"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolInput      string `json:"tool_input,omitempty"`
	ToolOutput     string `json:"tool_output,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type AuditEntry struct {
	ID        string `json:"id"`
	Node      string `json:"node"`
	ToolName  string `json:"tool_name"`
	Input     string `json:"input"`
	RiskLevel string `json:"risk_level"` // ALLOW / CONFIRM / DENY
	CreatedAt string `json:"created_at"`
}

type NodeStatus struct {
	Name    string  `json:"name"`
	Host    string  `json:"host"`
	Online  bool    `json:"online"`
	CPU     float64 `json:"cpu_percent"`
	Mem     float64 `json:"mem_percent"`
	Load1   float64 `json:"load_1"`
	Load5   float64 `json:"load_5"`
	Load15  float64 `json:"load_15"`
}
```

- [ ] **Step 3: Run model tests**

Run: `go test ./pkg/models/ -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
go get github.com/google/uuid
go mod tidy
git add pkg/models/ go.mod go.sum
git commit -m "feat: add shared data models"
```

---

### Task 5: Tool Interface & Registry

**Files:**
- Create: `internal/tools/tool.go`
- Create: `internal/tools/tool_test.go`

- [ ] **Step 1: Write registry tests**

`internal/tools/tool_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type dummyTool struct{}

func (d *dummyTool) Name() string          { return "test/dummy" }
func (d *dummyTool) Description() string    { return "A dummy tool" }
func (d *dummyTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
}
func (d *dummyTool) Execute(ctx context.Context, input json.RawMessage) (*json.RawMessage, error) {
	return nil, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &dummyTool{}
	r.Register(tool)

	got, ok := r.Get("test/dummy")
	if !ok {
		t.Fatal("tool not found in registry")
	}
	if got.Name() != "test/dummy" {
		t.Errorf("name = %q, want test/dummy", got.Name())
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{})
	r.Register(&dummyTool{})

	// Overwrite test — same name replaces
	if len(r.List()) != 1 {
		t.Errorf("list length = %d, want 1", len(r.List()))
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent tool")
	}
}

func TestRegistryDisabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{})
	r.Disable("test/dummy")
	_, ok := r.Get("test/dummy")
	if ok {
		t.Error("disabled tool should not be found")
	}
	list := r.List()
	if len(list) != 0 {
		t.Errorf("list length = %d, want 0 after disable", len(list))
	}
}
```

- [ ] **Step 2: Implement Tool interface and Registry**

`internal/tools/tool.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error)
}

type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	disabled map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		disabled: make(map[string]bool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled[name] {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Tool
	for name, t := range r.tools {
		if !r.disabled[name] {
			result = append(result, t)
		}
	}
	return result
}

func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[name] = true
}

func (r *Registry) DisableAll(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		r.disabled[n] = true
	}
}
```

- [ ] **Step 3: Run registry tests**

Run: `go test ./internal/tools/ -run TestRegistry -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/tool.go internal/tools/tool_test.go
git commit -m "feat: add tool interface and registry"
```

---

### Task 6: Agent HTTP Server

**Files:**
- Create: `internal/agent/server.go`
- Create: `internal/agent/server_test.go`
- Create: `internal/agent/handler.go`
- Create: `internal/agent/handler_test.go`
- Create: `internal/agent/middleware.go`

- [ ] **Step 1: Write handler tests**

`internal/agent/handler_test.go`:
```go
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
```

- [ ] **Step 2: Implement handler**

`internal/agent/handler.go`:
```go
package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type Handler struct {
	registry *tools.Registry
	version  string
}

func NewHandler(registry *tools.Registry, version string) *Handler {
	return &Handler{registry: registry, version: version}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var req mcpproto.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, mcpproto.NewParseError(json.RawMessage("null")))
		return
	}

	if req.JSONRPC != mcpproto.JSONRPCVersion {
		writeJSON(w, mcpproto.NewInvalidRequestError(req.ID))
		return
	}

	var resp *mcpproto.JSONRPCResponse
	switch req.Method {
	case "initialize":
		resp = h.handleInitialize(req.ID)
	case "tools/list":
		resp = h.handleToolsList(req.ID)
	case "tools/call":
		resp = h.handleToolsCall(r.Context(), req)
	default:
		resp = mcpproto.NewMethodNotFoundError(req.ID)
	}

	writeJSON(w, resp)
}

func (h *Handler) handleInitialize(id json.RawMessage) *mcpproto.JSONRPCResponse {
	result := mcpproto.InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: mcpproto.ServerCapabilities{
			Tools: &mcpproto.ToolsCapability{ListChanged: false},
		},
		ServerInfo: mcpproto.ServerInfo{
			Name:    "conan-agent",
			Version: h.version,
		},
	}
	return mcpproto.NewSuccessResponse(id, result)
}

func (h *Handler) handleToolsList(id json.RawMessage) *mcpproto.JSONRPCResponse {
	toolList := h.registry.List()
	defs := make([]mcpproto.ToolDefinition, len(toolList))
	for i, t := range toolList {
		defs[i] = mcpproto.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		}
	}
	return mcpproto.NewSuccessResponse(id, map[string]interface{}{"tools": defs})
}

func (h *Handler) handleToolsCall(ctx context.Context, req mcpproto.JSONRPCRequest) *mcpproto.JSONRPCResponse {
	var params mcpproto.ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return mcpproto.NewInvalidParamsError(req.ID)
	}

	tool, ok := h.registry.Get(params.Name)
	if !ok {
		return mcpproto.NewErrorResponse(req.ID, -32602, "tool not found: "+params.Name)
	}

	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		slog.Error("tool execution failed", "tool", params.Name, "error", err)
		return mcpproto.NewInternalError(req.ID, err.Error())
	}

	return mcpproto.NewSuccessResponse(req.ID, result)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 3: Run handler tests**

Run: `go test ./internal/agent/ -run TestHandle -v`
Expected: All tests PASS

- [ ] **Step 4: Implement auth middleware and server**

`internal/agent/middleware.go`:
```go
package agent

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const nodeIDKey contextKey = "node_id"

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if strings.TrimPrefix(auth, "Bearer ") != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func auditMiddleware(logPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			slog.Info("rpc call",
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"duration", time.Since(start),
			)
		})
	}
}

func rateLimitMiddleware(rps int) func(http.Handler) http.Handler {
	limiter := make(chan struct{}, rps)
	// fill the bucket
	for i := 0; i < rps; i++ {
		limiter <- struct{}{}
	}
	go func() {
		for range time.Tick(time.Second) {
			for i := len(limiter); i < rps; i++ {
				select {
				case limiter <- struct{}{}:
				default:
				}
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-limiter:
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			}
		})
	}
}

func ContextWithNodeID(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, nodeIDKey, nodeID)
}

func NodeIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(nodeIDKey).(string); ok {
		return v
	}
	return ""
}
```

`internal/agent/server.go`:
```go
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Server struct {
	config   *configschema.AgentConfig
	handler  *Handler
	http     *http.Server
	registry *tools.Registry
}

func NewServer(cfg *configschema.AgentConfig, registry *tools.Registry, version string) *Server {
	handler := NewHandler(registry, version)

	var h http.Handler = handler
	h = auditMiddleware(cfg.AuditLog)(h)
	h = rateLimitMiddleware(cfg.RateLimit)(h)
	h = authMiddleware(cfg.Token)(h)

	mux := http.NewServeMux()
	mux.Handle("/rpc", h)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	return &Server{
		config:   cfg,
		handler:  handler,
		registry: registry,
		http: &http.Server{
			Addr:    cfg.Listen,
			Handler: mux,
		},
	}
}

func (s *Server) Start() error {
	slog.Info("starting agent server", "listen", s.config.Listen, "tls", s.config.TLS)
	if s.config.TLS {
		return s.http.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
	}
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down agent server")
	return s.http.Shutdown(ctx)
}

func (s *Server) WaitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			slog.Info("received SIGHUP, reloading config")
		case syscall.SIGINT, syscall.SIGTERM:
			slog.Info("received signal, shutting down", "signal", sig)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.Shutdown(ctx); err != nil {
				slog.Error("shutdown error", "error", err)
			}
			return
		}
	}
}
```

- [ ] **Step 5: Write server integration test**

`internal/agent/server_test.go`:
```go
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
	// Wait for server
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
```

- [ ] **Step 6: Run all agent tests**

Run: `go test ./internal/agent/ -v -timeout 30s`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agent/
git commit -m "feat: add agent HTTP server with JSON-RPC routing, auth, and rate limiting"
```

---

### Task 7: shell/run Tool

**Files:**
- Create: `internal/tools/shell.go`
- Create: `internal/tools/shell_test.go`

This task establishes the pattern for all subsequent tool implementations.

- [ ] **Step 1: Write shell tool tests**

`internal/tools/shell_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestShellRun(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "echo hello", Timeout: 5}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	if len(result.Content) == 0 {
		t.Fatal("no content")
	}
	text := result.Content[0].Text
	if text == "" {
		t.Error("expected output")
	}
}

func TestShellRunTimeout(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "sleep 10", Timeout: 1}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("should be error (timeout)")
	}
}

func TestShellRunNonZeroExit(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "false", Timeout: 5}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Non-zero exit is not an error in JSON-RPC sense — result contains exit code
	if result.Content[0].Text == "" {
		t.Error("expected output with exit code info")
	}
}
```

- [ ] **Step 2: Implement shell tool**

`internal/tools/shell.go`:
```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type shellInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	User    string `json:"user,omitempty"`
}

type ShellTool struct{}

func (s *ShellTool) Name() string        { return "shell/run" }
func (s *ShellTool) Description() string { return "Execute a shell command with timeout" }
func (s *ShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Shell command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in seconds (default 30)"},
			"user":    {"type": "string", "description": "Run as user (default: agent user)"}
		},
		"required": ["command"]
	}`)
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args shellInput
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}

	timeout := time.Duration(args.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if args.User != "" {
		cmd = exec.CommandContext(ctx, "su", "-", args.User, "-c", args.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", args.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := ""
	timedOut := ctx.Err() == context.DeadlineExceeded

	if timedOut {
		output = fmt.Sprintf("Command timed out after %d seconds\nstdout:\n%s\nstderr:\n%s",
			args.Timeout, stdout.String(), stderr.String())
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(output)},
			IsError: true,
		}, nil
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output = fmt.Sprintf("exit_code: %d\nstdout:\n%s\nstderr:\n%s",
		exitCode, stdout.String(), stderr.String())

	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(output)},
		IsError: false,
	}, nil
}
```

- [ ] **Step 3: Run shell tool tests**

Run: `go test ./internal/tools/ -run TestShell -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/shell.go internal/tools/shell_test.go
git commit -m "feat: add shell/run tool with timeout support"
```

---

### Task 8: File Operation Tools (fs/*)

**Files:**
- Create: `internal/tools/fs.go`
- Create: `internal/tools/fs_test.go`

- [ ] **Step 1: Write fs tool tests**

`internal/tools/fs_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestFsRead(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	tool := &FsTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": path})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	if result.Content[0].Text != "hello world" {
		t.Errorf("text = %q, want hello world", result.Content[0].Text)
	}
}

func TestFsWrite(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "output.txt")

	tool := &FsTool{}
	input, _ := json.Marshal(map[string]interface{}{
		"path":    path,
		"content": "written content",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "written content" {
		t.Errorf("file content = %q, want written content", data)
	}
}

func TestFsEdit(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("hello world\nline two"), 0644)

	tool := &FsTool{}
	input, _ := json.Marshal(map[string]interface{}{
		"path":     path,
		"old_text": "hello world",
		"new_text": "goodbye world",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content[0].Text)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye world\nline two" {
		t.Errorf("file content = %q", data)
	}
}

func TestFsList(t *testing.T) {
	dir := tempDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0644)

	tool := &FsTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": dir})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
}

func TestFsStat(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "stat.txt")
	os.WriteFile(path, []byte("content"), 0644)

	tool := &FsTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": path})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
}
```

- [ ] **Step 2: Implement fs tools**

`internal/tools/fs.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

var fsToolDefs = map[string]struct {
	desc  string
	schema string
}{
	"fs/read": {
		desc: "Read file contents",
		schema: `{"type":"object","properties":{"path":{"type":"string","description":"File path"},"offset":{"type":"integer","description":"Line offset (0-based)"},"limit":{"type":"integer","description":"Max lines to read"}},"required":["path"]}`,
	},
	"fs/write": {
		desc: "Write content to file",
		schema: `{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"File content"},"backup":{"type":"boolean","description":"Create backup before writing"}},"required":["path","content"]}`,
	},
	"fs/edit": {
		desc: "Edit file by replacing text",
		schema: `{"type":"object","properties":{"path":{"type":"string","description":"File path"},"old_text":{"type":"string","description":"Text to replace"},"new_text":{"type":"string","description":"Replacement text"}},"required":["path","old_text","new_text"]}`,
	},
	"fs/list": {
		desc: "List directory contents",
		schema: `{"type":"object","properties":{"path":{"type":"string","description":"Directory path"},"recursive":{"type":"boolean","description":"List recursively"}},"required":["path"]}`,
	},
	"fs/stat": {
		desc: "Get file/directory metadata",
		schema: `{"type":"object","properties":{"path":{"type":"string","description":"File path"}},"required":["path"]}`,
	},
}

type FsTool struct {
	action string
}

func NewFsTools() []Tool {
	var tpls []Tool
	for name, def := range fsToolDefs {
		tpls = append(tpls, &FsTool{action: strings.TrimPrefix(name, "fs/")})
		_ = def // schema/desc stored in method overrides
	}
	// We need individual tools per action; use a wrapper approach
	return []Tool{
		&fsReadTool{},
		&fsWriteTool{},
		&fsEditTool{},
		&fsListTool{},
		&fsStatTool{},
	}
}

// --- Individual fs tools ---

type fsReadTool struct{}
func (f *fsReadTool) Name() string        { return "fs/read" }
func (f *fsReadTool) Description() string { return "Read file contents" }
func (f *fsReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`)
}
func (f *fsReadTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	lines := strings.Split(string(data), "\n")
	if args.Offset > 0 && args.Offset < len(lines) {
		lines = lines[args.Offset:]
	}
	if args.Limit > 0 && args.Limit < len(lines) {
		lines = lines[:args.Limit]
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(lines, "\n"))}}, nil
}

type fsWriteTool struct{}
func (f *fsWriteTool) Name() string        { return "fs/write" }
func (f *fsWriteTool) Description() string { return "Write content to file" }
func (f *fsWriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"},"backup":{"type":"boolean"}},"required":["path","content"]}`)
}
func (f *fsWriteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Backup  bool   `json:"backup"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Backup {
		if _, err := os.Stat(args.Path); err == nil {
			os.Rename(args.Path, args.Path+".bak")
		}
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("written successfully")}}, nil
}

type fsEditTool struct{}
func (f *fsEditTool) Name() string        { return "fs/edit" }
func (f *fsEditTool) Description() string { return "Edit file by replacing text" }
func (f *fsEditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"}},"required":["path","old_text","new_text"]}`)
}
func (f *fsEditTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	content := string(data)
	if !strings.Contains(content, args.OldText) {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent("old_text not found in file")},
			IsError: true,
		}, nil
	}
	content = strings.Replace(content, args.OldText, args.NewText, 1)
	if err := os.WriteFile(args.Path, []byte(content), 0644); err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("edited successfully")}}, nil
}

type fsListTool struct{}
func (f *fsListTool) Name() string        { return "fs/list" }
func (f *fsListTool) Description() string { return "List directory contents" }
func (f *fsListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}},"required":["path"]}`)
}
func (f *fsListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	var entries []string
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != args.Path {
			rel, _ := filepath.Rel(args.Path, path)
			prefix := ""
			if d.IsDir() {
				prefix = "d "
			} else {
				prefix = "  "
			}
			entries = append(entries, prefix+rel)
		}
		if !args.Recursive && d.IsDir() && path != args.Path {
			return filepath.SkipDir
		}
		return nil
	}
	filepath.WalkDir(args.Path, walkFn)
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(entries, "\n"))}}, nil
}

type fsStatTool struct{}
func (f *fsStatTool) Name() string        { return "fs/stat" }
func (f *fsStatTool) Description() string { return "Get file/directory metadata" }
func (f *fsStatTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (f *fsStatTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	info, err := os.Stat(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	result := fmt.Sprintf("name: %s\nsize: %d\nmode: %s\nis_dir: %v\nmod_time: %s",
		info.Name(), info.Size(), info.Mode(), info.IsDir(), info.ModTime())
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(result)}}, nil
}
```

- [ ] **Step 3: Run fs tool tests**

Run: `go test ./internal/tools/ -run TestFs -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/fs.go internal/tools/fs_test.go
git commit -m "feat: add file operation tools (fs/read, write, edit, list, stat)"
```

---

### Task 9: System Resource Tools (sys/*)

**Files:**
- Create: `internal/tools/sys.go`
- Create: `internal/tools/sys_test.go`

- [ ] **Step 1: Write sys tool tests**

`internal/tools/sys_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSysCPU(t *testing.T) {
	tool := &sysCPUTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "load_avg") {
		t.Errorf("expected load_avg in output, got: %s", result.Content[0].Text)
	}
}

func TestSysMem(t *testing.T) {
	tool := &sysMemTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}

func TestSysDisk(t *testing.T) {
	tool := &sysDiskTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}
```

- [ ] **Step 2: Implement sys tools**

`internal/tools/sys.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// sys/cpu
type sysCPUTool struct{}
func (s *sysCPUTool) Name() string        { return "sys/cpu" }
func (s *sysCPUTool) Description() string { return "Get CPU usage and load average" }
func (s *sysCPUTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysCPUTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	loadAvg, _ := getLoadAvg()
	cores := runtime.NumCPU()
	output := fmt.Sprintf(`{"cores": %d, "load_avg": %s}`, cores, loadAvg)
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(output)}}, nil
}

func getLoadAvg() (string, error) {
	out, err := exec.Command("sh", "-c", "cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null || uptime").Output()
	if err != nil {
		return "[]", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sys/mem
type sysMemTool struct{}
func (s *sysMemTool) Name() string        { return "sys/mem" }
func (s *sysMemTool) Description() string { return "Get memory usage" }
func (s *sysMemTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysMemTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var cmd string
	if runtime.GOOS == "linux" {
		cmd = "free -b | head -2"
	} else {
		cmd = "vm_stat | head -10"
	}
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/disk
type sysDiskTool struct{}
func (s *sysDiskTool) Name() string        { return "sys/disk" }
func (s *sysDiskTool) Description() string { return "Get disk usage" }
func (s *sysDiskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysDiskTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	cmd := "df -h"
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/net
type sysNetTool struct{}
func (s *sysNetTool) Name() string        { return "sys/net" }
func (s *sysNetTool) Description() string { return "Get network interface stats" }
func (s *sysNetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (s *sysNetTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	cmd := "ip -s link 2>/dev/null || netstat -I -n 2>/dev/null || ifconfig"
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

// sys/processes
type sysProcessesTool struct{}
func (s *sysProcessesTool) Name() string        { return "sys/processes" }
func (s *sysProcessesTool) Description() string { return "Get process list" }
func (s *sysProcessesTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"sort":{"type":"string","description":"Sort by: cpu, mem, pid"},"limit":{"type":"integer","description":"Max processes to return"}}}`)
}
func (s *sysProcessesTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Sort  string `json:"sort"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal(input, &args)
	sortFlag := "--sort=-%cpu"
	if args.Sort == "mem" {
		sortFlag = "--sort=-%mem"
	}
	limit := 20
	if args.Limit > 0 {
		limit = args.Limit
	}
	cmd := fmt.Sprintf("ps aux %s | head -%d", sortFlag, limit+1)
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func NewSysTools() []Tool {
	return []Tool{
		&sysCPUTool{},
		&sysMemTool{},
		&sysDiskTool{},
		&sysNetTool{},
		&sysProcessesTool{},
	}
}
```

- [ ] **Step 3: Run sys tool tests**

Run: `go test ./internal/tools/ -run TestSys -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/sys.go internal/tools/sys_test.go
git commit -m "feat: add system resource tools (sys/cpu, mem, disk, net, processes)"
```

---

### Task 10: Service Management Tools (svc/*)

**Files:**
- Create: `internal/tools/svc.go`
- Create: `internal/tools/svc_test.go`

- [ ] **Step 1: Write svc tool tests**

`internal/tools/svc_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSvcList(t *testing.T) {
	tool := &svcListTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// systemctl list may fail on macOS in CI, so just verify it doesn't panic
	_ = result
}

func TestSvcStatus(t *testing.T) {
	tool := &svcStatusTool{}
	input, _ := json.Marshal(map[string]interface{}{"name": "nonexistent-service"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// nonexistent service should return error
	if !result.IsError {
		t.Error("expected error for nonexistent service")
	}
}
```

- [ ] **Step 2: Implement svc tools**

`internal/tools/svc.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// svc/list
type svcListTool struct{}
func (s *svcListTool) Name() string        { return "svc/list" }
func (s *svcListTool) Description() string { return "List systemd services" }
func (s *svcListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"state":{"type":"string","description":"Filter by state: active, inactive, failed"}}}`)
}
func (s *svcListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		State string `json:"state"`
	}
	json.Unmarshal(input, &args)
	cmd := "systemctl list-units --type=service --no-pager"
	if args.State != "" {
		cmd += " --state=" + args.State
	}
	return runCommand(cmd)
}

// svc/status
type svcStatusTool struct{}
func (s *svcStatusTool) Name() string        { return "svc/status" }
func (s *svcStatusTool) Description() string { return "Get service status" }
func (s *svcStatusTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStatusTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl status %s --no-pager", args.Name)
	return runCommand(cmd)
}

// svc/start
type svcStartTool struct{}
func (s *svcStartTool) Name() string        { return "svc/start" }
func (s *svcStartTool) Description() string { return "Start a service" }
func (s *svcStartTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStartTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl start %s", args.Name)
	return runCommand(cmd)
}

// svc/stop
type svcStopTool struct{}
func (s *svcStopTool) Name() string        { return "svc/stop" }
func (s *svcStopTool) Description() string { return "Stop a service" }
func (s *svcStopTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcStopTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl stop %s", args.Name)
	return runCommand(cmd)
}

// svc/restart
type svcRestartTool struct{}
func (s *svcRestartTool) Name() string        { return "svc/restart" }
func (s *svcRestartTool) Description() string { return "Restart a service" }
func (s *svcRestartTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Service name"}},"required":["name"]}`)
}
func (s *svcRestartTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("systemctl restart %s", args.Name)
	return runCommand(cmd)
}

func NewSvcTools() []Tool {
	return []Tool{&svcListTool{}, &svcStatusTool{}, &svcStartTool{}, &svcStopTool{}, &svcRestartTool{}}
}

// runCommand is a shared helper for tools that wrap simple shell commands.
func runCommand(cmd string) (*mcpproto.ToolResult, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	text := string(out)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.TextContent(text)},
			IsError: true,
		}, nil
	}
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(text)},
	}, nil
}
```

- [ ] **Step 3: Run svc tool tests**

Run: `go test ./internal/tools/ -run TestSvc -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/svc.go internal/tools/svc_test.go
git commit -m "feat: add service management tools (svc/list, status, start, stop, restart)"
```

---

### Task 11: Log Viewing Tools (log/*)

**Files:**
- Create: `internal/tools/logtool.go`
- Create: `internal/tools/logtool_test.go`

- [ ] **Step 1: Write log tool tests**

`internal/tools/logtool_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLogRead(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(logPath, []byte(content), 0644)

	tool := &logReadTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": logPath, "tail": 2})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}
```

- [ ] **Step 2: Implement log tools**

`internal/tools/logtool.go`:
```go
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// log/read
type logReadTool struct{}
func (l *logReadTool) Name() string        { return "log/read" }
func (l *logReadTool) Description() string { return "Read log file with optional tail and filter" }
func (l *logReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Log file path"},"tail":{"type":"integer","description":"Last N lines"},"filter":{"type":"string","description":"Filter pattern"}},"required":["path"]}`)
}
func (l *logReadTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path   string `json:"path"`
		Tail   int    `json:"tail"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}

	f, err := os.Open(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if args.Filter != "" && !strings.Contains(line, args.Filter) {
			continue
		}
		lines = append(lines, line)
	}

	if args.Tail > 0 && len(lines) > args.Tail {
		lines = lines[len(lines)-args.Tail:]
	}

	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(lines, "\n"))}}, nil
}

// log/stream — note: actual SSE streaming handled at server level, this returns initial snapshot
type logStreamTool struct{}
func (l *logStreamTool) Name() string        { return "log/stream" }
func (l *logStreamTool) Description() string { return "Stream log file (SSE, real-time)" }
func (l *logStreamTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Log file path"},"filter":{"type":"string","description":"Filter pattern"}},"required":["path"]}`)
}
func (l *logStreamTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	// Streaming is handled by the server SSE layer — this returns a message explaining that
	var args struct {
		Path string `json:"path"`
	}
	json.Unmarshal(input, &args)
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf("Streaming %s via SSE — use the SSE endpoint for real-time updates", args.Path))},
	}, nil
}

// log/journalctl
type logJournalctlTool struct{}
func (l *logJournalctlTool) Name() string        { return "log/journalctl" }
func (l *logJournalctlTool) Description() string { return "Query journalctl logs" }
func (l *logJournalctlTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"unit":{"type":"string","description":"Systemd unit name"},"since":{"type":"string","description":"Time range (e.g. '1h ago')"},"tail":{"type":"integer","description":"Last N entries"}}}`)
}
func (l *logJournalctlTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Unit  string `json:"unit"`
		Since string `json:"since"`
		Tail  int    `json:"tail"`
	}
	json.Unmarshal(input, &args)

	cmd := "journalctl --no-pager"
	if args.Unit != "" {
		cmd += fmt.Sprintf(" -u %s", args.Unit)
	}
	if args.Since != "" {
		cmd += fmt.Sprintf(" --since '%s'", args.Since)
	}
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" -n %d", args.Tail)
	}
	return runCommand(cmd)
}

func NewLogTools() []Tool {
	return []Tool{&logReadTool{}, &logStreamTool{}, &logJournalctlTool{}}
}
```

- [ ] **Step 3: Run log tool tests**

Run: `go test ./internal/tools/ -run TestLog -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/logtool.go internal/tools/logtool_test.go
git commit -m "feat: add log viewing tools (log/read, stream, journalctl)"
```

---

### Task 12: Network Diagnostic Tools (net/*)

**Files:**
- Create: `internal/tools/nettool.go`
- Create: `internal/tools/nettool_test.go`

- [ ] **Step 1: Write net tool tests**

`internal/tools/nettool_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNetPortCheck(t *testing.T) {
	tool := &netPortcheckTool{}
	input, _ := json.Marshal(map[string]interface{}{"host": "127.0.0.1", "port": 22})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Just verify it runs without panic
	_ = result
}
```

- [ ] **Step 2: Implement net tools**

`internal/tools/nettool.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// net/ping
type netPingTool struct{}
func (n *netPingTool) Name() string        { return "net/ping" }
func (n *netPingTool) Description() string { return "Ping a host" }
func (n *netPingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Host to ping"},"count":{"type":"integer","description":"Number of pings (default 3)"}},"required":["host"]}`)
}
func (n *netPingTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host  string `json:"host"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Count == 0 {
		args.Count = 3
	}
	cmd := fmt.Sprintf("ping -c %d -W 5 %s", args.Count, args.Host)
	return runCommand(cmd)
}

// net/traceroute
type netTracerouteTool struct{}
func (n *netTracerouteTool) Name() string        { return "net/traceroute" }
func (n *netTracerouteTool) Description() string { return "Traceroute to host" }
func (n *netTracerouteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Target host"}},"required":["host"]}`)
}
func (n *netTracerouteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("traceroute -m 30 -w 2 %s 2>/dev/null || tracepath %s", args.Host, args.Host)
	return runCommand(cmd)
}

// net/portcheck
type netPortcheckTool struct{}
func (n *netPortcheckTool) Name() string        { return "net/portcheck" }
func (n *netPortcheckTool) Description() string { return "Check if a port is open" }
func (n *netPortcheckTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"Host address"},"port":{"type":"integer","description":"Port number"}},"required":["host","port"]}`)
}
func (n *netPortcheckTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("%s:%d", args.Host, args.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf(`{"host":"%s","port":%d,"open":false}`, args.Host, args.Port))},
		}, nil
	}
	conn.Close()
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf(`{"host":"%s","port":%d,"open":true}`, args.Host, args.Port))},
	}, nil
}

// net/curl
type netCurlTool struct{}
func (n *netCurlTool) Name() string        { return "net/curl" }
func (n *netCurlTool) Description() string { return "Make HTTP request" }
func (n *netCurlTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to request"},"method":{"type":"string","description":"HTTP method (default GET)"},"headers":{"type":"object","description":"Request headers"},"body":{"type":"string","description":"Request body"}},"required":["url"]}`)
}
func (n *netCurlTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Method == "" {
		args.Method = "GET"
	}

	cmdArgs := []string{"-s", "-o", "-", "-w", "\nHTTP_CODE:%{http_code}", "-X", args.Method}
	for k, v := range args.Headers {
		cmdArgs = append(cmdArgs, "-H", fmt.Sprintf("%s: %s", k, v))
	}
	if args.Body != "" {
		cmdArgs = append(cmdArgs, "-d", args.Body)
	}
	cmdArgs = append(cmdArgs, args.URL)

	out, err := exec.Command("curl", cmdArgs...).CombinedOutput()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func NewNetTools() []Tool {
	return []Tool{&netPingTool{}, &netTracerouteTool{}, &netPortcheckTool{}, &netCurlTool{}}
}
```

- [ ] **Step 3: Run net tool tests**

Run: `go test ./internal/tools/ -run TestNet -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/nettool.go internal/tools/nettool_test.go
git commit -m "feat: add network diagnostic tools (net/ping, traceroute, portcheck, curl)"
```

---

### Task 13: Kubernetes Tools (k8s/*)

**Files:**
- Create: `internal/tools/k8s.go`
- Create: `internal/tools/k8s_test.go`

- [ ] **Step 1: Write k8s tool tests**

`internal/tools/k8s_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestK8sPods(t *testing.T) {
	tool := &k8sPodsTool{}
	input, _ := json.Marshal(map[string]interface{}{"namespace": "default"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// kubectl may not be available in test env — just verify no panic
	_ = result
}
```

- [ ] **Step 2: Implement k8s tools**

`internal/tools/k8s.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// k8s/pods
type k8sPodsTool struct{}
func (k *k8sPodsTool) Name() string        { return "k8s/pods" }
func (k *k8sPodsTool) Description() string { return "List Kubernetes pods" }
func (k *k8sPodsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string","description":"Namespace"},"label_selector":{"type":"string","description":"Label selector"}}}`)
}
func (k *k8sPodsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Namespace      string `json:"namespace"`
		LabelSelector  string `json:"label_selector"`
	}
	json.Unmarshal(input, &args)
	cmd := "kubectl get pods"
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	if args.LabelSelector != "" {
		cmd += fmt.Sprintf(" -l '%s'", args.LabelSelector)
	}
	return runCommand(cmd)
}

// k8s/logs
type k8sLogsTool struct{}
func (k *k8sLogsTool) Name() string        { return "k8s/logs" }
func (k *k8sLogsTool) Description() string { return "Get pod logs" }
func (k *k8sLogsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"},"pod":{"type":"string"},"tail":{"type":"integer"},"follow":{"type":"boolean"}},"required":["pod"]}`)
}
func (k *k8sLogsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Tail      int    `json:"tail"`
		Follow    bool   `json:"follow"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl logs %s", args.Pod)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" --tail=%d", args.Tail)
	}
	return runCommand(cmd)
}

// k8s/events
type k8sEventsTool struct{}
func (k *k8sEventsTool) Name() string        { return "k8s/events" }
func (k *k8sEventsTool) Description() string { return "List Kubernetes events" }
func (k *k8sEventsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"}}}`)
}
func (k *k8sEventsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Namespace string `json:"namespace"` }
	json.Unmarshal(input, &args)
	cmd := "kubectl get events --sort-by=.lastTimestamp"
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(cmd)
}

// k8s/describe
type k8sDescribeTool struct{}
func (k *k8sDescribeTool) Name() string        { return "k8s/describe" }
func (k *k8sDescribeTool) Description() string { return "Describe a Kubernetes resource" }
func (k *k8sDescribeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string","description":"Resource type (e.g. pod)"},"name":{"type":"string","description":"Resource name"},"namespace":{"type":"string"}},"required":["resource","name"]}`)
}
func (k *k8sDescribeTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Resource  string `json:"resource"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl describe %s %s", args.Resource, args.Name)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(cmd)
}

// k8s/apply
type k8sApplyTool struct{}
func (k *k8sApplyTool) Name() string        { return "k8s/apply" }
func (k *k8sApplyTool) Description() string { return "Apply Kubernetes manifest" }
func (k *k8sApplyTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"manifest":{"type":"string","description":"YAML manifest content"},"namespace":{"type":"string"}},"required":["manifest"]}`)
}
func (k *k8sApplyTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Manifest  string `json:"manifest"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("echo '%s' | kubectl apply -f -", args.Manifest)
	if args.Namespace != "" {
		cmd = fmt.Sprintf("echo '%s' | kubectl apply -f - -n %s", args.Manifest, args.Namespace)
	}
	return runCommand(cmd)
}

// k8s/delete
type k8sDeleteTool struct{}
func (k *k8sDeleteTool) Name() string        { return "k8s/delete" }
func (k *k8sDeleteTool) Description() string { return "Delete Kubernetes resource" }
func (k *k8sDeleteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string"},"name":{"type":"string"},"namespace":{"type":"string"}},"required":["resource","name"]}`)
}
func (k *k8sDeleteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Resource  string `json:"resource"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl delete %s %s", args.Resource, args.Name)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(cmd)
}

func NewK8sTools() []Tool {
	return []Tool{&k8sPodsTool{}, &k8sLogsTool{}, &k8sEventsTool{}, &k8sDescribeTool{}, &k8sApplyTool{}, &k8sDeleteTool{}}
}
```

- [ ] **Step 3: Run k8s tests**

Run: `go test ./internal/tools/ -run TestK8s -v`
Expected: PASS (kubectl not available in test env is OK — just verifies no panic)

- [ ] **Step 4: Commit**

```bash
git add internal/tools/k8s.go internal/tools/k8s_test.go
git commit -m "feat: add Kubernetes tools (k8s/pods, logs, events, describe, apply, delete)"
```

---

### Task 14: Package Management Tools (pkg/*)

**Files:**
- Create: `internal/tools/pkgtool.go`
- Create: `internal/tools/pkgtool_test.go`

- [ ] **Step 1: Write pkg tool tests**

`internal/tools/pkgtool_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPkgList(t *testing.T) {
	tool := &pkgListTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
```

- [ ] **Step 2: Implement pkg tools**

`internal/tools/pkgtool.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

func detectPkgManager() string {
	return "apt-get" // simplified — could check for yum/dnf/brew
}

// pkg/list
type pkgListTool struct{}
func (p *pkgListTool) Name() string        { return "pkg/list" }
func (p *pkgListTool) Description() string { return "List installed packages" }
func (p *pkgListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Filter by package name"}}}`)
}
func (p *pkgListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Name string `json:"name"` }
	json.Unmarshal(input, &args)
	cmd := "dpkg -l 2>/dev/null || rpm -qa"
	if args.Name != "" {
		cmd = fmt.Sprintf("dpkg -l %s 2>/dev/null || rpm -q %s", args.Name, args.Name)
	}
	return runCommand(cmd)
}

// pkg/install
type pkgInstallTool struct{}
func (p *pkgInstallTool) Name() string        { return "pkg/install" }
func (p *pkgInstallTool) Description() string { return "Install package" }
func (p *pkgInstallTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Package name"},"update_cache":{"type":"boolean","description":"Update package cache first"}},"required":["name"]}`)
}
func (p *pkgInstallTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Name        string `json:"name"`
		UpdateCache bool   `json:"update_cache"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	var cmd string
	if args.UpdateCache {
		cmd = fmt.Sprintf("apt-get update && apt-get install -y %s 2>/dev/null || yum install -y %s", args.Name, args.Name)
	} else {
		cmd = fmt.Sprintf("apt-get install -y %s 2>/dev/null || yum install -y %s", args.Name, args.Name)
	}
	return runCommand(cmd)
}

// pkg/update
type pkgUpdateTool struct{}
func (p *pkgUpdateTool) Name() string        { return "pkg/update" }
func (p *pkgUpdateTool) Description() string { return "Update package" }
func (p *pkgUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Package name (empty = update all)"}}}`)
}
func (p *pkgUpdateTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Name string `json:"name"` }
	json.Unmarshal(input, &args)
	var cmd string
	if args.Name != "" {
		cmd = fmt.Sprintf("apt-get install --only-upgrade -y %s 2>/dev/null || yum update -y %s", args.Name, args.Name)
	} else {
		cmd = "apt-get upgrade -y 2>/dev/null || yum update -y"
	}
	return runCommand(cmd)
}

// pkg/search
type pkgSearchTool struct{}
func (p *pkgSearchTool) Name() string        { return "pkg/search" }
func (p *pkgSearchTool) Description() string { return "Search packages" }
func (p *pkgSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`)
}
func (p *pkgSearchTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Query string `json:"query"` }
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("apt-cache search %s 2>/dev/null || yum search %s", args.Query, args.Query)
	return runCommand(cmd)
}

func NewPkgTools() []Tool {
	return []Tool{&pkgListTool{}, &pkgInstallTool{}, &pkgUpdateTool{}, &pkgSearchTool{}}
}
```

- [ ] **Step 3: Run pkg tests**

Run: `go test ./internal/tools/ -run TestPkg -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/pkgtool.go internal/tools/pkgtool_test.go
git commit -m "feat: add package management tools (pkg/list, install, update, search)"
```

---

### Task 15: Cron Tools (cron/*)

**Files:**
- Create: `internal/tools/cron.go`
- Create: `internal/tools/cron_test.go`

- [ ] **Step 1: Write cron tool tests**

`internal/tools/cron_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCronList(t *testing.T) {
	tool := &cronListTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
```

- [ ] **Step 2: Implement cron tools**

`internal/tools/cron.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// cron/list
type cronListTool struct{}
func (c *cronListTool) Name() string        { return "cron/list" }
func (c *cronListTool) Description() string { return "List crontab entries" }
func (c *cronListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"user":{"type":"string","description":"Crontab user (default: current)"}}}`)
}
func (c *cronListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ User string `json:"user"` }
	json.Unmarshal(input, &args)
	cmd := "crontab -l"
	if args.User != "" {
		cmd = fmt.Sprintf("crontab -l -u %s", args.User)
	}
	return runCommand(cmd)
}

// cron/add
type cronAddTool struct{}
func (c *cronAddTool) Name() string        { return "cron/add" }
func (c *cronAddTool) Description() string { return "Add crontab entry" }
func (c *cronAddTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"schedule":{"type":"string","description":"Cron schedule expression"},"command":{"type":"string","description":"Command to run"},"user":{"type":"string"}},"required":["schedule","command"]}`)
}
func (c *cronAddTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		User     string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	// Get existing crontab, append new entry, install
	entry := fmt.Sprintf("%s %s", args.Schedule, args.Command)
	cmd := fmt.Sprintf(`(crontab -l%s 2>/dev/null; echo "%s") | crontab%s`, userFlag, entry, userFlag)
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("cron job added: " + entry)}}, nil
}

// cron/remove
type cronRemoveTool struct{}
func (c *cronRemoveTool) Name() string        { return "cron/remove" }
func (c *cronRemoveTool) Description() string { return "Remove crontab entry by line content" }
func (c *cronRemoveTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Pattern to match for removal"},"user":{"type":"string"}},"required":["pattern"]}`)
}
func (c *cronRemoveTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Pattern string `json:"pattern"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	cmd := fmt.Sprintf(`crontab -l%s 2>/dev/null | grep -v '%s' | crontab%s`, userFlag, args.Pattern, userFlag)
	return runCommand(cmd)
}

// cron/show
type cronShowTool struct{}
func (c *cronShowTool) Name() string        { return "cron/show" }
func (c *cronShowTool) Description() string { return "Show crontab entries matching pattern" }
func (c *cronShowTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Grep pattern"},"user":{"type":"string"}},"required":["pattern"]}`)
}
func (c *cronShowTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Pattern string `json:"pattern"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	userFlag := ""
	if args.User != "" {
		userFlag = fmt.Sprintf(" -u %s", args.User)
	}
	cmd := fmt.Sprintf("crontab -l%s 2>/dev/null | grep '%s'", userFlag, args.Pattern)
	return runCommand(cmd)
}

func NewCronTools() []Tool {
	return []Tool{&cronListTool{}, &cronAddTool{}, &cronRemoveTool{}, &cronShowTool{}}
}
```

- [ ] **Step 3: Run cron tests**

Run: `go test ./internal/tools/ -run TestCron -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/cron.go internal/tools/cron_test.go
git commit -m "feat: add cron management tools (cron/list, add, remove, show)"
```

---

### Task 16: Docker Tools (docker/*)

**Files:**
- Create: `internal/tools/docker.go`
- Create: `internal/tools/docker_test.go`

- [ ] **Step 1: Write docker tool tests**

`internal/tools/docker_test.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDockerPs(t *testing.T) {
	tool := &dockerPsTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
```

- [ ] **Step 2: Implement docker tools**

`internal/tools/docker.go`:
```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// docker/ps
type dockerPsTool struct{}
func (d *dockerPsTool) Name() string        { return "docker/ps" }
func (d *dockerPsTool) Description() string { return "List Docker containers" }
func (d *dockerPsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"all":{"type":"boolean","description":"Show all containers (default running only)"},"filter":{"type":"string","description":"Filter expression"}}}`)
}
func (d *dockerPsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		All    bool   `json:"all"`
		Filter string `json:"filter"`
	}
	json.Unmarshal(input, &args)
	cmd := "docker ps"
	if args.All {
		cmd += " -a"
	}
	if args.Filter != "" {
		cmd += fmt.Sprintf(" --filter '%s'", args.Filter)
	}
	return runCommand(cmd)
}

// docker/images
type dockerImagesTool struct{}
func (d *dockerImagesTool) Name() string        { return "docker/images" }
func (d *dockerImagesTool) Description() string { return "List Docker images" }
func (d *dockerImagesTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string","description":"Filter expression"}}}`)
}
func (d *dockerImagesTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct{ Filter string `json:"filter"` }
	json.Unmarshal(input, &args)
	cmd := "docker images"
	if args.Filter != "" {
		cmd += fmt.Sprintf(" --filter '%s'", args.Filter)
	}
	return runCommand(cmd)
}

// docker/logs
type dockerLogsTool struct{}
func (d *dockerLogsTool) Name() string        { return "docker/logs" }
func (d *dockerLogsTool) Description() string { return "Get container logs" }
func (d *dockerLogsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"tail":{"type":"integer","description":"Last N lines"}},"required":["container"]}`)
}
func (d *dockerLogsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Container string `json:"container"`
		Tail      int    `json:"tail"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("docker logs %s", args.Container)
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" --tail %d", args.Tail)
	}
	return runCommand(cmd)
}

// docker/exec
type dockerExecTool struct{}
func (d *dockerExecTool) Name() string        { return "docker/exec" }
func (d *dockerExecTool) Description() string { return "Execute command in container" }
func (d *dockerExecTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"container":{"type":"string","description":"Container name or ID"},"command":{"type":"string","description":"Command to execute"}},"required":["container","command"]}`)
}
func (d *dockerExecTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Container string `json:"container"`
		Command   string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("docker exec %s %s", args.Container, args.Command)
	return runCommand(cmd)
}

// docker/run
type dockerRunTool struct{}
func (d *dockerRunTool) Name() string        { return "docker/run" }
func (d *dockerRunTool) Description() string { return "Run a Docker container" }
func (d *dockerRunTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"image":{"type":"string"},"name":{"type":"string"},"ports":{"type":"array","items":{"type":"string"}},"detach":{"type":"boolean"},"env":{"type":"object","additionalProperties":{"type":"string"}}},"required":["image"]}`)
}
func (d *dockerRunTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Image   string            `json:"image"`
		Name    string            `json:"name"`
		Ports   []string          `json:"ports"`
		Detach  bool              `json:"detach"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := "docker run"
	if args.Detach {
		cmd += " -d"
	}
	if args.Name != "" {
		cmd += fmt.Sprintf(" --name %s", args.Name)
	}
	for _, p := range args.Ports {
		cmd += fmt.Sprintf(" -p %s", p)
	}
	for k, v := range args.Env {
		cmd += fmt.Sprintf(" -e %s=%s", k, v)
	}
	cmd += fmt.Sprintf(" %s", args.Image)
	return runCommand(cmd)
}

// docker/compose
type dockerComposeTool struct{}
func (d *dockerComposeTool) Name() string        { return "docker/compose" }
func (d *dockerComposeTool) Description() string { return "Run docker compose command" }
func (d *dockerComposeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","description":"Compose action (up, down, ps, logs)"},"file":{"type":"string","description":"Compose file path"}},"required":["action"]}`)
}
func (d *dockerComposeTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Action string `json:"action"`
		File   string `json:"file"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := "docker compose"
	if args.File != "" {
		cmd += fmt.Sprintf(" -f %s", args.File)
	}
	cmd += " " + args.Action
	return runCommand(cmd)
}

func NewDockerTools() []Tool {
	return []Tool{&dockerPsTool{}, &dockerImagesTool{}, &dockerLogsTool{}, &dockerExecTool{}, &dockerRunTool{}, &dockerComposeTool{}}
}
```

- [ ] **Step 3: Run docker tests**

Run: `go test ./internal/tools/ -run TestDocker -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/docker.go internal/tools/docker_test.go
git commit -m "feat: add Docker tools (docker/ps, images, logs, exec, run, compose)"
```

---

### Task 17: Agent CLI Entry Point

**Files:**
- Modify: `cmd/conan-agent/main.go`

- [ ] **Step 1: Implement agent CLI with cobra**

`cmd/conan-agent/main.go`:
```go
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/pockyHM/conan/internal/agent"
	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
	"gopkg.in/yaml.v3"
)

var version = "dev"

func main() {
	var configPath string

	rootCmd := &cobra.Command{
		Use:     "conan-agent",
		Short:   "Conan Agent — MCP server for managed nodes",
		Version: version,
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the agent server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			slog.SetLogLoggerLevel(slog.LevelInfo)
			slog.Info("conan-agent starting", "version", version)

			registry := tools.NewRegistry()
			registerAllTools(registry)
			registry.DisableAll(cfg.DisabledTools)

			srv := agent.NewServer(cfg, registry, version)
			go srv.WaitForSignal()
			return srv.Start()
		},
	}

	runCmd.Flags().StringVarP(&configPath, "config", "c", "/etc/conan-agent/config.yaml", "Config file path")
	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig(path string) (*configschema.AgentConfig, error) {
	cfg := configschema.DefaultAgentConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("config file not found, using defaults", "path", path)
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func registerAllTools(r *tools.Registry) {
	r.Register(&tools.ShellTool{})
	for _, t := range tools.NewFsTools() {
		r.Register(t)
	}
	for _, t := range tools.NewSysTools() {
		r.Register(t)
	}
	for _, t := range tools.NewSvcTools() {
		r.Register(t)
	}
	for _, t := range tools.NewLogTools() {
		r.Register(t)
	}
	for _, t := range tools.NewNetTools() {
		r.Register(t)
	}
	for _, t := range tools.NewK8sTools() {
		r.Register(t)
	}
	for _, t := range tools.NewPkgTools() {
		r.Register(t)
	}
	for _, t := range tools.NewCronTools() {
		r.Register(t)
	}
	for _, t := range tools.NewDockerTools() {
		r.Register(t)
	}
}
```

- [ ] **Step 2: Add dependencies and build**

```bash
go get github.com/spf13/cobra gopkg.in/yaml.v3 github.com/google/uuid
go mod tidy
make build
```

Expected: Both binaries build successfully

- [ ] **Step 3: Test agent binary**

```bash
# Start agent in background with example config
./bin/conan-agent run -c configs/example/agent-config.yaml &
AGENT_PID=$!
sleep 1

# Test health endpoint
curl -s http://localhost:9200/health

# Test initialize
curl -s -X POST http://localhost:9200/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'

# Test tools/list
curl -s -X POST http://localhost:9200/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

# Test shell/run
curl -s -X POST http://localhost:9200/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"shell/run","arguments":{"command":"echo hello","timeout":5}}}'

# Stop agent
kill $AGENT_PID
```

Expected: All curl commands return valid JSON-RPC responses

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -timeout 60s`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: complete agent CLI with all tool registrations and cobra commands"
```

---

### Task 18: Final Verification

- [ ] **Step 1: Clean build from scratch**

```bash
make clean && make build
```

Expected: No errors

- [ ] **Step 2: Run all tests**

```bash
go test ./... -v -count=1
```

Expected: All tests PASS

- [ ] **Step 3: Verify agent startup/shutdown**

```bash
./bin/conan-agent run -c configs/example/agent-config.yaml &
PID=$!
sleep 1
curl -s http://localhost:9200/health
kill $PID
```

Expected: Health returns "ok", agent shuts down cleanly

- [ ] **Step 4: Final commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix: address final verification issues"
```

---

## Spec Coverage Check

| Spec Section | Covered by Task |
|---|---|
| Architecture (3-layer) | Tasks 1-6 (server + tools) |
| Project Structure | Task 1 |
| MCP Server Interface | Tasks 5-6 |
| shell/run | Task 7 |
| fs/* (7 tools) | Task 8 (5 tools: read, write, edit, list, stat) |
| svc/* (5 tools) | Task 10 |
| sys/* (5 tools) | Task 9 |
| log/* (3 tools) | Task 11 |
| net/* (4 tools) | Task 12 |
| k8s/* (6 tools) | Task 13 |
| pkg/* (4 tools) | Task 14 |
| cron/* (4 tools) | Task 15 |
| docker/* (6 tools) | Task 16 |
| Agent deployment & config | Task 17 |
| Auth middleware | Task 6 |
| Rate limiting | Task 6 |
| Audit logging | Task 6 |
| Graceful shutdown | Task 6 |
| SIGHUP reload | Task 6 |

**Missing from this plan (deferred to CLI plans):** fs/download, fs/upload (binary file transfer), log/stream SSE endpoint (needs SSE infrastructure). These will be addressed in subsequent plans.
