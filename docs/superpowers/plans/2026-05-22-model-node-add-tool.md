# Model Node Add Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the TUI temporarily expose a `node_add` tool to the model after `/node`, then use the existing node add/deploy service to add a node and deploy `conan-agent`.

**Architecture:** Add a short-lived TUI authorization flag that controls whether `node_add` is included in model tool definitions. Keep node add dispatch in a focused `internal/tui/nodeadd_tool.go` file, reuse `internal/nodeadd.Service`, and refresh TUI node/client/tool state after successful deployment. Redact secrets before rendering, logging, auditing, or writing tool calls to conversation history.

**Tech Stack:** Go, Bubble Tea, Cobra, existing MCP client/server types, `internal/nodeadd`, `internal/deploy`, `internal/credentials`, `internal/security`.

---

## File Structure

- Modify `internal/tui/command.go`: add `/node` slash command parsing.
- Modify `internal/tui/command_test.go`: cover `/node` and `/node off`.
- Modify `internal/tui/metatools.go`: define `node_add` as a meta tool in a separate `nodeManagementToolDefs` slice.
- Modify `internal/tui/metatools_test.go`: verify `node_add` schema and default exclusion.
- Modify `internal/tui/model.go`: add short-lived node tool exposure state, clear it at stream terminal points, route `node_add`, and use sanitized tool-call arguments.
- Create `internal/tui/nodeadd_tool.go`: parse `node_add` arguments, build `nodeadd.Request`, call the runner, sanitize arguments, and refresh TUI node state.
- Create `internal/tui/nodeadd_tool_test.go`: test request mapping, missing host, refresh behavior, and redaction.
- Modify `internal/security/reviewer_test.go`: assert `node_add` is not read-only and requires confirmation when no risk model is configured.
- Modify `cmd/conan/main.go`: keep passing `ConfigHome` into `tui.ModelConfig`; no CLI wiring change is needed unless the implementation chooses to inject a test runner from command construction.
- Modify `internal/tui/model_test.go`: test exposure lifetime and stream cleanup.

## Task 1: Slash Command And Temporary Exposure State

**Files:**
- Modify: `internal/tui/command.go`
- Modify: `internal/tui/command_test.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/command_test.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing slash command tests**

Add these rows to `TestParseSlashCommand` in `internal/tui/command_test.go`:

```go
{input: "/node", kind: CommandNode},
{input: "/node off", kind: CommandNode, arg: "off"},
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestParseSlashCommand -count=1`

Expected: FAIL with `undefined: CommandNode`.

- [ ] **Step 2: Add the command kind and parser case**

In `internal/tui/command.go`, add the command kind:

```go
CommandNode      CommandKind = "node"
```

Add this parser case:

```go
case "node":
	return SlashCommand{Kind: CommandNode, Arg: arg}, true
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestParseSlashCommand -count=1`

Expected: PASS.

- [ ] **Step 3: Write failing model tests for `/node` state**

Add tests to `internal/tui/model_test.go`:

```go
func TestNodeCommandEnablesNodeToolsForNextResponse(t *testing.T) {
	model := NewModel(ModelConfig{})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode})

	if !next.nodeToolsEnabled {
		t.Fatal("/node should enable node tools")
	}
	if next.status != "Node management enabled for next model response" {
		t.Fatalf("status = %q", next.status)
	}
}

func TestNodeCommandOffDisablesNodeTools(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true

	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode, Arg: "off"})

	if next.nodeToolsEnabled {
		t.Fatal("/node off should disable node tools")
	}
	if next.status != "Node management disabled" {
		t.Fatalf("status = %q", next.status)
	}
}
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestNodeCommand(Enables|Off)' -count=1`

Expected: FAIL with missing `nodeToolsEnabled` or missing command handling.

- [ ] **Step 4: Implement the temporary state**

In `internal/tui/model.go`, add to `Model`:

```go
nodeToolsEnabled bool
```

Add this case to `applyCommand`:

```go
case CommandNode:
	switch strings.TrimSpace(cmd.Arg) {
	case "":
		m.nodeToolsEnabled = true
		m.status = "Node management enabled for next model response"
	case "off":
		m.nodeToolsEnabled = false
		m.status = "Node management disabled"
	default:
		m.status = "Usage: /node [off]"
	}
```

