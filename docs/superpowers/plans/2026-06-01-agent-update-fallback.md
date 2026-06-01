# Agent Update Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `conan node update --mode auto|ssh|agent`, with `auto` trying SSH first and falling back to an authenticated `agent_update` MCP tool when SSH cannot update the node.

**Architecture:** Create a focused `internal/agentupdate` package shared by the CLI updater and remote agent tool. The package owns the JSON request shape, local artifact packaging, binary architecture selection, and fixed-command install flow. `internal/nodeupdate` remains the orchestration layer: it chooses nodes, runs SSH update when requested, calls an `AgentUpdater` fallback when needed, and keeps Cobra thin.

**Tech Stack:** Go, Cobra, existing MCP JSON-RPC client, existing deploy artifact helpers, systemd, table-driven Go tests, TDD.

---

## File Structure

- Create `internal/agentupdate/request.go` — shared request schema and CLI-side artifact builder.
- Create `internal/agentupdate/request_test.go` — artifact builder tests for override and arch map modes.
- Create `internal/agentupdate/apply.go` — agent-side updater that selects a binary, writes temp files, and runs fixed install/systemd commands.
- Create `internal/agentupdate/apply_test.go` — invalid payload, arch selection, file permissions, command ordering, and secret redaction tests.
- Create `internal/tools/agent_update.go` — MCP tool wrapper around `agentupdate.Applier`.
- Create `internal/tools/agent_update_test.go` — tool name/schema and invalid request behavior.
- Modify `internal/tools/metadata.go` — classify `agent_update` as destructive node-scoped tooling.
- Modify `cmd/conan-agent/main.go` — register `agent_update`.
- Modify `cmd/conan-agent/main_test.go` — expect `agent_update` in registered tools and disabled-tools behavior.
- Create `internal/nodeupdate/agent_updater.go` — MCP implementation of the agent updater dependency with post-update health retry.
- Create `internal/nodeupdate/agent_updater_test.go` — JSON-RPC call and health retry behavior with an HTTP test server.
- Modify `internal/nodeupdate/service.go` — add update mode, agent updater dependency, artifact building, and fallback logic.
- Modify `internal/nodeupdate/service_test.go` — service mode and fallback tests.
- Modify `cmd/conan/main.go` — add `node update --mode` flag and pass it into `nodeupdate.Request`.
- Modify `cmd/conan/main_test.go` — help and invalid mode tests.
- Modify `README.md` and `README.zh-CN.md` — document `auto`, `ssh`, and `agent` modes.

---

### Task 1: Shared Agent Update Request And Artifact Builder

**Files:**
- Create: `internal/agentupdate/request.go`
- Create: `internal/agentupdate/request_test.go`

- [ ] **Step 1: Write failing tests for request building**

Create `internal/agentupdate/request_test.go`:

```go
package agentupdate

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestBuildRequestWithOverrideSendsSingleBinary(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "conan-agent")
	if err := os.WriteFile(override, []byte("override-binary"), 0755); err != nil {
		t.Fatalf("write override: %v", err)
	}

	req, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort:        9281,
		Token:            "node-token",
		AgentBinOverride: override,
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if got := decode(t, req.Binary); got != "override-binary" {
		t.Fatalf("binary = %q", got)
	}
	if len(req.Binaries) != 0 {
		t.Fatalf("binaries = %#v, want empty map when override is used", req.Binaries)
	}
	if !strings.Contains(req.Config, "listen: 0.0.0.0:9281") || !strings.Contains(req.Config, "token: node-token") {
		t.Fatalf("config =\n%s", req.Config)
	}
	if !strings.Contains(req.SystemdUnit, "ExecStart=/usr/local/bin/conan-agent run -c /etc/conan-agent/config.yaml") {
		t.Fatalf("systemd unit =\n%s", req.SystemdUnit)
	}
}

func TestBuildRequestWithoutOverrideSendsConfiguredArchitectureBinaries(t *testing.T) {
	dir := t.TempDir()
	amd64 := filepath.Join(dir, "amd64", "conan-agent")
	arm64 := filepath.Join(dir, "arm64", "conan-agent")
	mustWrite(t, amd64, "amd64-binary")
	mustWrite(t, arm64, "arm64-binary")

	req, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			Binaries: configschema.AgentBinaryConfig{AMD64: amd64, ARM64: arm64},
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort: 9280,
		Token:     "token",
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if req.Binary != "" {
		t.Fatalf("binary override = %q, want empty", req.Binary)
	}
	if got := decode(t, req.Binaries["amd64"]); got != "amd64-binary" {
		t.Fatalf("amd64 binary = %q", got)
	}
	if got := decode(t, req.Binaries["arm64"]); got != "arm64-binary" {
		t.Fatalf("arm64 binary = %q", got)
	}
}

func TestBuildRequestReturnsMissingBinaryError(t *testing.T) {
	_, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			Binaries:         configschema.AgentBinaryConfig{AMD64: "/missing/amd64", ARM64: "/missing/arm64"},
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort: 9280,
		Token:     "token",
	})
	if err == nil || !strings.Contains(err.Error(), "read amd64 agent binary") {
		t.Fatalf("err = %v", err)
	}
}

func mustWrite(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func decode(t *testing.T, encoded string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return string(data)
}
```

