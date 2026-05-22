package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/pkg/configschema"
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
		output := fmt.Sprintf("node added and deployed: %s\ncluster: %s\nhost: %s\nagent_port: %d\nhealth: ok", name, req.ClusterName, host, agentPort)
		return nodeAddLocalResult(streamID, call, output, true)
	}
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