Update help text to include `/node [off]`:

```go
content: "Conan: /help /clear /exit /cluster [name] /model [name] /nodes /node [off] /memory /resume /thinking <message> /agent <role> <task> /subagents [on|off|limit]",
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'Test(NodeCommand|ParseSlashCommand)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/command.go internal/tui/command_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: add node management slash command"
```

## Task 2: Define `node_add` As A Conditional Meta Tool

**Files:**
- Modify: `internal/tui/metatools.go`
- Modify: `internal/tui/metatools_test.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/metatools_test.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing tests for default exclusion and conditional inclusion**

Add tests to `internal/tui/metatools_test.go`:

```go
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
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestNodeAddToolOnlyAvailableWhenNodeToolsEnabled -count=1`

Expected: FAIL with `undefined: metaToolNodeAdd`.

- [ ] **Step 2: Add the tool definition**

In `internal/tui/metatools.go`, add:

```go
const metaToolNodeAdd = "node_add"
```

Add this slice after `metaToolDefs`:

```go
var nodeManagementToolDefs = []llm.ToolDef{
	{
		Name:        metaToolNodeAdd,
		Description: "Add or update a node, write local cluster configuration, deploys or updates conan-agent over SSH, and verifies the agent health endpoint. This is a high-impact node management operation and requires user confirmation.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"cluster": {"type": "string", "description": "Cluster name. Omit to use the current TUI cluster."},
				"host": {"type": "string", "description": "Hostname or IP address to add."},
				"name": {"type": "string", "description": "Node name override. Defaults to host."},
				"user": {"type": "string", "description": "SSH username. Omit to prompt or use saved credentials."},
				"password": {"type": "string", "description": "SSH password. Omit to prompt or use saved credentials."},
				"ssh_port": {"type": "integer", "description": "SSH port. Defaults to cluster node_defaults.ssh_port, then 22."},
				"agent_port": {"type": "integer", "description": "conan-agent listen port. Defaults to 9280."},
				"agent_bin": {"type": "string", "description": "Local conan-agent binary override for this deployment."},
				"update": {"type": "boolean", "description": "Update an existing node instead of failing on duplicate name."},
				"rotate_token": {"type": "boolean", "description": "Generate a new per-node agent token while updating."}
			},
			"required": ["host"]
		}`),
	},
}
```

In `availableToolDefs()`, append node management tools only when enabled:

```go
if m.nodeToolsEnabled {
	allTools = append(allTools, nodeManagementToolDefs...)
}
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestNodeAddToolOnlyAvailableWhenNodeToolsEnabled -count=1`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/metatools.go internal/tui/metatools_test.go internal/tui/model.go
git commit -m "feat: expose node add tool after node command"
```

## Task 3: Redact `node_add` Arguments Everywhere The TUI Stores Tool Calls

**Files:**
- Create: `internal/tui/nodeadd_tool.go`
- Create: `internal/tui/nodeadd_tool_test.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/nodeadd_tool_test.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing redaction tests**

Create `internal/tui/nodeadd_tool_test.go` with:

```go
package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeNodeAddArgumentsRedactsPassword(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.12","user":"deploy","password":"secret","agent_port":9280}`)

	got := string(sanitizeToolArguments(metaToolNodeAdd, raw))

	if strings.Contains(got, "secret") {
		t.Fatalf("sanitized arguments leaked password: %s", got)
	}
	if !strings.Contains(got, `"password":"[redacted]"`) {
		t.Fatalf("sanitized arguments missing redaction marker: %s", got)
	}
	if !strings.Contains(got, `"host":"10.0.0.12"`) {
		t.Fatalf("sanitized arguments should preserve non-secret fields: %s", got)
	}
}

func TestSanitizeToolArgumentsLeavesOtherToolsUnchanged(t *testing.T) {
	raw := json.RawMessage(`{"command":"echo secret"}`)

	got := string(sanitizeToolArguments(metaToolExec, raw))

	if got != string(raw) {
		t.Fatalf("non-node tool args changed: %s", got)
	}
}
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestSanitize -count=1`

Expected: FAIL with `undefined: sanitizeToolArguments`.

- [ ] **Step 2: Implement sanitizer**

Create `internal/tui/nodeadd_tool.go` with:

```go
package tui

