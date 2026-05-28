package nodeupdate

import (
	"context"
	"strings"
	"testing"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

func TestUpdateSingleNodeByHostUsesSavedCredentialsAndClusterDefaults(t *testing.T) {
	creds := &fakeCredentialStore{records: map[string]credentials.Credential{
		"ssh/prod/web-1": {Username: "deploy", Password: "secret"},
	}}
	deployer := &fakeDeployer{}
	service := Service{Credentials: creds, Deployer: deployer}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "web-1.example.com"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "web-1.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	results, err := service.Update(context.Background(), Request{
		ClusterName:  "prod",
		Cluster:      cluster,
		Selector:     "web-1.example.com",
		DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(results) != 1 || results[0].NodeName != "web-1" {
		t.Fatalf("results = %#v", results)
	}
	if len(deployer.targets) != 1 {
		t.Fatalf("deploy calls = %d, want 1", len(deployer.targets))
	}
	target := deployer.targets[0]
	if target.Host != "web-1.example.com" || target.SSHPort != 2222 || target.Username != "deploy" || target.Password != "secret" {
		t.Fatalf("target ssh fields = %+v", target)
	}
	if target.AgentPort != 9281 || target.Token != "node-token" {
		t.Fatalf("target agent fields = %+v", target)
	}
	if got := creds.records["ssh/prod/web-1"]; got.Username != "deploy" || got.Password != "secret" {
		t.Fatalf("saved credential = %+v", got)
	}
}

func TestUpdateAllNodes(t *testing.T) {
	deployer := &fakeDeployer{}
	service := Service{
		Credentials: &fakeCredentialStore{},
		Prompter:    fakePrompter{username: "root", password: "secret"},
		Deployer:    deployer,
	}
	cluster := testCluster([]cfgloader.Node{
		{NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"}, Agent: cfgloader.EffectiveAgentConfig{Host: "10.0.0.1", Port: 9280, User: "root", Token: "one"}},
		{NodeConfig: configschema.NodeConfig{Name: "web-2", Host: "10.0.0.2"}, Agent: cfgloader.EffectiveAgentConfig{Host: "10.0.0.2", Port: 9280, User: "root", Token: "two"}},
	})

	results, err := service.Update(context.Background(), Request{ClusterName: "prod", Cluster: cluster, All: true})
	if err != nil {
		t.Fatalf("update all: %v", err)
	}
	if len(results) != 2 || len(deployer.targets) != 2 {
		t.Fatalf("results=%d deploys=%d, want 2", len(results), len(deployer.targets))
	}
	if deployer.targets[0].Host != "10.0.0.1" || deployer.targets[1].Host != "10.0.0.2" {
		t.Fatalf("targets = %+v", deployer.targets)
	}
}

func TestUpdateNodeNotFound(t *testing.T) {
	service := Service{Deployer: &fakeDeployer{}}
	_, err := service.Update(context.Background(), Request{
		ClusterName: "prod",
		Cluster:     testCluster(nil),
		Selector:    "missing",
	})
	if err == nil || !strings.Contains(err.Error(), "node not found: missing") {
		t.Fatalf("err = %v", err)
	}
}

func testCluster(nodes []cfgloader.Node) *cfgloader.Cluster {
	return &cfgloader.Cluster{
		Cluster: configschema.ClusterConfig{NodeDefaults: configschema.NodeDefaults{User: "root", SSHPort: 2222}},
		Nodes:   nodes,
	}
}

type fakeCredentialStore struct {
	records map[string]credentials.Credential
}

func (s *fakeCredentialStore) Get(key string) (credentials.Credential, bool, error) {
	if s.records == nil {
		return credentials.Credential{}, false, nil
	}
	cred, ok := s.records[key]
	return cred, ok, nil
}

func (s *fakeCredentialStore) Put(key string, cred credentials.Credential) error {
	if s.records == nil {
		s.records = map[string]credentials.Credential{}
	}
	s.records[key] = cred
	return nil
}

type fakeDeployer struct {
	targets []deploy.Target
}

func (d *fakeDeployer) Deploy(_ context.Context, target deploy.Target) error {
	d.targets = append(d.targets, target)
	return nil
}

type fakePrompter struct {
	username string
	password string
}

func (p fakePrompter) PromptUsername(defaultValue string) (string, error) {
	if p.username == "" {
		return defaultValue, nil
	}
	return p.username, nil
}

func (p fakePrompter) PromptPassword() (string, error) {
	return p.password, nil
}
