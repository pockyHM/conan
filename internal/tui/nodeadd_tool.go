package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/mcpproto"
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

type nodeAddResultMsg struct {
	streamID uint64
	Call     llm.ToolCall
	Result   nodeadd.Result
	Cluster  string
	TLS      bool
	Output   string
}

type nodeAddToolsFetchedMsg struct {
	streamID uint64
	updates  []toolCacheMsg
}

type nodeAddPromptMsg struct {
	streamID uint64
	call     llm.ToolCall
	field    string
	label    string
	secret   bool
}

type nodeAddReadyMsg struct {
	streamID uint64
	call     llm.ToolCall
}

func (m Model) prepareNodeAddOrPrompt(streamID uint64, call llm.ToolCall) tea.Cmd {
	enabled := m.nodeToolsEnabled
	return func() tea.Msg {
		if !enabled {
			return nodeAddLocalResult(streamID, call, "node_add is not enabled. Use /node before asking the model to add nodes.", false)
		}
		args, err := parseNodeAddArgs(call.Arguments)
		if err != nil {
			return nodeAddLocalResult(streamID, call, "invalid node_add arguments: "+err.Error(), false)
		}
		if args.User == "" {
			return nodeAddPromptMsg{
				streamID: streamID,
				call:     call,
				field:    "user",
				label:    "SSH username",
				secret:   false,
			}
		}
		if args.Password == "" {
			return nodeAddPromptMsg{
				streamID: streamID,
				call:     call,
				field:    "password",
				label:    "SSH password",
				secret:   true,
			}
		}
		return nodeAddReadyMsg{streamID: streamID, call: call}
	}
}

func setNodeAddArg(raw json.RawMessage, field string, value string) (json.RawMessage, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, fmt.Errorf("node_add arguments must be an object")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	args[field] = encoded
	return json.Marshal(args)
}

func (m Model) dispatchNodeAdd(streamID uint64, call llm.ToolCall) tea.Cmd {
	enabled := m.nodeToolsEnabled
	home := m.configHome
	if home == "" {
		home = cfgloader.DefaultHome()
	}
	currentCluster := m.cluster
	clusterExplicit := m.clusterExplicit
	runner := m.nodeAddRunner
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	return func() tea.Msg {
		if !enabled {
			return nodeAddLocalResult(streamID, call, "node_add is not enabled. Use /node before asking the model to add nodes.", false)
		}

		args, err := parseNodeAddArgs(call.Arguments)
		if err != nil {
			return nodeAddLocalResult(streamID, call, "invalid node_add arguments: "+err.Error(), false)
		}

		loader := cfgloader.NewLoader(home)
		global, err := loader.LoadGlobal()
		if err != nil {
			return nodeAddLocalResult(streamID, call, "load global config: "+err.Error(), false)
		}
		clusterName := args.Cluster
		if clusterName == "" && clusterExplicit {
			clusterName = currentCluster
		}
		if clusterName == "" {
			clusterName = global.DefaultCluster
		}
		if clusterName == "" {
			clusterName = currentCluster
		}
		if clusterName == "" {
			clusterName = "default"
		}

		cluster, err := loader.LoadCluster(clusterName)
		if err != nil {
			return nodeAddLocalResult(streamID, call, "load cluster: "+err.Error(), false)
		}

		req := buildNodeAddRequest(loader.Home(), clusterName, args, global.AgentDeploy, cluster.Cluster.Agent.TLS)
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

		result, err := runner.Add(parentCtx, req)
		if err != nil {
			return nodeAddLocalResult(streamID, call, "node_add failed: "+redactNodeAddError(err, args), false)
		}

		name := result.Node.Name
		if name == "" {
			name = req.Name
		}
		if name == "" {
			name = req.Input
		}
		host := result.Node.Host
		if host == "" {
			host = req.Input
		}
		agentPort := req.AgentPort
		if result.Node.Agent != nil && result.Node.Agent.Port != 0 {
			agentPort = result.Node.Agent.Port
		}
		if agentPort == 0 {
			agentPort = 9280
		}
		if result.Node.Name == "" {
			result.Node.Name = name
		}
		if result.Node.Host == "" {
			result.Node.Host = host
		}
		if result.Node.Agent != nil && result.Node.Agent.Port == 0 {
			result.Node.Agent.Port = agentPort
		}
		output := fmt.Sprintf("node added and deployed: %s\ncluster: %s\nhost: %s\nagent_port: %d\nhealth: ok", name, req.ClusterName, host, agentPort)
		return nodeAddResultMsg{streamID: streamID, Call: call, Result: result, Cluster: req.ClusterName, TLS: req.TLS, Output: output}
	}
}

func (m Model) applyNodeAddResult(cluster string, result nodeadd.Result, tls bool) Model {
	if cluster != "" {
		m.cluster = cluster
		m.clusterExplicit = true
	}

	node := result.Node
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return m
	}
	info := NodeInfo{
		Name:             name,
		Host:             node.Host,
		CommandWhitelist: append([]string(nil), node.CommandWhitelist...),
		Online:           result.Deployed,
	}
	upserted := false
	for i := range m.nodes {
		if m.nodes[i].Name == name {
			m.nodes[i] = info
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
	m.selectedNodes[name] = true

	if node.Agent != nil && node.Host != "" && node.Agent.Port != 0 {
		if m.clients == nil {
			m.clients = make(map[string]*mcp.Client)
		}
		m.clients[name] = mcp.NewClient(mcp.Config{
			BaseURL: nodeAddClientURL(node.Host, node.Agent.Port, tls),
			Token:   node.Agent.Token,
		})
	}

	if m.mode == modeNodeSelect {
		m.nodeSelector = m.nodeSelector.SetNodes(m.nodes)
		if m.nodeSelector.checked == nil {
			m.nodeSelector.checked = make(map[string]bool)
		}
		m.nodeSelector.checked[name] = true
	}

	return m
}

func nodeAddClientURL(host string, port int, tls bool) string {
	return mcp.URL(host, port, tls)
}

func fetchNodeToolsBeforeNodeAddResume(streamID uint64, clients map[string]*mcp.Client) tea.Cmd {
	clientCopy := make(map[string]*mcp.Client, len(clients))
	for name, client := range clients {
		clientCopy[name] = client
	}
	return func() tea.Msg {
		return nodeAddToolsFetchedMsg{streamID: streamID, updates: fetchNodeToolUpdates(clientCopy)}
	}
}

func fetchNodeToolUpdates(clients map[string]*mcp.Client) []toolCacheMsg {
	type nodeTools struct {
		node  string
		tools []mcpproto.ToolDefinition
	}
	ch := make(chan nodeTools, len(clients))
	for name, client := range clients {
		n, c := name, client
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tools, err := c.ListTools(ctx)
			if err != nil {
				ch <- nodeTools{node: n, tools: nil}
				return
			}
			ch <- nodeTools{node: n, tools: tools}
		}()
	}

	updates := make([]toolCacheMsg, 0, len(clients))
	for range clients {
		nt := <-ch
		if nt.tools != nil {
			updates = append(updates, toolCacheMsg{node: nt.node, tools: nt.tools})
		}
	}
	return updates
}

func redactNodeAddError(err error, args nodeAddArgs) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if args.Password != "" {
		msg = strings.ReplaceAll(msg, args.Password, "[REDACTED]")
	}
	return msg
}

func nodeAddLocalResult(streamID uint64, call llm.ToolCall, output string, success bool) multiToolResultMsg {
	return multiToolResultMsg{
		streamID: streamID,
		Call:     call,
		Results:  []nodeToolResult{{Node: "local", Output: output, Success: success}},
	}
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
