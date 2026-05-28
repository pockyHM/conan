package nodeupdate

import (
	"context"
	"errors"
	"fmt"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Request struct {
	Home             string
	ClusterName      string
	Cluster          *cfgloader.Cluster
	Selector         string
	All              bool
	Username         string
	Password         string
	SSHPort          int
	AgentBinOverride string
	DeployConfig     configschema.AgentDeployConfig
	KnownHostsPath   string
}

type Result struct {
	ClusterName string
	NodeName    string
	Host        string
}

type CredentialStore interface {
	Get(key string) (credentials.Credential, bool, error)
	Put(key string, cred credentials.Credential) error
}

type Prompter interface {
	PromptUsername(defaultValue string) (string, error)
	PromptPassword() (string, error)
}

type Deployer interface {
	Deploy(ctx context.Context, target deploy.Target) error
}

type Service struct {
	Credentials CredentialStore
	Prompter    Prompter
	Deployer    Deployer
}

func (s Service) Update(ctx context.Context, req Request) ([]Result, error) {
	if req.ClusterName == "" {
		return nil, fmt.Errorf("cluster name is required")
	}
	if req.Cluster == nil {
		return nil, fmt.Errorf("cluster is required")
	}
	if s.Deployer == nil {
		return nil, fmt.Errorf("deployer is required")
	}

	nodes, err := selectNodes(req.Cluster.Nodes, req.Selector, req.All)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes to update in cluster %s", req.ClusterName)
	}

	results := make([]Result, 0, len(nodes))
	for _, node := range nodes {
		if err := s.updateNode(ctx, req, node); err != nil {
			return results, fmt.Errorf("update %s/%s: %w", req.ClusterName, node.Name, err)
		}
		results = append(results, Result{ClusterName: req.ClusterName, NodeName: node.Name, Host: node.Host})
	}
	return results, nil
}

func selectNodes(nodes []cfgloader.Node, selector string, all bool) ([]cfgloader.Node, error) {
	if all {
		return append([]cfgloader.Node(nil), nodes...), nil
	}
	if selector == "" {
		return nil, fmt.Errorf("node address, hostname, or name is required")
	}
	for _, node := range nodes {
		if node.Name == selector || node.Host == selector || node.Agent.Host == selector {
			return []cfgloader.Node{node}, nil
		}
	}
	return nil, fmt.Errorf("node not found: %s", selector)
}

func (s Service) updateNode(ctx context.Context, req Request, node cfgloader.Node) error {
	sshPort := req.SSHPort
	if sshPort == 0 {
		sshPort = req.Cluster.Cluster.NodeDefaults.SSHPort
	}
	if sshPort == 0 {
		sshPort = 22
	}

	username := req.Username
	password := req.Password
	if s.Credentials != nil {
		saved, ok, err := s.Credentials.Get(credentialKey(req.ClusterName, node.Name))
		if err != nil {
			return err
		}
		if ok {
			if username == "" {
				username = saved.Username
			}
			if password == "" {
				password = saved.Password
			}
		}
	}
	if username == "" {
		if s.Prompter == nil {
			return fmt.Errorf("ssh username is required")
		}
		var err error
		username, err = s.Prompter.PromptUsername(node.Agent.User)
		if err != nil {
			return err
		}
	}
	if password == "" {
		if s.Prompter == nil {
			return fmt.Errorf("ssh password is required")
		}
		var err error
		password, err = s.Prompter.PromptPassword()
		if err != nil {
			return err
		}
	}

	target := deploy.Target{
		Host:             node.Host,
		SSHPort:          sshPort,
		Username:         username,
		Password:         password,
		AgentPort:        node.Agent.Port,
		Token:            node.Agent.Token,
		AgentBinOverride: req.AgentBinOverride,
		Config:           req.DeployConfig,
		KnownHostsPath:   req.KnownHostsPath,
	}
	if err := s.Deployer.Deploy(ctx, target); err != nil {
		if !isAuthFailed(err) || s.Prompter == nil {
			return err
		}
		var promptErr error
		username, promptErr = s.Prompter.PromptUsername(node.Agent.User)
		if promptErr != nil {
			return promptErr
		}
		password, promptErr = s.Prompter.PromptPassword()
		if promptErr != nil {
			return promptErr
		}
		target.Username = username
		target.Password = password
		if err := s.Deployer.Deploy(ctx, target); err != nil {
			return err
		}
	}
	if s.Credentials != nil {
		return s.Credentials.Put(credentialKey(req.ClusterName, node.Name), credentials.Credential{Username: username, Password: password})
	}
	return nil
}

func credentialKey(clusterName string, nodeName string) string {
	return fmt.Sprintf("ssh/%s/%s", clusterName, nodeName)
}

func isAuthFailed(err error) bool {
	return errors.Is(err, deploy.ErrAuthFailed)
}