- [ ] **Step 2: Run request tests to verify RED**

Run:

```bash
go test ./internal/agentupdate -run 'TestBuildRequest' -v
```

Expected: FAIL because `internal/agentupdate` and `BuildRequest` do not exist.

- [ ] **Step 3: Implement request schema and builder**

Create `internal/agentupdate/request.go`:

```go
package agentupdate

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Request struct {
	Binary           string            `json:"binary,omitempty"`
	Binaries         map[string]string `json:"binaries,omitempty"`
	Config           string            `json:"config"`
	SystemdUnit      string            `json:"systemd_unit"`
	RemoteBinaryPath string            `json:"remote_binary_path"`
	RemoteConfigPath string            `json:"remote_config_path"`
	SystemdUnitPath  string            `json:"systemd_unit_path"`
}

type BuildOptions struct {
	DeployConfig     configschema.AgentDeployConfig
	AgentPort        int
	Token            string
	AgentBinOverride string
}

func BuildRequest(opts BuildOptions) (Request, error) {
	req := Request{
		Config:           deploy.RenderAgentConfig(opts.AgentPort, opts.Token),
		SystemdUnit:      deploy.RenderSystemdUnit(opts.DeployConfig.RemoteBinaryPath, opts.DeployConfig.RemoteConfigPath),
		RemoteBinaryPath: opts.DeployConfig.RemoteBinaryPath,
		RemoteConfigPath: opts.DeployConfig.RemoteConfigPath,
		SystemdUnitPath:  opts.DeployConfig.SystemdUnitPath,
	}
	if opts.AgentBinOverride != "" {
		encoded, err := readBase64(opts.AgentBinOverride)
		if err != nil {
			return Request{}, fmt.Errorf("read override agent binary: %w", err)
		}
		req.Binary = encoded
		return req, nil
	}

	req.Binaries = map[string]string{}
	amd64, err := readBase64(opts.DeployConfig.Binaries.AMD64)
	if err != nil {
		return Request{}, fmt.Errorf("read amd64 agent binary: %w", err)
	}
	arm64, err := readBase64(opts.DeployConfig.Binaries.ARM64)
	if err != nil {
		return Request{}, fmt.Errorf("read arm64 agent binary: %w", err)
	}
	req.Binaries["amd64"] = amd64
	req.Binaries["arm64"] = arm64
	return req, nil
}

func readBase64(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("binary is empty: %s", path)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
```

- [ ] **Step 4: Run request tests to verify GREEN**

Run:

```bash
go test ./internal/agentupdate -run 'TestBuildRequest' -v
```

Expected: PASS.

- [ ] **Step 5: Commit request builder**

```bash
git add internal/agentupdate/request.go internal/agentupdate/request_test.go
git commit -m "feat: add agent update request builder"
```

---

### Task 2: Agent-Side Apply Logic

**Files:**
- Create: `internal/agentupdate/apply.go`
- Create: `internal/agentupdate/apply_test.go`

- [ ] **Step 1: Write failing tests for apply behavior**

Create `internal/agentupdate/apply_test.go`:

