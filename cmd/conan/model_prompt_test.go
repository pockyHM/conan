package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgloader "github.com/pockyHM/conan/internal/config"
)

type stubLister struct {
	models []string
	err    error
}

func (s stubLister) ListModels(_ context.Context, _, _ string) ([]string, error) {
	return s.models, s.err
}

func TestModelAddManualFlow(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"1", // Anthropic
		"claude-main",
		"sk-ant-test",
		"claude-sonnet-4-6",
		"y", // set as default
		"", "\n",
	}, "\n")
	var out bytes.Buffer

	err := runModelAdd(strings.NewReader(input), &out, loader, stubLister{})
	if err != nil {
		t.Fatalf("runModelAdd: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if len(global.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(global.Models))
	}
	m := global.Models[0]
	if m.Name != "claude-main" {
		t.Fatalf("name = %q", m.Name)
	}
	if m.Type != "anthropic" {
		t.Fatalf("type = %q", m.Type)
	}
	if m.Model != "claude-sonnet-4-6" {
		t.Fatalf("model = %q", m.Model)
	}
	if global.DefaultModel != "claude-main" {
		t.Fatalf("default = %q", global.DefaultModel)
	}
}

func TestModelAddDiscoveredModels(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"5", // Qwen (supports list)
		"qwen-prod",
		"sk-qwen-test",
		"1", // select first discovered model
		"y", // set as default
		"", "\n",
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{models: []string{"qwen-max", "qwen-plus"}}
	err := runModelAdd(strings.NewReader(input), &out, loader, lister)
	if err != nil {
		t.Fatalf("runModelAdd: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[0].Model != "qwen-max" {
		t.Fatalf("model = %q, want qwen-max", global.Models[0].Model)
	}
}

func TestModelAddCustomOpenAIUsesDirectEndpoint(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"7", // Custom
		"1", // OpenAI-compatible
		"custom-openai",
		"sk-custom",
		"https://example.com/chat",
		"custom-model", // manual model name after discovery failure
		"y",            // set as default
		"", "\n",
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{err: fmt.Errorf("network error")}
	err := runModelAdd(strings.NewReader(input), &out, loader, lister)
	if err != nil {
		t.Fatalf("runModelAdd: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	m := global.Models[0]
	if m.Type != "openai" {
		t.Fatalf("type = %q, want openai", m.Type)
	}
	if m.Endpoint != "https://example.com/chat" {
		t.Fatalf("endpoint = %q", m.Endpoint)
	}
	if !m.UseEndpointDirectly {
		t.Fatal("UseEndpointDirectly = false, want true")
	}
	if m.Model != "custom-model" {
		t.Fatalf("model = %q, want custom-model", m.Model)
	}
}

func TestModelAddCustomAnthropicUsesDirectEndpoint(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"7", // Custom
		"2", // Anthropic-compatible
		"custom-anthropic",
		"sk-ant",
		"https://example.com/messages",
		"claude-custom",
		"y", // set as default
		"", "\n",
	}, "\n")
	var out bytes.Buffer

	err := runModelAdd(strings.NewReader(input), &out, loader, stubLister{})
	if err != nil {
		t.Fatalf("runModelAdd: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	m := global.Models[0]
	if m.Type != "anthropic" {
		t.Fatalf("type = %q, want anthropic", m.Type)
	}
	if !m.UseEndpointDirectly {
		t.Fatal("UseEndpointDirectly = false, want true")
	}
	if m.Model != "claude-custom" {
		t.Fatalf("model = %q, want claude-custom", m.Model)
	}
}

func TestModelAddRejectsDuplicateName(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, "config.yaml")
	os.WriteFile(cfgPath, []byte("models:\n  - name: existing\n    type: openai\n    endpoint: https://example.com\n    model: m1\n    api_key: k1\n"), 0644)

	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"2",        // OpenAI
		"existing", // duplicate name
	}, "\n")
	var out bytes.Buffer

	err := runModelAdd(strings.NewReader(input), &out, loader, stubLister{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want duplicate error", err)
	}
}

func TestModelAddDiscoveryFallsBackToManual(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("models:\n  - name: existing\n    type: openai\n    endpoint: https://example.com\n    model: m1\n    api_key: k1\n"), 0644)

	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"5", // Qwen
		"qwen-fallback",
		"sk-test",
		"qwen-custom", // manual model name after discovery failure
		"n",           // don't set as default
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{err: fmt.Errorf("network error")}
	err := runModelAdd(strings.NewReader(input), &out, loader, lister)
	if err != nil {
		t.Fatalf("runModelAdd: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[1].Model != "qwen-custom" {
		t.Fatalf("model = %q, want qwen-custom", global.Models[1].Model)
	}
	if global.DefaultModel != "" {
		t.Fatalf("default = %q, want empty", global.DefaultModel)
	}
}
