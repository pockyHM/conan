# Model Management CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `conan model add/list/use/remove` so users can configure Anthropic and OpenAI-compatible models without manually editing `~/.conan/config.yaml`.

**Architecture:** Reuse the existing `configschema.GlobalConfig` shape and add a small persistence API beside `internal/config.Loader`. Keep provider presets, OpenAI-compatible model discovery, and interactive prompting in focused CLI helper files under `cmd/conan`, then wire those helpers into the Cobra root command.

**Tech Stack:** Go, Cobra, `gopkg.in/yaml.v3`, `net/http`, existing `internal/config`, existing `pkg/configschema`.

---

## File Structure

- Modify `internal/config/loader.go` — add `ConfigPath`, `SaveGlobal`, and shared model normalization helpers.
- Modify `internal/config/loader_test.go` — add persistence tests for permissions, preservation, endpoint normalization, and atomic failure behavior where practical.
- Create `cmd/conan/model_presets.go` — provider preset table and lookup helpers.
- Create `cmd/conan/model_lister.go` — OpenAI-compatible `GET /models` discovery helper.
- Create `cmd/conan/model_prompt.go` — small stdin/stdout prompt abstraction used by `model add`.
- Create `cmd/conan/model_commands.go` — Cobra command construction and model command handlers.
- Modify `cmd/conan/main.go` — attach `model` command group to the root command.
- Modify `cmd/conan/main_test.go` — add command integration tests for list/use/remove/add.

## Important existing types

`pkg/configschema/config.go` already defines the storage shape:

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

type ModelConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"`
}
```

`internal/config/loader.go` already normalizes loaded model config by expanding API keys and trimming endpoint slashes. The writer added in this plan must trim endpoints before saving but must not expand API key values before writing.

---

### Task 1: Global Config Persistence

**Files:**
- Modify: `internal/config/loader.go`
- Modify: `internal/config/loader_test.go`

- [ ] **Step 1: Add failing config save tests**

Append these tests to `internal/config/loader_test.go`:

```go
func TestSaveGlobalCreatesConfigWithPrivatePermissions(t *testing.T) {
	home := t.TempDir()
	loader := NewLoader(home)
	cfg := &configschema.GlobalConfig{
		DefaultModel: "qwen-prod",
		Models: []configschema.ModelConfig{{
			Name:     "qwen-prod",
			Type:     "openai",
			Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1/",
			Model:    "qwen-max",
			APIKey:   "sk-test",
		}},
	}

	if err := loader.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	path := filepath.Join(home, "config.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 0600", got)
	}

	loaded, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "qwen-prod" {
		t.Fatalf("DefaultModel = %q", loaded.DefaultModel)
	}
	if len(loaded.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(loaded.Models))
	}
	if loaded.Models[0].Endpoint != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("endpoint = %q", loaded.Models[0].Endpoint)
	}
	if loaded.Models[0].APIKey != "sk-test" {
		t.Fatalf("api key = %q", loaded.Models[0].APIKey)
	}
}

