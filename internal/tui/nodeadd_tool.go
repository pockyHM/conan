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
	Cluster     string   `json:"cluster"`
	Host        string   `json:"host"`
	Name        string   `json:"name"`
	User        string   `json:"user"`
	Users       []string `json:"users"`
	Password    string   `json:"password"`
	Passwords   []string `json:"passwords"`
	SSHPort     int      `json:"ssh_port"`
	AgentPort   int      `json:"agent_port"`
	AgentBin    string   `json:"agent_bin"`
	Update      bool     `json:"update"`
	RotateToken bool     `json:"rotate_token"`
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
	for i := range args.Users {
		args.Users[i] = strings.TrimSpace(args.Users[i])
	}
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
	Results  []nodeadd.Result
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
	list     string
	index    int
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
		prompt, ready, err := nextNodeAddCredentialPrompt(streamID, call, args)
		if err != nil {
			return nodeAddLocalResult(streamID, call, "invalid node_add arguments: "+err.Error(), false)
		}
		if !ready {
			return prompt
		}
		return nodeAddReadyMsg{streamID: streamID, call: call}
	}
}

func nextNodeAddCredentialPrompt(streamID uint64, call llm.ToolCall, args nodeAddArgs) (nodeAddPromptMsg, bool, error) {
	inputs := nodeadd.SplitCommaList(args.Host)
	names := nodeadd.SplitCommaList(args.Name)
	if len(names) > 0 && len(names) != len(inputs) {
		return nodeAddPromptMsg{}, false, fmt.Errorf("name must be empty or contain %d comma-separated values", len(inputs))
	}
	if len(inputs) <= 1 {
		if args.User == "" {
			return nodeAddPromptMsg{streamID: streamID, call: call, field: "user", label: "SSH username", secret: false}, false, nil
		}
		if args.Password == "" {
			return nodeAddPromptMsg{streamID: streamID, call: call, field: "password", label: "SSH password", secret: true}, false, nil
		}
		return nodeAddPromptMsg{}, true, nil
	}
	if args.User == "" {
		if len(args.Users) > len(inputs) {
			return nodeAddPromptMsg{}, false, fmt.Errorf("users must contain at most %d values", len(inputs))
		}
		for i := range inputs {
			if i >= len(args.Users) || strings.TrimSpace(args.Users[i]) == "" {
				return nodeAddPromptMsg{
					streamID: streamID,
					call:     call,
					field:    "user",
					list:     "users",
					index:    i,
					label:    fmt.Sprintf("SSH username for %s", nodeAddPromptTarget(inputs, names, i)),
					secret:   false,
				}, false, nil
			}
		}
	}
	if args.Password == "" {
		if len(args.Passwords) > len(inputs) {
			return nodeAddPromptMsg{}, false, fmt.Errorf("passwords must contain at most %d values", len(inputs))
		}
		for i := range inputs {
			if i >= len(args.Passwords) || strings.TrimSpace(args.Passwords[i]) == "" {
				return nodeAddPromptMsg{
					streamID: streamID,
					call:     call,
					field:    "password",
					list:     "passwords",
					index:    i,
					label:    fmt.Sprintf("SSH password for %s", nodeAddPromptTarget(inputs, names, i)),
					secret:   true,
				}, false, nil
			}
		}
	}
	return nodeAddPromptMsg{}, true, nil
}

func nodeAddPromptTarget(inputs, names []string, index int) string {
	host := inputs[index]
	name := host
	if len(names) > index && names[index] != "" {
		name = names[index]
	}
	if name == host {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, host)
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

func setNodeAddArgListValue(raw json.RawMessage, field string, index int, value string) (json.RawMessage, error) {
	if index < 0 {
		return nil, fmt.Errorf("%s index must be non-negative", field)
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, fmt.Errorf("node_add arguments must be an object")
	}
	var values []string
	if existing, ok := args[field]; ok && len(existing) > 0 {
		if err := json.Unmarshal(existing, &values); err != nil {
			return nil, err
		}
	}
	for len(values) <= index {
		values = append(values, "")
	}
	values[index] = value
	encoded, err := json.Marshal(values)
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
		inputs := nodeadd.SplitCommaList(args.Host)
		if len(inputs) == 0 {
			return nodeAddLocalResult(streamID, call, "invalid node_add arguments: host is required", false)
		}
		names := nodeadd.SplitCommaList(args.Name)
		if len(names) > 0 && len(names) != len(inputs) {
			return nodeAddLocalResult(streamID, call, fmt.Sprintf("invalid node_add arguments: name must be empty or contain %d comma-separated values", len(inputs)), false)
		}
		if len(args.Users) > 0 && len(args.Users) != len(inputs) {
			return nodeAddLocalResult(streamID, call, fmt.Sprintf("invalid node_add arguments: users must contain %d values", len(inputs)), false)
		}
		if len(args.Passwords) > 0 && len(args.Passwords) != len(inputs) {
			return nodeAddLocalResult(streamID, call, fmt.Sprintf("invalid node_add arguments: passwords must contain %d values", len(inputs)), false)
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

		results := make([]nodeadd.Result, 0, len(inputs))
		outputs := make([]string, 0, len(inputs))
		for i, input := range inputs {
			currentArgs := args
			currentArgs.Host = input
			currentArgs.Name = ""
			if len(names) > 0 {
				currentArgs.Name = names[i]
			}
			currentReq := req
			currentReq.Input = input
			currentReq.Name = currentArgs.Name
			if len(args.Users) > 0 {
				currentArgs.User = args.Users[i]
				currentReq.Username = args.Users[i]
			}
			if len(args.Passwords) > 0 {
				currentArgs.Password = args.Passwords[i]
				currentReq.Password = args.Passwords[i]
			}

			result, err := runner.Add(parentCtx, currentReq)
			if err != nil {
				return nodeAddLocalResult(streamID, call, "node_add failed: "+redactNodeAddError(err, currentArgs), false)
			}
			output, normalized := formatNodeAddResultOutput(result, currentReq)
			results = append(results, normalized)
			outputs = append(outputs, output)
		}
		return nodeAddResultMsg{streamID: streamID, Call: call, Result: results[0], Results: results, Cluster: req.ClusterName, TLS: req.TLS, Output: strings.Join(outputs, "\n\n")}
	}
}

func formatNodeAddResultOutput(result nodeadd.Result, req nodeadd.Request) (string, nodeadd.Result) {
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
	return output, result
}

func nodeAddResults(primary nodeadd.Result, results []nodeadd.Result) []nodeadd.Result {
	if len(results) > 0 {
		return results
	}
	if strings.TrimSpace(primary.Node.Name) == "" {
		return nil
	}
	return []nodeadd.Result{primary}
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
