package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

type fakeRemote struct {
	outputs  map[string]string
	commands []string
	uploads  map[string]string
}

func (f *fakeRemote) Run(ctx context.Context, command string, stdin string) (string, error) {
	f.commands = append(f.commands, command)
	return f.outputs[command], nil
}

func (f *fakeRemote) Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error {
	if f.uploads == nil {
		f.uploads = map[string]string{}
	}
	f.uploads[remotePath] = string(contents)
	return nil
}

func TestDeployerUploadsArtifactsAndRunsSystemdCommands(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "conan-agent")
	if err := os.WriteFile(binary, []byte("agent-binary"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	remote := &fakeRemote{outputs: map[string]string{"uname -m": "x86_64\n"}}
	deployer := NewDeployer(func(Target) (Remote, error) { return remote, nil })

	err := deployer.Deploy(context.Background(), Target{
		Host: "10.0.0.11", SSHPort: 22, Username: "deploy", Password: "secret", AgentPort: 9280, Token: "node-token",
		Config: configschema.AgentDeployConfig{Binaries: configschema.AgentBinaryConfig{AMD64: binary}, RemoteBinaryPath: "/usr/local/bin/conan-agent", RemoteConfigPath: "/etc/conan-agent/config.yaml", SystemdUnitPath: "/etc/systemd/system/conan-agent.service"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(remote.uploads) != 3 {
		t.Fatalf("uploads = %#v", remote.uploads)
	}
	joined := strings.Join(remote.commands, "\n")
	for _, want := range []string{"uname -m", "sudo -S install -m 0755", "sudo -S systemctl daemon-reload", "sudo -S systemctl enable --now conan-agent", "sudo -S systemctl restart conan-agent"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "secret") || strings.Contains(joined, "node-token") {
		t.Fatalf("commands leaked secret:\n%s", joined)
	}
}

func TestDeployerUnsupportedArchFailsBeforeUpload(t *testing.T) {
	remote := &fakeRemote{outputs: map[string]string{"uname -m": "mips\n"}}
	deployer := NewDeployer(func(Target) (Remote, error) { return remote, nil })
	err := deployer.Deploy(context.Background(), Target{Host: "10.0.0.11", Username: "deploy", Password: "secret", AgentPort: 9280})
	if err == nil || !strings.Contains(err.Error(), "unsupported remote architecture") {
		t.Fatalf("err = %v", err)
	}
	if len(remote.uploads) != 0 {
		t.Fatalf("uploads happened before arch failure: %#v", remote.uploads)
	}
}