```go
package agentupdate

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplierRejectsInvalidBinary(t *testing.T) {
	applier := Applier{Arch: func() string { return "amd64" }, TempDir: t.TempDir(), Runner: &fakeRunner{}}
	_, err := applier.Apply(context.Background(), Request{
		Binary:           "not-base64",
		Config:           "listen: 0.0.0.0:9280\n",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})
	if err == nil || !strings.Contains(err.Error(), "decode binary") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplierSelectsBinaryForProcessArchitecture(t *testing.T) {
	runner := &fakeRunner{}
	dir := t.TempDir()
	applier := Applier{Arch: func() string { return "arm64" }, TempDir: dir, Runner: runner}

	result, err := applier.Apply(context.Background(), Request{
		Binaries: map[string]string{
			"amd64": encode("amd64-binary"),
			"arm64": encode("arm64-binary"),
		},
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: filepath.Join(dir, "usr/local/bin/conan-agent"),
		RemoteConfigPath: filepath.Join(dir, "etc/conan-agent/config.yaml"),
		SystemdUnitPath:  filepath.Join(dir, "etc/systemd/system/conan-agent.service"),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Arch != "arm64" || result.BinaryPath == "" {
		t.Fatalf("result = %#v", result)
	}
	binaryPath := runner.installedSourceFor("install -m 0755")
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read temp binary: %v", err)
	}
	if string(data) != "arm64-binary" {
		t.Fatalf("binary data = %q", data)
	}
}

func TestApplierWritesFilesWithExpectedPermissionsAndRunsFixedCommands(t *testing.T) {
	runner := &fakeRunner{}
	dir := t.TempDir()
	applier := Applier{Arch: func() string { return "amd64" }, TempDir: dir, Runner: runner}

	_, err := applier.Apply(context.Background(), Request{
		Binary:           encode("agent-binary"),
		Config:           "agent-config",
		SystemdUnit:      "agent-unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"install -m 0755",
		"mkdir -p '/etc/conan-agent'",
		"install -m 0600",
		"install -m 0644",
		"systemctl daemon-reload",
		"systemctl enable --now conan-agent",
		"systemctl restart conan-agent",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands missing %q:\n%s", want, joined)
		}
	}

	for _, path := range []string{
		runner.installedSourceFor("install -m 0755"),
		runner.installedSourceFor("install -m 0600"),
		runner.installedSourceFor("install -m 0644"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("temp file is empty: %s", path)
		}
	}
}

func TestApplierReturnsCommandFailureWithoutSecrets(t *testing.T) {
	runner := &fakeRunner{err: errors.New("systemctl failed")}
	applier := Applier{Arch: func() string { return "amd64" }, TempDir: t.TempDir(), Runner: runner}

	_, err := applier.Apply(context.Background(), Request{
		Binary:           encode("agent-binary"),
		Config:           "token: secret-token",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})
	if err == nil {
		t.Fatal("expected command error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
}

type fakeRunner struct {
	commands []string
	err      error
}

func (r *fakeRunner) Run(_ context.Context, command string) (string, error) {
	r.commands = append(r.commands, command)
	if r.err != nil {
		return "", r.err
	}
	return "", nil
}

func (r *fakeRunner) installedSourceFor(prefix string) string {
	for _, command := range r.commands {
		if strings.Contains(command, prefix) {
			parts := strings.Split(command, " ")
			if len(parts) >= 4 {
				return strings.Trim(parts[3], "'")
			}
		}
	}
	return ""
}

func encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
```

- [ ] **Step 2: Run apply tests to verify RED**

Run:

```bash
go test ./internal/agentupdate -run 'TestApplier' -v
```

Expected: FAIL because `Applier` and `Apply` do not exist.

- [ ] **Step 3: Implement applier**

Create `internal/agentupdate/apply.go`:

```go
package agentupdate

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, command string) (string, error)
}

type Applier struct {
	Arch    func() string
	TempDir string
	Runner  CommandRunner
}

type ApplyResult struct {
	BinaryPath string
	Arch       string
}

type shellRunner struct{}

func (shellRunner) Run(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

func (a Applier) Apply(ctx context.Context, req Request) (ApplyResult, error) {
	arch := runtime.GOARCH
	if a.Arch != nil {
		arch = a.Arch()
	}
	encoded := req.Binary
	if encoded == "" {
		encoded = req.Binaries[arch]
	}
	if encoded == "" {
		return ApplyResult{}, fmt.Errorf("no binary payload for architecture %s", arch)
	}
	binary, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("decode binary: %w", err)
	}
	if len(binary) == 0 {
		return ApplyResult{}, fmt.Errorf("binary payload is empty")
	}

	tempDir := a.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	binaryTmp := filepath.Join(tempDir, "conan-agent."+suffix)
	configTmp := filepath.Join(tempDir, "conan-agent-config."+suffix)
	unitTmp := filepath.Join(tempDir, "conan-agent.service."+suffix)
	if err := os.WriteFile(binaryTmp, binary, 0755); err != nil {
		return ApplyResult{}, fmt.Errorf("write temp binary: %w", err)
	}
	if err := os.WriteFile(configTmp, []byte(req.Config), 0600); err != nil {
		return ApplyResult{}, fmt.Errorf("write temp config: %w", err)
	}
	if err := os.WriteFile(unitTmp, []byte(req.SystemdUnit), 0644); err != nil {
		return ApplyResult{}, fmt.Errorf("write temp systemd unit: %w", err)
	}

	runner := a.Runner
	if runner == nil {
		runner = shellRunner{}
	}
	commands := []string{
		fmt.Sprintf("install -m 0755 %s %s", shellQuote(binaryTmp), shellQuote(req.RemoteBinaryPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.Dir(req.RemoteConfigPath))),
		fmt.Sprintf("install -m 0600 %s %s", shellQuote(configTmp), shellQuote(req.RemoteConfigPath)),
		fmt.Sprintf("install -m 0644 %s %s", shellQuote(unitTmp), shellQuote(req.SystemdUnitPath)),
		"systemctl daemon-reload",
		"systemctl enable --now conan-agent",
		"systemctl restart conan-agent",
	}
	for _, command := range commands {
		if _, err := runner.Run(ctx, command); err != nil {
			return ApplyResult{}, fmt.Errorf("agent update command failed: %w", err)
		}
	}
	return ApplyResult{BinaryPath: req.RemoteBinaryPath, Arch: arch}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
```

