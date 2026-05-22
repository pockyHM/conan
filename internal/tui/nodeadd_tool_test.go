package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/pkg/configschema"
)

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

func TestDispatchNodeAddUsesInjectedRunnerAndPreservesRawPassword(t *testing.T) {
	var gotReq nodeadd.Request
	model := NewModel(ModelConfig{
		Cluster:    "prod",
		ConfigHome: t.TempDir(),
		NodeAddRunner: nodeAddRunnerFunc(func(_ context.Context, req nodeadd.Request) (nodeadd.Result, error) {
			gotReq = req
			return nodeadd.Result{
				Node: configschema.NodeConfig{
					Name: "web-1",
					Host: "10.0.0.12",
					Agent: &configschema.NodeAgentOverride{
						Port: req.AgentPort,
					},
				},
				Deployed: true,
			}, nil
		}),
	})
	model.nodeToolsEnabled = true
	rawArgs := json.RawMessage(`{
		"host":"10.0.0.12",
		"name":"web-1",
		"user":"deploy",
		"password":"secret",
		"ssh_port":2222,
		"agent_port":9281,
		"agent_bin":"/tmp/conan-agent",
		"update":true,
		"rotate_token":true
	}`)
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: rawArgs}

	msg := execCmd(t, model.dispatchNodeAdd(7, call))
	result, ok := msg.(nodeAddResultMsg)
	if !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want nodeAddResultMsg", msg)
	}

	if gotReq.ClusterName != "prod" || gotReq.Input != "10.0.0.12" || gotReq.Name != "web-1" {
		t.Fatalf("request identity = %#v", gotReq)
	}
	if gotReq.Username != "deploy" || gotReq.Password != "secret" {
		t.Fatalf("request credentials = %#v", gotReq)
	}
	if gotReq.SSHPort != 2222 || gotReq.AgentPort != 9281 {
		t.Fatalf("request ports = ssh %d agent %d", gotReq.SSHPort, gotReq.AgentPort)
	}
	if !gotReq.Update || !gotReq.RotateToken || gotReq.AgentBinOverride != "/tmp/conan-agent" {
		t.Fatalf("request options = %#v", gotReq)
	}
	if string(result.Call.Arguments) != string(rawArgs) {
		t.Fatalf("dispatch should preserve raw arguments, got %s", string(result.Call.Arguments))
	}
	output := result.Output
	for _, want := range []string{"node added and deployed: web-1", "cluster: prod", "host: 10.0.0.12", "agent_port: 9281", "health: ok"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "secret") {
		t.Fatalf("output leaked password:\n%s", output)
	}

	model.messages = append(model.messages, chatMsg{role: "tool", toolCallID: call.ID, toolName: call.Name})
	model.streaming = true
	model.streamID = 7
	model.activeStreamID = 7
	model.streamEnded = true
	model.streamToolExpected = 1
	next, _ := model.Update(result)
	model = next.(Model)

	if len(model.nodes) != 1 || model.nodes[0].Name != "web-1" || model.nodes[0].Host != "10.0.0.12" || !model.nodes[0].Online {
		t.Fatalf("nodes = %#v, want deployed web-1", model.nodes)
	}
	if !model.selectedNodes["web-1"] {
		t.Fatalf("selectedNodes = %#v, want web-1 selected", model.selectedNodes)
	}
	if _, ok := model.clients["web-1"]; !ok {
		t.Fatalf("clients = %#v, want web-1 client", model.clients)
	}
}

func TestDispatchNodeAddRequiresAuthorization(t *testing.T) {
	model := NewModel(ModelConfig{
		ConfigHome: t.TempDir(),
		NodeAddRunner: nodeAddRunnerFunc(func(context.Context, nodeadd.Request) (nodeadd.Result, error) {
			t.Fatal("runner should not be called when node tools are disabled")
			return nodeadd.Result{}, nil
		}),
	})
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12"}`)}

	msg := execCmd(t, model.dispatchNodeAdd(7, call))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 || result.Results[0].Success {
		t.Fatalf("results = %#v, want one failed local result", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "node_add is not enabled") {
		t.Fatalf("output = %q, want authorization error", result.Results[0].Output)
	}
}

func TestDispatchNodeAddRedactsPasswordFromRunnerError(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster:    "prod",
		ConfigHome: t.TempDir(),
		NodeAddRunner: nodeAddRunnerFunc(func(context.Context, nodeadd.Request) (nodeadd.Result, error) {
			return nodeadd.Result{}, fmt.Errorf("bad password secret")
		}),
	})
	model.nodeToolsEnabled = true
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12","password":"secret"}`)}

	msg := execCmd(t, model.dispatchNodeAdd(7, call))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 || result.Results[0].Success {
		t.Fatalf("results = %#v, want one failed local result", result.Results)
	}
	output := result.Results[0].Output
	if strings.Contains(output, "secret") {
		t.Fatalf("output leaked password: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("output = %q, want redaction marker", output)
	}
}

