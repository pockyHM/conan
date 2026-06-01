package agentupdate

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestBuildRequestWithOverrideSendsSingleBinary(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "conan-agent")
	if err := os.WriteFile(override, []byte("override-binary"), 0755); err != nil {
		t.Fatalf("write override: %v", err)
	}

	req, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort:        9281,
		Token:            "node-token",
		AgentBinOverride: override,
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if got := decode(t, req.Binary); got != "override-binary" {
		t.Fatalf("binary = %q", got)
	}
	if len(req.Binaries) != 0 {
		t.Fatalf("binaries = %#v, want empty map when override is used", req.Binaries)
	}
	if !strings.Contains(req.Config, "listen: 0.0.0.0:9281") || !strings.Contains(req.Config, "token: node-token") {
		t.Fatalf("config =\n%s", req.Config)
	}
	if !strings.Contains(req.SystemdUnit, "ExecStart=/usr/local/bin/conan-agent run -c /etc/conan-agent/config.yaml") {
		t.Fatalf("systemd unit =\n%s", req.SystemdUnit)
	}
}

func TestBuildRequestWithoutOverrideSendsConfiguredArchitectureBinaries(t *testing.T) {
	dir := t.TempDir()
	amd64 := filepath.Join(dir, "amd64", "conan-agent")
	arm64 := filepath.Join(dir, "arm64", "conan-agent")
	mustWrite(t, amd64, "amd64-binary")
	mustWrite(t, arm64, "arm64-binary")

	req, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			Binaries:         configschema.AgentBinaryConfig{AMD64: amd64, ARM64: arm64},
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort: 9280,
		Token:     "token",
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if req.Binary != "" {
		t.Fatalf("binary override = %q, want empty", req.Binary)
	}
	if got := decode(t, req.Binaries["amd64"]); got != "amd64-binary" {
		t.Fatalf("amd64 binary = %q", got)
	}
	if got := decode(t, req.Binaries["arm64"]); got != "arm64-binary" {
		t.Fatalf("arm64 binary = %q", got)
	}
}

func TestBuildRequestReturnsMissingBinaryError(t *testing.T) {
	_, err := BuildRequest(BuildOptions{
		DeployConfig: configschema.AgentDeployConfig{
			Binaries:         configschema.AgentBinaryConfig{AMD64: "/missing/amd64", ARM64: "/missing/arm64"},
			RemoteBinaryPath: "/usr/local/bin/conan-agent",
			RemoteConfigPath: "/etc/conan-agent/config.yaml",
			SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
		},
		AgentPort: 9280,
		Token:     "token",
	})
	if err == nil || !strings.Contains(err.Error(), "read amd64 agent binary") {
		t.Fatalf("err = %v", err)
	}
}

func mustWrite(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func decode(t *testing.T, encoded string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return string(data)
}
