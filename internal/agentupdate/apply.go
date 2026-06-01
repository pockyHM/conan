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

	binaryTmp, err := writeTempFile(tempDir, "conan-agent.*", binary, 0755)
	if err != nil {
		return ApplyResult{}, err
	}
	defer os.Remove(binaryTmp)
	configTmp, err := writeTempFile(tempDir, "conan-agent-config.*", []byte(req.Config), 0600)
	if err != nil {
		return ApplyResult{}, err
	}
	defer os.Remove(configTmp)
	unitTmp, err := writeTempFile(tempDir, "conan-agent.service.*", []byte(req.SystemdUnit), 0644)
	if err != nil {
		return ApplyResult{}, err
	}
	defer os.Remove(unitTmp)

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
			return ApplyResult{}, fmt.Errorf("agent update command failed: %s", sanitizeCommandError(err.Error(), req))
		}
	}

	return ApplyResult{BinaryPath: req.RemoteBinaryPath, Arch: arch}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func sanitizeCommandError(message string, req Request) string {
	sanitized := message
	if req.Config != "" {
		sanitized = strings.ReplaceAll(sanitized, req.Config, "[redacted config]")
	}
	for _, line := range strings.Split(req.Config, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "token" {
			continue
		}
		token := strings.TrimSpace(value)
		if token != "" {
			sanitized = strings.ReplaceAll(sanitized, token, "[redacted token]")
		}
	}
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "redacted command error"
	}
	return sanitized
}

func writeTempFile(dir, pattern string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(name)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	removeOnError = false
	return name, nil
}