func TestDispatchNodeAddUsesGlobalDefaultClusterWhenModelClusterImplicit(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.yaml"), "default_cluster: prod\n")
	writeTestFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")

	var gotReq nodeadd.Request
	model := NewModel(ModelConfig{
		ConfigHome: home,
		NodeAddRunner: nodeAddRunnerFunc(func(_ context.Context, req nodeadd.Request) (nodeadd.Result, error) {
			gotReq = req
			return nodeadd.Result{
				Node: configschema.NodeConfig{
					Name: "web-1",
					Host: req.Input,
					Agent: &configschema.NodeAgentOverride{
						Port: req.AgentPort,
					},
				},
				Deployed: true,
			}, nil
		}),
	})
	model.nodeToolsEnabled = true
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12"}`)}

	msg := execCmd(t, model.dispatchNodeAdd(7, call))
	if _, ok := msg.(nodeAddResultMsg); !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want nodeAddResultMsg", msg)
	}
	if gotReq.ClusterName != "prod" {
		t.Fatalf("cluster name = %q, want prod", gotReq.ClusterName)
	}
}

func TestApplyNodeAddResultSelectsNewNodeAndPreservesExistingSelectedNodes(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "old",
		Nodes: []NodeInfo{
			{Name: "node-01", Host: "10.0.0.1", Online: true},
		},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.mode = modeNodeSelect
	model.nodeSelector = newNodeSelector(model.nodes, model.selectedNodes)

	model = model.applyNodeAddResult("prod", nodeadd.Result{
		Node: configschema.NodeConfig{
			Name:             "web-1",
			Host:             "10.0.0.12",
			CommandWhitelist: []string{"uptime"},
			Agent: &configschema.NodeAgentOverride{
				Port:  9281,
				Token: "agent-token",
			},
		},
		Deployed: true,
	})

	if model.cluster != "prod" || !model.clusterExplicit {
		t.Fatalf("cluster = %q explicit=%v, want prod explicit", model.cluster, model.clusterExplicit)
	}
	if !model.selectedNodes["node-01"] || !model.selectedNodes["web-1"] {
		t.Fatalf("selectedNodes = %#v, want existing and new selections", model.selectedNodes)
	}
	if len(model.nodes) != 2 {
		t.Fatalf("nodes = %#v, want two nodes", model.nodes)
	}
	got := model.nodes[1]
	if got.Name != "web-1" || got.Host != "10.0.0.12" || !got.Online || len(got.CommandWhitelist) != 1 || got.CommandWhitelist[0] != "uptime" {
		t.Fatalf("new node = %#v", got)
	}
	if _, ok := model.clients["web-1"]; !ok {
		t.Fatalf("clients = %#v, want web-1 client", model.clients)
	}
	if !model.nodeSelector.checked["web-1"] {
		t.Fatalf("nodeSelector checked = %#v, want web-1 selected", model.nodeSelector.checked)
	}
}

func TestNodeAddResultUpdateFillsPlaceholderConversationAndStatus(t *testing.T) {
	conv := conversation.New("test", nil, "m")
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12"}`)}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolCallID: call.ID, toolName: call.Name})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1

	output := "node added and deployed: web-1"
	next, _ := model.Update(nodeAddResultMsg{
		streamID: 1,
		Call:     call,
		Cluster:  "prod",
		Output:   output,
		Result: nodeadd.Result{
			Node: configschema.NodeConfig{
				Name: "web-1",
				Host: "10.0.0.12",
				Agent: &configschema.NodeAgentOverride{
					Port:  9281,
					Token: "agent-token",
				},
			},
			Deployed: true,
		},
	})
	model = next.(Model)

	if model.status != "Node added and deployed" {
		t.Fatalf("status = %q, want node added status", model.status)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != output {
		t.Fatalf("messages = %#v, want filled tool placeholder", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != conversation.RoleTool || msgs[0].ToolCallID != call.ID || msgs[0].Content != output {
		t.Fatalf("conversation messages = %#v, want node_add tool result", msgs)
	}
	if !model.selectedNodes["web-1"] {
		t.Fatalf("selectedNodes = %#v, want web-1 selected", model.selectedNodes)
	}
}
