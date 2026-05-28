package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
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

func TestNodeAddPreparePromptsForMissingPassword(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true
	call := llm.ToolCall{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.12","user":"deploy"}`),
	}

	msg := execCmd(t, model.prepareNodeAddOrPrompt(7, call))
	prompt, ok := msg.(nodeAddPromptMsg)
	if !ok {
		t.Fatalf("prepareNodeAddOrPrompt returned %T, want nodeAddPromptMsg", msg)
	}
	if prompt.streamID != 7 || prompt.field != "password" || prompt.label != "SSH password" || !prompt.secret {
		t.Fatalf("prompt = %#v, want password secret prompt", prompt)
	}
}

func TestNodePromptSubmitPasswordDoesNotAddSecretToConversation(t *testing.T) {
	const secret = "super-secret"
	var gotReq nodeadd.Request
	conv := conversation.New("prod", nil, "m")
	call := llm.ToolCall{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.12","user":"deploy"}`),
	}
	conv.AddToolCall(call.ID, call.Name, string(sanitizeToolArguments(call.Name, call.Arguments)))
	model := NewModel(ModelConfig{
		Cluster:    "prod",
		ConfigHome: t.TempDir(),
		Conv:       conv,
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
	model.mode = modeNodePrompt
	model.nodePrompt = nodePromptState{streamID: 7, call: call, field: "password", label: "SSH password", secret: true}
	model.input = secret
	model.streaming = true
	model.streamID = 7
	model.activeStreamID = 7
	model.inputHistory = []string{"previous"}
	model.messages = append(model.messages, chatMsg{role: "tool", toolCallID: call.ID, toolName: call.Name, toolInput: string(sanitizeToolArguments(call.Name, call.Arguments))})

	next, cmd := model.submit()
	model = next.(Model)
	if len(model.inputHistory) != 1 || model.inputHistory[0] != "previous" {
		t.Fatalf("inputHistory = %#v, want prompt input excluded", model.inputHistory)
	}
	for _, msg := range model.messages {
		if msg.role == "user" || strings.Contains(fmt.Sprintf("%#v", msg), secret) {
			t.Fatalf("messages leaked prompt input: %#v", model.messages)
		}
	}
	for _, msg := range conv.Messages() {
		if msg.Role == conversation.RoleUser || strings.Contains(fmt.Sprintf("%#v", msg), secret) {
			t.Fatalf("conversation leaked prompt input: %#v", conv.Messages())
		}
	}

	msg := execCmd(t, cmd)
	ready, ok := msg.(nodeAddReadyMsg)
	if !ok {
		t.Fatalf("submit prompt command returned %T, want nodeAddReadyMsg", msg)
	}
	next, cmd = model.Update(ready)
	model = next.(Model)
	msg = execCmd(t, cmd)
	result, ok := msg.(nodeAddResultMsg)
	if !ok {
		t.Fatalf("ready update command returned %T, want nodeAddResultMsg", msg)
	}
	if gotReq.Password != secret {
		t.Fatalf("runner password = %q, want prompt secret", gotReq.Password)
	}
	if strings.Contains(result.Output, secret) {
		t.Fatalf("node_add output leaked password: %s", result.Output)
	}
}

func TestRenderNodePromptFooterMasksSecretInput(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.mode = modeNodePrompt
	model.nodePrompt = nodePromptState{label: "SSH password", secret: true}
	model.input = "secret"
	model.status = "SSH password required"

	view := model.renderNodePromptFooter()
	if strings.Contains(view, "secret") {
		t.Fatalf("footer leaked secret:\n%s", view)
	}
	if !strings.Contains(view, "SSH password") || !strings.Contains(view, "******") {
		t.Fatalf("footer = %q, want label and masked input", view)
	}
}

func TestNodePromptEscCancelsWithoutLeakingPassword(t *testing.T) {
	const secret = "super-secret"
	conv := conversation.New("prod", nil, "m")
	call := llm.ToolCall{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.12","user":"deploy"}`),
	}
	conv.AddToolCall(call.ID, call.Name, string(sanitizeToolArguments(call.Name, call.Arguments)))
	model := NewModel(ModelConfig{Cluster: "prod", Model: "m", Conv: conv})
	model.mode = modeNodePrompt
	model.nodePrompt = nodePromptState{streamID: 1, call: call, field: "password", label: "SSH password", secret: true}
	model.input = secret
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.messages = append(model.messages, chatMsg{role: "tool", toolCallID: call.ID, toolName: call.Name, toolInput: string(sanitizeToolArguments(call.Name, call.Arguments))})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.status != "Ready" {
		t.Fatalf("status = %q, want Ready", model.status)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != "Cancelled by user" {
		t.Fatalf("messages = %#v, want cancelled tool output", model.messages)
	}
	if strings.Contains(fmt.Sprintf("%#v", model.messages), secret) {
		t.Fatalf("messages leaked password: %#v", model.messages)
	}
	if strings.Contains(fmt.Sprintf("%#v", conv.Messages()), secret) {
		t.Fatalf("conversation leaked password: %#v", conv.Messages())
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

func TestDispatchNodeAddCarriesClusterTLSForRefreshedClient(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.yaml"), "default_cluster: prod\n")
	writeTestFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\nagent:\n  tls: true\n")

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
						Port:  req.AgentPort,
						Token: "agent-token",
					},
				},
				Deployed: true,
			}, nil
		}),
	})
	model.nodeToolsEnabled = true
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"10.0.0.12","agent_port":9281}`)}

	msg := execCmd(t, model.dispatchNodeAdd(7, call))
	result, ok := msg.(nodeAddResultMsg)
	if !ok {
		t.Fatalf("dispatchNodeAdd returned %T, want nodeAddResultMsg", msg)
	}
	if !gotReq.TLS || !result.TLS {
		t.Fatalf("TLS = req %v msg %v, want true", gotReq.TLS, result.TLS)
	}
	if got := nodeAddClientURL("10.0.0.12", 9281, result.TLS); got != "https://10.0.0.12:9281" {
		t.Fatalf("nodeAddClientURL = %q, want https URL", got)
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
	}, false)

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

func TestNodeAddResultFetchesToolsBeforeResume(t *testing.T) {
	var toolListRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method != "tools/list" {
			t.Fatalf("method = %q, want tools/list", req.Method)
		}
		toolListRequests++
		_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]interface{}{
			"tools": []mcpproto.ToolDefinition{
				{Name: "sys_uptime", Description: "uptime", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		}))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	provider := &nodeAddCaptureStreamProvider{}
	conv := conversation.New("test", nil, "m")
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: json.RawMessage(`{"host":"127.0.0.1"}`)}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: provider, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolCallID: call.ID, toolName: call.Name})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamToolExpected = 1

	next, cmd := model.Update(nodeAddResultMsg{
		streamID: 1,
		Call:     call,
		Cluster:  "prod",
		Output:   "node added and deployed: web-1",
		Result: nodeadd.Result{
			Node: configschema.NodeConfig{
				Name: "web-1",
				Host: u.Hostname(),
				Agent: &configschema.NodeAgentOverride{
					Port:  port,
					Token: "agent-token",
				},
			},
			Deployed: true,
		},
	})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("node add update returned nil command, want fetch-before-resume command")
	}
	if provider.req != nil {
		t.Fatal("provider was called before node tools were fetched")
	}
	if model.streamToolDone != 0 {
		t.Fatalf("streamToolDone = %d, want 0 before node tool refresh", model.streamToolDone)
	}

	next, prematureResumeCmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopToolUse}})
	model = next.(Model)
	if prematureResumeCmd != nil {
		t.Fatal("StopToolUse before node tool refresh returned resume command")
	}
	if provider.req != nil {
		t.Fatal("provider was called after StopToolUse before node tools were fetched")
	}
	if !model.streamEnded || model.streamToolDone != 0 {
		t.Fatalf("streamEnded=%v streamToolDone=%d, want ended with pending node refresh", model.streamEnded, model.streamToolDone)
	}

	msg := execCmd(t, cmd)
	fetched, ok := msg.(nodeAddToolsFetchedMsg)
	if !ok {
		t.Fatalf("fetch command returned %T, want nodeAddToolsFetchedMsg", msg)
	}
	if toolListRequests != 1 {
		t.Fatalf("tool list requests = %d, want 1", toolListRequests)
	}

	next, resumeCmd := model.Update(fetched)
	model = next.(Model)
	if tools, ok := model.toolCache.Get("web-1"); !ok || len(tools) != 1 || tools[0].Name != "sys_uptime" {
		t.Fatalf("cached tools = %#v ok=%v, want sys_uptime before resume", tools, ok)
	}
	if provider.req != nil {
		t.Fatal("provider was called before processing fetched tools")
	}
	if resumeCmd == nil {
		t.Fatal("resume command = nil, want stream resume after tool fetch")
	}
	nodeAddExecMaybeBatch(t, resumeCmd)
	if provider.req == nil {
		t.Fatal("provider was not called after tool fetch and resume")
	}
}

type nodeAddCaptureStreamProvider struct {
	req *llm.ChatRequest
}

func (p *nodeAddCaptureStreamProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: models.Message{Role: "assistant", Content: "ok"}, StopReason: llm.StopEndTurn}, nil
}

func (p *nodeAddCaptureStreamProvider) ChatStream(_ context.Context, req *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.req = req
	ch := make(chan llm.ChatEvent, 2)
	ch <- llm.TextDeltaEvent{Delta: "ok"}
	ch <- llm.StopEvent{Reason: llm.StopEndTurn}
	close(ch)
	return ch, nil
}

func nodeAddExecMaybeBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return
	}
	for _, c := range batch {
		execCmd(t, c)
	}
}
