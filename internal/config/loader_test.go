package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if cfg.Security.RiskAssessmentModel != "claude-sonnet" {
		t.Fatalf("RiskAssessmentModel = %q", cfg.Security.RiskAssessmentModel)
	}
	if cfg.Memory.RulesTokenBudget != 2000 {
		t.Fatalf("RulesTokenBudget = %d", cfg.Memory.RulesTokenBudget)
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
	if cfg.Security.RiskAssessmentModel != "claude-sonnet" {
		t.Fatalf("RiskAssessmentModel = %q", cfg.Security.RiskAssessmentModel)
	}
}

func TestLoadClusterMergesBaseClusterAndNode(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "base.yaml"), `agent:
  listen: 0.0.0.0:9200
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
	if len(cluster.Nodes) != 2 {
		t.Fatalf("nodes = %d", len(cluster.Nodes))
	}
	master := cluster.Nodes[0]
	if master.Agent.User != "deploy" || master.Agent.Port != 9200 {
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
  listen: 0.0.0.0:9200
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

func TestListClusters(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod`)
	writeFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), `name: staging`)
	writeFile(t, filepath.Join(home, "clusters", "base.yaml"), `agent:
  listen: 0.0.0.0:9200
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
