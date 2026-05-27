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
	"github.com/pockyHM/conan/pkg/configschema"
)

type stubLister struct {
	models []string
	err    error
}

func (s stubLister) ListModels(_ context.Context, _, _ string) ([]string, error) {
	return s.models, s.err
}

type stubTester struct {
	err error
}

func (s stubTester) TestConnection(_ context.Context, _ configschema.ModelConfig) error {
	return s.err
}

func TestModelAddManualFlow(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"1",                // Anthropic
		"claude-main",      // config name
		"sk-ant-test",      // API key (no env set, so askSecret reads directly)
		"claude-sonnet-4-6", // model
	}, "\n")
	var out bytes.Buffer

	err := runModelAddInteractive(strings.NewReader(input), &out, loader, stubLister{}, stubTester{})
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
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

func TestModelAddAutoName(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"1",                // Anthropic
		"",                 // accept auto name "anthropic"
		"sk-ant-test",      // API key
		"claude-sonnet-4-6", // model
	}, "\n")
	var out bytes.Buffer

	err := runModelAddInteractive(strings.NewReader(input), &out, loader, stubLister{}, stubTester{})
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[0].Name != "anthropic" {
		t.Fatalf("name = %q, want anthropic", global.Models[0].Name)
	}
}

func TestModelAddDiscoveredModels(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"7",           // Qwen (supports list)
		"qwen-prod",   // config name
		"sk-qwen-test", // API key
		"1",           // select first discovered model
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{models: []string{"qwen-max", "qwen-plus"}}
	err := runModelAddInteractive(strings.NewReader(input), &out, loader, lister, stubTester{})
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[0].Model != "qwen-max" {
		t.Fatalf("model = %q, want qwen-max", global.Models[0].Model)
	}
}

func TestModelAddCustomOpenAIUsesBaseURL(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"9",                      // Custom
		"1",                      // OpenAI-compatible
		"custom-openai",          // config name
		"sk-custom",              // API key
		"https://example.com/v1", // endpoint (toggle defaults to Base URL in non-terminal)
		"custom-model",           // manual model name after discovery failure
		"y",                      // set as default
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{err: fmt.Errorf("network error")}
	err := runModelAddInteractive(strings.NewReader(input), &out, loader, lister, stubTester{})
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	m := global.Models[0]
	if m.Type != "openai" {
		t.Fatalf("type = %q, want openai", m.Type)
	}
	if m.Endpoint != "https://example.com/v1" {
		t.Fatalf("endpoint = %q", m.Endpoint)
	}
	if m.UseEndpointDirectly {
		t.Fatal("UseEndpointDirectly = true, want false (base URL mode)")
	}
	if m.Model != "custom-model" {
		t.Fatalf("model = %q, want custom-model", m.Model)
	}
}

