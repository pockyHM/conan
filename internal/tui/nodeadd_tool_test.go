package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want multiToolResultMsg", msg)
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
	if len(result.Results) != 1 || !result.Results[0].Success {
		t.Fatalf("results = %#v, want one successful local result", result.Results)
	}
	output := result.Results[0].Output
	for _, want := range []string{"node added and deployed: web-1", "cluster: prod", "host: 10.0.0.12", "agent_port: 9281", "health: ok"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "secret") {
		t.Fatalf("output leaked password:\n%s", output)
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
