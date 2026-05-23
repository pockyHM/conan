package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestLoadGlobalConfigExpandsEnvAndDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TEST_CONAN_API_KEY", "secret-key")
	writeFile(t, filepath.Join(home, "config.yaml"), `default_model: claude-sonnet
models:
  - name: claude-sonnet
    type: anthropic
    endpoint: https://api.anthropic.com/
    model: claude-sonnet-4-6
    api_key: ${TEST_CONAN_API_KEY}
    thinking: false
`)

	loader := NewLoader(home)
	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.DefaultModel != "claude-sonnet" {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if cfg.Models[0].APIKey != "secret-key" {
		t.Fatalf("APIKey = %q", cfg.Models[0].APIKey)
	}
	if cfg.Models[0].Endpoint != "https://api.anthropic.com" {
		t.Fatalf("Endpoint = %q", cfg.Models[0].Endpoint)
	}
	if cfg.Models[0].Thinking == nil || *cfg.Models[0].Thinking {
		t.Fatalf("Thinking = %#v, want false", cfg.Models[0].Thinking)
	}
	if cfg.Security.RiskAssessmentModel != "claude-sonnet" {
		t.Fatalf("RiskAssessmentModel = %q", cfg.Security.RiskAssessmentModel)
	}
	if strings.Join(cfg.Security.CommandBlacklist, ",") != `.*\|\s*bash.*` {
		t.Fatalf("CommandBlacklist = %#v", cfg.Security.CommandBlacklist)
	}
	if cfg.Memory.RulesTokenBudget != 2000 {
		t.Fatalf("RulesTokenBudget = %d", cfg.Memory.RulesTokenBudget)
	}
	if cfg.Subagents.Enabled {
		t.Fatal("Subagents.Enabled = true, want false by default")
	}
	if cfg.Subagents.MaxParallel != 3 {
		t.Fatalf("Subagents.MaxParallel = %d, want 3", cfg.Subagents.MaxParallel)
	}
	if cfg.Subagents.TimeoutSeconds != 120 {
		t.Fatalf("Subagents.TimeoutSeconds = %d, want 120", cfg.Subagents.TimeoutSeconds)
	}
}

