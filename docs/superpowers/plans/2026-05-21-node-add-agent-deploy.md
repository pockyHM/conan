# Node Add Agent Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `conan node add` so one command registers a node, stores encrypted SSH credentials, deploys or updates `conan-agent` over Go-native SSH/SFTP, and verifies agent health.

**Architecture:** Extend config schema and loading first, then add focused packages for node config writing, encrypted credentials, deployment artifact rendering, SSH deployment, and the node-add orchestration service. Keep `cmd/conan` thin: parse flags and prompts, then call the service.

**Tech Stack:** Go, Cobra, YAML v3, AES-GCM, `golang.org/x/crypto/ssh`, `golang.org/x/crypto/ssh/knownhosts`, `github.com/pkg/sftp`, systemd, existing MCP health client.

---

## File Structure

- Modify `pkg/configschema/config.go` — add global `agent_deploy` config and node-level agent token.
- Modify `internal/config/loader.go` — add global deploy defaults and node token precedence.
- Modify `internal/config/loader_test.go` — verify deploy defaults and node token override.
- Create `internal/config/nodes_writer.go` — append/update `clusters/<cluster>/nodes.yaml`.
- Create `internal/config/nodes_writer_test.go` — writer behavior tests.
- Create `internal/credentials/store.go` — encrypted local credential store.
- Create `internal/credentials/store_test.go` — encryption, permissions, corrupt-file tests.
- Create `internal/deploy/artifacts.go` — arch mapping, agent config rendering, systemd unit rendering, local binary resolution.
- Create `internal/deploy/artifacts_test.go` — artifact tests.
- Create `internal/deploy/deployer.go` — SSH/SFTP deployer interfaces and native implementation.
- Create `internal/deploy/deployer_test.go` — fake transport tests for command order and no secret leakage.
- Create `internal/nodeadd/service.go` — node-add orchestration.
- Create `internal/nodeadd/service_test.go` — service tests with fakes.
- Modify `cmd/conan/main.go` — add `node add` command and prompt helpers.
- Modify `cmd/conan/main_test.go` — CLI registration and `--no-deploy` smoke tests.
- Modify `go.mod` / `go.sum` — add SSH/SFTP dependencies.

---

### Task 1: Config schema and effective node token

**Files:**
- Modify: `pkg/configschema/config.go`
- Modify: `internal/config/loader.go`
- Test: `internal/config/loader_test.go`

- [ ] **Step 1: Write failing tests for deploy defaults and node token precedence**

Add these tests to `internal/config/loader_test.go`:

```go
func TestLoadGlobalAppliesAgentDeployDefaults(t *testing.T) {
	home := t.TempDir()

	loader := NewLoader(home)
	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	wantAMD64 := filepath.Join(home, "agent", "amd64", "conan-agent")
	wantARM64 := filepath.Join(home, "agent", "arm64", "conan-agent")
	if cfg.AgentDeploy.Binaries.AMD64 != wantAMD64 {
		t.Fatalf("amd64 binary = %q, want %q", cfg.AgentDeploy.Binaries.AMD64, wantAMD64)
	}
	if cfg.AgentDeploy.Binaries.ARM64 != wantARM64 {
		t.Fatalf("arm64 binary = %q, want %q", cfg.AgentDeploy.Binaries.ARM64, wantARM64)
	}
	if cfg.AgentDeploy.RemoteBinaryPath != "/usr/local/bin/conan-agent" {
		t.Fatalf("remote binary path = %q", cfg.AgentDeploy.RemoteBinaryPath)
	}
	if cfg.AgentDeploy.RemoteConfigPath != "/etc/conan-agent/config.yaml" {
		t.Fatalf("remote config path = %q", cfg.AgentDeploy.RemoteConfigPath)
	}
	if cfg.AgentDeploy.SystemdUnitPath != "/etc/systemd/system/conan-agent.service" {
		t.Fatalf("systemd unit path = %q", cfg.AgentDeploy.SystemdUnitPath)
	}
}

func TestLoadGlobalExpandsAgentDeployBinaryPaths(t *testing.T) {
	home := t.TempDir()
	customRoot := filepath.Join(home, "custom")
	t.Setenv("CONAN_AGENT_ROOT", customRoot)
	writeFile(t, filepath.Join(home, "config.yaml"), `agent_deploy:
  binaries:
    amd64: ${CONAN_AGENT_ROOT}/amd64/agent
    arm64: ~/agents/arm64/conan-agent
  remote_binary_path: /opt/conan/conan-agent
`)

	loader := NewLoader(home)
	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	if cfg.AgentDeploy.Binaries.AMD64 != filepath.Join(customRoot, "amd64", "agent") {
		t.Fatalf("amd64 path = %q", cfg.AgentDeploy.Binaries.AMD64)
	}
	if !strings.HasSuffix(cfg.AgentDeploy.Binaries.ARM64, filepath.Join("agents", "arm64", "conan-agent")) {
		t.Fatalf("arm64 path was not expanded from home: %q", cfg.AgentDeploy.Binaries.ARM64)
	}
	if cfg.AgentDeploy.RemoteBinaryPath != "/opt/conan/conan-agent" {
		t.Fatalf("remote binary path = %q", cfg.AgentDeploy.RemoteBinaryPath)
	}
	if cfg.AgentDeploy.RemoteConfigPath != "/etc/conan-agent/config.yaml" {
		t.Fatalf("remote config path default = %q", cfg.AgentDeploy.RemoteConfigPath)
	}
}

func TestLoadClusterNodeTokenOverridesClusterToken(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
agent:
  token: cluster-token
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
    agent:
      token: node-token
  - name: web-2
    host: 10.0.0.12
`)

	cluster, err := NewLoader(home).LoadCluster("prod")
	if err != nil {
		t.Fatalf("LoadCluster: %v", err)
	}
	if cluster.Nodes[0].Agent.Token != "node-token" {
		t.Fatalf("web-1 token = %q", cluster.Nodes[0].Agent.Token)
	}
	if cluster.Nodes[1].Agent.Token != "cluster-token" {
		t.Fatalf("web-2 token = %q", cluster.Nodes[1].Agent.Token)
	}
}
```

Add `strings` to the imports in `internal/config/loader_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/config
```

Expected: FAIL because `GlobalConfig.AgentDeploy`, `NodeAgentOverride.Token`, and node token precedence do not exist.

- [ ] **Step 3: Add schema fields**

In `pkg/configschema/config.go`, update `GlobalConfig` and `NodeAgentOverride`, and add deploy structs:

```go
type GlobalConfig struct {
	DefaultModel   string            `yaml:"default_model"`
	DefaultCluster string            `yaml:"default_cluster"`
	Models         []ModelConfig     `yaml:"models"`
	Security       SecurityConfig    `yaml:"security"`
	Memory         MemoryConfig      `yaml:"memory"`
	Logging        LoggingConfig     `yaml:"logging"`
	AgentDeploy    AgentDeployConfig `yaml:"agent_deploy"`
}

