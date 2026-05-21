package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, home string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(home, "config.yaml")), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestModelListHidesAPIKeys(t *testing.T) {
	home := t.TempDir()
	writeTestConfig(t, home, `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-secret-key-do-not-show
`)

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "model", "list"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("model list: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "sk-secret-key-do-not-show") {
		t.Fatalf("model list exposed API key:\n%s", out)
	}
	if !strings.Contains(out, "qwen-prod") {
		t.Fatalf("model list missing model name:\n%s", out)
	}
	if !strings.Contains(out, "*") {
		t.Fatalf("model list missing default marker:\n%s", out)
	}
}

func TestModelUseUpdatesDefault(t *testing.T) {
	home := t.TempDir()
	writeTestConfig(t, home, `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-1
  - name: openai-main
    type: openai
    endpoint: https://api.openai.com/v1
    model: gpt-4.1
    api_key: sk-2
`)

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "model", "use", "openai-main"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("model use: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "default_model: openai-main") {
		t.Fatalf("config = %s, want default_model: openai-main", data)
	}
}

func TestModelUseRejectsUnknownModel(t *testing.T) {
	home := t.TempDir()
	writeTestConfig(t, home, `models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-1
`)

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "model", "use", "nonexistent"})
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestModelRemoveClearsDefault(t *testing.T) {
	home := t.TempDir()
	writeTestConfig(t, home, `default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max
    api_key: sk-1
  - name: openai-main
    type: openai
    endpoint: https://api.openai.com/v1
    model: gpt-4.1
    api_key: sk-2
`)

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "model", "remove", "qwen-prod"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("model remove: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "qwen-prod") {
		t.Fatalf("config still contains removed model:\n%s", data)
	}
	if strings.Contains(string(data), "default_model: qwen-prod") {
		t.Fatalf("default_model not cleared:\n%s", data)
	}
	if !strings.Contains(string(data), "openai-main") {
		t.Fatalf("other model was removed:\n%s", data)
	}
}