- [ ] **Step 4: Run apply tests to verify GREEN**

Run:

```bash
go test ./internal/agentupdate -run 'TestApplier' -v
```

Expected: PASS.

- [ ] **Step 5: Commit applier**

```bash
git add internal/agentupdate/apply.go internal/agentupdate/apply_test.go
git commit -m "feat: add agent-side update applier"
```

---

### Task 3: MCP Tool Registration For agent_update

**Files:**
- Create: `internal/tools/agent_update.go`
- Create: `internal/tools/agent_update_test.go`
- Modify: `internal/tools/metadata.go`
- Modify: `cmd/conan-agent/main.go`
- Modify: `cmd/conan-agent/main_test.go`

- [ ] **Step 1: Write failing tool tests**

Create `internal/tools/agent_update_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/agentupdate"
)

func TestAgentUpdateToolSchemaAndMetadata(t *testing.T) {
	tool := NewAgentUpdateTool(agentupdate.Applier{})
	if tool.Name() != "agent_update" {
		t.Fatalf("name = %q", tool.Name())
	}
	if !strings.Contains(tool.Description(), "Update conan-agent") {
		t.Fatalf("description = %q", tool.Description())
	}
	if !strings.Contains(string(tool.InputSchema()), "remote_binary_path") {
		t.Fatalf("schema = %s", tool.InputSchema())
	}
	meta, ok := MetadataFor("agent_update")
	if !ok {
		t.Fatal("agent_update metadata missing")
	}
	if meta.Safety != SafetyDestructive || meta.Scope != ScopeNode {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestAgentUpdateToolReturnsErrorResultForInvalidPayload(t *testing.T) {
	tool := NewAgentUpdateTool(agentupdate.Applier{})
	input, _ := json.Marshal(agentupdate.Request{
		Binary:           "not-base64",
		Config:           "config",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned transport error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result = %#v, want IsError", result)
	}
	if !strings.Contains(result.Content[0].Text, "decode binary") {
		t.Fatalf("text = %q", result.Content[0].Text)
	}
}
```

- [ ] **Step 2: Update conan-agent registration test before implementation**

Modify the `allowed` map in `cmd/conan-agent/main_test.go` by adding:

```go
"agent_update":    true,
```

Add this test to `cmd/conan-agent/main_test.go`:

```go
func TestRegisterAllToolsCanDisableAgentUpdate(t *testing.T) {
	registry := tools.NewRegistry()
	registerAllTools(registry, &configschema.AgentConfig{DisabledTools: []string{"agent_update"}})
	registry.DisableAll([]string{"agent_update"})

	if _, ok := registry.Get("agent_update"); ok {
		t.Fatal("agent_update should be disabled")
	}
}
```

- [ ] **Step 3: Run tool tests to verify RED**

Run:

```bash
go test ./internal/tools ./cmd/conan-agent -run 'TestAgentUpdate|TestRegisterAllTools' -v
```

Expected: FAIL because `NewAgentUpdateTool` does not exist and the registration test expects `agent_update`.

- [ ] **Step 4: Implement the tool wrapper**

Create `internal/tools/agent_update.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type AgentUpdateTool struct {
	applier agentupdate.Applier
}

func NewAgentUpdateTool(applier agentupdate.Applier) *AgentUpdateTool {
	return &AgentUpdateTool{applier: applier}
}

func (t *AgentUpdateTool) Name() string { return "agent_update" }

func (t *AgentUpdateTool) Description() string {
	return "Update conan-agent binary, config, and systemd unit on this node"
}

func (t *AgentUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"binary": {"type": "string", "description": "Optional base64 conan-agent binary override"},
			"binaries": {
				"type": "object",
				"additionalProperties": {"type": "string"},
				"description": "Architecture to base64 conan-agent binary map"
			},
			"config": {"type": "string", "description": "Agent YAML config content"},
			"systemd_unit": {"type": "string", "description": "Systemd unit content"},
			"remote_binary_path": {"type": "string", "description": "Target binary path"},
			"remote_config_path": {"type": "string", "description": "Target config path"},
			"systemd_unit_path": {"type": "string", "description": "Target systemd unit path"}
		},
		"required": ["config", "systemd_unit", "remote_binary_path", "remote_config_path", "systemd_unit_path"]
	}`)
}

