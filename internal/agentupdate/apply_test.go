package agentupdate

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
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
	runner.onRun = func(command string) error {
		if !strings.HasPrefix(command, "install -m 0755 ") {
			return nil
		}
		binaryTmp := runner.installedSourceFor("install -m 0755 ")
		data, err := os.ReadFile(binaryTmp)
		if err != nil {
			t.Fatalf("read temp binary: %v", err)
		}
		if string(data) != "arm64-binary" {
			t.Fatalf("temp binary = %q, want arm64-binary", data)
		}
		return nil
	}
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
}

func TestApplierWritesFilesWithExpectedPermissionsAndRunsFixedCommands(t *testing.T) {
	runner := &fakeRunner{}
	checkedSources := map[string]bool{}
	runner.onRun = func(command string) error {
		for _, tt := range []struct {
			name   string
			prefix string
			mode   os.FileMode
		}{
			{name: "binary", prefix: "install -m 0755 ", mode: 0755},
			{name: "config", prefix: "install -m 0600 ", mode: 0600},
			{name: "unit", prefix: "install -m 0644 ", mode: 0644},
		} {
			if !strings.HasPrefix(command, tt.prefix) {
				continue
			}
			source := runner.installedSourceFor(tt.prefix)
			info, err := os.Stat(source)
			if err != nil {
				t.Fatalf("stat %s source: %v", tt.name, err)
			}
			if got := info.Mode().Perm(); got != tt.mode {
				t.Fatalf("%s mode = %v, want %v", tt.name, got, tt.mode)
			}
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatalf("read %s source: %v", tt.name, err)
			}
			if len(data) == 0 {
				t.Fatalf("%s source is empty", tt.name)
			}
			checkedSources[tt.name] = true
		}
		return nil
	}
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

	binaryTmp := runner.installedSourceFor("install -m 0755 ")
	configTmp := runner.installedSourceFor("install -m 0600 ")
	unitTmp := runner.installedSourceFor("install -m 0644 ")
	wantCommands := []string{
		"install -m 0755 " + shellQuote(binaryTmp) + " '/usr/local/bin/conan-agent'",
		"mkdir -p '/etc/conan-agent'",
		"install -m 0600 " + shellQuote(configTmp) + " '/etc/conan-agent/config.yaml'",
		"install -m 0644 " + shellQuote(unitTmp) + " '/etc/systemd/system/conan-agent.service'",
		"systemctl daemon-reload",
		"systemctl enable --now conan-agent",
		"sh -c 'sleep 1; systemctl restart conan-agent' >/dev/null 2>&1 &",
	}
	if len(runner.commands) != len(wantCommands) {
		t.Fatalf("commands = %#v, want %d commands", runner.commands, len(wantCommands))
	}
	for i, want := range wantCommands {
		if runner.commands[i] != want {
			t.Fatalf("command %d = %q, want %q", i, runner.commands[i], want)
		}
	}

	if filepath.Base(binaryTmp) == filepath.Base(configTmp) ||
		filepath.Base(binaryTmp) == filepath.Base(unitTmp) ||
		filepath.Base(configTmp) == filepath.Base(unitTmp) {
		t.Fatalf("temp source basenames must be unique: binary=%q config=%q unit=%q", binaryTmp, configTmp, unitTmp)
	}
	if suffixAfterLastDot(binaryTmp) == suffixAfterLastDot(configTmp) ||
		suffixAfterLastDot(binaryTmp) == suffixAfterLastDot(unitTmp) ||
		suffixAfterLastDot(configTmp) == suffixAfterLastDot(unitTmp) {
		t.Fatalf("temp source suffixes must be independently unique: binary=%q config=%q unit=%q", binaryTmp, configTmp, unitTmp)
	}

	for _, name := range []string{"binary", "config", "unit"} {
		if !checkedSources[name] {
			t.Fatalf("%s source was not checked during install", name)
		}
	}
}