func TestSaveGlobalPreservesUnrelatedFields(t *testing.T) {
	home := t.TempDir()
	loader := NewLoader(home)
	cfg := &configschema.GlobalConfig{
		DefaultCluster: "prod",
		Security: configschema.SecurityConfig{
			RiskAssessmentModel: "claude-risk",
			CommandWhitelist:    []string{"ls", "df"},
		},
		Memory: configschema.MemoryConfig{
			RulesTokenBudget:     123,
			KnowledgeTokenBudget: 456,
		},
		Logging: configschema.LoggingConfig{
			Level: "debug",
			File:  "/tmp/conan.log",
			Audit: true,
		},
		AgentDeploy: configschema.AgentDeployConfig{
			RemoteBinaryPath: "/opt/conan-agent",
			RemoteConfigPath: "/etc/conan-agent.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		Models: []configschema.ModelConfig{{Name: "kimi", Type: "openai", Endpoint: "https://api.moonshot.cn/v1", Model: "kimi-k2", APIKey: "moon"}},
	}

	if err := loader.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	loaded, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	if loaded.DefaultCluster != "prod" {
		t.Fatalf("DefaultCluster = %q", loaded.DefaultCluster)
	}
	if loaded.Security.RiskAssessmentModel != "claude-risk" || strings.Join(loaded.Security.CommandWhitelist, ",") != "ls,df" {
		t.Fatalf("security = %#v", loaded.Security)
	}
	if loaded.Memory.RulesTokenBudget != 123 || loaded.Memory.KnowledgeTokenBudget != 456 {
		t.Fatalf("memory = %#v", loaded.Memory)
	}
	if loaded.Logging.Level != "debug" || loaded.Logging.File != "/tmp/conan.log" || !loaded.Logging.Audit {
		t.Fatalf("logging = %#v", loaded.Logging)
	}
	if loaded.AgentDeploy.RemoteBinaryPath != "/opt/conan-agent" {
		t.Fatalf("agent deploy = %#v", loaded.AgentDeploy)
	}
}

func TestSaveGlobalDoesNotExpandAPIKeyBeforeWriting(t *testing.T) {
	home := t.TempDir()
	loader := NewLoader(home)
	cfg := &configschema.GlobalConfig{
		Models: []configschema.ModelConfig{{
			Name:     "env-model",
			Type:     "openai",
			Endpoint: "https://api.openai.com/v1",
			Model:    "gpt-4.1",
			APIKey:   "${OPENAI_API_KEY}",
		}},
	}

	if err := loader.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "${OPENAI_API_KEY}") {
		t.Fatalf("saved config = %s, want unexpanded api key", data)
	}
}
```

Make sure `internal/config/loader_test.go` imports `strings` if it does not already.

- [ ] **Step 2: Run config tests and verify failure**

Run:

```bash
go test ./internal/config -run 'TestSaveGlobal' -count=1
```

Expected: FAIL because `Loader.SaveGlobal` does not exist.

- [ ] **Step 3: Implement config persistence**

In `internal/config/loader.go`, add these methods and helpers near `Home()` / `LoadGlobal()`:

```go
func (l *Loader) ConfigPath() string {
	return filepath.Join(l.home, "config.yaml")
}

func (l *Loader) SaveGlobal(cfg *configschema.GlobalConfig) error {
	if cfg == nil {
		return fmt.Errorf("global config is nil")
	}
	normalizeModelsForSave(cfg.Models)
	path := l.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func normalizeModelsForSave(models []configschema.ModelConfig) {
	for i := range models {
		models[i].Endpoint = strings.TrimRight(models[i].Endpoint, "/")
	}
}
```

Then update `LoadGlobal()` to use `ConfigPath()` and a shared load normalizer:

```go
path := l.ConfigPath()
```

and replace the inline loop:

```go
for i := range cfg.Models {
	cfg.Models[i].APIKey = configschema.ExpandEnv(cfg.Models[i].APIKey)
	cfg.Models[i].Endpoint = strings.TrimRight(cfg.Models[i].Endpoint, "/")
}
```

with:

```go
normalizeModelsForLoad(cfg.Models)
```

Add:

```go
func normalizeModelsForLoad(models []configschema.ModelConfig) {
	for i := range models {
		models[i].APIKey = configschema.ExpandEnv(models[i].APIKey)
		models[i].Endpoint = strings.TrimRight(models[i].Endpoint, "/")
	}
}
```

- [ ] **Step 4: Run config tests and verify pass**

Run:

```bash
go test ./internal/config -run 'TestSaveGlobal|TestLoadGlobal' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

Do not commit unless the human explicitly asks for commits. If commits are authorized later, stage only:

```bash
git add internal/config/loader.go internal/config/loader_test.go
git commit -m "feat: add global config persistence"
```

---

### Task 2: Provider Presets and Model Discovery

**Files:**
- Create: `cmd/conan/model_presets.go`
- Create: `cmd/conan/model_lister.go`
- Modify: `cmd/conan/main_test.go`

- [ ] **Step 1: Add failing tests for presets and OpenAI-compatible model listing**

Append these tests to `cmd/conan/main_test.go`:

```go
func TestModelPresetLookup(t *testing.T) {
	preset, ok := modelPresetByID("qwen")
	if !ok {
		t.Fatal("qwen preset not found")
	}
	if preset.Type != "openai" {
		t.Fatalf("qwen type = %q, want openai", preset.Type)
	}
	if preset.Endpoint == "" {
		t.Fatal("qwen endpoint is empty")
	}
	if !preset.SupportsList {
		t.Fatal("qwen should support OpenAI-compatible model listing")
	}

	custom, ok := modelPresetByID("custom")
	if !ok {
		t.Fatal("custom preset not found")
	}
	if custom.Endpoint != "" || !custom.NeedsEndpoint {
		t.Fatalf("custom preset = %#v, want empty endpoint and NeedsEndpoint", custom)
	}
}

func TestOpenAIModelListerParsesModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"}]}`))
	}))
	defer srv.Close()

	models, err := OpenAIModelLister{Client: srv.Client()}.ListModels(context.Background(), srv.URL+"/v1", "sk-test")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if strings.Join(models, ",") != "a-model,z-model" {
		t.Fatalf("models = %v", models)
	}
}

func TestOpenAIModelListerReturnsErrorForBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := OpenAIModelLister{Client: srv.Client()}.ListModels(context.Background(), srv.URL, "bad")
	if err == nil || !strings.Contains(err.Error(), "http 401") {
		t.Fatalf("err = %v, want http 401", err)
	}
}

func TestOpenAIModelListerReturnsErrorForMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":`))
	}))
	defer srv.Close()

	_, err := OpenAIModelLister{Client: srv.Client()}.ListModels(context.Background(), srv.URL, "sk-test")
	if err == nil {
		t.Fatal("ListModels succeeded for malformed JSON")
	}
}
```

Add `context` to the imports in `cmd/conan/main_test.go` if needed.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./cmd/conan -run 'TestModelPresetLookup|TestOpenAIModelLister' -count=1
```

Expected: FAIL because `modelPresetByID` and `OpenAIModelLister` do not exist.

- [ ] **Step 3: Implement provider presets**

Create `cmd/conan/model_presets.go`:

```go
package main

type ModelPreset struct {
	ID               string
	DisplayName      string
	Type             string
	Endpoint         string
	SupportsList     bool
	NeedsEndpoint    bool
	DefaultModelHint string
}

var modelPresets = []ModelPreset{
	{ID: "anthropic", DisplayName: "Anthropic", Type: "anthropic", Endpoint: "https://api.anthropic.com", DefaultModelHint: "claude-sonnet-4-6"},
	{ID: "openai", DisplayName: "OpenAI", Type: "openai", Endpoint: "https://api.openai.com/v1", SupportsList: true, DefaultModelHint: "gpt-4.1"},
	{ID: "glm", DisplayName: "GLM", Type: "openai", Endpoint: "https://open.bigmodel.cn/api/paas/v4", SupportsList: true, DefaultModelHint: "glm-4.5"},
	{ID: "minimax", DisplayName: "MiniMax", Type: "openai", Endpoint: "https://api.minimax.chat/v1", SupportsList: true, DefaultModelHint: "MiniMax-M1"},
	{ID: "qwen", DisplayName: "Qwen", Type: "openai", Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", SupportsList: true, DefaultModelHint: "qwen-max"},
	{ID: "kimi", DisplayName: "Kimi", Type: "openai", Endpoint: "https://api.moonshot.cn/v1", SupportsList: true, DefaultModelHint: "kimi-k2"},
	{ID: "custom", DisplayName: "Custom OpenAI-compatible", Type: "openai", NeedsEndpoint: true, SupportsList: true},
}

func modelPresetByID(id string) (ModelPreset, bool) {
	for _, preset := range modelPresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return ModelPreset{}, false
}
```

- [ ] **Step 4: Implement OpenAI-compatible model lister**

Create `cmd/conan/model_lister.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ModelLister interface {
	ListModels(ctx context.Context, endpoint string, apiKey string) ([]string, error)
}

type OpenAIModelLister struct {
	Client *http.Client
}

func (l OpenAIModelLister) ListModels(ctx context.Context, endpoint string, apiKey string) ([]string, error) {
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	endpoint = strings.TrimRight(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(body.Data))
	for _, model := range body.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}
```

- [ ] **Step 5: Run tests and verify pass**

Run:

```bash
go test ./cmd/conan -run 'TestModelPresetLookup|TestOpenAIModelLister' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

Do not commit unless the human explicitly asks for commits. If commits are authorized later, stage only:

```bash
git add cmd/conan/model_presets.go cmd/conan/model_lister.go cmd/conan/main_test.go
git commit -m "feat: add model provider presets and discovery"
```

---

### Task 3: Non-interactive Model Commands

**Files:**
- Create: `cmd/conan/model_commands.go`
- Modify: `cmd/conan/main.go`
- Modify: `cmd/conan/main_test.go`

- [ ] **Step 1: Add failing tests for `model list`, `model use`, and `model remove`**

Append these tests to `cmd/conan/main_test.go`:

```go
func TestModelListHidesAPIKeysAndMarksDefault(t *testing.T) {
	home := t.TempDir()
	config := `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-secret
  - name: claude-main
    type: anthropic
    endpoint: https://api.anthropic.com
    model: claude-sonnet-4-6
    api_key: ant-secret
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "model", "list")
	if err != nil {
		t.Fatalf("model list: %v", err)
	}
	if !strings.Contains(stdout, "qwen-prod") || !strings.Contains(stdout, "claude-main") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "sk-secret") || strings.Contains(stdout, "ant-secret") {
		t.Fatalf("stdout leaked api key: %q", stdout)
	}
	if !strings.Contains(stdout, "qwen-prod") || !strings.Contains(stdout, "*") {
		t.Fatalf("stdout = %q, want default marker", stdout)
	}
}

func TestModelUseUpdatesDefaultModel(t *testing.T) {
	home := t.TempDir()
	config := `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-secret
  - name: kimi
    type: openai
    endpoint: https://api.moonshot.cn/v1
    model: kimi-k2
    api_key: moon-secret
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "model", "use", "kimi")
	if err != nil {
		t.Fatalf("model use: %v", err)
	}
	if !strings.Contains(stdout, "default model: kimi") {
		t.Fatalf("stdout = %q", stdout)
	}
	loaded, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "kimi" {
		t.Fatalf("DefaultModel = %q", loaded.DefaultModel)
	}
}

func TestModelUseRejectsUnknownModel(t *testing.T) {
	home := t.TempDir()
	config := `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-secret
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := executeCommand("--home", home, "model", "use", "missing")
	if err == nil || !strings.Contains(err.Error(), "model missing not found") {
		t.Fatalf("err = %v", err)
	}
	loaded, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "qwen-prod" {
		t.Fatalf("DefaultModel = %q", loaded.DefaultModel)
	}
}

func TestModelRemoveDeletesModelAndClearsDefault(t *testing.T) {
	home := t.TempDir()
	config := `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-secret
  - name: kimi
    type: openai
    endpoint: https://api.moonshot.cn/v1
    model: kimi-k2
    api_key: moon-secret
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "model", "remove", "qwen-prod")
	if err != nil {
		t.Fatalf("model remove: %v", err)
	}
	if !strings.Contains(stdout, "removed model: qwen-prod") {
		t.Fatalf("stdout = %q", stdout)
	}
	loaded, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "" {
		t.Fatalf("DefaultModel = %q, want empty", loaded.DefaultModel)
	}
	if len(loaded.Models) != 1 || loaded.Models[0].Name != "kimi" {
		t.Fatalf("models = %#v", loaded.Models)
	}
}
```

Add this import to `cmd/conan/main_test.go` if it is not already present:

```go
cfgloader "github.com/pockyHM/conan/internal/config"
```

- [ ] **Step 2: Run command tests and verify failure**

Run:

```bash
go test ./cmd/conan -run 'TestModelList|TestModelUse|TestModelRemove' -count=1
```

Expected: FAIL because `model` command does not exist.

- [ ] **Step 3: Implement model command group and non-interactive handlers**

Create `cmd/conan/model_commands.go`:

```go
package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/spf13/cobra"
)

type modelCommandConfig struct {
	home       *string
	newLister  func() ModelLister
	newPrompter func(in io.Reader, out io.Writer) *modelPrompter
}

func newModelCommand(cfg modelCommandConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "Manage LLM model configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelList(cmd, *cfg.home)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "use <name>",
		Short: "Set the default model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelUse(cmd, *cfg.home, args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a configured model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelRemove(cmd, *cfg.home, args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add",
		Short: "Interactively add a model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			newLister := cfg.newLister
			if newLister == nil {
				newLister = func() ModelLister { return OpenAIModelLister{} }
			}
			newPrompter := cfg.newPrompter
			if newPrompter == nil {
				newPrompter = newModelPrompter
			}
			return runModelAdd(cmd, *cfg.home, newLister(), newPrompter(cmd.InOrStdin(), cmd.OutOrStdout()))
		},
	})
	return cmd
}