type AgentDeployConfig struct {
	Binaries         AgentBinaryConfig `yaml:"binaries"`
	RemoteBinaryPath string            `yaml:"remote_binary_path"`
	RemoteConfigPath string            `yaml:"remote_config_path"`
	SystemdUnitPath  string            `yaml:"systemd_unit_path"`
}

type AgentBinaryConfig struct {
	AMD64 string `yaml:"amd64"`
	ARM64 string `yaml:"arm64"`
}

type NodeAgentOverride struct {
	User  string `yaml:"user"`
	Port  int    `yaml:"port"`
	Token string `yaml:"token"`
}
```

- [ ] **Step 4: Apply global deploy defaults and path expansion**

In `internal/config/loader.go`, add helper functions below `applyGlobalDefaults`:

```go
func applyAgentDeployDefaults(cfg *configschema.GlobalConfig, home string) {
	if cfg.AgentDeploy.Binaries.AMD64 == "" {
		cfg.AgentDeploy.Binaries.AMD64 = filepath.Join(home, "agent", "amd64", "conan-agent")
	}
	if cfg.AgentDeploy.Binaries.ARM64 == "" {
		cfg.AgentDeploy.Binaries.ARM64 = filepath.Join(home, "agent", "arm64", "conan-agent")
	}
	if cfg.AgentDeploy.RemoteBinaryPath == "" {
		cfg.AgentDeploy.RemoteBinaryPath = "/usr/local/bin/conan-agent"
	}
	if cfg.AgentDeploy.RemoteConfigPath == "" {
		cfg.AgentDeploy.RemoteConfigPath = "/etc/conan-agent/config.yaml"
	}
	if cfg.AgentDeploy.SystemdUnitPath == "" {
		cfg.AgentDeploy.SystemdUnitPath = "/etc/systemd/system/conan-agent.service"
	}
	cfg.AgentDeploy.Binaries.AMD64 = expandPath(cfg.AgentDeploy.Binaries.AMD64)
	cfg.AgentDeploy.Binaries.ARM64 = expandPath(cfg.AgentDeploy.Binaries.ARM64)
}