import (
	"encoding/json"
)

func sanitizeToolArguments(toolName string, raw json.RawMessage) json.RawMessage {
	if toolName != metaToolNodeAdd {
		return raw
	}
	if len(raw) == 0 {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return json.RawMessage(`{"error":"invalid node_add arguments"}`)
	}
	if _, ok := obj["password"]; ok {
		obj["password"] = "[redacted]"
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return json.RawMessage(`{"error":"invalid node_add arguments"}`)
	}
	return b
}
```

In `internal/tui/model.go`, when handling `llm.ToolCallEvent`, compute sanitized arguments once:

```go
sanitizedArgs := sanitizeToolArguments(e.Name, e.Arguments)
```

Use `string(sanitizedArgs)` for `chatMsg.toolInput` and `m.conv.AddToolCall(...)`. Keep `e.Arguments` in the `llm.ToolCall` used for actual dispatch:

```go
m.messages = append(m.messages, chatMsg{
	role:       "tool",
	toolCallID: e.ID,
	toolName:   e.Name,
	toolInput:  string(sanitizedArgs),
	hidden:     hidden,
})
if m.conv != nil {
	m.conv.AddToolCall(e.ID, e.Name, string(sanitizedArgs))
}
call := llm.ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments}
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestSanitize -count=1`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go internal/tui/model.go
git commit -m "feat: redact node add tool secrets"
```

## Task 4: Dispatch `node_add` Through Existing Node Add Service

**Files:**
- Modify: `internal/tui/nodeadd_tool.go`
- Modify: `internal/tui/nodeadd_tool_test.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/nodeadd_tool_test.go`

- [ ] **Step 1: Write failing request mapping tests**

Append to `internal/tui/nodeadd_tool_test.go`:

```go
func TestParseNodeAddArgsRequiresHost(t *testing.T) {
	_, err := parseNodeAddArgs(json.RawMessage(`{"cluster":"prod"}`))
	if err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("err = %v, want host is required", err)
	}
}

func TestBuildNodeAddRequestMapsArguments(t *testing.T) {
	args, err := parseNodeAddArgs(json.RawMessage(`{
		"cluster":"prod",
		"host":"10.0.0.12",
		"name":"web-1",
		"user":"deploy",
		"password":"secret",
		"ssh_port":2222,
		"agent_port":9281,
		"agent_bin":"/tmp/conan-agent",
		"update":true,
		"rotate_token":true
	}`))
	if err != nil {
		t.Fatal(err)
	}

	req := buildNodeAddRequest("/tmp/home", "current", args, configschema.AgentDeployConfig{}, false)

	if req.Home != "/tmp/home" || req.ClusterName != "prod" || req.Input != "10.0.0.12" {
		t.Fatalf("basic request fields = %#v", req)
	}
	if req.Name != "web-1" || req.Username != "deploy" || req.Password != "secret" {
		t.Fatalf("identity fields = %#v", req)
	}
	if req.SSHPort != 2222 || req.AgentPort != 9281 {
		t.Fatalf("ports = ssh %d agent %d", req.SSHPort, req.AgentPort)
	}
	if !req.Update || !req.RotateToken {
		t.Fatalf("update flags = update %v rotate %v", req.Update, req.RotateToken)
	}
	if req.AgentBinOverride != "/tmp/conan-agent" {
		t.Fatalf("agent bin = %q", req.AgentBinOverride)
	}
}
```

Add this import to `internal/tui/nodeadd_tool_test.go`:

```go
"github.com/pockyHM/conan/pkg/configschema"
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'Test(ParseNodeAddArgs|BuildNodeAddRequest)' -count=1`

Expected: FAIL with undefined functions.

- [ ] **Step 2: Implement argument parsing and request building**

