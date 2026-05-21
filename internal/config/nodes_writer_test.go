package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
	"gopkg.in/yaml.v3"
)

func TestWriteNodeAppendsNewNode(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)

	writer := NewNodeWriter(home)
	result, err := writer.WriteNode("prod", configschema.NodeConfig{
		Name:   "web-1",
		Host:   "10.0.0.11",
		Labels: []string{"web"},
		Agent: &configschema.NodeAgentOverride{
			User:  "deploy",
			Port:  9201,
			Token: "node-token",
		},
	}, WriteNodeOptions{})
	if err != nil {
		t.Fatalf("WriteNode: %v", err)
	}
	if result.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if result.Node.Name != "web-1" || result.Node.Host != "10.0.0.11" {
		t.Fatalf("result node = %+v", result.Node)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	if len(nodes.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes.Nodes))
	}
	got := nodes.Nodes[0]
	if got.Name != "web-1" || got.Host != "10.0.0.11" {
		t.Fatalf("node = %+v", got)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "web" {
		t.Fatalf("labels = %#v", got.Labels)
	}
	if got.Agent == nil || got.Agent.User != "deploy" || got.Agent.Port != 9201 || got.Agent.Token != "node-token" {
		t.Fatalf("agent = %+v", got.Agent)
	}
}

func TestWriteNodeDuplicateWithoutUpdateFails(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
`)

	writer := NewNodeWriter(home)
	_, err := writer.WriteNode("prod", configschema.NodeConfig{Name: "web-1", Host: "10.0.0.12"}, WriteNodeOptions{})
	if err == nil {
		t.Fatal("expected duplicate node error")
	}
	if !strings.Contains(err.Error(), "node already exists") {
		t.Fatalf("error = %q, want node already exists", err.Error())
	}
}

func TestWriteNodeUpdatePreservesTokenByDefault(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
    agent:
      user: old-user
      port: 9201
      token: old-token
`)

	writer := NewNodeWriter(home)
	result, err := writer.WriteNode("prod", configschema.NodeConfig{
		Name: "web-1",
		Host: "10.0.0.12",
		Agent: &configschema.NodeAgentOverride{
			User:  "new-user",
			Port:  9202,
			Token: "new-token",
		},
	}, WriteNodeOptions{Update: true})
	if err != nil {
		t.Fatalf("WriteNode: %v", err)
	}
	if !result.Updated {
		t.Fatalf("Updated = false, want true")
	}
	if result.Node.Agent == nil || result.Node.Agent.Token != "old-token" {
		t.Fatalf("result agent = %+v, want old-token", result.Node.Agent)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	got := nodes.Nodes[0]
	if got.Host != "10.0.0.12" {
		t.Fatalf("host = %q", got.Host)
	}
	if got.Agent == nil {
		t.Fatal("agent is nil")
	}
	if got.Agent.User != "new-user" || got.Agent.Port != 9202 {
		t.Fatalf("agent = %+v", got.Agent)
	}
	if got.Agent.Token != "old-token" {
		t.Fatalf("token = %q, want old-token", got.Agent.Token)
	}
}

func TestWriteNodeRotateTokenReplacesToken(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
    agent:
      token: old-token
`)

	writer := NewNodeWriter(home)
	result, err := writer.WriteNode("prod", configschema.NodeConfig{
		Name: "web-1",
		Host: "10.0.0.12",
		Agent: &configschema.NodeAgentOverride{
			Token: "new-token",
		},
	}, WriteNodeOptions{Update: true, RotateToken: true})
	if err != nil {
		t.Fatalf("WriteNode: %v", err)
	}
	if !result.Updated {
		t.Fatalf("Updated = false, want true")
	}
	if result.Node.Agent == nil || result.Node.Agent.Token != "new-token" {
		t.Fatalf("result agent = %+v, want new-token", result.Node.Agent)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	got := nodes.Nodes[0]
	if got.Agent == nil || got.Agent.Token != "new-token" {
		t.Fatalf("agent = %+v, want new-token", got.Agent)
	}
}

func TestWriteNodeAutoCreatesCluster(t *testing.T) {
	home := t.TempDir()
	writer := NewNodeWriter(home)

	result, err := writer.WriteNode("prod", configschema.NodeConfig{Name: "web-1", Host: "10.0.0.11"}, WriteNodeOptions{})
	if err != nil {
		t.Fatalf("WriteNode: %v", err)
	}
	if result.Updated {
		t.Fatal("Updated = true, want append")
	}

	clusterData, err := os.ReadFile(filepath.Join(home, "clusters", "prod", "cluster.yaml"))
	if err != nil {
		t.Fatalf("cluster.yaml not created: %v", err)
	}
	if !strings.Contains(string(clusterData), "name: prod") {
		t.Fatalf("cluster.yaml = %q", clusterData)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].Name != "web-1" {
		t.Fatalf("nodes = %+v", nodes.Nodes)
	}
}

func TestAddNodeCommandWhitelistAppendsExactCommand(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
    command_whitelist:
      - uptime
`)

	writer := NewNodeWriter(home)
	if err := writer.AddCommandWhitelist("prod", "web-1", "df -h"); err != nil {
		t.Fatalf("AddCommandWhitelist: %v", err)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	got := nodes.Nodes[0].CommandWhitelist
	if strings.Join(got, ",") != "uptime,df -h" {
		t.Fatalf("command_whitelist = %#v", got)
	}
}

func TestAddNodeCommandWhitelistDoesNotDuplicate(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), `name: prod
`)
	writeFile(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"), `nodes:
  - name: web-1
    host: 10.0.0.11
    command_whitelist:
      - uptime
`)

	writer := NewNodeWriter(home)
	if err := writer.AddCommandWhitelist("prod", "web-1", " uptime "); err != nil {
		t.Fatalf("AddCommandWhitelist: %v", err)
	}

	nodes := readNodeList(t, filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	got := nodes.Nodes[0].CommandWhitelist
	if strings.Join(got, ",") != "uptime" {
		t.Fatalf("command_whitelist = %#v", got)
	}
}

func readNodeList(t *testing.T, path string) configschema.NodeList {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	var nodes configschema.NodeList
	if err := yaml.Unmarshal(data, &nodes); err != nil {
		t.Fatalf("parse nodes.yaml: %v", err)
	}
	return nodes
}
