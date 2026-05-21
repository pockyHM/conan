package nodeadd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Request struct {
	Home             string
	ClusterName      string
	Input            string
	Name             string
	Username         string
	Password         string
	SSHPort          int
	AgentPort        int
	NoDeploy         bool
	Update           bool
	RotateToken      bool
	AgentBinOverride string
	DeployConfig     configschema.AgentDeployConfig
	KnownHostsPath   string
	TLS              bool
}

type Result struct {
	Node     configschema.NodeConfig
	Deployed bool
}

type CredentialStore interface {
	Get(key string) (credentials.Credential, bool, error)
	Put(key string, cred credentials.Credential) error
}

type Prompter interface {
	PromptUsername(defaultValue string) (string, error)
	PromptPassword() (string, error)
	PromptIP(hostname string) (string, error)
}

type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]net.IP, error)
}

type NodeWriter interface {
	Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error)
}

type WriteOptions struct {
	Update      bool
	RotateToken bool
}

type Deployer interface {
	Deploy(ctx context.Context, target deploy.Target) error
}

type HealthChecker interface {
	Check(ctx context.Context, host string, port int, tls bool, token string) error
}

type Service struct {
	Credentials CredentialStore
	Prompter    Prompter
	Resolver    Resolver
	Writer      NodeWriter
	Deployer    Deployer
	Health      HealthChecker
}

var ErrAuthFailed = fmt.Errorf("ssh authentication failed")

func isAuthFailed(err error) bool {
	return errors.Is(err, ErrAuthFailed) || errors.Is(err, deploy.ErrAuthFailed)
}

func (s Service) Add(ctx context.Context, req Request) (Result, error) {
	if req.ClusterName == "" {
		return Result{}, fmt.Errorf("cluster name is required")
	}
	if req.Input == "" {
		return Result{}, fmt.Errorf("node host or ip is required")
	}
	if req.AgentPort == 0 {
		req.AgentPort = 9280
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}

	name := req.Name
	if name == "" {
		name = req.Input
	}
	host, err := s.resolveHost(ctx, req.Input)
	if err != nil {
		return Result{}, err
	}

	username := req.Username
	password := req.Password
	credKey := fmt.Sprintf("ssh/%s/%s", req.ClusterName, name)
	if !req.NoDeploy && username == "" && password == "" && s.Credentials != nil {
		if saved, ok, err := s.Credentials.Get(credKey); err != nil {
			return Result{}, err
		} else if ok {
			username = saved.Username
			password = saved.Password
		}
	}
	if !req.NoDeploy {
		if username == "" {
			username, err = s.Prompter.PromptUsername("")
			if err != nil {
				return Result{}, err
			}
		}
		if password == "" {
			password, err = s.Prompter.PromptPassword()
			if err != nil {
				return Result{}, err
			}
		}
	}

	token, err := deploy.GenerateToken()
	if err != nil {
		return Result{}, err
	}
	node := configschema.NodeConfig{Name: name, Host: host, Agent: &configschema.NodeAgentOverride{User: username, Port: req.AgentPort, Token: token}}
	written, _, err := s.Writer.Write(req.ClusterName, node, WriteOptions{Update: req.Update, RotateToken: req.RotateToken})
	if err != nil {
		return Result{}, err
	}
	if written.Agent != nil && written.Agent.Token != "" {
		token = written.Agent.Token
	}

	if req.NoDeploy {
		return Result{Node: written}, nil
	}
	target := deploy.Target{Host: host, SSHPort: req.SSHPort, Username: username, Password: password, AgentPort: req.AgentPort, Token: token, AgentBinOverride: req.AgentBinOverride, Config: req.DeployConfig, KnownHostsPath: req.KnownHostsPath}
	if err := s.Deployer.Deploy(ctx, target); err != nil {
		if !isAuthFailed(err) {
			return Result{}, err
		}
		username, err = s.Prompter.PromptUsername("")
		if err != nil {
			return Result{}, err
		}
		password, err = s.Prompter.PromptPassword()
		if err != nil {
			return Result{}, err
		}
		target.Username = username
		target.Password = password
		if err := s.Deployer.Deploy(ctx, target); err != nil {
			return Result{}, err
		}
	}
	if s.Credentials != nil {
		if err := s.Credentials.Put(credKey, credentials.Credential{Username: username, Password: password}); err != nil {
			return Result{}, err
		}
	}
	if s.Health != nil {
		if err := s.Health.Check(ctx, host, req.AgentPort, req.TLS, token); err != nil {
			return Result{}, err
		}
	}
	return Result{Node: written, Deployed: true}, nil
}

func (s Service) resolveHost(ctx context.Context, input string) (string, error) {
	if ip := net.ParseIP(input); ip != nil {
		return input, nil
	}
	ips, err := s.Resolver.LookupHost(ctx, input)
	if err == nil && len(ips) > 0 {
		return input, nil
	}
	ip, err := s.Prompter.PromptIP(input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ip) == "" {
		return "", fmt.Errorf("ip address is required for unresolved hostname %s", input)
	}
	return strings.TrimSpace(ip), nil
}

type NetResolver struct{}

func (NetResolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type ConfigNodeWriter struct{ Home string }

func (w ConfigNodeWriter) Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error) {
	result, err := cfgloader.NewNodeWriter(w.Home).WriteNode(cluster, node, cfgloader.WriteNodeOptions{Update: opts.Update, RotateToken: opts.RotateToken})
	return result.Node, result.Updated, err
}

type MCPHealthChecker struct{}

func (MCPHealthChecker) Check(ctx context.Context, host string, port int, tls bool, token string) error {
	return mcp.NewClient(mcp.Config{BaseURL: mcp.URL(host, port, tls), Token: token}).Ping(ctx)
}
