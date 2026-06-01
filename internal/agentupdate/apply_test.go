package agentupdate

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestApplierRejectsInvalidBinary(t *testing.T) {
	_, err := Applier{
		Arch:    func() string { return "amd64" },
		TempDir: t.TempDir(),
		Runner:  &fakeRunner{},
	}.Apply(context.Background(), Request{
		Binary:           "not-base64",
		Config:           "listen: 0.0.0.0:9280",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})

	if err == nil || !strings.Contains(err.Error(), "decode binary") {
		t.Fatalf("err = %v, want decode binary", err)
	}
}

func TestApplierSelectsBinaryForProcessArchitecture(t *testing.T) {
	runner := &fakeRunner{}
	result, err := Applier{
		Arch:    func() string { return "arm64" },
		TempDir: t.TempDir(),
		Runner:  runner,
	}.Apply(context.Background(), Request{
		Binaries: map[string]string{
			"amd64": encode("amd64-binary"),
			"arm64": encode("arm64-binary"),
		},
		Config:           "listen: 0.0.0.0:9280",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Arch != "arm64" {
		t.Fatalf("arch = %q, want arm64", result.Arch)
	}
	if result.BinaryPath == "" {
		t.Fatalf("binary path is empty")
	}
	binaryTmp := runner.installedSourceFor("install -m 0755 ")
	data, err := os.ReadFile(binaryTmp)
	if err != nil {
		t.Fatalf("read temp binary: %v", err)
	}
	if string(data) != "arm64-binary" {
		t.Fatalf("temp binary = %q, want arm64-binary", data)
	}
}

func TestApplierWritesFilesWithExpectedPermissionsAndRunsFixedCommands(t *testing.T) {
	runner := &fakeRunner{}
	_, err := Applier{
		Arch:    func() string { return "amd64" },
		TempDir: t.TempDir(),
		Runner:  runner,
	}.Apply(context.Background(), Request{
		Binary:           encode("override-binary"),
		Config:           "listen: 0.0.0.0:9280\ntoken: node-token\n",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	wantCommands := []string{
		"install -m 0755 '/",
		"mkdir -p '/etc/conan-agent'",
		"install -m 0600 '/",
		"install -m 0644 '/",
		"systemctl daemon-reload",
		"systemctl enable --now conan-agent",
		"systemctl restart conan-agent",
	}
	if len(runner.commands) != len(wantCommands) {
		t.Fatalf("commands = %#v, want %d commands", runner.commands, len(wantCommands))
	}
	for i, want := range wantCommands {
		if !strings.HasPrefix(runner.commands[i], want) {
			t.Fatalf("command %d = %q, want prefix %q", i, runner.commands[i], want)
		}
	}

	for _, prefix := range []string{"install -m 0755 ", "install -m 0600 ", "install -m 0644 "} {
		source := runner.installedSourceFor(prefix)
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read source for %q: %v", prefix, err)
		}
		if len(data) == 0 {
			t.Fatalf("source for %q is empty", prefix)
		}
	}
}

func TestApplierReturnsCommandFailureWithoutSecrets(t *testing.T) {
	_, err := Applier{
		Arch:    func() string { return "amd64" },
		TempDir: t.TempDir(),
		Runner:  &fakeRunner{err: errors.New("systemctl failed")},
	}.Apply(context.Background(), Request{
		Binary:           encode("override-binary"),
		Config:           "listen: 0.0.0.0:9280\ntoken: secret-token\n",
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})

	if err == nil {
		t.Fatalf("err = nil, want command failure")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err leaks secret: %v", err)
	}
}

type fakeRunner struct {
	commands []string
	err      error
}

func (r *fakeRunner) Run(_ context.Context, command string) (string, error) {
	r.commands = append(r.commands, command)
	return "", r.err
}

func (r *fakeRunner) installedSourceFor(prefix string) string {
	for _, command := range r.commands {
		if strings.HasPrefix(command, prefix) {
			rest := strings.TrimPrefix(command, prefix)
			if !strings.HasPrefix(rest, "'") {
				return strings.Fields(rest)[0]
			}
			rest = strings.TrimPrefix(rest, "'")
			source, _, _ := strings.Cut(rest, "'")
			return "/" + strings.TrimPrefix(source, "/")
		}
	}
	return ""
}

func encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