func expandPath(path string) string {
	path = configschema.ExpandEnv(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
```

Change `LoadGlobal` so defaults are applied with the loader home both before and after YAML unmarshal:

```go
func (l *Loader) LoadGlobal() (*configschema.GlobalConfig, error) {
	cfg := &configschema.GlobalConfig{}
	applyGlobalDefaults(cfg)
	applyAgentDeployDefaults(cfg, l.home)

	path := filepath.Join(l.home, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	applyGlobalDefaults(cfg)
	applyAgentDeployDefaults(cfg, l.home)
	for i := range cfg.Models {
		cfg.Models[i].APIKey = configschema.ExpandEnv(cfg.Models[i].APIKey)
		cfg.Models[i].Endpoint = strings.TrimRight(cfg.Models[i].Endpoint, "/")
	}
	return cfg, nil
}
```

- [ ] **Step 5: Implement node token precedence**

Replace the token logic in `effectiveAgent` in `internal/config/loader.go`:

```go
func effectiveAgent(node configschema.NodeConfig, cluster configschema.ClusterConfig) EffectiveAgentConfig {
	port := portFromListen(cluster.Agent.Listen)
	user := cluster.NodeDefaults.User
	token := cluster.Agent.Token
	if node.Agent != nil {
		if node.Agent.Port != 0 {
			port = node.Agent.Port
		}
		if node.Agent.User != "" {
			user = node.Agent.User
		}
		if node.Agent.Token != "" {
			token = node.Agent.Token
		}
	}
	return EffectiveAgentConfig{
		Host:  node.Host,
		Port:  port,
		User:  user,
		TLS:   cluster.Agent.TLS,
		Token: configschema.ExpandEnv(token),
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run:

```bash
go test ./internal/config ./pkg/configschema
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/configschema/config.go internal/config/loader.go internal/config/loader_test.go
git commit -m "feat: add agent deploy config schema"
```

---

### Task 2: Nodes YAML writer

**Files:**
- Create: `internal/config/nodes_writer.go`
- Test: `internal/config/nodes_writer_test.go`

- [ ] **Step 1: Write failing writer tests**

Create `internal/config/nodes_writer_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestWriteNodeAppendsNewNode(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")

	result, err := NewNodeWriter(home).WriteNode("prod", configschema.NodeConfig{
		Name: "web-1",
		Host: "10.0.0.11",
		Agent: &configschema.NodeAgentOverride{User: "deploy", Port: 9200, Token: "node-token"},
	}, WriteNodeOptions{})
	if err != nil {
		t.Fatalf("WriteNode: %v", err)
	}
	if result.Updated {
		t.Fatal("Updated = true, want append")
	}

	data, err := os.ReadFile(filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"name: web-1", "host: 10.0.0.11", "user: deploy", "port: 9200", "token: node-token"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("nodes.yaml missing %q:\n%s", want, contents)
		}
	}
}

func TestWriteNodeDuplicateWithoutUpdateFails(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: old.example.com
`)

	_, err := NewNodeWriter(home).WriteNode("prod", configschema.NodeConfig{Name: "web-1", Host: "new.example.com"}, WriteNodeOptions{})
	if err == nil || !strings.Contains(err.Error(), "node already exists") {
		t.Fatalf("err = %v, want duplicate error", err)
	}
}

func TestWriteNodeUpdatePreservesTokenByDefault(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: old.example.com
    agent:
      user: old
      port: 9200
      token: keep-token
`)

	result, err := NewNodeWriter(home).WriteNode("prod", configschema.NodeConfig{
		Name: "web-1",
		Host: "10.0.0.11",
		Agent: &configschema.NodeAgentOverride{User: "deploy", Port: 9300, Token: "new-token"},
	}, WriteNodeOptions{Update: true})
	if err != nil {
		t.Fatalf("WriteNode update: %v", err)
	}
	if !result.Updated {
		t.Fatal("Updated = false, want update")
	}
	if result.Node.Agent.Token != "keep-token" {
		t.Fatalf("token = %q, want preserved", result.Node.Agent.Token)
	}
}

func TestWriteNodeRotateTokenReplacesToken(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: old.example.com
    agent:
      token: old-token
`)

	result, err := NewNodeWriter(home).WriteNode("prod", configschema.NodeConfig{
		Name: "web-1",
		Host: "10.0.0.11",
		Agent: &configschema.NodeAgentOverride{Token: "new-token"},
	}, WriteNodeOptions{Update: true, RotateToken: true})
	if err != nil {
		t.Fatalf("WriteNode rotate: %v", err)
	}
	if result.Node.Agent.Token != "new-token" {
		t.Fatalf("token = %q, want rotated", result.Node.Agent.Token)
	}
}

func TestWriteNodeMissingClusterFails(t *testing.T) {
	home := t.TempDir()
	_, err := NewNodeWriter(home).WriteNode("prod", configschema.NodeConfig{Name: "web-1", Host: "10.0.0.11"}, WriteNodeOptions{})
	if err == nil || !strings.Contains(err.Error(), "cluster not found") {
		t.Fatalf("err = %v, want missing cluster error", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/config
```

Expected: FAIL because `NewNodeWriter`, `WriteNodeOptions`, and writer behavior do not exist.

- [ ] **Step 3: Implement nodes writer**

Create `internal/config/nodes_writer.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pockyHM/conan/pkg/configschema"
	"gopkg.in/yaml.v3"
)

type NodeWriter struct {
	home string
}

type WriteNodeOptions struct {
	Update      bool
	RotateToken bool
}

type WriteNodeResult struct {
	Node    configschema.NodeConfig
	Updated bool
}

func NewNodeWriter(home string) *NodeWriter {
	if home == "" {
		home = DefaultHome()
	}
	return &NodeWriter{home: home}
}

func (w *NodeWriter) WriteNode(clusterName string, node configschema.NodeConfig, opts WriteNodeOptions) (WriteNodeResult, error) {
	if clusterName == "" {
		return WriteNodeResult{}, fmt.Errorf("cluster name is required")
	}
	if node.Name == "" {
		return WriteNodeResult{}, fmt.Errorf("node name is required")
	}
	if node.Host == "" {
		return WriteNodeResult{}, fmt.Errorf("node host is required")
	}
	clusterDir := filepath.Join(w.home, "clusters", clusterName)
	if _, err := os.Stat(filepath.Join(clusterDir, "cluster.yaml")); err != nil {
		if os.IsNotExist(err) {
			return WriteNodeResult{}, fmt.Errorf("cluster not found: %s", clusterName)
		}
		return WriteNodeResult{}, err
	}

	path := filepath.Join(clusterDir, "nodes.yaml")
	var list configschema.NodeList
	if err := readYAMLIfExists(path, &list); err != nil {
		return WriteNodeResult{}, err
	}

	for i := range list.Nodes {
		if list.Nodes[i].Name != node.Name {
			continue
		}
		if !opts.Update {
			return WriteNodeResult{}, fmt.Errorf("node already exists: %s", node.Name)
		}
		merged := node
		if merged.Agent == nil {
			merged.Agent = &configschema.NodeAgentOverride{}
		}
		if list.Nodes[i].Agent != nil && list.Nodes[i].Agent.Token != "" && !opts.RotateToken {
			merged.Agent.Token = list.Nodes[i].Agent.Token
		}
		list.Nodes[i] = merged
		if err := writeNodeList(path, list); err != nil {
			return WriteNodeResult{}, err
		}
		return WriteNodeResult{Node: merged, Updated: true}, nil
	}

	list.Nodes = append(list.Nodes, node)
	if err := writeNodeList(path, list); err != nil {
		return WriteNodeResult{}, err
	}
	return WriteNodeResult{Node: node}, nil
}

func writeNodeList(path string, list configschema.NodeList) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/config
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/nodes_writer.go internal/config/nodes_writer_test.go
git commit -m "feat: add cluster node config writer"
```

---

### Task 3: Encrypted credential store

**Files:**
- Create: `internal/credentials/store.go`
- Test: `internal/credentials/store_test.go`

- [ ] **Step 1: Write failing credential tests**

Create `internal/credentials/store_test.go`:

```go
package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStorePutGetCreatesEncryptedFiles(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)

	cred := Credential{Username: "deploy", Password: "secret-password"}
	if err := store.Put("ssh/prod/web-1", cred); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := store.Get("ssh/prod/web-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("credential missing")
	}
	if got != cred {
		t.Fatalf("credential = %+v, want %+v", got, cred)
	}

	data, err := os.ReadFile(filepath.Join(home, "credentials.enc"))
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if strings.Contains(string(data), "deploy") || strings.Contains(string(data), "secret-password") {
		t.Fatalf("encrypted file contains plaintext: %q", data)
	}
}

func TestStoreMissingCredential(t *testing.T) {
	got, ok, err := NewStore(t.TempDir()).Get("ssh/prod/web-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, got %+v", got)
	}
}

func TestStoreFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are platform-specific on windows")
	}
	home := t.TempDir()
	store := NewStore(home)
	if err := store.Put("ssh/prod/web-1", Credential{Username: "deploy", Password: "secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for _, name := range []string{"credentials.key", "credentials.enc"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode = %o, want 0600", name, info.Mode().Perm())
		}
	}
}