Add to `internal/tui/nodeadd_tool.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/pkg/configschema"
)

type nodeAddArgs struct {
	Cluster     string `json:"cluster"`
	Host        string `json:"host"`
	Name        string `json:"name"`
	User        string `json:"user"`
	Password    string `json:"password"`
	SSHPort     int    `json:"ssh_port"`
	AgentPort   int    `json:"agent_port"`
	AgentBin    string `json:"agent_bin"`
	Update      bool   `json:"update"`
	RotateToken bool   `json:"rotate_token"`
}

func parseNodeAddArgs(raw json.RawMessage) (nodeAddArgs, error) {
	var args nodeAddArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, err
	}
	args.Cluster = strings.TrimSpace(args.Cluster)
	args.Host = strings.TrimSpace(args.Host)
	args.Name = strings.TrimSpace(args.Name)
	args.User = strings.TrimSpace(args.User)
	args.AgentBin = strings.TrimSpace(args.AgentBin)
	if args.Host == "" {
		return args, fmt.Errorf("host is required")
	}
	return args, nil
}

func buildNodeAddRequest(home, currentCluster string, args nodeAddArgs, deployCfg configschema.AgentDeployConfig, tls bool) nodeadd.Request {
	cluster := args.Cluster
	if cluster == "" {
		cluster = currentCluster
	}
	return nodeadd.Request{
		Home:             home,
		ClusterName:      cluster,
		Input:            args.Host,
		Name:             args.Name,
		Username:         args.User,
		Password:         args.Password,
		SSHPort:          args.SSHPort,
		AgentPort:        args.AgentPort,
		Update:           args.Update,
		RotateToken:      args.RotateToken,
		AgentBinOverride: args.AgentBin,
		DeployConfig:     deployCfg,
		KnownHostsPath:   filepath.Join(home, "known_hosts"),
		TLS:              tls,
	}
}

type nodeAddRunner interface {
	Add(context.Context, nodeadd.Request) (nodeadd.Result, error)
}

type nodeAddRunnerFunc func(context.Context, nodeadd.Request) (nodeadd.Result, error)

func (f nodeAddRunnerFunc) Add(ctx context.Context, req nodeadd.Request) (nodeadd.Result, error) {
	return f(ctx, req)
}
```

Run: `gofmt -w internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'Test(ParseNodeAddArgs|BuildNodeAddRequest|Sanitize)' -count=1`

Expected: PASS.

- [ ] **Step 3: Wire dispatch and production runner**

Add to `ModelConfig` in `internal/tui/model.go`:

```go
NodeAddRunner nodeAddRunner
```

Add to `Model`:

```go
nodeAddRunner nodeAddRunner
```

In `NewModel`, set:

```go
nodeAddRunner: cfg.NodeAddRunner,
```

Add to `dispatchTool`:

```go
case metaToolNodeAdd:
	return m.dispatchNodeAdd(streamID, call)
```

Add to `internal/tui/nodeadd_tool.go`:

```go
func (m Model) dispatchNodeAdd(streamID uint64, call llm.ToolCall) tea.Cmd {
	home := m.configHome
	if home == "" {
		home = cfgloader.DefaultHome()
	}
	currentCluster := m.cluster
	runner := m.nodeAddRunner
	return func() tea.Msg {
		args, err := parseNodeAddArgs(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "invalid node_add arguments: " + err.Error(), Success: false}}}
		}
		loader := cfgloader.NewLoader(home)
		global, err := loader.LoadGlobal()
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "load global config: " + err.Error(), Success: false}}}
		}
		clusterName := args.Cluster
		if clusterName == "" {
			clusterName = currentCluster
		}
		if clusterName == "" {
			clusterName = global.DefaultCluster
		}
		if clusterName == "" {
			clusterName = "default"
		}
		cluster, err := loader.LoadCluster(clusterName)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "load cluster: " + err.Error(), Success: false}}}
		}
		req := buildNodeAddRequest(home, clusterName, args, global.AgentDeploy, cluster.Cluster.Agent.TLS)
		if req.SSHPort == 0 {
			req.SSHPort = cluster.Cluster.NodeDefaults.SSHPort
		}
		if runner == nil {
			runner = nodeadd.Service{
				Credentials: credentials.NewStore(loader.Home()),
				Prompter:    nonInteractiveNodeAddPrompter{},
				Resolver:    nodeadd.NetResolver{},
				Writer:      nodeadd.ConfigNodeWriter{Home: loader.Home()},
				Deployer:    deploy.NewNativeDeployer(),
				Health:      nodeadd.MCPHealthChecker{},
			}
		}
		result, err := runner.Add(parentContext(m.streamCtx), req)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "node_add failed: " + err.Error(), Success: false}}}
		}
		output := fmt.Sprintf("node added and deployed: %s\ncluster: %s\nhost: %s\nagent_port: %d\nhealth: ok", result.Node.Name, clusterName, result.Node.Host, req.AgentPort)
		return nodeAddResultMsg{streamID: streamID, Call: call, Result: result, Cluster: clusterName, Output: output}
	}
}

func parentContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

type nonInteractiveNodeAddPrompter struct{}

func (nonInteractiveNodeAddPrompter) PromptUsername(string) (string, error) {
	return "", fmt.Errorf("SSH username is required; provide user after /node")
}

func (nonInteractiveNodeAddPrompter) PromptPassword() (string, error) {
	return "", fmt.Errorf("SSH password is required; provide password after /node")
}

func (nonInteractiveNodeAddPrompter) PromptIP(hostname string) (string, error) {
	return "", fmt.Errorf("hostname %s could not be resolved; provide an IP address as host", hostname)
}
```