func TestLoadGlobalMissingFileUsesDefaults(t *testing.T) {
	loader := NewLoader(t.TempDir())
	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.DefaultModel != "" {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if cfg.UILanguage != "en-US" {
		t.Fatalf("UILanguage = %q, want en-US", cfg.UILanguage)
	}
	if cfg.Security.RiskAssessmentModel != "claude-sonnet" {
		t.Fatalf("RiskAssessmentModel = %q", cfg.Security.RiskAssessmentModel)
	}
}

func TestLoadGlobalUILanguage(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config.yaml"), `ui_language: zh-CN
`)

	cfg, err := NewLoader(home).LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.UILanguage != "zh-CN" {
		t.Fatalf("UILanguage = %q, want zh-CN", cfg.UILanguage)
	}
}

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
	amd64Path := filepath.Join(home, "bin", "amd64", "conan-agent")
	arm64Path := filepath.Join(home, "bin", "arm64", "conan-agent")
	remoteBinaryPath := "/opt/conan-agent"
	remoteConfigPath := "/etc/conan-agent.yaml"
	systemdUnitPath := "/etc/systemd/system/conan-agent.service"
	cfg := &configschema.GlobalConfig{
		DefaultCluster: "prod",
		Security: configschema.SecurityConfig{
			RiskAssessmentModel: "claude-risk",
			CommandBlacklist:    []string{`.*\|\s*bash.*`, `.*curl.*sh.*`},
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
			Binaries: configschema.AgentBinaryConfig{
				AMD64: amd64Path,
				ARM64: arm64Path,
			},
			RemoteBinaryPath: remoteBinaryPath,
			RemoteConfigPath: remoteConfigPath,
			SystemdUnitPath:  systemdUnitPath,
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
	if loaded.Security.RiskAssessmentModel != "claude-risk" || strings.Join(loaded.Security.CommandBlacklist, ",") != `.*\|\s*bash.*,.*curl.*sh.*` {
		t.Fatalf("security = %#v", loaded.Security)
	}
	if loaded.Memory.RulesTokenBudget != 123 || loaded.Memory.KnowledgeTokenBudget != 456 {
		t.Fatalf("memory = %#v", loaded.Memory)
	}
	if loaded.Logging.Level != "debug" || loaded.Logging.File != "/tmp/conan.log" || !loaded.Logging.Audit {
		t.Fatalf("logging = %#v", loaded.Logging)
	}
	if loaded.AgentDeploy.Binaries.AMD64 != amd64Path {
		t.Fatalf("agent deploy amd64 binary = %q, want %q", loaded.AgentDeploy.Binaries.AMD64, amd64Path)
	}
	if loaded.AgentDeploy.Binaries.ARM64 != arm64Path {
		t.Fatalf("agent deploy arm64 binary = %q, want %q", loaded.AgentDeploy.Binaries.ARM64, arm64Path)
	}
	if loaded.AgentDeploy.RemoteBinaryPath != remoteBinaryPath {
		t.Fatalf("agent deploy remote binary path = %q, want %q", loaded.AgentDeploy.RemoteBinaryPath, remoteBinaryPath)
	}
	if loaded.AgentDeploy.RemoteConfigPath != remoteConfigPath {
		t.Fatalf("agent deploy remote config path = %q, want %q", loaded.AgentDeploy.RemoteConfigPath, remoteConfigPath)
	}
	if loaded.AgentDeploy.SystemdUnitPath != systemdUnitPath {
		t.Fatalf("agent deploy systemd unit path = %q, want %q", loaded.AgentDeploy.SystemdUnitPath, systemdUnitPath)
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

func TestSaveGlobalPreservesExistingAPIKeyPlaceholders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENAI_API_KEY", "expanded-secret")
	writeFile(t, filepath.Join(home, "config.yaml"), `default_model: env-model
models:
  - name: env-model
    type: openai
    endpoint: https://api.openai.com/v1
    model: gpt-4.1
    api_key: ${OPENAI_API_KEY}
`)

	loader := NewLoader(home)
	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.Models[0].APIKey != "expanded-secret" {
		t.Fatalf("APIKey = %q", cfg.Models[0].APIKey)
	}
	cfg.DefaultModel = "env-model"

	if err := loader.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "${OPENAI_API_KEY}") {
		t.Fatalf("saved config = %s, want original api key placeholder", data)
	}
}

func TestLoadClusterMergesBaseClusterAndNode(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "base.yaml"), `agent:
  listen: 0.0.0.0:9280
  token: base-token
  tls: false
  audit_log: /tmp/base-audit.log
  rate_limit: 10
node_defaults:
  user: root
  ssh_port: 22
`)
	writeFile(t, filepath.Join(home, "clusters", "production", "cluster.yaml"), `name: production
description: Production cluster
inherits: base
agent:
  tls: true
  rate_limit: 25
  web:
    search_provider: brave
    search_api_key_env: BRAVE_SEARCH_API_KEY
    fetch_max_chars: 8000
node_defaults:
  user: deploy
`)
	writeFile(t, filepath.Join(home, "clusters", "production", "nodes.yaml"), `nodes:
  - name: master-01
    host: 10.0.1.1
    labels: [master, k8s]
  - name: db-01
    host: 10.0.2.1
    labels: [database]
    agent:
      user: admin
      port: 9201
`)

	loader := NewLoader(home)
	cluster, err := loader.LoadCluster("production")
	if err != nil {
		t.Fatalf("LoadCluster: %v", err)
	}
	if cluster.Cluster.Name != "production" {
		t.Fatalf("cluster name = %q", cluster.Cluster.Name)
	}
	if !cluster.Cluster.Agent.TLS {
		t.Fatal("expected TLS inherited from cluster override")
	}
	if cluster.Cluster.Agent.Token != "base-token" {
		t.Fatalf("token = %q", cluster.Cluster.Agent.Token)
	}
	if cluster.Cluster.Agent.AuditLog != "/tmp/base-audit.log" {
		t.Fatalf("audit_log = %q", cluster.Cluster.Agent.AuditLog)
	}
	if cluster.Cluster.Agent.RateLimit != 25 {
		t.Fatalf("rate_limit = %d", cluster.Cluster.Agent.RateLimit)
	}
	if cluster.Cluster.Agent.Web.SearchProvider != "brave" || cluster.Cluster.Agent.Web.SearchAPIKeyEnv != "BRAVE_SEARCH_API_KEY" || cluster.Cluster.Agent.Web.FetchMaxChars != 8000 {
		t.Fatalf("web config = %+v", cluster.Cluster.Agent.Web)
	}
	if len(cluster.Nodes) != 2 {
		t.Fatalf("nodes = %d", len(cluster.Nodes))
	}
	master := cluster.Nodes[0]
	if master.Agent.User != "deploy" || master.Agent.Port != 9280 {
		t.Fatalf("master agent = %+v", master.Agent)
	}
	db := cluster.Nodes[1]
	if db.Agent.User != "admin" || db.Agent.Port != 9201 {
		t.Fatalf("db agent = %+v", db.Agent)
	}
}

func TestLoadClusterAllowsTLSFalseOverride(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "base.yaml"), `agent:
  listen: 0.0.0.0:9280
  tls: true
node_defaults:
  user: root
`)
	writeFile(t, filepath.Join(home, "clusters", "dev", "cluster.yaml"), `name: dev
agent:
  tls: false
`)

	loader := NewLoader(home)
	cluster, err := loader.LoadCluster("dev")
	if err != nil {
		t.Fatalf("LoadCluster: %v", err)
	}
	if cluster.Cluster.Agent.TLS {
		t.Fatal("expected cluster tls: false to override base tls: true")
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

func TestListClusters(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod`)
	writeFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), `name: staging`)
	writeFile(t, filepath.Join(home, "clusters", "base.yaml"), `agent:
  listen: 0.0.0.0:9280
`)

	loader := NewLoader(home)
	clusters, err := loader.ListClusters()
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %#v", clusters)
	}
	if clusters[0] != "prod" || clusters[1] != "staging" {
		t.Fatalf("clusters = %#v", clusters)
	}
}

func TestListClustersReturnsStatErrors(t *testing.T) {
	home := t.TempDir()
	clusterDir := filepath.Join(home, "clusters", "broken")
	if err := os.MkdirAll(clusterDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("cluster.yaml", filepath.Join(clusterDir, "cluster.yaml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	loader := NewLoader(home)
	_, err := loader.ListClusters()
	if err == nil {
		t.Fatal("expected ListClusters to return stat error")
	}
}