func TestStoreCorruptEncryptedFileReturnsError(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	if err := store.Put("ssh/prod/web-1", Credential{Username: "deploy", Password: "secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "credentials.enc"), []byte("not-ciphertext"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	_, _, err := store.Get("ssh/prod/web-1")
	if err == nil {
		t.Fatal("expected corrupt file error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/credentials
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement credential store**

Create `internal/credentials/store.go`:

```go
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Store struct {
	home string
}

func NewStore(home string) *Store {
	return &Store{home: home}
}

func (s *Store) Get(key string) (Credential, bool, error) {
	records, err := s.readRecords()
	if err != nil {
		return Credential{}, false, err
	}
	cred, ok := records[key]
	return cred, ok, nil
}

func (s *Store) Put(key string, cred Credential) error {
	records, err := s.readRecords()
	if err != nil {
		return err
	}
	records[key] = cred
	return s.writeRecords(records)
}

func (s *Store) readRecords() (map[string]Credential, error) {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.home, "credentials.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil
		}
		return nil, err
	}
	plain, err := decrypt(key, data)
	if err != nil {
		return nil, err
	}
	var records map[string]Credential
	if err := json.Unmarshal(plain, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = map[string]Credential{}
	}
	return records, nil
}

func (s *Store) writeRecords(records map[string]Credential) error {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(records)
	if err != nil {
		return err
	}
	sealed, err := encrypt(key, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.home, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.home, "credentials.enc"), sealed, 0600)
}

func (s *Store) loadOrCreateKey() ([]byte, error) {
	if err := os.MkdirAll(s.home, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.home, "credentials.key")
	key, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0600); err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid credential key length: %d", len(key))
	}
	return key, nil
}

func encrypt(key []byte, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func decrypt(key []byte, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted credentials file is too short")
	}
	nonce := sealed[:gcm.NonceSize()]
	ciphertext := sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/credentials
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/credentials/store.go internal/credentials/store_test.go
git commit -m "feat: add encrypted ssh credential store"
```

---

### Task 4: Deploy artifact rendering and token generation

**Files:**
- Create: `internal/deploy/artifacts.go`
- Test: `internal/deploy/artifacts_test.go`

- [ ] **Step 1: Write failing artifact tests**

Create `internal/deploy/artifacts_test.go`:

```go
package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestArchFromUname(t *testing.T) {
	cases := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
	for input, want := range cases {
		got, err := ArchFromUname(input)
		if err != nil {
			t.Fatalf("ArchFromUname(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ArchFromUname(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ArchFromUname("mips"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestResolveAgentBinary(t *testing.T) {
	home := t.TempDir()
	amd64 := filepath.Join(home, "amd64", "conan-agent")
	if err := os.MkdirAll(filepath.Dir(amd64), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(amd64, []byte("binary"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	path, err := ResolveAgentBinary(configschema.AgentDeployConfig{Binaries: configschema.AgentBinaryConfig{AMD64: amd64}}, "amd64", "")
	if err != nil {
		t.Fatalf("ResolveAgentBinary: %v", err)
	}
	if path != amd64 {
		t.Fatalf("path = %q, want %q", path, amd64)
	}
	if _, err := ResolveAgentBinary(configschema.AgentDeployConfig{}, "arm64", ""); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestRenderAgentConfig(t *testing.T) {
	config := RenderAgentConfig(9300, "node-token")
	for _, want := range []string{"listen: 0.0.0.0:9300", "token: node-token", "tls: false", "rate_limit: 10", "log_level: info"} {
		if !strings.Contains(config, want) {
			t.Fatalf("agent config missing %q:\n%s", want, config)
		}
	}
}

func TestRenderSystemdUnitUsesPaths(t *testing.T) {
	unit := RenderSystemdUnit("/opt/conan/conan-agent", "/etc/conan/custom.yaml")
	if !strings.Contains(unit, "ExecStart=/opt/conan/conan-agent run -c /etc/conan/custom.yaml") {
		t.Fatalf("unit =\n%s", unit)
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Fatalf("unit missing restart policy:\n%s", unit)
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken a: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken b: %v", err)
	}
	if len(a) < 40 {
		t.Fatalf("token too short: %q", a)
	}
	if a == b {
		t.Fatalf("tokens should be unique: %q", a)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/deploy
```

Expected: FAIL because package functions do not exist.

- [ ] **Step 3: Implement artifact helpers**

Create `internal/deploy/artifacts.go`:

```go
package deploy

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/pockyHM/conan/pkg/configschema"
)

func ArchFromUname(uname string) (string, error) {
	switch strings.TrimSpace(uname) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote architecture: %s", strings.TrimSpace(uname))
	}
}

func ResolveAgentBinary(cfg configschema.AgentDeployConfig, arch string, override string) (string, error) {
	path := override
	if path == "" {
		switch arch {
		case "amd64":
			path = cfg.Binaries.AMD64
		case "arm64":
			path = cfg.Binaries.ARM64
		default:
			return "", fmt.Errorf("unsupported agent architecture: %s", arch)
		}
	}
	if path == "" {
		return "", fmt.Errorf("agent binary path is empty for architecture %s", arch)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("agent binary not found at %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("agent binary path is a directory: %s", path)
	}
	return path, nil
}

func RenderAgentConfig(port int, token string) string {
	return fmt.Sprintf("listen: 0.0.0.0:%d\ntoken: %s\ntls: false\nrate_limit: 10\nlog_level: info\n", port, token)
}

func RenderSystemdUnit(binaryPath string, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Conan Agent
After=network-online.target

[Service]
ExecStart=%s run -c %s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, binaryPath, configPath)
}

func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/deploy
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/deploy/artifacts.go internal/deploy/artifacts_test.go
git commit -m "feat: add agent deploy artifact rendering"
```

---

### Task 5: SSH deployer with fakeable transport

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/deploy/deployer.go`
- Test: `internal/deploy/deployer_test.go`

- [ ] **Step 1: Add dependencies**

Run:

```bash
go get golang.org/x/crypto/ssh golang.org/x/crypto/ssh/knownhosts github.com/pkg/sftp
```

Expected: `go.mod` includes `golang.org/x/crypto` and `github.com/pkg/sftp`.

- [ ] **Step 2: Write failing deployer tests using fakes**

Create `internal/deploy/deployer_test.go`:

```go
package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

type fakeRemote struct {
	outputs  map[string]string
	commands []string
	uploads  map[string]string
}

func (f *fakeRemote) Run(ctx context.Context, command string, stdin string) (string, error) {
	f.commands = append(f.commands, command)
	return f.outputs[command], nil
}

func (f *fakeRemote) Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error {
	if f.uploads == nil {
		f.uploads = map[string]string{}
	}
	f.uploads[remotePath] = string(contents)
	return nil
}

func TestDeployerUploadsArtifactsAndRunsSystemdCommands(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "conan-agent")
	if err := os.WriteFile(binary, []byte("agent-binary"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	remote := &fakeRemote{outputs: map[string]string{"uname -m": "x86_64\n"}}
	deployer := NewDeployer(func(Target) (Remote, error) { return remote, nil })

	err := deployer.Deploy(context.Background(), Target{
		Host: "10.0.0.11", SSHPort: 22, Username: "deploy", Password: "secret", AgentPort: 9200, Token: "node-token",
		Config: configschema.AgentDeployConfig{Binaries: configschema.AgentBinaryConfig{AMD64: binary}, RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(remote.uploads) != 3 {
		t.Fatalf("uploads = %#v", remote.uploads)
	}
	joined := strings.Join(remote.commands, "\n")
	for _, want := range []string{"uname -m", "sudo -S install -m 0755", "sudo -S systemctl daemon-reload", "sudo -S systemctl enable --now conan-agent", "sudo -S systemctl restart conan-agent"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "secret") || strings.Contains(joined, "node-token") {
		t.Fatalf("commands leaked secret:\n%s", joined)
	}
}

func TestDeployerUnsupportedArchFailsBeforeUpload(t *testing.T) {
	remote := &fakeRemote{outputs: map[string]string{"uname -m": "mips\n"}}
	deployer := NewDeployer(func(Target) (Remote, error) { return remote, nil })
	err := deployer.Deploy(context.Background(), Target{Host: "10.0.0.11", Username: "deploy", Password: "secret", AgentPort: 9200})
	if err == nil || !strings.Contains(err.Error(), "unsupported remote architecture") {
		t.Fatalf("err = %v", err)
	}
	if len(remote.uploads) != 0 {
		t.Fatalf("uploads happened before arch failure: %#v", remote.uploads)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./internal/deploy
```

Expected: FAIL because deployer types do not exist.

- [ ] **Step 4: Implement deployer and native SSH/SFTP adapter**

Create `internal/deploy/deployer.go`:

```go
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/pockyHM/conan/pkg/configschema"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Target struct {
	Host             string
	SSHPort          int
	Username         string
	Password         string
	AgentPort        int
	Token            string
	AgentBinOverride string
	Config           configschema.AgentDeployConfig
	KnownHostsPath   string
}

type Remote interface {
	Run(ctx context.Context, command string, stdin string) (string, error)
	Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error
}

type Connector func(Target) (Remote, error)

type Deployer struct {
	connect Connector
}

func NewDeployer(connect Connector) *Deployer {
	return &Deployer{connect: connect}
}

func NewNativeDeployer() *Deployer {
	return NewDeployer(connectNative)
}

func (d *Deployer) Deploy(ctx context.Context, target Target) error {
	if target.SSHPort == 0 {
		target.SSHPort = 22
	}
	remote, err := d.connect(target)
	if err != nil {
		return err
	}
	uname, err := remote.Run(ctx, "uname -m", "")
	if err != nil {
		return err
	}
	arch, err := ArchFromUname(uname)
	if err != nil {
		return err
	}
	binaryPath, err := ResolveAgentBinary(target.Config, arch, target.AgentBinOverride)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	remoteBinaryTmp := "/tmp/conan-agent." + suffix
	remoteConfigTmp := "/tmp/conan-agent-config." + suffix
	remoteUnitTmp := "/tmp/conan-agent.service." + suffix
	if err := remote.Upload(ctx, remoteBinaryTmp, binary, 0755); err != nil {
		return err
	}
	if err := remote.Upload(ctx, remoteConfigTmp, []byte(RenderAgentConfig(target.AgentPort, target.Token)), 0600); err != nil {
		return err
	}
	if err := remote.Upload(ctx, remoteUnitTmp, []byte(RenderSystemdUnit(target.Config.RemoteBinaryPath, target.Config.RemoteConfigPath)), 0644); err != nil {
		return err
	}

	commands := []string{
		fmt.Sprintf("sudo -S install -m 0755 %s %s", remoteBinaryTmp, shellQuote(target.Config.RemoteBinaryPath)),
		fmt.Sprintf("sudo -S mkdir -p %s", shellQuote(filepath.Dir(target.Config.RemoteConfigPath))),
		fmt.Sprintf("sudo -S install -m 0600 %s %s", remoteConfigTmp, shellQuote(target.Config.RemoteConfigPath)),
		fmt.Sprintf("sudo -S install -m 0644 %s %s", remoteUnitTmp, shellQuote(target.Config.SystemdUnitPath)),
		"sudo -S systemctl daemon-reload",
		"sudo -S systemctl enable --now conan-agent",
		"sudo -S systemctl restart conan-agent",
	}
	stdin := target.Password + "\n"
	for _, command := range commands {
		if _, err := remote.Run(ctx, command, stdin); err != nil {
			return err
		}
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type sshRemote struct {
	client *ssh.Client
}

func connectNative(target Target) (Remote, error) {
	callback, err := knownhosts.New(target.KnownHostsPath)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(target.Password)},
		HostKeyCallback: callback,
		Timeout:         15 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", target.Host, target.SSHPort), config)
	if err != nil {
		return nil, err
	}
	return &sshRemote{client: client}, nil
}

func (r *sshRemote) Run(ctx context.Context, command string, stdin string) (string, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("remote command failed: %s: %s", command, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
}

func (r *sshRemote) Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error {
	client, err := sftp.NewClient(r.client)
	if err != nil {
		return err
	}
	defer client.Close()
	file, err := client.OpenFile(remotePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return client.Chmod(remotePath, perm)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/deploy
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/deploy/deployer.go internal/deploy/deployer_test.go
git commit -m "feat: add ssh agent deployer"
```

---

### Task 6: Node-add orchestration service

**Files:**
- Create: `internal/nodeadd/service.go`
- Test: `internal/nodeadd/service_test.go`

- [ ] **Step 1: Write failing service tests**

Create `internal/nodeadd/service_test.go`:

```go
package nodeadd

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

type fakeCredentialStore struct {
	values map[string]credentials.Credential
}

func (f *fakeCredentialStore) Get(key string) (credentials.Credential, bool, error) {
	cred, ok := f.values[key]
	return cred, ok, nil
}

func (f *fakeCredentialStore) Put(key string, cred credentials.Credential) error {
	if f.values == nil {
		f.values = map[string]credentials.Credential{}
	}
	f.values[key] = cred
	return nil
}

type fakePrompter struct {
	username string
	password string
	ip       string
}

func (f fakePrompter) PromptUsername(defaultValue string) (string, error) { return f.username, nil }
func (f fakePrompter) PromptPassword() (string, error) { return f.password, nil }
func (f fakePrompter) PromptIP(hostname string) (string, error) { return f.ip, nil }

type fakeResolver struct{ ips []net.IP }
func (f fakeResolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) { return f.ips, nil }

type fakeNodeWriter struct {
	written configschema.NodeConfig
	opts    WriteOptions
}

func (f *fakeNodeWriter) Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error) {
	f.written = node
	f.opts = opts
	return node, false, nil
}

type fakeDeployer struct{ target deploy.Target }
func (f *fakeDeployer) Deploy(ctx context.Context, target deploy.Target) error { f.target = target; return nil }

type fakeHealthChecker struct{ called bool }
func (f *fakeHealthChecker) Check(ctx context.Context, host string, port int, tls bool, token string) error { f.called = true; return nil }

func TestServiceAddsNodePromptsIPAndDeploys(t *testing.T) {
	creds := &fakeCredentialStore{values: map[string]credentials.Credential{}}
	writer := &fakeNodeWriter{}
	deployer := &fakeDeployer{}
	health := &fakeHealthChecker{}
	svc := Service{Credentials: creds, Prompter: fakePrompter{username: "deploy", password: "secret", ip: "10.0.0.11"}, Resolver: fakeResolver{}, Writer: writer, Deployer: deployer, Health: health}

	_, err := svc.Add(context.Background(), Request{Home: "/tmp/conan", ClusterName: "prod", Input: "web-1", AgentPort: 9200, SSHPort: 22, DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if writer.written.Name != "web-1" || writer.written.Host != "10.0.0.11" {
		t.Fatalf("written node = %+v", writer.written)
	}
	if writer.written.Agent.User != "deploy" || writer.written.Agent.Port != 9200 || writer.written.Agent.Token == "" {
		t.Fatalf("written agent = %+v", writer.written.Agent)
	}
	if deployer.target.Username != "deploy" || deployer.target.Password != "secret" || deployer.target.Token != writer.written.Agent.Token {
		t.Fatalf("deploy target = %+v", deployer.target)
	}
	if !health.called {
		t.Fatal("health check was not called")
	}
	if _, ok := creds.values["ssh/prod/web-1"]; !ok {
		t.Fatal("credentials were not saved")
	}
}

func TestServiceNoDeploySkipsDeployerAndHealth(t *testing.T) {
	writer := &fakeNodeWriter{}
	deployer := &fakeDeployer{}
	health := &fakeHealthChecker{}
	svc := Service{Credentials: &fakeCredentialStore{}, Prompter: fakePrompter{}, Resolver: fakeResolver{ips: []net.IP{net.ParseIP("10.0.0.11")}}, Writer: writer, Deployer: deployer, Health: health}

	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9200, NoDeploy: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if deployer.target.Host != "" {
		t.Fatalf("deployer was called: %+v", deployer.target)
	}
	if health.called {
		t.Fatal("health check was called")
	}
}

func TestServiceUsesSavedCredentials(t *testing.T) {
	creds := &fakeCredentialStore{values: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "saved", Password: "saved-pass"}}}
	deployer := &fakeDeployer{}
	svc := Service{Credentials: creds, Prompter: fakePrompter{username: "prompt", password: "prompt-pass"}, Resolver: fakeResolver{ips: []net.IP{net.ParseIP("10.0.0.11")}}, Writer: &fakeNodeWriter{}, Deployer: deployer, Health: &fakeHealthChecker{}}

	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9200, SSHPort: 22, DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if deployer.target.Username != "saved" || deployer.target.Password != "saved-pass" {
		t.Fatalf("target credentials = %s/%s", deployer.target.Username, deployer.target.Password)
	}
}

func TestServiceRequiresIPWhenHostnameUnresolved(t *testing.T) {
	svc := Service{Credentials: &fakeCredentialStore{}, Prompter: fakePrompter{ip: ""}, Resolver: fakeResolver{}, Writer: &fakeNodeWriter{}, Deployer: &fakeDeployer{}, Health: &fakeHealthChecker{}}
	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9200, SSHPort: 22})
	if err == nil || !strings.Contains(err.Error(), "ip address is required") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/nodeadd
```

Expected: FAIL because package does not exist.

- [ ] **Step 3: Implement service**

Create `internal/nodeadd/service.go`:

```go
package nodeadd

import (
	"context"
	"fmt"
	"net"
	"strings"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Request struct {
	Home             string
	ClusterName      string
	Input            string
	Name             string
	Username         string
	Password         string
	SSHPort          int
	AgentPort        int
	NoDeploy         bool
	Update           bool
	RotateToken      bool
	AgentBinOverride string
	DeployConfig     configschema.AgentDeployConfig
	TLS              bool
}

type Result struct {
	Node     configschema.NodeConfig
	Deployed bool
}

type CredentialStore interface {
	Get(key string) (credentials.Credential, bool, error)
	Put(key string, cred credentials.Credential) error
}

type Prompter interface {
	PromptUsername(defaultValue string) (string, error)
	PromptPassword() (string, error)
	PromptIP(hostname string) (string, error)
}

type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]net.IP, error)
}

type NodeWriter interface {
	Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error)
}

type WriteOptions struct {
	Update      bool
	RotateToken bool
}

type Deployer interface {
	Deploy(ctx context.Context, target deploy.Target) error
}

type HealthChecker interface {
	Check(ctx context.Context, host string, port int, tls bool, token string) error
}

type Service struct {
	Credentials CredentialStore
	Prompter    Prompter
	Resolver    Resolver
	Writer      NodeWriter
	Deployer    Deployer
	Health      HealthChecker
}

func (s Service) Add(ctx context.Context, req Request) (Result, error) {
	if req.ClusterName == "" {
		return Result{}, fmt.Errorf("cluster name is required")
	}
	if req.Input == "" {
		return Result{}, fmt.Errorf("node host or ip is required")
	}
	if req.AgentPort == 0 {
		req.AgentPort = 9200
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}

	name := req.Name
	if name == "" {
		name = req.Input
	}
	host, err := s.resolveHost(ctx, req.Input)
	if err != nil {
		return Result{}, err
	}

	username := req.Username
	password := req.Password
	credKey := fmt.Sprintf("ssh/%s/%s", req.ClusterName, name)
	if !req.NoDeploy && username == "" && password == "" && s.Credentials != nil {
		if saved, ok, err := s.Credentials.Get(credKey); err != nil {
			return Result{}, err
		} else if ok {
			username = saved.Username
			password = saved.Password
		}
	}
	if !req.NoDeploy {
		if username == "" {
			username, err = s.Prompter.PromptUsername("")
			if err != nil {
				return Result{}, err
			}
		}
		if password == "" {
			password, err = s.Prompter.PromptPassword()
			if err != nil {
				return Result{}, err
			}
		}
	}

	token, err := deploy.GenerateToken()
	if err != nil {
		return Result{}, err
	}
	node := configschema.NodeConfig{Name: name, Host: host, Agent: &configschema.NodeAgentOverride{User: username, Port: req.AgentPort, Token: token}}
	written, _, err := s.Writer.Write(req.ClusterName, node, WriteOptions{Update: req.Update, RotateToken: req.RotateToken})
	if err != nil {
		return Result{}, err
	}
	if written.Agent != nil && written.Agent.Token != "" {
		token = written.Agent.Token
	}

	if req.NoDeploy {
		return Result{Node: written}, nil
	}
	if err := s.Deployer.Deploy(ctx, deploy.Target{Host: host, SSHPort: req.SSHPort, Username: username, Password: password, AgentPort: req.AgentPort, Token: token, AgentBinOverride: req.AgentBinOverride, Config: req.DeployConfig}); err != nil {
		return Result{}, err
	}
	if s.Credentials != nil {
		if err := s.Credentials.Put(credKey, credentials.Credential{Username: username, Password: password}); err != nil {
			return Result{}, err
		}
	}
	if s.Health != nil {
		if err := s.Health.Check(ctx, host, req.AgentPort, req.TLS, token); err != nil {
			return Result{}, err
		}
	}
	return Result{Node: written, Deployed: true}, nil
}

func (s Service) resolveHost(ctx context.Context, input string) (string, error) {
	if ip := net.ParseIP(input); ip != nil {
		return input, nil
	}
	ips, err := s.Resolver.LookupHost(ctx, input)
	if err == nil && len(ips) > 0 {
		return input, nil
	}
	ip, err := s.Prompter.PromptIP(input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ip) == "" {
		return "", fmt.Errorf("ip address is required for unresolved hostname %s", input)
	}
	return strings.TrimSpace(ip), nil
}

type NetResolver struct{}
func (NetResolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) { return net.DefaultResolver.LookupIP(ctx, "ip", host) }

type ConfigNodeWriter struct{ Home string }
func (w ConfigNodeWriter) Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error) {
	result, err := cfgloader.NewNodeWriter(w.Home).WriteNode(cluster, node, cfgloader.WriteNodeOptions{Update: opts.Update, RotateToken: opts.RotateToken})
	return result.Node, result.Updated, err
}

type MCPHealthChecker struct{}
func (MCPHealthChecker) Check(ctx context.Context, host string, port int, tls bool, token string) error {
	return mcp.NewClient(mcp.Config{BaseURL: mcp.URL(host, port, tls), Token: token}).Ping(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/nodeadd
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/nodeadd/service.go internal/nodeadd/service_test.go
git commit -m "feat: add node add orchestration service"
```

---

### Task 7: CLI command wiring and prompts

**Files:**
- Modify: `cmd/conan/main.go`
- Modify: `cmd/conan/main_test.go`

- [ ] **Step 1: Write failing CLI tests**

Add these tests to `cmd/conan/main_test.go`:

```go
func TestNodeAddCommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("node", "add", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "add <hostname-or-ip>") {
		t.Fatalf("help output = %q", stdout)
	}
	if !strings.Contains(stdout, "--no-deploy") || !strings.Contains(stdout, "--rotate-token") {
		t.Fatalf("help output missing node add flags: %q", stdout)
	}
}

func TestNodeAddNoDeployWritesConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "clusters", "prod"), 0755); err != nil {
		t.Fatalf("mkdir cluster: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_cluster: prod\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "clusters", "prod", "cluster.yaml"), []byte("name: prod\n"), 0644); err != nil {
		t.Fatalf("write cluster: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "node", "add", "127.0.0.1", "--no-deploy", "--port", "9300")
	if err != nil {
		t.Fatalf("node add: %v", err)
	}
	if !strings.Contains(stdout, "node added: 127.0.0.1") {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"name: 127.0.0.1", "host: 127.0.0.1", "port: 9300", "token:"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("nodes.yaml missing %q:\n%s", want, contents)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./cmd/conan
```

Expected: FAIL because `node add` is not registered.

- [ ] **Step 3: Add CLI prompts and command wiring**

In `cmd/conan/main.go`, add imports:

```go
"bufio"
"net"

"github.com/pockyHM/conan/internal/credentials"
"github.com/pockyHM/conan/internal/deploy"
"github.com/pockyHM/conan/internal/nodeadd"
"golang.org/x/term"
```

Add prompt type near `runTeaProgram`:

```go
type cliPrompter struct {
	in  io.Reader
	out io.Writer
}

func (p cliPrompter) PromptUsername(defaultValue string) (string, error) {
	fmt.Fprint(p.out, "SSH username: ")
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (p cliPrompter) PromptPassword() (string, error) {
	fmt.Fprint(p.out, "SSH password: ")
	if file, ok := p.in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(p.out)
		return string(data), err
	}
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (p cliPrompter) PromptIP(hostname string) (string, error) {
	fmt.Fprintf(p.out, "Hostname %s could not be resolved. Enter IP address: ", hostname)
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
```

Inside `newRootCommand`, before `tuiCmd`, add:

```go
nodeCmd := &cobra.Command{Use: "node", Short: "Node management commands"}
var nodeAddUser string
var nodeAddPassword string
var nodeAddSSHPort int
var nodeAddAgentPort int
var nodeAddName string
var nodeAddAgentBin string
var nodeAddNoDeploy bool
var nodeAddUpdate bool
var nodeAddRotateToken bool
nodeAddCmd := &cobra.Command{
	Use:   "add <hostname-or-ip>",
	Short: "Add a node and deploy conan-agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		loader := cfgloader.NewLoader(home)
		global, err := loader.LoadGlobal()
		if err != nil {
			return err
		}
		cluster, err := loader.LoadCluster(clusterName)
		if err != nil {
			return err
		}
		selectedCluster := clusterName
		if selectedCluster == "" {
			selectedCluster = cluster.Cluster.Name
		}
		sshPort := nodeAddSSHPort
		if sshPort == 0 {
			sshPort = cluster.Cluster.NodeDefaults.SSHPort
		}
		if sshPort == 0 {
			sshPort = 22
		}
		agentPort := nodeAddAgentPort
		if agentPort == 0 {
			agentPort = 9200
		}
		service := nodeadd.Service{
			Credentials: credentials.NewStore(loader.Home()),
			Prompter:    cliPrompter{in: cmd.InOrStdin(), out: cmd.OutOrStdout()},
			Resolver:    nodeadd.NetResolver{},
			Writer:      nodeadd.ConfigNodeWriter{Home: loader.Home()},
			Deployer:    deploy.NewNativeDeployer(),
			Health:      nodeadd.MCPHealthChecker{},
		}
		result, err := service.Add(cmd.Context(), nodeadd.Request{
			Home:             loader.Home(),
			ClusterName:      selectedCluster,
			Input:            args[0],
			Name:             nodeAddName,
			Username:         nodeAddUser,
			Password:         nodeAddPassword,
			SSHPort:          sshPort,
			AgentPort:        agentPort,
			NoDeploy:         nodeAddNoDeploy,
			Update:           nodeAddUpdate,
			RotateToken:      nodeAddRotateToken,
			AgentBinOverride: nodeAddAgentBin,
			DeployConfig:     global.AgentDeploy,
			TLS:              cluster.Cluster.Agent.TLS,
		})
		if err != nil {
			return err
		}
		if result.Deployed {
			fmt.Fprintf(cmd.OutOrStdout(), "node added and deployed: %s\n", result.Node.Name)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "node added: %s\n", result.Node.Name)
		}
		return nil
	},
}
nodeAddCmd.Flags().StringVarP(&nodeAddUser, "user", "u", "", "SSH username")
nodeAddCmd.Flags().StringVarP(&nodeAddPassword, "password", "p", "", "SSH password")
nodeAddCmd.Flags().IntVar(&nodeAddSSHPort, "ssh-port", 0, "SSH port")
nodeAddCmd.Flags().IntVar(&nodeAddAgentPort, "port", 9200, "Agent listen port")
nodeAddCmd.Flags().StringVar(&nodeAddName, "name", "", "Node name override")
nodeAddCmd.Flags().StringVar(&nodeAddAgentBin, "agent-bin", "", "Local conan-agent binary path override")
nodeAddCmd.Flags().BoolVar(&nodeAddNoDeploy, "no-deploy", false, "Only write node configuration")
nodeAddCmd.Flags().BoolVar(&nodeAddUpdate, "update", false, "Update an existing node")
nodeAddCmd.Flags().BoolVar(&nodeAddRotateToken, "rotate-token", false, "Rotate the node agent token while updating")
nodeCmd.AddCommand(nodeAddCmd)
```

Update the command registration line:

```go
rootCmd.AddCommand(configCmd, clustersCmd, nodesCmd, nodeCmd, pingCmd, toolsCmd, tuiCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./cmd/conan
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/conan/main.go cmd/conan/main_test.go
git commit -m "feat: add node add command"
```

---

### Task 8: Saved credential retry on auth failure and known-hosts path

**Files:**
- Modify: `internal/deploy/deployer.go`
- Modify: `internal/nodeadd/service.go`
- Modify: `internal/nodeadd/service_test.go`
- Modify: `cmd/conan/main.go`

- [ ] **Step 1: Write failing service test for auth retry**

Add to `internal/nodeadd/service_test.go`:

```go
type failingOnceDeployer struct {
	calls int
}
func (d *failingOnceDeployer) Deploy(ctx context.Context, target deploy.Target) error {
	d.calls++
	if d.calls == 1 {
		return ErrAuthFailed
	}
	return nil
}

func TestServicePromptsAgainWhenSavedCredentialsFail(t *testing.T) {
	creds := &fakeCredentialStore{values: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "bad", Password: "bad-pass"}}}
	deployer := &failingOnceDeployer{}
	svc := Service{Credentials: creds, Prompter: fakePrompter{username: "good", password: "good-pass"}, Resolver: fakeResolver{ips: []net.IP{net.ParseIP("10.0.0.11")}}, Writer: &fakeNodeWriter{}, Deployer: deployer, Health: &fakeHealthChecker{}}

	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9200, SSHPort: 22, DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if deployer.calls != 2 {
		t.Fatalf("deploy calls = %d, want 2", deployer.calls)
	}
	got := creds.values["ssh/prod/web-1"]
	if got.Username != "good" || got.Password != "good-pass" {
		t.Fatalf("saved credential = %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/nodeadd
```

Expected: FAIL because `ErrAuthFailed` and retry logic do not exist.

- [ ] **Step 3: Add auth error classification**

In `internal/nodeadd/service.go`, add:

```go
var ErrAuthFailed = fmt.Errorf("ssh authentication failed")
```

In `internal/deploy/deployer.go`, import `errors` and wrap native auth errors in `connectNative`:

```go
var ErrAuthFailed = errors.New("ssh authentication failed")
```

In `connectNative`, after `ssh.Dial` error:

```go
if err != nil {
	if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") {
		return nil, ErrAuthFailed
	}
	return nil, err
}
```

In `internal/nodeadd/service.go`, import `errors` and map deploy auth errors by checking both package errors:

```go
func isAuthFailed(err error) bool {
	return errors.Is(err, ErrAuthFailed) || errors.Is(err, deploy.ErrAuthFailed)
}
```

- [ ] **Step 4: Retry once with prompted credentials**

Replace the deploy call in `Service.Add` with:

```go	target := deploy.Target{Host: host, SSHPort: req.SSHPort, Username: username, Password: password, AgentPort: req.AgentPort, Token: token, AgentBinOverride: req.AgentBinOverride, Config: req.DeployConfig, KnownHostsPath: req.KnownHostsPath}
	if err := s.Deployer.Deploy(ctx, target); err != nil {
		if !isAuthFailed(err) {
			return Result{}, err
		}
		username, err = s.Prompter.PromptUsername("")
		if err != nil {
			return Result{}, err
		}
		password, err = s.Prompter.PromptPassword()
		if err != nil {
			return Result{}, err
		}
		target.Username = username
		target.Password = password
		if err := s.Deployer.Deploy(ctx, target); err != nil {
			return Result{}, err
		}
	}
```

Add `KnownHostsPath string` to `nodeadd.Request`.

- [ ] **Step 5: Pass known-hosts path from CLI**

In `cmd/conan/main.go`, set request field:

```go
KnownHostsPath: filepath.Join(loader.Home(), "known_hosts"),
```

- [ ] **Step 6: Run tests to verify they pass**

Run:

```bash
go test ./internal/nodeadd ./internal/deploy ./cmd/conan
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/deploy/deployer.go internal/nodeadd/service.go internal/nodeadd/service_test.go cmd/conan/main.go
git commit -m "feat: retry node deploy with refreshed credentials"
```

---

### Task 9: Full verification and documentation alignment

**Files:**
- Modify: `CLAUDE.md` only if implementation status is updated by the user or project convention requires it.
- No production files should need changes unless tests reveal gaps.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run build**

Run:

```bash
make build
```

Expected: both binaries build successfully to `./bin/`.

- [ ] **Step 3: Manually inspect command help**

Run:

```bash
./bin/conan node add --help
```

Expected output includes:

```text
add <hostname-or-ip>
--user
--password
--ssh-port
--port
--name
--agent-bin
--no-deploy
--update
--rotate-token
```

- [ ] **Step 4: Smoke test config-only add**

Run with a temporary Conan home:

```bash
TMP_HOME=$(mktemp -d)
mkdir -p "$TMP_HOME/clusters/prod"
printf 'default_cluster: prod\n' > "$TMP_HOME/config.yaml"
printf 'name: prod\n' > "$TMP_HOME/clusters/prod/cluster.yaml"
./bin/conan --home "$TMP_HOME" node add 127.0.0.1 --no-deploy --port 9300
```

Expected output:

```text
node added: 127.0.0.1
```

Then inspect `$TMP_HOME/clusters/prod/nodes.yaml`; it must include `name`, `host`, `agent.port`, and `agent.token`.

- [ ] **Step 5: Commit final verification fixes if any**

If verification required code fixes, commit them:

```bash
git add <fixed-files>
git commit -m "fix: stabilize node add deployment"
```

If no fixes were needed, do not create an empty commit.

---

## Self-Review

- Spec coverage: command UX, config schema, encrypted credentials, per-node token, Go-native SSH/SFTP deploy, systemd install, health check, error behavior, and tests are covered by Tasks 1-9.
- Placeholder scan: the plan contains no `TBD`, no deferred implementation instructions, and each task includes exact files, code, commands, and expected outcomes.
- Type consistency: `AgentDeployConfig`, `NodeAgentOverride.Token`, `WriteNodeOptions`, `deploy.Target`, `nodeadd.Request`, and credential types are introduced before later tasks use them.
