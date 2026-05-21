package deploy

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/pockyHM/conan/pkg/configschema"
)

func ArchFromUname(uname string) (string, error) {
	switch strings.TrimSpace(uname) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote architecture: %s", strings.TrimSpace(uname))
	}
}

func ResolveAgentBinary(cfg configschema.AgentDeployConfig, arch string, override string) (string, error) {
	path := override
	if path == "" {
		switch arch {
		case "amd64":
			path = cfg.Binaries.AMD64
		case "arm64":
			path = cfg.Binaries.ARM64
		default:
			return "", fmt.Errorf("unsupported agent architecture: %s", arch)
		}
	}
	if path == "" {
		return "", fmt.Errorf("agent binary path is empty for architecture %s", arch)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("agent binary not found at %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("agent binary path is a directory: %s", path)
	}
	return path, nil
}

func RenderAgentConfig(port int, token string) string {
	return fmt.Sprintf("listen: 0.0.0.0:%d\ntoken: %s\ntls: false\nrate_limit: 10\nlog_level: info\n", port, token)
}

func RenderSystemdUnit(binaryPath string, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Conan Agent
After=network-online.target

[Service]
ExecStart=%s run -c %s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, binaryPath, configPath)
}

func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