func (t *AgentUpdateTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var req agentupdate.Request
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	result, err := t.applier.Apply(ctx, req)
	if err != nil {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())},
			IsError: true,
		}, nil
	}
	return &mcpproto.ToolResult{
		Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf("updated conan-agent at %s for %s", result.BinaryPath, result.Arch))},
	}, nil
}
```

- [ ] **Step 5: Register metadata and agent tool**

In `internal/tools/metadata.go`, add this entry near `node_add`:

```go
meta("agent_update", SafetyDestructive, ScopeNode, []string{"agent", "deploy"}, []string{"update", "self-update"}),
```

In `cmd/conan-agent/main.go`, add this in `registerAllTools`:

```go
r.Register(tools.NewAgentUpdateTool(agentupdate.Applier{}))
```

Add the import:

```go
"github.com/pockyHM/conan/internal/agentupdate"
```

- [ ] **Step 6: Run tool registration tests to verify GREEN**

Run:

```bash
go test ./internal/tools ./cmd/conan-agent -run 'TestAgentUpdate|TestRegisterAllTools' -v
```

Expected: PASS.

- [ ] **Step 7: Commit tool registration**

```bash
git add internal/tools/agent_update.go internal/tools/agent_update_test.go internal/tools/metadata.go cmd/conan-agent/main.go cmd/conan-agent/main_test.go
git commit -m "feat: expose agent update tool"
```

---

### Task 4: MCP Agent Updater Client

**Files:**
- Create: `internal/nodeupdate/agent_updater.go`
- Create: `internal/nodeupdate/agent_updater_test.go`

- [ ] **Step 1: Write failing MCP updater tests**

Create `internal/nodeupdate/agent_updater_test.go`:

```go
package nodeupdate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestMCPAgentUpdaterCallsAgentUpdateTool(t *testing.T) {
	var sawTool bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc: %v", err)
		}
		var params mcpproto.ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.Name != "agent_update" {
			t.Fatalf("tool = %q", params.Name)
		}
		if !strings.Contains(string(params.Arguments), "remote_binary_path") {
			t.Fatalf("arguments = %s", params.Arguments)
		}
		sawTool = true
		writeRPCResult(t, w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}})
	}))
	defer srv.Close()

	err := MCPAgentUpdater{BaseURL: func(AgentTarget) string { return srv.URL }}.Update(t.Context(), AgentTarget{
		Host:  "127.0.0.1",
		Port:  9280,
		Token: "token",
		Request: agentupdate.Request{
			Binary:           "Ymlu",
			Config:           "config",
			SystemdUnit:      "unit",
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !sawTool {
		t.Fatal("agent_update was not called")
	}
}

func TestMCPAgentUpdaterRetriesHealthAfterToolCall(t *testing.T) {
	var healthCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			call := atomic.AddInt32(&healthCalls, 1)
			if call < 3 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		var req mcpproto.JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		writeRPCResult(t, w, req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("updated")}})
	}))
	defer srv.Close()

	err := MCPAgentUpdater{BaseURL: func(AgentTarget) string { return srv.URL }, HealthAttempts: 3}.Update(t.Context(), AgentTarget{
		Token: "token",
		Request: agentupdate.Request{
			Binary:           "Ymlu",
			Config:           "config",
			SystemdUnit:      "unit",
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if healthCalls != 3 {
		t.Fatalf("health calls = %d, want 3", healthCalls)
	}
}

func TestMCPAgentUpdaterReturnsToolErrorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcpproto.JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		writeRPCResult(t, w, req.ID, mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent("install failed")},
			IsError: true,
		})
	}))
	defer srv.Close()

	err := MCPAgentUpdater{BaseURL: func(AgentTarget) string { return srv.URL }, HealthAttempts: 1}.Update(t.Context(), AgentTarget{Request: agentupdate.Request{Binary: "Ymlu"}})
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("err = %v", err)
	}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result mcpproto.ToolResult) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mcpproto.JSONRPCResponse{
		JSONRPC: mcpproto.JSONRPCVersion,
		ID:      id,
		Result:  mustRaw(t, result),
	})
}

func mustRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
```

- [ ] **Step 2: Run MCP updater tests to verify RED**

Run:

```bash
go test ./internal/nodeupdate -run 'TestMCPAgentUpdater' -v
```

Expected: FAIL because `MCPAgentUpdater` does not exist.

- [ ] **Step 3: Implement MCP updater**

Create `internal/nodeupdate/agent_updater.go`:

```go
package nodeupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type AgentTarget struct {
	Host    string
	Port    int
	TLS     bool
	Token   string
	Request agentupdate.Request
}

type AgentUpdater interface {
	Update(ctx context.Context, target AgentTarget) error
}

type MCPAgentUpdater struct {
	HTTPClient     *http.Client
	BaseURL        func(AgentTarget) string
	HealthAttempts int
	HealthDelay    time.Duration
}

