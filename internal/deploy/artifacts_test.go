package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestArchFromUname(t *testing.T) {
	cases := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
	for input, want := range cases {
		got, err := ArchFromUname(input)
		if err != nil {
			t.Fatalf("ArchFromUname(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ArchFromUname(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ArchFromUname("mips"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestResolveAgentBinary(t *testing.T) {
	home := t.TempDir()
	amd64 := filepath.Join(home, "amd64", "conan-agent")
	if err := os.MkdirAll(filepath.Dir(amd64), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(amd64, []byte("binary"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	path, err := ResolveAgentBinary(configschema.AgentDeployConfig{Binaries: configschema.AgentBinaryConfig{AMD64: amd64}}, "amd64", "")
	if err != nil {
		t.Fatalf("ResolveAgentBinary: %v", err)
	}
	if path != amd64 {
		t.Fatalf("path = %q, want %q", path, amd64)
	}
	if _, err := ResolveAgentBinary(configschema.AgentDeployConfig{}, "arm64", ""); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestRenderAgentConfig(t *testing.T) {
	config := RenderAgentConfig(9300, "node-token")
	for _, want := range []string{"listen: 0.0.0.0:9300", "token: node-token", "tls: false", "rate_limit: 10", "log_level: info"} {
		if !strings.Contains(config, want) {
			t.Fatalf("agent config missing %q:\n%s", want, config)
		}
	}
}

func TestRenderSystemdUnitUsesPaths(t *testing.T) {
	unit := RenderSystemdUnit("/opt/conan/conan-agent", "/etc/conan/custom.yaml")
	if !strings.Contains(unit, "ExecStart=/opt/conan/conan-agent run -c /etc/conan/custom.yaml") {
		t.Fatalf("unit =\n%s", unit)
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Fatalf("unit missing restart policy:\n%s", unit)
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken a: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken b: %v", err)
	}
	if len(a) < 40 {
		t.Fatalf("token too short: %q", a)
	}
	if a == b {
		t.Fatalf("tokens should be unique: %q", a)
	}
}