Add needed imports to `internal/tui/nodeadd_tool.go`:

```go
tea "github.com/charmbracelet/bubbletea"
cfgloader "github.com/pockyHM/conan/internal/config"
"github.com/pockyHM/conan/internal/llm"
```

This step returns clear errors for missing interactive values. Task 6 replaces the non-interactive prompter with a Bubble Tea prompt flow.

Run: `gofmt -w internal/tui/model.go internal/tui/nodeadd_tool.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'Test(ParseNodeAddArgs|BuildNodeAddRequest|Sanitize)' -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go
git commit -m "feat: dispatch node add tool"
```

## Task 5: Handle `nodeAddResultMsg` And Refresh TUI State

**Files:**
- Modify: `internal/tui/nodeadd_tool.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/nodeadd_tool_test.go`
- Test: `internal/tui/nodeadd_tool_test.go`

- [ ] **Step 1: Write failing refresh test**

Append to `internal/tui/nodeadd_tool_test.go`:

```go
func TestApplyNodeAddResultSelectsNewNode(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Nodes: []NodeInfo{{Name: "old", Host: "10.0.0.1"}},
	})
	model.selectedNodes = map[string]bool{"old": true}

	result := nodeadd.Result{
		Node: configschema.NodeConfig{
			Name: "web-1",
			Host: "10.0.0.12",
			Agent: &configschema.NodeAgentOverride{
				Port: 9280,
			},
		},
		Deployed: true,
	}

	next := model.applyNodeAddResult("prod", result)

	if !next.selectedNodes["old"] || !next.selectedNodes["web-1"] {
		t.Fatalf("selected nodes = %#v", next.selectedNodes)
	}
	found := false
	for _, node := range next.nodes {
		if node.Name == "web-1" && node.Host == "10.0.0.12" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nodes did not include web-1: %#v", next.nodes)
	}
}
```

Add this import:

```go
"github.com/pockyHM/conan/internal/nodeadd"
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestApplyNodeAddResultSelectsNewNode -count=1`

Expected: FAIL with `undefined: applyNodeAddResult`.

- [ ] **Step 2: Add result message and state updater**

In `internal/tui/nodeadd_tool.go`, add:

```go
type nodeAddResultMsg struct {
	streamID uint64
	Call     llm.ToolCall
	Result   nodeadd.Result
	Cluster  string
	Output   string
}

func (m Model) applyNodeAddResult(cluster string, result nodeadd.Result) Model {
	if cluster != "" {
		m.cluster = cluster
	}
	upserted := false
	info := NodeInfo{
		Name: result.Node.Name,
		Host: result.Node.Host,
	}
	if result.Node.Agent != nil {
		info.Host = result.Node.Host
	}
	for i := range m.nodes {
		if m.nodes[i].Name == info.Name {
			m.nodes[i].Host = info.Host
			upserted = true
			break
		}
	}
	if !upserted {
		m.nodes = append(m.nodes, info)
	}
	if m.selectedNodes == nil {
		m.selectedNodes = make(map[string]bool)
	}
	m.selectedNodes[info.Name] = true
	return m
}
```

Run: `gofmt -w internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestApplyNodeAddResultSelectsNewNode -count=1`

Expected: PASS.

- [ ] **Step 3: Handle result message in Update**

