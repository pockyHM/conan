package agentupdate

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, command string) (string, error)
}

type Applier struct {
	Arch    func() string
	TempDir string
	Runner  CommandRunner
}

type ApplyResult struct {
	BinaryPath string
	Arch       string
}

type shellRunner struct{}

func (shellRunner) Run(ctx context.Context, command string) (string, error) {
	out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed == "" {
			return trimmed, err
		}
		return trimmed, fmt.Errorf("%w: %s", err, trimmed)
	}
	return trimmed, nil
}

func (a Applier) Apply(ctx context.Context, req Request) (ApplyResult, error) {
	arch := runtime.GOARCH
	if a.Arch != nil {
		arch = a.Arch()
	}

	payload := req.Binary
	if payload == "" {
		payload = req.Binaries[arch]
	}
	if payload == "" {
		return ApplyResult{}, fmt.Errorf("no binary payload for architecture %s", arch)
	}

	binary, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("decode binary: %w", err)
	}
	if len(binary) == 0 {
		return ApplyResult{}, fmt.Errorf("binary payload is empty")
	}

	tempDir := a.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	nanos := time.Now().UnixNano()
	binaryTmp := filepath.Join(tempDir, fmt.Sprintf("conan-agent.%d", nanos))
	configTmp := filepath.Join(tempDir, fmt.Sprintf("conan-agent-config.%d", nanos))
	unitTmp := filepath.Join(tempDir, fmt.Sprintf("conan-agent.service.%d", nanos))

	if err := os.WriteFile(binaryTmp, binary, 0755); err != nil {
		return ApplyResult{}, err
	}
	if err := os.WriteFile(configTmp, []byte(req.Config), 0600); err != nil {
		return ApplyResult{}, err
	}
	if err := os.WriteFile(unitTmp, []byte(req.SystemdUnit), 0644); err != nil {
		return ApplyResult{}, err
	}

	runner := a.Runner
	if runner == nil {
		runner = shellRunner{}
	}

	commands := []string{
		fmt.Sprintf("install -m 0755 %s %s", shellQuote(binaryTmp), shellQuote(req.RemoteBinaryPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.Dir(req.RemoteConfigPath))),
		fmt.Sprintf("install -m 0600 %s %s", shellQuote(configTmp), shellQuote(req.RemoteConfigPath)),
		fmt.Sprintf("install -m 0644 %s %s", shellQuote(unitTmp), shellQuote(req.SystemdUnitPath)),
		"systemctl daemon-reload",
		"systemctl enable --now conan-agent",
		"systemctl restart conan-agent",
	}
	for _, command := range commands {
		if _, err := runner.Run(ctx, command); err != nil {
			return ApplyResult{}, fmt.Errorf("agent update command failed: %w", err)
		}
	}

	return ApplyResult{BinaryPath: req.RemoteBinaryPath, Arch: arch}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
