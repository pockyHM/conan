package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesLogDir(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "logs", "conan.jsonl")
	defer Close()

	if err := Setup(Config{File: logFile}); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	info, err := os.Stat(filepath.Dir(logFile))
	if err != nil {
		t.Fatalf("log dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("log path parent is not a directory")
	}
}

func TestSetupDefaultAndEmptyLevelSucceeds(t *testing.T) {
	for _, level := range []string{"", "info"} {
		t.Run("level_"+level, func(t *testing.T) {
			logFile := filepath.Join(t.TempDir(), "conan.jsonl")
			defer Close()

			if err := Setup(Config{Level: level, File: logFile}); err != nil {
				t.Fatalf("Setup() error = %v", err)
			}
		})
	}
}

func TestSetupNoFileSucceeds(t *testing.T) {
	defer Close()

	if err := Setup(Config{}); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
}

func TestWriteMessageAppearsInFileAfterClose(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")

	if err := Setup(Config{File: logFile}); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	Write("hello from logging test")
	Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(contents), "hello from logging test") {
		t.Fatalf("log file = %q, want written message", contents)
	}
}

func TestSetupDebugLevelAllowsDebugLogs(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")

	if err := Setup(Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	slog.Debug("debug logging enabled")
	Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(contents), "debug logging enabled") {
		t.Fatalf("log file = %q, want debug message", contents)
	}
}

func TestSetupFailurePreservesExistingLogger(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "conan.jsonl")
	badFile := filepath.Join(dir, "missing", "child", "conan.jsonl")
	if err := os.WriteFile(filepath.Join(dir, "missing"), []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	defer Close()

	if err := Setup(Config{File: logFile}); err != nil {
		t.Fatalf("initial Setup() error = %v", err)
	}
	if err := Setup(Config{File: badFile}); err == nil {
		t.Fatal("Setup() error = nil, want error")
	}
	Write("after failed setup")
	Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(contents), "after failed setup") {
		t.Fatalf("log file = %q, want existing logger to remain active", contents)
	}
}