In `internal/tui/model.go`, add a `case nodeAddResultMsg` before `case multiToolResultMsg` if that case exists, otherwise before `case streamReadyMsg`:

```go
case nodeAddResultMsg:
	if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
		return m, nil
	}
	m = m.applyNodeAddResult(msg.Cluster, msg.Result)
	m.fillToolPlaceholder(msg.Call, msg.Output, []nodeToolResult{{Node: "local", Output: msg.Output, Success: true}})
	if m.conv != nil {
		m.conv.AddToolResult(msg.Call.ID, msg.Output)
	}
	m.markStreamToolDone(msg.streamID)
	m.status = "Node added and deployed"
	m.updateViewportContent()
	cmds := []tea.Cmd{m.resumeAfterStreamTools(msg.streamID)}
	if len(m.clients) > 0 {
		cmds = append(cmds, fetchNodeTools(m.clients))
	}
	return m, tea.Batch(cmds...)
```

If `resumeAfterStreamTools` returns `(tea.Model, tea.Cmd)` in the local code, use this exact form instead:

```go
next, cmd := m.resumeAfterStreamTools(msg.streamID)
cmds := []tea.Cmd{cmd}
if len(m.clients) > 0 {
	cmds = append(cmds, fetchNodeTools(m.clients))
}
return next, tea.Batch(cmds...)
```

Run: `gofmt -w internal/tui/model.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestApplyNodeAddResultSelectsNewNode|TestToolCall' -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go
git commit -m "feat: refresh tui after node add"
```

## Task 6: Add TUI Prompt Mode For Missing Node Add Inputs

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/nodeadd_tool.go`
- Modify: `internal/tui/nodeadd_tool_test.go`
- Test: `internal/tui/model_test.go`
- Test: `internal/tui/nodeadd_tool_test.go`

- [ ] **Step 1: Write failing prompt-state tests**

Add to `internal/tui/nodeadd_tool_test.go`:

```go
func TestNodeAddNeedsPromptWhenPasswordMissing(t *testing.T) {
	model := NewModel(ModelConfig{})
	call := llm.ToolCall{ID: "n1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12","user":"deploy"}`)}

	msg := execCmd(t, model.prepareNodeAddOrPrompt(1, call))

	if _, ok := msg.(nodeAddPromptMsg); !ok {
		t.Fatalf("message = %T, want nodeAddPromptMsg", msg)
	}
}

func TestNodeAddPromptStoresMaskedPassword(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.mode = modeNodePrompt
	model.nodePrompt = &nodePromptState{
		streamID: 1,
		call: llm.ToolCall{ID: "n1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12","user":"deploy"}`)},
		field: "password",
		label: "SSH password",
		secret: true,
	}
	model.input = "secret"

	nextModel, _ := model.submit()
	next := nextModel.(Model)

	if strings.Contains(string(next.nodePrompt.call.Arguments), "secret") && next.mode == modeNodePrompt {
		t.Fatal("password should not remain visible in prompt state after submit")
	}
}
```

Add `llm` import:

```go
"github.com/pockyHM/conan/internal/llm"
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestNodeAdd.*Prompt' -count=1`

Expected: FAIL with undefined prompt types and methods.

- [ ] **Step 2: Add prompt mode and state**

In `internal/tui/model.go`, add mode:

```go
modeNodePrompt
```

Add to `Model`:

```go
nodePrompt *nodePromptState
```

In `internal/tui/nodeadd_tool.go`, add:

```go
type nodePromptState struct {
	streamID uint64
	call     llm.ToolCall
	field    string
	label    string
	secret   bool
}

type nodeAddPromptMsg struct {
	state nodePromptState
}
```

Add this preparation command:

```go
func (m Model) prepareNodeAddOrPrompt(streamID uint64, call llm.ToolCall) tea.Cmd {
	return func() tea.Msg {
		args, err := parseNodeAddArgs(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "invalid node_add arguments: " + err.Error(), Success: false}}}
		}
		if args.User == "" {
			return nodeAddPromptMsg{state: nodePromptState{streamID: streamID, call: call, field: "user", label: "SSH username"}}
		}
		if args.Password == "" {
			return nodeAddPromptMsg{state: nodePromptState{streamID: streamID, call: call, field: "password", label: "SSH password", secret: true}}
		}
		return nodeAddReadyMsg{streamID: streamID, call: call}
	}
}

type nodeAddReadyMsg struct {
	streamID uint64
	call     llm.ToolCall
}
```