func runModelList(cmd *cobra.Command, home string) error {
	global, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tMODEL\tENDPOINT\tDEFAULT")
	for _, model := range global.Models {
		marker := ""
		if model.Name == global.DefaultModel {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", model.Name, model.Type, model.Model, model.Endpoint, marker)
	}
	return w.Flush()
}

func runModelUse(cmd *cobra.Command, home string, name string) error {
	loader := cfgloader.NewLoader(home)
	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	if findModel(global.Models, name) < 0 {
		return fmt.Errorf("model %s not found", name)
	}
	global.DefaultModel = name
	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "default model: %s\n", name)
	return nil
}

func runModelRemove(cmd *cobra.Command, home string, name string) error {
	loader := cfgloader.NewLoader(home)
	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	idx := findModel(global.Models, name)
	if idx < 0 {
		return fmt.Errorf("model %s not found", name)
	}
	global.Models = append(global.Models[:idx], global.Models[idx+1:]...)
	if global.DefaultModel == name {
		global.DefaultModel = ""
	}
	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed model: %s\n", name)
	return nil
}

func findModel(models []configschema.ModelConfig, name string) int {
	for i, model := range models {
		if model.Name == name {
			return i
		}
	}
	return -1
}

func cleanModelConfig(model configschema.ModelConfig) configschema.ModelConfig {
	model.Name = strings.TrimSpace(model.Name)
	model.Type = strings.TrimSpace(model.Type)
	model.Endpoint = strings.TrimRight(strings.TrimSpace(model.Endpoint), "/")
	model.Model = strings.TrimSpace(model.Model)
	model.APIKey = strings.TrimSpace(model.APIKey)
	return model
}
```

`runModelAdd` and `modelPrompter` will be implemented in Task 4; for this task add a temporary stub at the bottom of `model_commands.go` so the package compiles:

```go
func runModelAdd(cmd *cobra.Command, home string, lister ModelLister, prompter *modelPrompter) error {
	return fmt.Errorf("model add is not implemented")
}
```

Do not add `modelPrompter` yet; Task 4 will replace the stub. If the compiler needs the type before Task 4, add this temporary type:

```go
type modelPrompter struct{}

func newModelPrompter(in io.Reader, out io.Writer) *modelPrompter {
	return &modelPrompter{}
}
```

- [ ] **Step 4: Wire command into root**

In `cmd/conan/main.go`, after persistent flags are defined, add:

```go
rootCmd.AddCommand(newModelCommand(modelCommandConfig{home: &home}))
```

Make sure this happens before `return rootCmd` and before or after other `AddCommand` calls consistently with the current style.

- [ ] **Step 5: Run command tests and verify pass**

Run:

```bash
go test ./cmd/conan -run 'TestModelList|TestModelUse|TestModelRemove|TestNoArgCommandsRejectExtraArgs' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

Do not commit unless the human explicitly asks for commits. If commits are authorized later, stage only:

```bash
git add cmd/conan/main.go cmd/conan/model_commands.go cmd/conan/main_test.go
git commit -m "feat: add model management commands"
```

---

### Task 4: Interactive `conan model add`

**Files:**
- Create: `cmd/conan/model_prompt.go`
- Modify: `cmd/conan/model_commands.go`
- Modify: `cmd/conan/main_test.go`

- [ ] **Step 1: Add failing scripted interaction tests**

Append these tests to `cmd/conan/main_test.go`:

```go
func TestModelAddScriptedFlowWithManualModel(t *testing.T) {
	home := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		"5",
		"qwen-prod",
		"sk-qwen",
		"3",
		"qwen-max",
		"y",
		"",
	}, "\n"))
	var output bytes.Buffer

	cmd := newRootCommand()
	cmd.SetIn(input)
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--home", home, "model", "add"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("model add: %v\noutput: %s", err, output.String())
	}

	loaded, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "qwen-prod" {
		t.Fatalf("DefaultModel = %q", loaded.DefaultModel)
	}
	if len(loaded.Models) != 1 {
		t.Fatalf("models len = %d", len(loaded.Models))
	}
	model := loaded.Models[0]
	if model.Name != "qwen-prod" || model.Type != "openai" || model.Endpoint != "https://dashscope.aliyuncs.com/compatible-mode/v1" || model.Model != "qwen-max" || model.APIKey != "sk-qwen" {
		t.Fatalf("model = %#v", model)
	}
	if !strings.Contains(output.String(), "Saved model qwen-prod") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestModelAddUsesDiscoveredModel(t *testing.T) {
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b-model"},{"id":"a-model"}]}`))
	}))
	defer srv.Close()

	input := strings.NewReader(strings.Join([]string{
		"7",
		"custom-prod",
		"sk-custom",
		srv.URL + "/v1",
		"1",
		"n",
		"",
	}, "\n"))
	var output bytes.Buffer

	cmd := newRootCommand()
	cmd.SetIn(input)
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--home", home, "model", "add"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("model add: %v\noutput: %s", err, output.String())
	}

	loaded, err := cfgloader.NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if loaded.DefaultModel != "" {
		t.Fatalf("DefaultModel = %q, want empty", loaded.DefaultModel)
	}
	if len(loaded.Models) != 1 || loaded.Models[0].Model != "a-model" {
		t.Fatalf("models = %#v", loaded.Models)
	}
}

func TestModelAddRejectsDuplicateName(t *testing.T) {
	home := t.TempDir()
	config := `models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-existing
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	input := strings.NewReader(strings.Join([]string{
		"5",
		"qwen-prod",
		"sk-qwen",
		"3",
		"qwen-plus",
		"n",
		"",
	}, "\n"))
	var output bytes.Buffer

	cmd := newRootCommand()
	cmd.SetIn(input)
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--home", home, "model", "add"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "model qwen-prod already exists") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run model add tests and verify failure**

Run:

```bash
go test ./cmd/conan -run 'TestModelAdd' -count=1
```

Expected: FAIL because `runModelAdd` is still a stub.

- [ ] **Step 3: Implement prompt helper**

Create `cmd/conan/model_prompt.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type modelPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func newModelPrompter(in io.Reader, out io.Writer) *modelPrompter {
	return &modelPrompter{in: bufio.NewReader(in), out: out}
}

func (p *modelPrompter) line(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	text, err := p.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func (p *modelPrompter) choose(prompt string, labels []string) (int, error) {
	fmt.Fprintln(p.out, prompt)
	for i, label := range labels {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, label)
	}
	for {
		answer, err := p.line(fmt.Sprintf("Choice [1-%d]: ", len(labels)))
		if err != nil {
			return 0, err
		}
		idx, err := strconv.Atoi(answer)
		if err == nil && idx >= 1 && idx <= len(labels) {
			return idx - 1, nil
		}
		fmt.Fprintf(p.out, "Enter a number from 1 to %d.\n", len(labels))
	}
}

func (p *modelPrompter) confirm(prompt string) (bool, error) {
	answer, err := p.line(prompt + " [y/N]: ")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
```

If Task 3 added temporary `modelPrompter` definitions in `model_commands.go`, remove those temporary definitions now.

- [ ] **Step 4: Implement `runModelAdd`**

Replace the Task 3 `runModelAdd` stub in `cmd/conan/model_commands.go` with:

```go
func runModelAdd(cmd *cobra.Command, home string, lister ModelLister, prompter *modelPrompter) error {
	loader := cfgloader.NewLoader(home)
	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}

	presetLabels := make([]string, 0, len(modelPresets))
	for _, preset := range modelPresets {
		presetLabels = append(presetLabels, preset.DisplayName)
	}
	presetIdx, err := prompter.choose("Select provider:", presetLabels)
	if err != nil {
		return err
	}
	preset := modelPresets[presetIdx]

	name, err := prompter.line("Config name: ")
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("model name is required")
	}
	if findModel(global.Models, name) >= 0 {
		return fmt.Errorf("model %s already exists", name)
	}

	apiKey, err := prompter.line("API key: ")
	if err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api key is required")
	}

	endpoint := preset.Endpoint
	if preset.NeedsEndpoint {
		endpoint, err = prompter.line("Endpoint: ")
		if err != nil {
			return err
		}
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			return fmt.Errorf("endpoint is required")
		}
	}

	modelName := ""
	if preset.SupportsList && lister != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Fetching available models...")
		models, listErr := lister.ListModels(cmd.Context(), endpoint, apiKey)
		if listErr == nil && len(models) > 0 {
			labels := append([]string{}, models...)
			labels = append(labels, "Enter manually")
			modelIdx, err := prompter.choose("Select model:", labels)
			if err != nil {
				return err
			}
			if modelIdx < len(models) {
				modelName = models[modelIdx]
			}
		} else if listErr != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Could not fetch models: %v\n", listErr)
		}
	}
	if modelName == "" {
		prompt := "Model name"
		if preset.DefaultModelHint != "" {
			prompt += " [e.g. " + preset.DefaultModelHint + "]"
		}
		prompt += ": "
		modelName, err = prompter.line(prompt)
		if err != nil {
			return err
		}
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return fmt.Errorf("model name is required")
		}
	}

	setDefault, err := prompter.confirm("Set as default model?")
	if err != nil {
		return err
	}

	model := cleanModelConfig(configschema.ModelConfig{
		Name:     name,
		Type:     preset.Type,
		Endpoint: endpoint,
		Model:    modelName,
		APIKey:   apiKey,
	})
	global.Models = append(global.Models, model)
	if setDefault {
		global.DefaultModel = model.Name
	}
	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved model %s to %s\n", model.Name, loader.ConfigPath())
	return nil
}
```

- [ ] **Step 5: Run model add tests and verify pass**

Run:

```bash
go test ./cmd/conan -run 'TestModelAdd' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