func TestModelAddCustomAnthropicUsesFullURL(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	// Test full URL mode through direct mode since toggle doesn't consume input in non-terminal
	var out bytes.Buffer

	flags := ModelAddFlags{
		Provider:     "custom",
		Type:         "anthropic",
		APIKey:       "sk-ant",
		Endpoint:     "https://example.com/v1/messages",
		EndpointMode: "full",
		Model:        "claude-custom",
		Name:         "custom-anthropic",
		SetDefault:   true,
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err != nil {
		t.Fatalf("runModelAddDirect: %v", err)
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
		t.Fatal("UseEndpointDirectly = false, want true (full URL mode)")
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

	err := runModelAddInteractive(strings.NewReader(input), &out, loader, stubLister{}, stubTester{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want duplicate error", err)
	}
}

func TestModelAddDiscoveryFallsBackToManual(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("models:\n  - name: existing\n    type: openai\n    endpoint: https://example.com\n    model: m1\n    api_key: k1\n"), 0644)

	loader := cfgloader.NewLoader(home)
	input := strings.Join([]string{
		"6",            // Qwen
		"qwen-fallback",
		"sk-test",
		"qwen-custom", // manual model name after discovery failure
		"n",           // don't set as default
	}, "\n")
	var out bytes.Buffer

	lister := stubLister{err: fmt.Errorf("network error")}
	err := runModelAddInteractive(strings.NewReader(input), &out, loader, lister, stubTester{})
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
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

func TestModelAddConnectionTestRetry(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)

	tester := &failingTester{failCount: 1}

	input := strings.Join([]string{
		// First attempt
		"1",                // Anthropic
		"claude-main",      // config name
		"sk-ant-bad",       // API key (will fail test)
		"claude-sonnet-4-6", // model
		// Connection test fails → modify
		"y",
		// Second attempt (reconfigure - provider not re-asked)
		"claude-main",       // same config name
		"sk-ant-good",       // new API key
		"claude-sonnet-4-6", // model
		// Connection test passes → first model, auto default
	}, "\n")
	var out bytes.Buffer

	err := runModelAddInteractive(strings.NewReader(input), &out, loader, stubLister{}, tester)
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[0].APIKey != "sk-ant-good" {
		t.Fatalf("api_key = %q, want sk-ant-good", global.Models[0].APIKey)
	}
	if !strings.Contains(out.String(), "Reconfigure") {
		t.Fatalf("output missing reconfigure hint:\n%s", out.String())
	}
}

func TestModelAddConnectionTestSaveAnyway(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)

	tester := &failingTester{failCount: 100} // always fails

	input := strings.Join([]string{
		"1",                // Anthropic
		"claude-main",      // config name
		"sk-ant-test",      // API key
		"claude-sonnet-4-6", // model
		// Connection test fails
		"n", // don't modify, save anyway
	}, "\n")
	var out bytes.Buffer

	err := runModelAddInteractive(strings.NewReader(input), &out, loader, stubLister{}, tester)
	if err != nil {
		t.Fatalf("runModelAddInteractive: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if len(global.Models) != 1 {
		t.Fatalf("models = %d, want 1 (saved anyway)", len(global.Models))
	}
	if !strings.Contains(out.String(), "failed") {
		t.Fatalf("output missing failure message:\n%s", out.String())
	}
}

type failingTester struct {
	failCount int
	calls     int
}

func (t *failingTester) TestConnection(_ context.Context, _ configschema.ModelConfig) error {
	t.calls++
	if t.calls <= t.failCount {
		return fmt.Errorf("connection refused")
	}
	return nil
}

func TestModelAddDirectMode(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	var out bytes.Buffer

	flags := ModelAddFlags{
		Provider:   "openai",
		APIKey:     "sk-test",
		Model:      "gpt-4.1",
		Name:       "my-openai",
		SetDefault: true,
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err != nil {
		t.Fatalf("runModelAddDirect: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if len(global.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(global.Models))
	}
	m := global.Models[0]
	if m.Name != "my-openai" {
		t.Fatalf("name = %q", m.Name)
	}
	if m.Type != "openai" {
		t.Fatalf("type = %q", m.Type)
	}
	if m.Model != "gpt-4.1" {
		t.Fatalf("model = %q", m.Model)
	}
	if global.DefaultModel != "my-openai" {
		t.Fatalf("default = %q", global.DefaultModel)
	}
}

func TestModelAddDirectCustomProvider(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	var out bytes.Buffer

	flags := ModelAddFlags{
		Provider:     "custom",
		Type:         "openai",
		APIKey:       "sk-custom",
		Endpoint:     "https://example.com/v1",
		EndpointMode: "full",
		Model:        "deepseek-chat",
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err != nil {
		t.Fatalf("runModelAddDirect: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	m := global.Models[0]
	if m.Type != "openai" {
		t.Fatalf("type = %q", m.Type)
	}
	if !m.UseEndpointDirectly {
		t.Fatal("UseEndpointDirectly should be true for full URL mode")
	}
}

func TestModelAddDirectAutoName(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	var out bytes.Buffer

	flags := ModelAddFlags{
		Provider: "anthropic",
		APIKey:   "sk-ant-test",
		Model:    "claude-sonnet-4-6",
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err != nil {
		t.Fatalf("runModelAddDirect: %v", err)
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if global.Models[0].Name != "anthropic" {
		t.Fatalf("name = %q, want anthropic (auto-generated)", global.Models[0].Name)
	}
}

func TestModelAddDirectMissingProvider(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	var out bytes.Buffer

	flags := ModelAddFlags{
		APIKey: "sk-test",
		Model:  "gpt-4.1",
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestModelAddDirectCustomMissingType(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)
	var out bytes.Buffer

	flags := ModelAddFlags{
		Provider: "custom",
		APIKey:   "sk-test",
		Endpoint: "https://example.com/v1",
		Model:    "test",
	}

	err := runModelAddDirect(&out, loader, stubTester{}, flags)
	if err == nil || !strings.Contains(err.Error(), "--type") {
		t.Fatalf("err = %v, want --type required error", err)
	}
}

func TestRenderChoiceTerminalUsesCarriageReturns(t *testing.T) {
	var out bytes.Buffer

	renderChoiceTerminal(&out, "Provider", []string{"Anthropic", "OpenAI"}, 1)

	got := out.String()
	if !strings.Contains(got, "\r\n") {
		t.Fatalf("rendered output = %q, want CRLF line endings", got)
	}
	if strings.Contains(got, "Provider\n") {
		t.Fatalf("rendered output = %q, should not use bare LF in raw terminal mode", got)
	}
	if !strings.Contains(got, "> OpenAI\r\n") {
		t.Fatalf("rendered output = %q, want selected option marker", got)
	}
}

func TestMoveChoiceCursorUpIncludesPromptLine(t *testing.T) {
	var out bytes.Buffer

	moveChoiceCursorUp(&out, 2)

	if got := out.String(); got != "\x1b[3A" {
		t.Fatalf("cursor movement = %q, want prompt plus option lines", got)
	}
}

func TestGenerateName(t *testing.T) {
	home := t.TempDir()
	loader := cfgloader.NewLoader(home)

	preset := modelPresets[0] // Anthropic
	name := generateName(preset, loader)
	if name != "anthropic" {
		t.Fatalf("name = %q, want anthropic", name)
	}

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("models:\n  - name: anthropic\n    type: anthropic\n    endpoint: https://api.anthropic.com\n    model: claude-sonnet-4-6\n    api_key: k1\n"), 0644)
	name = generateName(preset, loader)
	if name != "anthropic-2" {
		t.Fatalf("name = %q, want anthropic-2", name)
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct{ input, want string }{
		{"sk-ant-1234567890", "sk*************90"},
		{"abc", "****"},
		{"", "****"},
		{"abcd", "****"},
		{"abcde", "ab*de"},
	}
	for _, tt := range tests {
		got := maskKey(tt.input)
		if got != tt.want {
			t.Errorf("maskKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderToggle(t *testing.T) {
	var out bytes.Buffer
	renderToggle(&out, "Endpoint mode", []string{"Base URL", "Full URL"}, 0)

	got := out.String()
	if !strings.Contains(got, "[Base URL]") {
		t.Fatalf("toggle output = %q, want [Base URL] selected", got)
	}
	if !strings.Contains(got, "Full URL") {
		t.Fatalf("toggle output = %q, want Full URL option", got)
	}
}

func TestSelectModelWithRecommendations(t *testing.T) {
	var out bytes.Buffer
	pr := newPrompter(strings.NewReader("1\n"), &out)

	lister := stubLister{models: []string{"gpt-4.1", "gpt-4.1-mini", "o3"}}
	preset := modelPresets[1] // OpenAI with RecommendedModels

	modelID, err := selectModel(pr, lister, preset, "https://api.openai.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("selectModel: %v", err)
	}
	if modelID != "gpt-4.1" {
		t.Fatalf("model = %q, want gpt-4.1", modelID)
	}
	if !strings.Contains(out.String(), "recommended") {
		t.Fatalf("output missing recommended marker:\n%s", out.String())
	}
}

func TestIsRecommended(t *testing.T) {
	if !isRecommended("gpt-4.1", []string{"gpt-4.1", "o3"}) {
		t.Fatal("gpt-4.1 should be recommended")
	}
	if isRecommended("gpt-3.5", []string{"gpt-4.1", "o3"}) {
		t.Fatal("gpt-3.5 should not be recommended")
	}
}