func (u MCPAgentUpdater) Update(ctx context.Context, target AgentTarget) error {
	baseURL := mcp.URL(target.Host, target.Port, target.TLS)
	if u.BaseURL != nil {
		baseURL = u.BaseURL(target)
	}
	client := mcp.NewClient(mcp.Config{BaseURL: baseURL, Token: target.Token, Client: u.HTTPClient})
	data, err := json.Marshal(target.Request)
	if err != nil {
		return err
	}
	result, err := client.CallTool(ctx, "agent_update", data)
	if err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("agent_update failed: %s", toolText(result))
	}
	attempts := u.HealthAttempts
	if attempts == 0 {
		attempts = 10
	}
	delay := u.HealthDelay
	if delay == 0 {
		delay = 250 * time.Millisecond
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := client.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("agent health check after update failed: %w", lastErr)
}

func toolText(result *mcpproto.ToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 4: Run MCP updater tests to verify GREEN**

Run:

```bash
go test ./internal/nodeupdate -run 'TestMCPAgentUpdater' -v
```

Expected: PASS.

- [ ] **Step 5: Commit MCP updater**

```bash
git add internal/nodeupdate/agent_updater.go internal/nodeupdate/agent_updater_test.go
git commit -m "feat: add mcp agent updater client"
```

---

### Task 5: Node Update Modes And Fallback

**Files:**
- Modify: `internal/nodeupdate/service.go`
- Modify: `internal/nodeupdate/service_test.go`

- [ ] **Step 1: Add failing service tests for mode behavior**

Append these tests to `internal/nodeupdate/service_test.go`:

```go
func TestUpdateAutoUsesSSHAndSkipsAgentWhenSSHWorks(t *testing.T) {
	agent := &fakeAgentUpdater{}
	deployer := &fakeDeployer{}
	service := Service{
		Credentials: &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:    deployer,
		AgentUpdater: agent,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, Token: "token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "override"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(deployer.targets) != 1 {
		t.Fatalf("deploy calls = %d", len(deployer.targets))
	}
	if len(agent.targets) != 0 {
		t.Fatalf("agent calls = %#v", agent.targets)
	}
}

func TestUpdateAutoFallsBackToAgentWhenSSHFails(t *testing.T) {
	agent := &fakeAgentUpdater{}
	deployer := &fakeDeployer{err: errors.New("ssh connection refused")}
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     deployer,
		AgentUpdater: agent,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, Token: "token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "override"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(agent.targets) != 1 {
		t.Fatalf("agent calls = %d", len(agent.targets))
	}
	if agent.targets[0].Host != "10.0.0.1" || agent.targets[0].Port != 9280 || agent.targets[0].Token != "token" {
		t.Fatalf("agent target = %#v", agent.targets[0])
	}
}

func TestUpdateAutoReturnsBothErrorsWhenFallbackFails(t *testing.T) {
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     &fakeDeployer{err: errors.New("ssh failed")},
		AgentUpdater: &fakeAgentUpdater{err: errors.New("agent failed")},
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, Token: "token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "override"),
		DeployConfig:     testDeployConfig(t),
	})
	if err == nil || !strings.Contains(err.Error(), "ssh update failed") || !strings.Contains(err.Error(), "agent update fallback failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateAgentModeSkipsSSHCredentialsAndPrompts(t *testing.T) {
	agent := &fakeAgentUpdater{}
	service := Service{
		Prompter:     fakePrompter{username: "should-not-be-used", password: "should-not-be-used"},
		Deployer:     &fakeDeployer{},
		AgentUpdater: agent,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, Token: "token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAgent,
		AgentBinOverride: testAgentBinary(t, "override"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(agent.targets) != 1 {
		t.Fatalf("agent calls = %d", len(agent.targets))
	}
}

func TestUpdateSSHModeDoesNotCallAgent(t *testing.T) {
	agent := &fakeAgentUpdater{}
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     &fakeDeployer{err: errors.New("ssh failed")},
		AgentUpdater: agent,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, Token: "token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeSSH,
		AgentBinOverride: testAgentBinary(t, "override"),
		DeployConfig:     testDeployConfig(t),
	})
	if err == nil || !strings.Contains(err.Error(), "ssh failed") {
		t.Fatalf("err = %v", err)
	}
	if len(agent.targets) != 0 {
		t.Fatalf("agent calls = %#v", agent.targets)
	}
}
```

Add these test helpers and update `fakeDeployer`:

```go
type fakeAgentUpdater struct {
	targets []AgentTarget
	err     error
}

func (u *fakeAgentUpdater) Update(_ context.Context, target AgentTarget) error {
	u.targets = append(u.targets, target)
	return u.err
}

func testAgentBinary(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conan-agent")
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatalf("write agent binary: %v", err)
	}
	return path
}

func testDeployConfig(t *testing.T) configschema.AgentDeployConfig {
	t.Helper()
	return configschema.AgentDeployConfig{
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	}
}

type fakeDeployer struct {
	targets []deploy.Target
	err     error
}

func (d *fakeDeployer) Deploy(_ context.Context, target deploy.Target) error {
	d.targets = append(d.targets, target)
	return d.err
}
```

Add imports to `internal/nodeupdate/service_test.go`:

```go
"errors"
"os"
"path/filepath"
```

- [ ] **Step 2: Run service tests to verify RED**

Run:

```bash
go test ./internal/nodeupdate -run 'TestUpdate(Auto|AgentMode|SSHMode)' -v
```

Expected: FAIL because modes, `AgentUpdater`, and fallback are not implemented.

- [ ] **Step 3: Add modes and request fields**

Modify `internal/nodeupdate/service.go` imports:

```go
"github.com/pockyHM/conan/internal/agentupdate"
```

Add mode constants:

```go
type UpdateMode string

const (
	ModeAuto  UpdateMode = "auto"
	ModeSSH   UpdateMode = "ssh"
	ModeAgent UpdateMode = "agent"
)

func normalizeMode(mode UpdateMode) (UpdateMode, error) {
	if mode == "" {
		return ModeAuto, nil
	}
	switch mode {
	case ModeAuto, ModeSSH, ModeAgent:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid update mode %q", mode)
	}
}
```

Add fields:

```go
Mode UpdateMode
```

to `Request`, and:

```go
AgentUpdater AgentUpdater
```

to `Service`.

- [ ] **Step 4: Split SSH update and add agent update helper**

In `Update`, normalize the mode before selecting nodes:

```go
mode, err := normalizeMode(req.Mode)
if err != nil {
	return nil, err
}
req.Mode = mode
```

Replace the loop body with:

```go
if err := s.updateNode(ctx, req, node); err != nil {
	return results, fmt.Errorf("update %s/%s: %w", req.ClusterName, node.Name, err)
}
```

Replace `updateNode` with mode dispatch:

```go
func (s Service) updateNode(ctx context.Context, req Request, node cfgloader.Node) error {
	switch req.Mode {
	case ModeSSH:
		return s.updateNodeViaSSH(ctx, req, node)
	case ModeAgent:
		return s.updateNodeViaAgent(ctx, req, node)
	case ModeAuto:
		sshErr := s.updateNodeViaSSH(ctx, req, node)
		if sshErr == nil {
			return nil
		}
		if err := s.updateNodeViaAgent(ctx, req, node); err != nil {
			return fmt.Errorf("ssh update failed: %v; agent update fallback failed: %w", sshErr, err)
		}
		return nil
	default:
		return fmt.Errorf("invalid update mode %q", req.Mode)
	}
}
```

Rename the existing SSH body to:

```go
func (s Service) updateNodeViaSSH(ctx context.Context, req Request, node cfgloader.Node) error {
	// move the current updateNode implementation here unchanged
}
```

Add agent update helper:

```go
func (s Service) updateNodeViaAgent(ctx context.Context, req Request, node cfgloader.Node) error {
	if s.AgentUpdater == nil {
		return fmt.Errorf("agent updater is required")
	}
	updateReq, err := agentupdate.BuildRequest(agentupdate.BuildOptions{
		DeployConfig:     req.DeployConfig,
		AgentPort:        node.Agent.Port,
		Token:            node.Agent.Token,
		AgentBinOverride: req.AgentBinOverride,
	})
	if err != nil {
		return err
	}
	return s.AgentUpdater.Update(ctx, AgentTarget{
		Host:    node.Agent.Host,
		Port:    node.Agent.Port,
		TLS:     node.Agent.TLS,
		Token:   node.Agent.Token,
		Request: updateReq,
	})
}
```

- [ ] **Step 5: Run service tests to verify GREEN**

Run:

```bash
go test ./internal/nodeupdate -run 'TestUpdate(Auto|AgentMode|SSHMode|SingleNode|AllNodes|NodeNotFound)' -v
```

Expected: PASS.

- [ ] **Step 6: Commit service fallback**

```bash
git add internal/nodeupdate/service.go internal/nodeupdate/service_test.go
git commit -m "feat: add node update modes and fallback"
```

---

### Task 6: CLI Mode Flag

**Files:**
- Modify: `cmd/conan/main.go`
- Modify: `cmd/conan/main_test.go`

- [ ] **Step 1: Write failing CLI tests**

Modify `TestNodeUpdateCommandRegistered` in `cmd/conan/main_test.go` to include `--mode`:

```go
for _, want := range []string{"--all", "--all-cluster", "--agent-bin", "--mode"} {
	if !strings.Contains(stdout, want) {
		t.Fatalf("help output missing %q: %q", want, stdout)
	}
}
```

Add this test:

```go
func TestNodeUpdateRejectsInvalidMode(t *testing.T) {
	_, _, err := executeCommand("node", "update", "web-1", "--mode", "invalid")
	if err == nil || !strings.Contains(err.Error(), "invalid update mode") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run CLI tests to verify RED**

Run:

```bash
go test ./cmd/conan -run 'TestNodeUpdate(CommandRegistered|RejectsInvalidMode)' -v
```

Expected: FAIL because `--mode` does not exist.

- [ ] **Step 3: Add mode flag and pass updater dependency**

In `cmd/conan/main.go`, add a variable next to other node update flags:

```go
var nodeUpdateMode string
```

Before loading config in `RunE`, validate mode:

```go
mode := nodeupdate.UpdateMode(nodeUpdateMode)
if mode == "" {
	mode = nodeupdate.ModeAuto
}
if mode != nodeupdate.ModeAuto && mode != nodeupdate.ModeSSH && mode != nodeupdate.ModeAgent {
	return fmt.Errorf("invalid update mode %q", nodeUpdateMode)
}
```

Add the updater dependency:

```go
service := nodeupdate.Service{
	Credentials:  credentials.NewStore(loader.Home()),
	Prompter:     cliPrompter{in: cmd.InOrStdin(), out: cmd.OutOrStdout()},
	Deployer:     deploy.NewNativeDeployer(),
	AgentUpdater: nodeupdate.MCPAgentUpdater{},
}
```

Pass mode in the request:

```go
Mode: mode,
```

Register the flag:

```go
nodeUpdateCmd.Flags().StringVar(&nodeUpdateMode, "mode", "auto", "Update mode: auto, ssh, or agent")
```

- [ ] **Step 4: Run CLI tests to verify GREEN**

Run:

```bash
go test ./cmd/conan -run 'TestNodeUpdate(CommandRegistered|RejectsInvalidMode)' -v
```

Expected: PASS.

- [ ] **Step 5: Commit CLI mode flag**

```bash
git add cmd/conan/main.go cmd/conan/main_test.go
git commit -m "feat: add node update mode flag"
```

---

### Task 7: Documentation

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] **Step 1: Update English README node update section**

In `README.md`, replace the `node update` paragraph under "Update `conan-agent` on existing nodes" with:

```markdown
`node update` defaults to `--mode auto`: it reads configured nodes and saved SSH credentials, tries the SSH/SFTP update path first, and falls back to the authenticated agent update interface if SSH cannot complete. Use `--mode ssh` to force the old SSH-only behavior, or `--mode agent` to skip SSH credentials and update through the running agent.

Use `--agent-bin` to point at a local binary override. In `auto` and `ssh` modes, `--user`, `--password`, and `--ssh-port` override SSH connection settings.
```

- [ ] **Step 2: Update Chinese README node update section**

In `README.zh-CN.md`, replace the matching paragraph with:

```markdown
`node update` 默认使用 `--mode auto`：读取已配置节点和已保存的 SSH 凭据，先尝试 SSH/SFTP 更新；如果 SSH 无法完成，则回退到已鉴权的 agent 更新接口。可以用 `--mode ssh` 强制旧的仅 SSH 行为，也可以用 `--mode agent` 跳过 SSH 凭据、直接通过正在运行的 agent 更新。

可以用 `--agent-bin` 指定本地二进制覆盖路径。在 `auto` 和 `ssh` 模式下，`--user`、`--password` 和 `--ssh-port` 会覆盖 SSH 连接参数。
```

- [ ] **Step 3: Verify docs mention all modes**

Run:

```bash
rg -n -- '--mode (auto|ssh|agent)|mode auto|mode ssh|mode agent' README.md README.zh-CN.md
```

Expected: output includes both README files.

- [ ] **Step 4: Commit docs**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: document node update modes"
```

---

### Task 8: Full Verification

**Files:**
- No new files.

- [ ] **Step 1: Run focused package tests**

Run:

```bash
go test ./internal/agentupdate ./internal/tools ./cmd/conan-agent ./internal/nodeupdate ./cmd/conan -v
```

Expected: PASS.

- [ ] **Step 2: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Check worktree**

Run:

```bash
git status --short
```

Expected: only intentional changes are present. If unrelated pre-existing changes remain, leave them untouched and mention them in the handoff.

---

## Self-Review

Spec coverage:

- Default `auto` mode with SSH first and agent fallback: Task 5 and Task 6.
- Explicit `ssh` and `agent` modes: Task 5 and Task 6.
- New `agent_update` MCP tool: Task 2 and Task 3.
- Shared artifact handling with override and architecture map: Task 1 and Task 2.
- Post-update health verification: Task 4.
- Security metadata and disable support: Task 3.
- Clear dual-error context: Task 5.
- README updates: Task 7.

Placeholder scan:

- The plan contains no unresolved placeholders and no references to undefined implementation types after their defining task.

Type consistency:

- `agentupdate.Request`, `agentupdate.BuildOptions`, `nodeupdate.AgentTarget`, `nodeupdate.AgentUpdater`, and `nodeupdate.MCPAgentUpdater` are introduced before later tasks use them.
- Mode names are consistently `ModeAuto`, `ModeSSH`, and `ModeAgent`.