Do not commit unless the human explicitly asks for commits. If commits are authorized later, stage only:

```bash
git add cmd/conan/model_prompt.go cmd/conan/model_commands.go cmd/conan/main_test.go
git commit -m "feat: add interactive model add wizard"
```

---

### Task 5: Full Verification and Documentation Update

**Files:**
- Modify: `CLAUDE.md`
- Optional modify: `docs/superpowers/specs/2026-05-21-model-management-design.md` if implementation changes a design detail during execution.

- [ ] **Step 1: Add model management note to CLAUDE.md**

In `CLAUDE.md`, under the CLI-related architecture/progress area, add a concise note that Conan now includes model management commands. Use this text:

```markdown
### Model Management CLI

Conan supports `conan model add/list/use/remove` for managing `~/.conan/config.yaml` model entries. `model add` is an interactive shell wizard with Anthropic/OpenAI-compatible presets, optional OpenAI-compatible `/models` discovery, and direct API key storage in the config file.
```

- [ ] **Step 2: Run focused tests**

Run:

```bash
go test ./internal/config ./cmd/conan -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full verification**

Run:

```bash
go vet ./... && go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Self-review implementation against spec**

Check these exact requirements:

- `conan model add`, `conan model list`, `conan model use <name>`, and `conan model remove <name>` exist.
- `model add` supports Anthropic, OpenAI, GLM, MiniMax, Qwen, Kimi, and custom OpenAI-compatible.
- API keys are saved directly to `~/.conan/config.yaml`.
- `model list` never prints API keys.
- OpenAI-compatible model discovery calls `GET <endpoint>/models` and falls back to manual model entry on failure.
- Config writes use `0600` permissions.
- Existing global fields are preserved when saving.
- Duplicate names are rejected.
- Removing the default model clears `default_model`.

- [ ] **Step 5: Commit Task 5**

Do not commit unless the human explicitly asks for commits. If commits are authorized later, stage only:

```bash
git add CLAUDE.md docs/superpowers/specs/2026-05-21-model-management-design.md
git commit -m "docs: document model management CLI"
```

---

## Plan Self-Review

Spec coverage:

- Commands: covered in Tasks 3 and 4.
- Existing config shape: covered in Task 1 and command handlers.
- API key direct storage: covered in Task 1 and Task 4 tests.
- Presets: covered in Task 2.
- OpenAI-compatible model discovery: covered in Task 2 and Task 4.
- Config write permissions and field preservation: covered in Task 1.
- Testability: covered through stdin/stdout command tests.

Placeholder scan:

- No placeholder implementation steps are left. Provider endpoint constants are concrete in the plan. If official docs reveal an endpoint change during implementation, update `model_presets.go` and the design spec in the same task.

Type consistency:

- `ModelPreset`, `ModelLister`, `OpenAIModelLister`, `modelPrompter`, and command handler names are used consistently across tasks.
