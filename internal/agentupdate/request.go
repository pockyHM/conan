package agentupdate

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/pkg/configschema"
)

type Request struct {
	Binary           string            `json:"binary,omitempty"`
	Binaries         map[string]string `json:"binaries,omitempty"`
	Config           string            `json:"config"`
	SystemdUnit      string            `json:"systemd_unit"`
	RemoteBinaryPath string            `json:"remote_binary_path"`
	RemoteConfigPath string            `json:"remote_config_path"`
	SystemdUnitPath  string            `json:"systemd_unit_path"`
}

type BuildOptions struct {
	DeployConfig     configschema.AgentDeployConfig
	AgentPort        int
	Token            string
	AgentBinOverride string
}

func BuildRequest(opts BuildOptions) (Request, error) {
	req := Request{
		Config:           deploy.RenderAgentConfig(opts.AgentPort, opts.Token),
		SystemdUnit:      deploy.RenderSystemdUnit(opts.DeployConfig.RemoteBinaryPath, opts.DeployConfig.RemoteConfigPath),
		RemoteBinaryPath: opts.DeployConfig.RemoteBinaryPath,
		RemoteConfigPath: opts.DeployConfig.RemoteConfigPath,
		SystemdUnitPath:  opts.DeployConfig.SystemdUnitPath,
	}

	if opts.AgentBinOverride != "" {
		binary, err := readBase64(opts.AgentBinOverride)
		if err != nil {
			return Request{}, fmt.Errorf("read override agent binary: %w", err)
		}
		req.Binary = binary
		return req, nil
	}

	binaries := map[string]string{}
	failures := []string{}
	if binary, err := readBase64(opts.DeployConfig.Binaries.AMD64); err == nil {
		binaries["amd64"] = binary
	} else {
		failures = append(failures, fmt.Sprintf("amd64: %v", err))
	}
	if binary, err := readBase64(opts.DeployConfig.Binaries.ARM64); err == nil {
		binaries["arm64"] = binary
	} else {
		failures = append(failures, fmt.Sprintf("arm64: %v", err))
	}

	if len(binaries) == 0 {
		return Request{}, fmt.Errorf("no architecture binaries could be read: %s", strings.Join(failures, "; "))
	}
	req.Binaries = binaries

	return req, nil
}

func readBase64(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("file is empty")
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