func TestApplierReturnsCommandFailureWithoutSecrets(t *testing.T) {
	config := "listen: 0.0.0.0:9280\ntoken: secret-token\n"
	_, err := Applier{
		Arch:    func() string { return "amd64" },
		TempDir: t.TempDir(),
		Runner:  &fakeRunner{err: errors.New("systemctl failed with config:\n" + config + "raw secret-token")},
	}.Apply(context.Background(), Request{
		Binary:           encode("override-binary"),
		Config:           config,
		SystemdUnit:      "unit",
		RemoteBinaryPath: "/usr/local/bin/conan-agent",
		RemoteConfigPath: "/etc/conan-agent/config.yaml",
		SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
	})

	if err == nil {
		t.Fatalf("err = nil, want command failure")
	}
	if !strings.Contains(err.Error(), "agent update command failed") {
		t.Fatalf("err = %v, want command failure context", err)
	}
	if strings.Contains(err.Error(), config) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err leaks secret: %v", err)
	}
}

func TestApplierCreatesUniqueTempSourcesAcrossConcurrentCalls(t *testing.T) {
	const workers = 32
	tempDir := t.TempDir()
	results := make(chan []string, workers)
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			runner := &fakeRunner{}
			_, err := Applier{
				Arch:    func() string { return "amd64" },
				TempDir: tempDir,
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
				errs <- err
				return
			}
			results <- []string{
				runner.installedSourceFor("install -m 0755 "),
				runner.installedSourceFor("install -m 0600 "),
				runner.installedSourceFor("install -m 0644 "),
			}
		}()
	}

	seen := map[string]bool{}
	for i := 0; i < workers; i++ {
		select {
		case err := <-errs:
			t.Fatalf("Apply: %v", err)
		case paths := <-results:
			for _, path := range paths {
				if path == "" {
					t.Fatalf("temp source path is empty")
				}
				if seen[path] {
					t.Fatalf("temp source path reused: %q", path)
				}
				seen[path] = true
			}
		}
	}
}

func TestApplierRemovesTempSourcesAfterSuccessAndCommandFailure(t *testing.T) {
	for _, tt := range []struct {
		name        string
		failCommand string
		wantErr     bool
	}{
		{name: "success"},
		{name: "later command failure", failCommand: "systemctl daemon-reload", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			runner := &fakeRunner{}
			var paths []string
			runner.onRun = func(command string) error {
				for _, prefix := range []string{"install -m 0755 ", "install -m 0600 ", "install -m 0644 "} {
					if strings.HasPrefix(command, prefix) {
						paths = append(paths, runner.installedSourceFor(prefix))
					}
				}
				if command == tt.failCommand {
					return errors.New("systemctl failed")
				}
				return nil
			}

			_, err := Applier{
				Arch:    func() string { return "amd64" },
				TempDir: tempDir,
				Runner:  runner,
			}.Apply(context.Background(), Request{
				Binary:           encode("override-binary"),
				Config:           "listen: 0.0.0.0:9280\ntoken: node-token\n",
				SystemdUnit:      "unit",
				RemoteBinaryPath: "/usr/local/bin/conan-agent",
				RemoteConfigPath: "/etc/conan-agent/config.yaml",
				SystemdUnitPath:  "/etc/systemd/system/conan-agent.service",
			})
			if tt.wantErr && err == nil {
				t.Fatalf("err = nil, want command failure")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if len(paths) != 3 {
				t.Fatalf("temp source paths = %v, want 3", paths)
			}
			for _, path := range paths {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("stat %q err = %v, want not exist", path, err)
				}
			}
		})
	}
}

type fakeRunner struct {
	commands []string
	err      error
	onRun    func(command string) error
}

func (r *fakeRunner) Run(_ context.Context, command string) (string, error) {
	r.commands = append(r.commands, command)
	if r.onRun != nil {
		if err := r.onRun(command); err != nil {
			return "", err
		}
	}
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

func suffixAfterLastDot(path string) string {
	base := filepath.Base(path)
	idx := strings.LastIndex(base, ".")
	if idx == -1 {
		return base
	}
	return base[idx+1:]
}
