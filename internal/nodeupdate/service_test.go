package nodeupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestUpdateAutoUsesSSHAndSkipsAgentWhenSSHWorks(t *testing.T) {
	creds := &fakeCredentialStore{records: map[string]credentials.Credential{
		"ssh/prod/web-1": {Username: "deploy", Password: "secret"},
	}}
	deployer := &fakeDeployer{}
	agentUpdater := &fakeAgentUpdater{}
	service := Service{Credentials: creds, Deployer: deployer, AgentUpdater: agentUpdater}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "web-1.example.com"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "web-1.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "agent binary"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(deployer.targets) != 1 {
		t.Fatalf("deploy calls = %d, want 1", len(deployer.targets))
	}
	if len(agentUpdater.targets) != 0 {
		t.Fatalf("agent calls = %d, want 0", len(agentUpdater.targets))
	}
}

func TestUpdateAutoFallsBackToAgentWhenSSHFails(t *testing.T) {
	deployer := &fakeDeployer{err: errors.New("ssh connection refused")}
	agentUpdater := &fakeAgentUpdater{}
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     deployer,
		AgentUpdater: agentUpdater,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "agent.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "agent binary"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(agentUpdater.targets) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agentUpdater.targets))
	}
	target := agentUpdater.targets[0]
	if target.Host != "agent.example.com" || target.Port != 9281 || target.Token != "node-token" {
		t.Fatalf("agent target = %+v", target)
	}
}

func TestUpdateAutoReturnsBothErrorsWhenFallbackFails(t *testing.T) {
	deployer := &fakeDeployer{err: errors.New("ssh failed")}
	agentUpdater := &fakeAgentUpdater{err: errors.New("agent failed")}
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     deployer,
		AgentUpdater: agentUpdater,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "agent.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAuto,
		AgentBinOverride: testAgentBinary(t, "agent binary"),
		DeployConfig:     testDeployConfig(t),
	})
	if err == nil || !strings.Contains(err.Error(), "ssh update failed") || !strings.Contains(err.Error(), "agent update fallback failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateAgentModeSkipsSSHCredentialsAndPrompts(t *testing.T) {
	deployer := &fakeDeployer{}
	agentUpdater := &fakeAgentUpdater{}
	service := Service{
		Prompter:     fakePrompter{err: errors.New("prompt called")},
		Deployer:     deployer,
		AgentUpdater: agentUpdater,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "agent.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName:      "prod",
		Cluster:          cluster,
		Selector:         "web-1",
		Mode:             ModeAgent,
		AgentBinOverride: testAgentBinary(t, "agent binary"),
		DeployConfig:     testDeployConfig(t),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(deployer.targets) != 0 {
		t.Fatalf("deploy calls = %d, want 0", len(deployer.targets))
	}
	if len(agentUpdater.targets) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agentUpdater.targets))
	}
}

func TestUpdateSSHModeDoesNotCallAgent(t *testing.T) {
	deployer := &fakeDeployer{err: errors.New("ssh failed")}
	agentUpdater := &fakeAgentUpdater{}
	service := Service{
		Credentials:  &fakeCredentialStore{records: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "deploy", Password: "secret"}}},
		Deployer:     deployer,
		AgentUpdater: agentUpdater,
	}
	cluster := testCluster([]cfgloader.Node{{
		NodeConfig: configschema.NodeConfig{Name: "web-1", Host: "10.0.0.1"},
		Agent:      cfgloader.EffectiveAgentConfig{Host: "agent.example.com", Port: 9281, User: "root", Token: "node-token"},
	}})

	_, err := service.Update(context.Background(), Request{
		ClusterName: "prod",
		Cluster:     cluster,
		Selector:    "web-1",
		Mode:        ModeSSH,
	})
	if err == nil || !strings.Contains(err.Error(), "ssh failed") {
		t.Fatalf("err = %v", err)
	}
	if len(agentUpdater.targets) != 0 {
		t.Fatalf("agent calls = %d, want 0", len(agentUpdater.targets))
	}
}

func testCluster(nodes []cfgloader.Node) *cfgloader.Cluster {
	return &cfgloader.Cluster{
		Cluster: configschema.ClusterConfig{NodeDefaults: configschema.NodeDefaults{User: "root", SSHPort: 2222}},
		Nodes:   nodes,
	}
}

func testAgentBinary(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "conan-agent")
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatalf("write test agent binary: %v", err)
	}
	return path
}

func testDeployConfig(t *testing.T) configschema.AgentDeployConfig {
	t.Helper()

	return configschema.AgentDeployConfig{
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
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
	err     error
}

func (d *fakeDeployer) Deploy(_ context.Context, target deploy.Target) error {
	d.targets = append(d.targets, target)
	return d.err
}

type fakeAgentUpdater struct {
	targets []AgentTarget
	err     error
}

func (u *fakeAgentUpdater) Update(_ context.Context, target AgentTarget) error {
	u.targets = append(u.targets, target)
	return u.err
}

type fakePrompter struct {
	username string
	password string
	err      error
}

func (p fakePrompter) PromptUsername(defaultValue string) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.username == "" {
		return defaultValue, nil
	}
	return p.username, nil
}

func (p fakePrompter) PromptPassword() (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return p.password, nil
}
