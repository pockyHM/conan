package nodeadd

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

type fakeCredentialStore struct {
	values map[string]credentials.Credential
}

func (f *fakeCredentialStore) Get(key string) (credentials.Credential, bool, error) {
	cred, ok := f.values[key]
	return cred, ok, nil
}

func (f *fakeCredentialStore) Put(key string, cred credentials.Credential) error {
	if f.values == nil {
		f.values = map[string]credentials.Credential{}
	}
	f.values[key] = cred
	return nil
}

type fakePrompter struct {
	username string
	password string
	ip       string
}

func (f fakePrompter) PromptUsername(defaultValue string) (string, error) { return f.username, nil }
func (f fakePrompter) PromptPassword() (string, error)                    { return f.password, nil }
func (f fakePrompter) PromptIP(hostname string) (string, error)           { return f.ip, nil }

type fakeResolver struct{ ips []net.IP }

func (f fakeResolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) { return f.ips, nil }

type fakeNodeWriter struct {
	written configschema.NodeConfig
	opts    WriteOptions
}

func (f *fakeNodeWriter) Write(cluster string, node configschema.NodeConfig, opts WriteOptions) (configschema.NodeConfig, bool, error) {
	f.written = node
	f.opts = opts
	return node, false, nil
}

type fakeDeployer struct{ target deploy.Target }

func (f *fakeDeployer) Deploy(ctx context.Context, target deploy.Target) error {
	f.target = target
	return nil
}

type fakeHealthChecker struct{ called bool }

func (f *fakeHealthChecker) Check(ctx context.Context, host string, port int, tls bool, token string) error {
	f.called = true
	return nil
}

func TestServiceAddsNodePromptsIPAndDeploys(t *testing.T) {
	creds := &fakeCredentialStore{values: map[string]credentials.Credential{}}
	writer := &fakeNodeWriter{}
	deployer := &fakeDeployer{}
	health := &fakeHealthChecker{}
	svc := Service{Credentials: creds, Prompter: fakePrompter{username: "deploy", password: "secret", ip: "10.0.0.11"}, Resolver: fakeResolver{}, Writer: writer, Deployer: deployer, Health: health}

	_, err := svc.Add(context.Background(), Request{Home: "/tmp/conan", ClusterName: "prod", Input: "web-1", AgentPort: 9280, SSHPort: 22, DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if writer.written.Name != "web-1" || writer.written.Host != "10.0.0.11" {
		t.Fatalf("written node = %+v", writer.written)
	}
	if writer.written.Agent.User != "deploy" || writer.written.Agent.Port != 9280 || writer.written.Agent.Token == "" {
		t.Fatalf("written agent = %+v", writer.written.Agent)
	}
	if deployer.target.Username != "deploy" || deployer.target.Password != "secret" || deployer.target.Token != writer.written.Agent.Token {
		t.Fatalf("deploy target = %+v", deployer.target)
	}
	if !health.called {
		t.Fatal("health check was not called")
	}
	if _, ok := creds.values["ssh/prod/web-1"]; !ok {
		t.Fatal("credentials were not saved")
	}
}

func TestServiceNoDeploySkipsDeployerAndHealth(t *testing.T) {
	writer := &fakeNodeWriter{}
	deployer := &fakeDeployer{}
	health := &fakeHealthChecker{}
	svc := Service{Credentials: &fakeCredentialStore{}, Prompter: fakePrompter{}, Resolver: fakeResolver{ips: []net.IP{net.ParseIP("10.0.0.11")}}, Writer: writer, Deployer: deployer, Health: health}

	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9280, NoDeploy: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if deployer.target.Host != "" {
		t.Fatalf("deployer was called: %+v", deployer.target)
	}
	if health.called {
		t.Fatal("health check was called")
	}
}

func TestServiceUsesSavedCredentials(t *testing.T) {
	creds := &fakeCredentialStore{values: map[string]credentials.Credential{"ssh/prod/web-1": {Username: "saved", Password: "saved-pass"}}}
	deployer := &fakeDeployer{}
	svc := Service{Credentials: creds, Prompter: fakePrompter{username: "prompt", password: "prompt-pass"}, Resolver: fakeResolver{ips: []net.IP{net.ParseIP("10.0.0.11")}}, Writer: &fakeNodeWriter{}, Deployer: deployer, Health: &fakeHealthChecker{}}

	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9280, SSHPort: 22, DeployConfig: configschema.AgentDeployConfig{RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"}})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if deployer.target.Username != "saved" || deployer.target.Password != "saved-pass" {
		t.Fatalf("target credentials = %s/%s", deployer.target.Username, deployer.target.Password)
	}
}

func TestServiceRequiresIPWhenHostnameUnresolved(t *testing.T) {
	svc := Service{Credentials: &fakeCredentialStore{}, Prompter: fakePrompter{ip: ""}, Resolver: fakeResolver{}, Writer: &fakeNodeWriter{}, Deployer: &fakeDeployer{}, Health: &fakeHealthChecker{}}
	_, err := svc.Add(context.Background(), Request{ClusterName: "prod", Input: "web-1", AgentPort: 9280, SSHPort: 22})
	if err == nil || !strings.Contains(err.Error(), "ip address is required") {
		t.Fatalf("err = %v", err)
	}
}