Change `dispatchTool` for `metaToolNodeAdd` to:

```go
return m.prepareNodeAddOrPrompt(streamID, call)
```

Handle messages in `Update`:

```go
case nodeAddPromptMsg:
	m.mode = modeNodePrompt
	m.nodePrompt = &msg.state
	m.input = ""
	m.status = msg.state.label + " required"
	return m, nil

case nodeAddReadyMsg:
	return m, m.dispatchNodeAdd(msg.streamID, msg.call)
```

Run: `gofmt -w internal/tui/model.go internal/tui/nodeadd_tool.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestNodeAdd.*Prompt' -count=1`

Expected: test still fails until submit handling is added.

- [ ] **Step 3: Handle prompt submission**

In `submit()`, before normal slash command parsing, add:

```go
if m.mode == modeNodePrompt {
	return m.submitNodePrompt(input)
}
```

Add to `internal/tui/nodeadd_tool.go`:

```go
func (m Model) submitNodePrompt(value string) (tea.Model, tea.Cmd) {
	if m.nodePrompt == nil {
		m.mode = modeChat
		m.status = "Ready"
		return m, nil
	}
	state := *m.nodePrompt
	m.nodePrompt = nil
	m.mode = modeChat
	updated, err := setNodeAddArg(state.call.Arguments, state.field, value)
	if err != nil {
		m.status = "node_add prompt error: " + err.Error()
		return m, nil
	}
	state.call.Arguments = updated
	m.status = "Node management input received"
	return m, m.prepareNodeAddOrPrompt(state.streamID, state.call)
}

func setNodeAddArg(raw json.RawMessage, field, value string) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	obj[field] = value
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return b, nil
}
```

In `View()`, render password prompt by replacing the footer when `m.mode == modeNodePrompt`:

```go
if m.mode == modeNodePrompt {
	footer = m.renderNodePromptFooter()
}
```

Add:

```go
func (m Model) renderNodePromptFooter() string {
	label := "Node input"
	secret := false
	if m.nodePrompt != nil {
		label = m.nodePrompt.label
		secret = m.nodePrompt.secret
	}
	value := m.input
	if secret {
		value = strings.Repeat("*", len([]rune(value)))
	}
	return inputPromptStyle.Render(label+": ") + value
}
```

Run: `gofmt -w internal/tui/model.go internal/tui/nodeadd_tool.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestNodeAdd.*Prompt' -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go internal/tui/render.go internal/tui/nodeadd_tool.go internal/tui/nodeadd_tool_test.go
git commit -m "feat: prompt for node add credentials in tui"
```

## Task 7: Clear Node Tool Exposure At Terminal Points

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing cleanup tests**

Add to `internal/tui/model_test.go`:

```go
func TestNodeToolExposureClearsWhenStreamFinishes(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true
	model.activeStreamID = 1
	model.streamID = 1
	model.streaming = true

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: "stop"}})
	got := next.(Model)

	if got.nodeToolsEnabled {
		t.Fatal("node tool exposure should clear when stream finishes")
	}
}

func TestNodeToolExposureClearsWhenConfirmationCancelled(t *testing.T) {
	call := llm.ToolCall{ID: "n1", Name: metaToolNodeAdd, Arguments: []byte(`{"host":"10.0.0.12","user":"deploy","password":"secret"}`)}
	model := NewModel(ModelConfig{Conv: conversation.New("prod", nil, "test")})
	model.nodeToolsEnabled = true
	model.mode = modeConfirm
	model.pendingToolCall = &call
	model.pendingRisk = &security.RiskAssessment{Level: security.RiskConfirm, Reason: "confirm"}
	model.activeStreamID = 1
	model.streamID = 1
	model.streamToolExpected = 1
	model.streamEnded = true
	model.confirmChoice = 1

	next, _ := model.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.nodeToolsEnabled {
		t.Fatal("node tool exposure should clear when confirmation is denied")
	}
}
```

Add imports if missing:

```go
tea "github.com/charmbracelet/bubbletea"
"github.com/pockyHM/conan/internal/conversation"
"github.com/pockyHM/conan/internal/llm"
"github.com/pockyHM/conan/internal/security"
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestNodeToolExposureClears -count=1`

Expected: FAIL because the flag remains true.

- [ ] **Step 2: Implement cleanup helper**

In `internal/tui/model.go`, add:

```go
func (m *Model) clearNodeToolExposure() {
	m.nodeToolsEnabled = false
}
```

Call `m.clearNodeToolExposure()` in:

- `finishStream`
- `finishEmptyResponse`
- `handleConfirmKey` denial path before `resumeAfterStreamTools`
- `handleConfirmKey` escape path before `resumeAfterStreamTools`
- `case llm.ErrorEvent` before returning
- stale `streamReadyMsg` error path after `finishStream(false)`

Run: `gofmt -w internal/tui/model.go internal/tui/model_test.go`

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run TestNodeToolExposureClears -count=1`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: clear node tool exposure after use"
```

## Task 8: Security Review Coverage

**Files:**
- Modify: `internal/security/reviewer_test.go`
- Test: `internal/security/reviewer_test.go`

- [ ] **Step 1: Write failing or confirming security test**

Add to `internal/security/reviewer_test.go`:

```go
func TestNodeAddRequiresConfirmationWithoutRiskProvider(t *testing.T) {
	reviewer := NewReviewer(ReviewerConfig{})

	assessment, err := reviewer.Review(context.Background(), "node_add", `{"host":"10.0.0.12","user":"deploy"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Level != RiskConfirm {
		t.Fatalf("assessment = %#v, want confirm", assessment)
	}
	if !strings.Contains(assessment.Reason, "no risk assessment model configured") {
		t.Fatalf("reason = %q", assessment.Reason)
	}
}
```

Add imports if missing:

```go
"context"
"strings"
```

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/security -run TestNodeAddRequiresConfirmationWithoutRiskProvider -count=1`

Expected: PASS if `node_add` is already not read-only; FAIL if it was accidentally added to `readOnlyTools`.

- [ ] **Step 2: Keep `node_add` out of read-only tools**

If the test fails because `node_add` was added to `readOnlyTools`, remove it from `readOnlyTools` in `internal/security/reviewer.go`.

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/security -run TestNodeAddRequiresConfirmationWithoutRiskProvider -count=1`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/security/reviewer.go internal/security/reviewer_test.go
git commit -m "test: require confirmation for node add tool"
```

## Task 9: Final Verification

**Files:**
- No code files changed in this task.

- [ ] **Step 1: Run focused TUI tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -count=1`

Expected: PASS.

- [ ] **Step 2: Run node add and security tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/nodeadd ./internal/security -count=1`

Expected: PASS.

- [ ] **Step 3: Run CLI tests that construct the TUI**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./cmd/conan -count=1`

Expected: PASS.

- [ ] **Step 4: Run integration package set**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./cmd/conan ./internal/tui ./internal/nodeadd ./internal/security -count=1`

Expected: PASS.

- [ ] **Step 5: Commit verification fixes when a previous step changed code**

When verification exposes a compile error or failing assertion, fix the smallest responsible code path, rerun the failing command, then commit the changed files:

```bash
git status --short
git add <changed-files-from-this-task>
git commit -m "fix: stabilize node add tool tests"
```

Expected when all commands pass without extra changes: skip this commit step.

## Self-Review

Spec coverage:

- Explicit `/node` activation before exposure: Tasks 1 and 2.
- `node_add` hidden by default: Task 2.
- Full node add dispatch through existing service: Task 4.
- Secret redaction: Task 3.
- TUI credential prompt path: Task 6.
- Security confirmation: Task 8.
- Cleanup after terminal states: Task 7.
- State refresh after success: Task 5.
- Focused verification: Task 9.

Placeholder scan:

- The plan contains no placeholder tokens or incomplete sections.
- Each code-changing step includes concrete code or exact behavior and verification commands.

Type consistency:

- The plan uses `metaToolNodeAdd`, `nodeToolsEnabled`, `nodeAddRunner`, `nodeAddResultMsg`, and `nodePromptState` consistently across tasks.
- `nodeadd.Request` fields match the current `internal/nodeadd.Service.Add` API.
