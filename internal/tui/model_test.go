package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/logging"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/internal/skills"
	"github.com/pockyHM/conan/internal/subagent"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

type fakeProvider struct{}

func (f *fakeProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: "hello"},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (f *fakeProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent, 10)
	go func() {
		ch <- llm.TextDeltaEvent{Delta: "Hi"}
		ch <- llm.TextDeltaEvent{Delta: " there"}
		ch <- llm.StopEvent{Reason: llm.StopEndTurn}
		close(ch)
	}()
	return ch, nil
}

type captureStreamProvider struct {
	req *llm.ChatRequest
}

func (p *captureStreamProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: models.Message{Role: "assistant", Content: "ok"}, StopReason: llm.StopEndTurn}, nil
}

func (p *captureStreamProvider) ChatStream(_ context.Context, req *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.req = req
	ch := make(chan llm.ChatEvent, 2)
	ch <- llm.TextDeltaEvent{Delta: "ok"}
	ch <- llm.StopEvent{Reason: llm.StopEndTurn}
	close(ch)
	return ch, nil
}

type compactCaptureProvider struct {
	req     *llm.ChatRequest
	content string
	err     error
}

func (p *compactCaptureProvider) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	content := p.content
	if content == "" {
		content = "summary"
	}
	return &llm.ChatResponse{Message: models.Message{Role: "assistant", Content: content}, StopReason: llm.StopEndTurn}, nil
}

func (p *compactCaptureProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent)
	close(ch)
	return ch, nil
}

type stubVisionProvider struct {
	req     *llm.VisionRequest
	summary string
	err     error
}

func (p *stubVisionProvider) SupportsVision() bool {
	return true
}

func (p *stubVisionProvider) DescribeImages(_ context.Context, req *llm.VisionRequest) (*llm.VisionResponse, error) {
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	return &llm.VisionResponse{Summary: p.summary}, nil
}

type stubMemoryExtractor struct {
	candidates  []memory.MemoryCandidate
	err         error
	inputs      []MemoryExtractionInput
	deadlines   []time.Time
	hasDeadline []bool
}

func (s *stubMemoryExtractor) ExtractMemory(ctx context.Context, input MemoryExtractionInput) ([]memory.MemoryCandidate, error) {
	s.inputs = append(s.inputs, input)
	deadline, ok := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	s.hasDeadline = append(s.hasDeadline, ok)
	return s.candidates, s.err
}

type tuiFixtureFetcher struct {
	src string
}

func (f tuiFixtureFetcher) Fetch(_ context.Context, _ skills.RepoSource, dest string) error {
	return copyDirForTUITest(f.src, dest)
}

func copyDirForTUITest(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func writeTUISkill(t *testing.T, root string, name string, description string, body string) {
	t.Helper()
	path := filepath.Join(root, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, description, body)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInitialModelView(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	view := model.View()
	for _, want := range []string{"Conan", "production", "claude-sonnet", "❯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestUILanguageChineseAffectsTUIOnly(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", UILanguage: "zh-CN"})
	view := model.View()
	for _, want := range []string{"就绪", "集群", "模型", "节点"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	prompt := model.buildSystemPromptWithMemory()
	if strings.Contains(prompt, "就绪") || strings.Contains(prompt, "集群") {
		t.Fatalf("ui language leaked into system prompt:\n%s", prompt)
	}
}

func TestLangCommandOpensSelectorAndConfirmsLanguage(t *testing.T) {
	home := t.TempDir()
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", ConfigHome: home})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/lang")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeLangSelect {
		t.Fatalf("mode = %v, want modeLangSelect", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Select UI Language") {
		t.Fatalf("language selector missing title:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.uiLanguage != uiLanguageChinese {
		t.Fatalf("uiLanguage = %q, want zh-CN", model.uiLanguage)
	}
	if !strings.Contains(model.View(), "界面语言已切换为 中文") {
		t.Fatalf("view missing changed language status:\n%s", model.View())
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "ui_language: zh-CN") {
		t.Fatalf("config missing ui_language:\n%s", data)
	}
}

func TestLangCommandAcceptsDirectLanguageArg(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/lang zh")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.uiLanguage != uiLanguageChinese {
		t.Fatalf("uiLanguage = %q, want zh-CN", model.uiLanguage)
	}
	if !strings.Contains(model.View(), "界面语言已切换为 中文") {
		t.Fatalf("view missing changed language status:\n%s", model.View())
	}
}

func TestModelCommandWithoutArgOpensSelectorAndSwitchesModel(t *testing.T) {
	models := []configschema.ModelConfig{
		{Name: "claude", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-ant"},
		{Name: "gpt", Type: "openai", Model: "gpt-4.1", APIKey: "sk-oai"},
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ModelConfigs: models})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeModelSelect {
		t.Fatalf("mode = %v, want modeModelSelect", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Select Model", "claude", "gpt", "(current)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("model selector view missing %q:\n%s", want, view)
		}
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.model != "gpt" {
		t.Fatalf("model = %q, want gpt", model.model)
	}
	if _, ok := model.provider.(*llm.RetryProvider); !ok {
		t.Fatalf("provider = %T, want *llm.RetryProvider", model.provider)
	}
	if !strings.Contains(model.View(), "Model switched to gpt") {
		t.Fatalf("view missing switched status:\n%s", model.View())
	}
}

func TestClusterCommandWithoutArgOpensSelectorAndSwitchesCluster(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), "name: staging\n")
	model := NewModel(ModelConfig{Cluster: "prod", Model: "claude", ConfigHome: home})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/cluster")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeClusterSelect {
		t.Fatalf("mode = %v, want modeClusterSelect", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Select Cluster", "prod", "staging", "(current)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("cluster selector view missing %q:\n%s", want, view)
		}
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.cluster != "staging" {
		t.Fatalf("cluster = %q, want staging", model.cluster)
	}
	if !model.clusterExplicit {
		t.Fatal("cluster should be explicit after selector switch")
	}
	if !strings.Contains(model.View(), "Cluster switched to staging") {
		t.Fatalf("view missing switched status:\n%s", model.View())
	}
}

func TestClusterCommandWithoutArgShowsStatusWhenNoClustersConfigured(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "prod", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/cluster")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if !strings.Contains(model.View(), "No configured clusters") {
		t.Fatalf("view missing no clusters status:\n%s", model.View())
	}
}

func TestClusterSwitchReloadsNodesForNodesSelector(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), "name: staging\n")
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "nodes.yaml"), `nodes:
  - name: staging-node
    host: 10.0.2.10
    agent:
      port: 9380
`)
	model := NewModel(ModelConfig{
		Cluster:    "prod",
		Model:      "claude",
		ConfigHome: home,
		Nodes:      []NodeInfo{{Name: "prod-node", Host: "10.0.1.10", Online: true}},
	})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandCluster, Arg: "staging"})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandNodes})

	view := model.View()
	if !strings.Contains(view, "staging-node") || !strings.Contains(view, "10.0.2.10") {
		t.Fatalf("nodes selector missing staging node after cluster switch:\n%s", view)
	}
	if strings.Contains(view, "prod-node") || strings.Contains(view, "10.0.1.10") {
		t.Fatalf("nodes selector still shows old cluster node:\n%s", view)
	}
	if !model.selectedNodes["staging-node"] || model.selectedNodes["prod-node"] {
		t.Fatalf("selectedNodes = %#v, want only staging-node selected", model.selectedNodes)
	}
	if _, ok := model.clients["staging-node"]; !ok {
		t.Fatalf("clients = %#v, want staging-node client", model.clients)
	}
}

func TestClusterSwitchRefreshesNodeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("request path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), "name: staging\n")
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "nodes.yaml"), fmt.Sprintf(`nodes:
  - name: staging-node
    host: %s
    agent:
      port: %s
`, u.Hostname(), u.Port()))

	model := NewModel(ModelConfig{
		Cluster:    "prod",
		Model:      "claude",
		ConfigHome: home,
	})

	next, cmd := model.applyCommand(SlashCommand{Kind: CommandCluster, Arg: "staging"})
	model = next
	if cmd == nil {
		t.Fatal("cluster switch should trigger a node status refresh command")
	}

	pingMsg := execPingResultFromBatch(t, cmd)
	updated, _ := model.Update(pingMsg)
	model = updated.(Model)

	if !model.nodes[0].Online {
		t.Fatalf("node = %#v, want online after cluster switch refresh", model.nodes[0])
	}
	if !strings.Contains(model.View(), "1 online") {
		t.Fatalf("view missing refreshed online count:\n%s", model.View())
	}
}

func TestClusterSelectorSwitchRefreshesNodeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("request path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "prod", "cluster.yaml"), "name: prod\n")
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "cluster.yaml"), "name: staging\n")
	writeTestFile(t, filepath.Join(home, "clusters", "staging", "nodes.yaml"), fmt.Sprintf(`nodes:
  - name: staging-node
    host: %s
    agent:
      port: %s
`, u.Hostname(), u.Port()))

	model := NewModel(ModelConfig{Cluster: "prod", Model: "claude", ConfigHome: home})

	next, cmd := model.applyCommand(SlashCommand{Kind: CommandCluster})
	model = next
	if cmd != nil {
		t.Fatal("opening cluster selector should not start status refresh yet")
	}
	nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = nextModel.(Model)
	nextModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	if cmd == nil {
		t.Fatal("cluster selector switch should trigger a node status refresh command")
	}

	pingMsg := execPingResultFromBatch(t, cmd)
	nextModel, _ = model.Update(pingMsg)
	model = nextModel.(Model)

	if !model.nodes[0].Online {
		t.Fatalf("node = %#v, want online after cluster selector refresh", model.nodes[0])
	}
}

func TestConfigCommandOpensGlobalConfigPage(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_model: claude\nlogging:\n  level: info\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: home})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/config")})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeConfig {
		t.Fatalf("mode = %v, want modeConfig", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Global Config", "default_model", "logging.level", "info"} {
		if !strings.Contains(view, want) {
			t.Fatalf("config view missing %q:\n%s", want, view)
		}
	}
}

func TestConfigPageSelectsLoggingLevelAndSaves(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("logging:\n  level: info\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: home})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for !strings.Contains(model.configScreen.SelectedKey(), "logging.level") {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	if model.mode != modeConfig {
		t.Fatalf("mode = %v, want modeConfig", model.mode)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "level: debug") {
		t.Fatalf("config missing debug level:\n%s", data)
	}
	if !strings.Contains(model.View(), "Saved config.yaml") {
		t.Fatalf("view missing saved status:\n%s", model.View())
	}
}

func TestConfigPageEditsStringAndSaves(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_cluster: old\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "old", Model: "claude", ConfigHome: home})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for model.configScreen.SelectedKey() != "default_cluster" {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	for range []rune("old") {
		nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = nextModel.(Model)
	}
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("prod")})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	if model.cluster != "prod" {
		t.Fatalf("cluster = %q, want prod", model.cluster)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "default_cluster: prod") {
		t.Fatalf("config missing default_cluster prod:\n%s", data)
	}
}

func TestConfigPageDefaultModelRebuildsProvider(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`default_model: claude
models:
  - name: claude
    type: anthropic
    model: claude-sonnet-4-6
    api_key: sk-ant
  - name: gpt
    type: openai
    model: gpt-4.1
    api_key: sk-oai
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	models := []configschema.ModelConfig{
		{Name: "claude", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-ant"},
		{Name: "gpt", Type: "openai", Model: "gpt-4.1", APIKey: "sk-oai"},
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ModelConfigs: models, ConfigHome: home, Provider: &fakeProvider{}})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for model.configScreen.SelectedKey() != "default_model" {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	for range []rune("claude") {
		nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = nextModel.(Model)
	}
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt")})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	if model.model != "gpt" {
		t.Fatalf("model = %q, want gpt", model.model)
	}
	if _, ok := model.provider.(*llm.RetryProvider); !ok {
		t.Fatalf("provider = %T, want *llm.RetryProvider", model.provider)
	}
	if !strings.Contains(model.View(), "Saved config.yaml") {
		t.Fatalf("view missing saved status:\n%s", model.View())
	}
}

func TestConfigPageTogglesBoolAndSaves(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("logging:\n  audit: false\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: home})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for model.configScreen.SelectedKey() != "logging.audit" {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "audit: true") {
		t.Fatalf("config missing audit true:\n%s", data)
	}
}

func TestConfigPageRejectsInvalidIntWithoutSaving(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("vision:\n  max_images: 10\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: home})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for model.configScreen.SelectedKey() != "vision.max_images" {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	for range []rune("10") {
		nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = nextModel.(Model)
	}
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad")})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "max_images: 10") {
		t.Fatalf("config should keep max_images 10:\n%s", data)
	}
	if !strings.Contains(model.View(), "Config validation failed") {
		t.Fatalf("view missing validation error:\n%s", model.View())
	}
}

func TestConfigPageEditsListAndSaves(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("security:\n  local_file_whitelist:\n    - README.md\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: home})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	for model.configScreen.SelectedKey() != "security.local_file_whitelist" {
		nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = nextModel.(Model)
	}
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(", docs/spec.md")})
	model = nextModel.(Model)
	nextModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"- README.md", "- docs/spec.md"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("config missing %q:\n%s", want, contents)
		}
	}
}

func TestConfigPageEscReturnsToChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandConfig})
	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = nextModel.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}

func TestSystemPromptPrefersSpecializedToolsBeforeExec(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "production",
		Model:   "claude-sonnet",
		Nodes:   []NodeInfo{{Name: "node-01", Online: true}},
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, want := range []string{
		"high-discipline infrastructure operations agent",
		"Tool routing contract:",
		"call tool_search first unless the user explicitly asked for shell",
		"use file_put or file_get directly",
		"Do not call tool_search first for file transfer",
		"use call_tool with a discovered specialized tool",
		"Use exec only as fallback",
		"Do not use ad hoc scp, rsync, curl, or wget for file transfer",
		"For resource-changing operations, first use read-only tools",
		"Use local_fs_read, local_fs_list, and local_fs_stat",
		"use local_fs_write, local_fs_patch, and local_fs_delete",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestModelInjectsSkillIndexAndTool(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "prod",
		Model:    "test",
		Provider: provider,
		Conv:     conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "body",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, IndexTokenBudget: 800, MaxSkillChars: 6000, MaxVisibleSkills: 50},
	})

	next, cmd := model.submitMessage("pods are failing", nil)
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start stream")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	if !strings.Contains(provider.req.SystemPrompt, "Available skills:") {
		t.Fatalf("system prompt missing skills index: %s", provider.req.SystemPrompt)
	}
	found := false
	for _, tool := range provider.req.Tools {
		if tool.Name == skills.ToolName {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill_read tool missing from %#v", provider.req.Tools)
	}
}

func TestModelSkillsDisabledDoesNotInjectSkillIndexOrTool(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "prod",
		Model:    "test",
		Provider: provider,
		Conv:     conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "body",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: false, IndexTokenBudget: 800, MaxSkillChars: 6000, MaxVisibleSkills: 50},
	})

	next, cmd := model.submitMessage("pods are failing", nil)
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start stream")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	if strings.Contains(provider.req.SystemPrompt, "[Skills]") || strings.Contains(provider.req.SystemPrompt, "Available skills:") {
		t.Fatalf("system prompt should not include skills when disabled: %s", provider.req.SystemPrompt)
	}
	for _, tool := range provider.req.Tools {
		if tool.Name == skills.ToolName {
			t.Fatalf("skill_read tool should not be exposed when disabled: %#v", provider.req.Tools)
		}
	}
}

func TestModelSkillsDisabledWithZeroLimitsDoesNotExposeTool(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "skill body",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: false},
	})

	if strings.Contains(model.buildSystemPromptWithMemory(), "Available skills:") {
		t.Fatal("system prompt should not include skills when disabled")
	}
	for _, tool := range model.availableToolDefs() {
		if tool.Name == skills.ToolName {
			t.Fatalf("skill_read tool should not be exposed when disabled: %#v", model.availableToolDefs())
		}
	}
	msg := execCmd(t, model.dispatchTool(7, llm.ToolCall{
		ID:        "skill-1",
		Name:      skills.ToolName,
		Arguments: json.RawMessage(`{"name":"k8s-debug","reason":"diagnose"}`),
	}))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 || result.Results[0].Success || strings.Contains(result.Results[0].Output, "skill body") {
		t.Fatalf("disabled skill_read result = %#v", result.Results)
	}
}

func TestDispatchSkillRead(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Conv:    conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:    "k8s-debug",
			Scope:   skills.ScopeCluster,
			Cluster: "prod",
			Body:    "skill body",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})
	msg := execCmd(t, model.dispatchTool(7, llm.ToolCall{
		ID:        "skill-1",
		Name:      skills.ToolName,
		Arguments: json.RawMessage(`{"name":"k8s-debug","reason":"diagnose"}`),
	}))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 {
		t.Fatalf("results = %#v, want one result", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "skill body") {
		t.Fatalf("output = %q", result.Results[0].Output)
	}
}

func TestDispatchSkillReadDisabledReturnsUnavailableError(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Conv:    conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:    "k8s-debug",
			Scope:   skills.ScopeCluster,
			Cluster: "prod",
			Body:    "skill body",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: false, MaxSkillChars: 6000},
	})
	msg := execCmd(t, model.dispatchTool(7, llm.ToolCall{
		ID:        "skill-1",
		Name:      skills.ToolName,
		Arguments: json.RawMessage(`{"name":"k8s-debug","reason":"diagnose"}`),
	}))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 {
		t.Fatalf("results = %#v, want one result", result.Results)
	}
	if result.Results[0].Success {
		t.Fatalf("skill_read should fail when disabled: %#v", result.Results[0])
	}
	if strings.Contains(result.Results[0].Output, "skill body") {
		t.Fatalf("disabled skill_read leaked body: %q", result.Results[0].Output)
	}
	if !strings.Contains(result.Results[0].Output, "not available") {
		t.Fatalf("output = %q, want not available error", result.Results[0].Output)
	}
}

func TestSlashSkillsListsVisibleSkills(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Conv:    conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true},
	})

	model.input = "/skills"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("/skills returned command %#v, want immediate UI update", cmd)
	}
	visible := model.status + "\n" + model.View()
	if !strings.Contains(visible, "k8s-debug") || !strings.Contains(visible, "Diagnose Kubernetes failures.") {
		t.Fatalf("skill list not visible; status=%q view=%q", model.status, model.View())
	}
	if strings.Contains(model.status, "Diagnose Kubernetes failures.") {
		t.Fatalf("status should be concise, got %q", model.status)
	}
	if model.mode != modeSkillsManage {
		t.Fatalf("/skills mode = %v, want skills management", model.mode)
	}
}

func TestSlashSkillInjectsSkillForNextRequest(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "prod",
		Provider: provider,
		Conv:     conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "Use kubectl describe before changes.",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})

	model.input = "/skill k8s-debug pods failing"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd == nil {
		t.Fatal("/skill should start a model request")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	found := false
	for _, msg := range provider.req.Messages {
		if strings.Contains(msg.Content, "Use kubectl describe before changes.") && strings.Contains(msg.Content, "pods failing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("request messages missing explicit skill content: %#v", provider.req.Messages)
	}
	if got := model.messages[len(model.messages)-1].content; got != "/skill k8s-debug pods failing" {
		t.Fatalf("visible message = %q", got)
	}
}

func TestSlashSkillInjectsReferencedLocalFileContext(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("project notes"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:            "prod",
		Provider:           provider,
		Conv:               conversation.New("prod", nil, "test"),
		LocalWorkspaceRoot: workspace,
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "Use kubectl describe before changes.",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})

	model.input = "/skill k8s-debug inspect @README.md"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd == nil {
		t.Fatal("/skill with file reference should start a model request")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil || len(provider.req.Messages) != 1 {
		t.Fatalf("request messages = %#v", provider.req)
	}
	content := provider.req.Messages[0].Content
	for _, want := range []string{"Use kubectl describe before changes.", "inspect @README.md", `<file path="README.md">`, "project notes"} {
		if !strings.Contains(content, want) {
			t.Fatalf("request missing %q:\n%s", want, content)
		}
	}
	if got := model.messages[len(model.messages)-1].content; got != "/skill k8s-debug inspect @README.md" {
		t.Fatalf("visible message = %q", got)
	}
}

func TestSlashSkillsRemoveUpdatesVisibleSkills(t *testing.T) {
	home := t.TempDir()
	reg := skills.Registry{Skills: []skills.RegistryEntry{{
		Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Source: "github.com/acme/ops", Ref: "main", Path: "skills/k8s-debug", CachePath: "skills/repos/github.com/acme/ops/main/skills/k8s-debug",
	}}}
	if err := skills.SaveRegistry(skills.GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:    "prod",
		ConfigHome: home,
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeGlobal,
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxVisibleSkills: 50, MaxSkillChars: 6000, IndexTokenBudget: 800},
	})

	model.input = "/skills remove k8s-debug --global"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd == nil {
		t.Fatal("/skills remove should return management command")
	}
	msg := execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	if len(model.skills) != 0 {
		t.Fatalf("skills = %#v, want empty after remove", model.skills)
	}
	if !strings.Contains(model.status, "Removed skill") {
		t.Fatalf("status = %q", model.status)
	}
	got, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 0 {
		t.Fatalf("registry = %#v, want empty", got)
	}
}

func TestSlashSkillsManagerRemovesSelectedSkill(t *testing.T) {
	home := t.TempDir()
	reg := skills.Registry{Skills: []skills.RegistryEntry{{
		Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Source: "github.com/acme/ops", Ref: "main", Path: "skills/k8s-debug", CachePath: "skills/repos/github.com/acme/ops/main/skills/k8s-debug",
	}}}
	if err := skills.SaveRegistry(skills.GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:      "prod",
		ConfigHome:   home,
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxVisibleSkills: 50, MaxSkillChars: 6000, IndexTokenBudget: 800},
	})

	model.input = "/skills"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("/skills returned command %#v, want immediate management view", cmd)
	}
	if model.mode != modeSkillsManage || !strings.Contains(model.View(), "k8s-debug") {
		t.Fatalf("skills manager not opened correctly; mode=%v view=%s", model.mode, model.View())
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("first uninstall key should only ask for confirmation, got cmd %#v", cmd)
	}
	if model.pendingSkillRemove.entry.Name != "k8s-debug" || !strings.Contains(model.status, "Confirm uninstall") {
		t.Fatalf("pending remove=%#v status=%q, want confirmation", model.pendingSkillRemove, model.status)
	}
	got, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 {
		t.Fatalf("registry changed before confirmation: %#v", got)
	}
	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("confirmed uninstall should return management command")
	}
	msg := execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	got, err = skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 0 {
		t.Fatalf("registry = %#v, want empty", got)
	}
	if len(model.skills) != 0 || !strings.Contains(model.status, "Removed skill") {
		t.Fatalf("status=%q skills=%#v, want removed and refreshed", model.status, model.skills)
	}
	if model.mode != modeSkillsManage {
		t.Fatalf("mode = %v, want to stay in skills manager after uninstall", model.mode)
	}
	if len(model.skillsManager.entries) != 0 {
		t.Fatalf("skills manager entries = %#v, want removed skill gone", model.skillsManager.entries)
	}
}

func TestSlashSkillsManagerUninstallConfirmationCanCancel(t *testing.T) {
	home := t.TempDir()
	reg := skills.Registry{Skills: []skills.RegistryEntry{{
		Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Source: "github.com/acme/ops", Ref: "main", Path: "skills/k8s-debug", CachePath: "skills/repos/github.com/acme/ops/main/skills/k8s-debug",
	}}}
	if err := skills.SaveRegistry(skills.GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:      "prod",
		ConfigHome:   home,
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxVisibleSkills: 50, MaxSkillChars: 6000, IndexTokenBudget: 800},
	})
	model.input = "/skills"
	next, _ := model.submit()
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("uninstall prompt returned cmd %#v", cmd)
	}
	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("cancel returned cmd %#v", cmd)
	}

	got, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 {
		t.Fatalf("registry = %#v, want skill preserved after cancel", got)
	}
	if model.mode != modeSkillsManage || model.pendingSkillRemove.entry.Name != "" {
		t.Fatalf("mode=%v pending=%#v, want manager with no pending remove", model.mode, model.pendingSkillRemove)
	}
}

func TestSlashSkillsNameInjectsSkillForNextRequest(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "prod",
		Provider: provider,
		Conv:     conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "Skills namespace shortcut body.",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})

	model.input = "/skills k8s-debug pods failing"
	_, cmd := model.submit()
	if cmd == nil {
		t.Fatal("/skills <name> should start a model request")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	var combined strings.Builder
	for _, msg := range provider.req.Messages {
		combined.WriteString(msg.Content)
		combined.WriteString("\n")
	}
	if got := combined.String(); !strings.Contains(got, "Skills namespace shortcut body.") || !strings.Contains(got, "pods failing") {
		t.Fatalf("request messages missing /skills shortcut content:\n%s", got)
	}
}

func TestSlashSkillsInstallOpensSelectionAndInstallsCheckedSkills(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	writeTUISkill(t, fixture, "one", "first skill", "first body")
	writeTUISkill(t, fixture, "two", "second skill", "second body")
	model := NewModel(ModelConfig{
		Cluster:       "prod",
		ConfigHome:    home,
		SkillsFetcher: tuiFixtureFetcher{src: fixture},
		SkillsConfig:  configschema.SkillsConfig{Enabled: true, MaxVisibleSkills: 50, MaxSkillChars: 6000, IndexTokenBudget: 800},
	})

	model.input = "/skills install org/repo --global"
	next, cmd := model.submit()
	model = next.(Model)
	if cmd == nil {
		t.Fatal("/skills install should discover skills before installing")
	}
	msg := execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	if model.mode != modeSkillInstallSelect {
		t.Fatalf("mode = %v, want install selection", model.mode)
	}
	if got := model.skillInstall.SelectedNames(); strings.Join(got, ",") != "one,two" {
		t.Fatalf("default selected names = %#v, want all skills selected", got)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if got := model.skillInstall.SelectedNames(); strings.Join(got, ",") != "one" {
		t.Fatalf("selected names after toggle = %#v, want only one", got)
	}
	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("confirming install selection should return install command")
	}
	msg = execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	reg, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Skills) != 1 || reg.Skills[0].Name != "one" {
		t.Fatalf("registry = %#v, want only selected skill one", reg)
	}
	if len(model.skills) != 1 || model.skills[0].Name != "one" {
		t.Fatalf("visible skills = %#v, want selected skill one", model.skills)
	}
	if !strings.Contains(model.status, "Installed 1") {
		t.Fatalf("status = %q, want installed count", model.status)
	}
}

func TestSkillShortcutInjectsSkillForNextRequest(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "prod",
		Provider: provider,
		Conv:     conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
			Body:        "Shortcut skill body.",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})

	model.input = "/k8s-debug pods failing"
	_, cmd := model.submit()
	if cmd == nil {
		t.Fatal("skill shortcut should start a model request")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	var combined strings.Builder
	for _, msg := range provider.req.Messages {
		combined.WriteString(msg.Content)
		combined.WriteString("\n")
	}
	if got := combined.String(); !strings.Contains(got, "Shortcut skill body.") || !strings.Contains(got, "pods failing") {
		t.Fatalf("request messages missing shortcut skill content:\n%s", got)
	}
}

func TestSlashAutocompleteIncludesVisibleSkills(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
			Scope:       skills.ScopeCluster,
			Cluster:     "prod",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true},
	})

	model = typeRunes(t, model, "/k8")

	if !model.ac.visible {
		t.Fatal("autocomplete should be visible for skill shortcut prefix")
	}
	if got := model.ac.completion(); got != "/k8s-debug " {
		t.Fatalf("completion = %q, want /k8s-debug", got)
	}
	if view := model.ac.View(80); !strings.Contains(view, "/k8s-debug") || !strings.Contains(view, "Diagnose Kubernetes failures.") {
		t.Fatalf("autocomplete view missing skill:\n%s", view)
	}
}

func TestSlashAutocompleteDoesNotIncludeSkillsWhenDisabled(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Skills: []skills.Skill{{
			Name:        "k8s-debug",
			Description: "Diagnose Kubernetes failures.",
		}},
		SkillsConfig: configschema.SkillsConfig{Enabled: false},
	})

	model = typeRunes(t, model, "/k8")

	if model.ac.View(80) != "" {
		t.Fatalf("disabled skill should not be in autocomplete:\n%s", model.ac.View(80))
	}
}

func TestSubmitMessageInjectsReferencedLocalFileContext(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("project notes"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	provider := &captureStreamProvider{}
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{
		Cluster:            "test",
		Model:              "m",
		Provider:           provider,
		Conv:               conv,
		LocalWorkspaceRoot: workspace,
	})

	next, cmd := model.submitMessage("summarize @README.md", nil)
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start stream")
	}
	execMaybeBatch(t, cmd)
	if provider.req == nil {
		t.Fatal("provider did not receive request")
	}
	msgs := provider.req.Messages
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v, want one user message", msgs)
	}
	if !strings.Contains(msgs[0].Content, "summarize @README.md") ||
		!strings.Contains(msgs[0].Content, `<file path="README.md">`) ||
		!strings.Contains(msgs[0].Content, "project notes") {
		t.Fatalf("message missing referenced file context:\n%s", msgs[0].Content)
	}
	if len(model.messages) != 1 || model.messages[0].content != "summarize @README.md" {
		t.Fatalf("visible messages = %#v, want original user input", model.messages)
	}
}

func TestSubmitMessageWithInvalidReferenceDoesNotCallProvider(t *testing.T) {
	workspace := t.TempDir()
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:            "test",
		Model:              "m",
		Provider:           provider,
		Conv:               conversation.New("test", nil, "model"),
		LocalWorkspaceRoot: workspace,
	})

	next, cmd := model.submitMessage("read @missing.md", nil)
	model = next.(Model)
	if cmd != nil {
		t.Fatal("invalid file reference should not start stream")
	}
	if provider.req != nil {
		t.Fatal("provider should not be called for invalid file reference")
	}
	if !strings.Contains(model.status, "File reference error") {
		t.Fatalf("status = %q", model.status)
	}
}

func TestSystemPromptIncludesSubagentPolicyWhenEnabled(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "production",
		Model:   "claude-sonnet",
		Subagents: configschema.SubagentConfig{
			Enabled: true,
		},
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, want := range []string{
		"Subagent policy:",
		"Use subagents_run",
		"Do not delegate destructive actions",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSubagentsRunToolOnlyExposedWhenEnabled(t *testing.T) {
	disabled := NewModel(ModelConfig{})
	for _, tool := range disabled.availableToolDefs() {
		if tool.Name == metaToolSubagentsRun {
			t.Fatal("subagents_run exposed while subagents are disabled")
		}
	}

	enabled := NewModel(ModelConfig{Subagents: configschema.SubagentConfig{Enabled: true}})
	found := false
	for _, tool := range enabled.availableToolDefs() {
		if tool.Name == metaToolSubagentsRun {
			found = true
		}
	}
	if !found {
		t.Fatal("subagents_run not exposed while subagents are enabled")
	}
}

func TestConfigPageDoesNotExposeSubagentDefaultModel(t *testing.T) {
	screen := newConfigScreen(&configschema.GlobalConfig{})

	for _, item := range screen.items {
		if item.Key == "subagents.default_model" {
			t.Fatalf("config screen exposed %s; subagents should inherit the main model", item.Key)
		}
	}
}

func TestNewSubagentRequestUsesCurrentModelAfterSwitch(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "claude"})

	model, _ = model.switchModel("gpt")
	req := model.newSubagentRequest(subagent.RoleInvestigator, "hello", nil)

	if req.Model != "gpt" {
		t.Fatalf("subagent model = %q, want current main model gpt", req.Model)
	}
}

func TestRunSubagentUsesProviderFromCurrentModelAfterSwitch(t *testing.T) {
	var requestedModels []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if model, _ := body["model"].(string); model != "" {
			requestedModels = append(requestedModels, model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"subagent ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "claude",
		ModelConfigs: []configschema.ModelConfig{
			{Name: "claude", Type: "openai", Endpoint: server.URL, Model: "claude-compatible", APIKey: "sk-test"},
			{Name: "gpt", Type: "openai", Endpoint: server.URL, Model: "gpt-4.1", APIKey: "sk-test"},
		},
	})

	model, _ = model.switchModel("gpt")
	result := model.runSubagent(context.Background(), model.newSubagentRequest(subagent.RoleInvestigator, "hello", nil))

	if result.Err != nil {
		t.Fatalf("runSubagent error: %v", result.Err)
	}
	if len(requestedModels) != 1 || requestedModels[0] != "gpt-4.1" {
		t.Fatalf("requested models = %#v, want only gpt-4.1", requestedModels)
	}
}

func TestRiskAssessmentUsesProviderFromCurrentModelAfterSwitch(t *testing.T) {
	var requestedModels []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if model, _ := body["model"].(string); model != "" {
			requestedModels = append(requestedModels, model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"risk_level\":\"confirm\",\"reason\":\"Risky\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	models := []configschema.ModelConfig{
		{Name: "claude", Type: "openai", Endpoint: server.URL, Model: "claude-compatible", APIKey: "sk-test"},
		{Name: "gpt", Type: "openai", Endpoint: server.URL, Model: "gpt-4.1", APIKey: "sk-test"},
	}
	provider, modelName, err := llm.NewProvider(models, "claude")
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider = llm.NewRetryProvider(provider, llm.DefaultRetryConfig())
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider:  provider,
		ModelName: modelName,
	})
	model := NewModel(ModelConfig{
		Cluster:      "test",
		Model:        modelName,
		ModelConfigs: models,
		Provider:     provider,
		Reviewer:     reviewer,
	})

	model, _ = model.switchModel("gpt")
	call := llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /tmp/conan-risk-test"}`)}
	msg := execCmd(t, model.assessToolRisk(7, call))
	if result, ok := msg.(riskAssessmentMsg); !ok {
		t.Fatalf("assessToolRisk returned %T, want riskAssessmentMsg", msg)
	} else if result.err != nil {
		t.Fatalf("risk assessment error: %v", result.err)
	}

	if len(requestedModels) != 1 || requestedModels[0] != "gpt-4.1" {
		t.Fatalf("requested models = %#v, want only gpt-4.1", requestedModels)
	}
}

func TestNewSubagentRequestExcludesPendingToolCallContext(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	conv.AddUser("check nodes")
	conv.AddToolCall("call_pending", metaToolSubagentsRun, `{"tasks":[{"task":"hello"}]}`)
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Conv:    conv,
	})

	req := model.newSubagentRequest(subagent.RoleInvestigator, "hello", nil)

	for _, msg := range req.Context {
		if msg.ToolCallID == "call_pending" {
			t.Fatalf("subagent context included pending tool call: %#v", req.Context)
		}
	}
}

func TestSystemPromptExplainsMemoryPolicy(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conversation.New("production", nil, "claude-sonnet"),
		MemoryStore: store,
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, want := range []string{
		"Memory policy:",
		"Use memory_patch or memory_write_note when the user explicitly asks you to remember",
		"Save durable operational facts",
		"Do not save casual chat",
		"Use memory_search",
		"Use memory_read",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, legacy := range []string{"memory_save"} {
		if strings.Contains(prompt, legacy) {
			t.Fatalf("system prompt should not mention legacy tool name %q:\n%s", legacy, prompt)
		}
	}
}

func TestSystemPromptInjectsCoreRulesAndClusterMemory(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(memoryRoot, "clusters"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("core preference: keep responses terse"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "rules", "ops.md"), []byte("ops rule: require production health checks"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "clusters", "production.md"), []byte("production topology: api runs on node-01"), 0600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conversation.New("production", nil, "claude-sonnet"),
		MemoryStore: store,
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, want := range []string{
		"core preference: keep responses terse",
		"ops rule: require production health checks",
		"production topology: api runs on node-01",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSystemPromptDoesNotFollowClusterMemorySymlink(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "clusters"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("SECRET cluster data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(memoryRoot, "clusters", "production.md")); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		MemoryStore: store,
	})

	prompt := model.buildSystemPromptWithMemory()

	if strings.Contains(prompt, "SECRET") {
		t.Fatalf("system prompt followed cluster memory symlink:\n%s", prompt)
	}
}

func TestSystemPromptBoundsMarkdownAndSQLiteMemory(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "clusters"), 0700); err != nil {
		t.Fatal(err)
	}
	longCluster := strings.Repeat("cluster-prefix ", 400) + "CLUSTER_LONG_TAIL"
	if err := os.WriteFile(filepath.Join(memoryRoot, "clusters", "production.md"), []byte(longCluster), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMemory(memory.MemoryEntry{
		ID:       models.NewID(),
		Category: "experience",
		Title:    "long sqlite",
		Content:  strings.Repeat("sqlite-prefix ", 120) + "SQLITE_LONG_TAIL",
		Tags:     `["test"]`,
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		MemoryStore: store,
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, tail := range []string{"CLUSTER_LONG_TAIL", "SQLITE_LONG_TAIL"} {
		if strings.Contains(prompt, tail) {
			t.Fatalf("system prompt included unbounded memory tail %q:\n%s", tail, prompt)
		}
	}
	if len(prompt) > 7000 {
		t.Fatalf("system prompt length = %d, want bounded prompt under 7000", len(prompt))
	}
}

func TestSystemPromptUsesConfiguredMemoryBudgets(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(memoryRoot, "clusters"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "rules", "ops.md"), []byte(strings.Repeat("rules-prefix ", 40)+"RULES_TAIL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "clusters", "production.md"), []byte(strings.Repeat("cluster-prefix ", 40)+"CLUSTER_TAIL"), 0600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		MemoryStore: store,
		Memory: configschema.MemoryConfig{
			RulesTokenBudget:     80,
			KnowledgeTokenBudget: 90,
		},
	})

	prompt := model.buildSystemPromptWithMemory()

	for _, tail := range []string{"RULES_TAIL", "CLUSTER_TAIL"} {
		if strings.Contains(prompt, tail) {
			t.Fatalf("system prompt ignored configured memory budget and included %q:\n%s", tail, prompt)
		}
	}
	if !strings.Contains(prompt, "[truncated]") {
		t.Fatalf("system prompt should mark budgeted memory as truncated:\n%s", prompt)
	}
}

func TestSystemPromptSkipsSQLiteMemoryDuplicatedInMarkdown(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "clusters"), 0700); err != nil {
		t.Fatal(err)
	}
	duplicate := "api runs on node-01"
	if err := os.WriteFile(filepath.Join(memoryRoot, "clusters", "production.md"), []byte(duplicate), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMemory(memory.MemoryEntry{
		ID:       models.NewID(),
		Category: "topology",
		Title:    "api location",
		Content:  duplicate,
		Tags:     `["test"]`,
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		MemoryStore: store,
	})

	prompt := model.buildSystemPromptWithMemory()

	if count := strings.Count(prompt, duplicate); count != 1 {
		t.Fatalf("duplicate memory appeared %d times, want once:\n%s", count, prompt)
	}
	if strings.Contains(prompt, "[Memory Context]") {
		t.Fatalf("duplicate SQLite memory should be skipped when markdown already contains it:\n%s", prompt)
	}
}

func TestExplicitRememberMessageAutoSavesMemory(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conversation.New("production", nil, "claude-sonnet"),
		MemoryStore: store,
	})

	next, _ := model.submitMessage("记住生产集群 API 地址是 https://api.example.com", nil)
	model = next.(Model)

	data, err := os.ReadFile(filepath.Join(store.Dir(), "memory", "clusters", "production.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "生产集群 API 地址是 https://api.example.com") {
		t.Fatalf("cluster markdown did not contain explicit memory:\n%s", string(data))
	}

	results, err := store.SearchMemories("api.example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("topology explicit remember should route to markdown, got SQLite results: %#v", results)
	}
}

func TestExplicitRememberNameWritesProfileMarkdown(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conversation.New("production", nil, "claude-sonnet"),
		MemoryStore: store,
	})

	next, _ := model.submitMessage("记住我叫小王", nil)
	model = next.(Model)

	data, err := os.ReadFile(filepath.Join(store.Dir(), "memory", "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "我叫小王") {
		t.Fatalf("profile markdown did not contain explicit memory:\n%s", string(data))
	}

	results, err := store.SearchMemories("小王", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("profile explicit remember should route to markdown, got SQLite results: %#v", results)
	}
}

func TestExplicitRememberEventWritesSQLiteMemoryWithSourceConversation(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conv := conversation.New("production", nil, "claude-sonnet")
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conv,
		MemoryStore: store,
	})

	next, _ := model.submitMessage("remember that deploy v2 happened today", nil)
	model = next.(Model)

	results, err := store.SearchMemories("deploy v2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 SQLite memory, got %d: %#v", len(results), results)
	}
	got := results[0]
	if got.ID == "" {
		t.Fatalf("memory ID is empty: %#v", got)
	}
	var tags []string
	if err := json.Unmarshal([]byte(got.Tags), &tags); err != nil {
		t.Fatalf("memory tags are not valid JSON: %q: %v", got.Tags, err)
	}
	for _, want := range []string{"user", "explicit"} {
		if !stringSliceContains(tags, want) {
			t.Fatalf("memory tags = %#v, want %q", tags, want)
		}
	}
	if got.SourceConv != conv.ID() {
		t.Fatalf("SourceConv = %q, want %q", got.SourceConv, conv.ID())
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "remember that deploy v2 happened today" {
		t.Fatalf("conversation messages = %#v, want source user message", msgs)
	}
}

func TestExplicitRememberRejectsSecretLikeContent(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "claude-sonnet",
		Conv:        conversation.New("production", nil, "claude-sonnet"),
		MemoryStore: store,
	})

	next, _ := model.submitMessage("remember that my token is abc", nil)
	model = next.(Model)

	results, err := store.ListMemories("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("secret-like explicit remember should not create SQLite memory: %#v", results)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), "memory")); !os.IsNotExist(err) {
		t.Fatalf("secret-like explicit remember should not create markdown memory dir, stat err=%v", err)
	}
}

func TestPostTurnExtractionWritesIncidentNote(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	extractor := &stubMemoryExtractor{
		candidates: []memory.MemoryCandidate{{
			ID:       models.NewID(),
			Category: "incident",
			Title:    "API OOM",
			Content:  "Root cause was cache pressure.",
			Tags:     []string{"api", "oom"},
		}},
	}
	model := NewModel(ModelConfig{
		Cluster:         "production",
		Model:           "claude-sonnet",
		Conv:            conversation.New("production", nil, "claude-sonnet"),
		MemoryStore:     store,
		MemoryExtractor: extractor,
	})

	model.runMemoryExtraction("api oom", "Root cause was cache pressure.")

	if len(extractor.inputs) != 1 {
		t.Fatalf("extractor calls = %d, want 1", len(extractor.inputs))
	}
	entries, err := os.ReadDir(filepath.Join(store.Dir(), "memory", "incidents"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("incident note count = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(store.Dir(), "memory", "incidents", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	note := string(data)
	for _, want := range []string{"API OOM", "Root cause was cache pressure.", "api", "oom"} {
		if !strings.Contains(note, want) {
			t.Fatalf("incident note missing %q:\n%s", want, note)
		}
	}
}

func TestPostTurnExtractionUsesBoundedContext(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	extractor := &stubMemoryExtractor{}
	model := NewModel(ModelConfig{
		Cluster:         "production",
		Model:           "claude-sonnet",
		MemoryStore:     store,
		MemoryExtractor: extractor,
	})

	model.runMemoryExtraction("api oom", "Root cause was cache pressure.")

	if len(extractor.hasDeadline) != 1 || !extractor.hasDeadline[0] {
		t.Fatalf("memory extractor context deadline missing: %#v", extractor.hasDeadline)
	}
	remaining := time.Until(extractor.deadlines[0])
	if remaining <= 0 || remaining > memoryExtractionTimeout {
		t.Fatalf("memory extractor deadline remaining = %v, want within %v", remaining, memoryExtractionTimeout)
	}
}

func TestPostTurnExtractionRejectsSecretLikeCandidate(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	extractor := &stubMemoryExtractor{
		candidates: []memory.MemoryCandidate{{
			ID:       models.NewID(),
			Category: "event",
			Title:    "Credential",
			Content:  "password=abc",
			Tags:     []string{"security"},
		}},
	}
	model := NewModel(ModelConfig{
		Cluster:         "production",
		Model:           "claude-sonnet",
		Conv:            conversation.New("production", nil, "claude-sonnet"),
		MemoryStore:     store,
		MemoryExtractor: extractor,
	})

	model.runMemoryExtraction("we rotated credentials", "Do not store the password value.")

	results, err := store.ListMemories("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("secret-like extraction candidate should not persist: %#v", results)
	}
}

func TestPostTurnExtractionRequiresEvidence(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	extractor := &stubMemoryExtractor{
		candidates: []memory.MemoryCandidate{{
			ID:       models.NewID(),
			Category: "incident",
			Title:    "Invented Incident",
			Content:  "Root cause was a database failover.",
			Tags:     []string{"api"},
		}},
	}
	model := NewModel(ModelConfig{
		Cluster:         "production",
		Model:           "claude-sonnet",
		Conv:            conversation.New("production", nil, "claude-sonnet"),
		MemoryStore:     store,
		MemoryExtractor: extractor,
	})

	model.runMemoryExtraction("api oom", "Root cause was cache pressure.")

	if _, err := os.Stat(filepath.Join(store.Dir(), "memory", "incidents")); !os.IsNotExist(err) {
		t.Fatalf("unsupported evidence candidate should not create incidents dir, stat err=%v", err)
	}
	results, err := store.ListMemories("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("unsupported evidence candidate should not create SQLite memory: %#v", results)
	}
}

func TestPostTurnExtractionStopEventPassesTurnInput(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	extractor := &stubMemoryExtractor{}
	conv := conversation.New("production", nil, "claude-sonnet")
	model := NewModel(ModelConfig{
		Cluster:         "production",
		Model:           "claude-sonnet",
		Conv:            conv,
		MemoryStore:     store,
		MemoryExtractor: extractor,
	})
	model.messages = append(model.messages, chatMsg{role: "user", content: "api oom"})
	conv.AddUser("api oom")
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamBuf = "Root cause was cache pressure."

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("normal stop should not return a command")
	}
	if len(extractor.inputs) != 1 {
		t.Fatalf("extractor calls = %d, want 1", len(extractor.inputs))
	}
	want := MemoryExtractionInput{
		Cluster:   "production",
		Model:     "claude-sonnet",
		User:      "api oom",
		Assistant: "Root cause was cache pressure.",
	}
	if extractor.inputs[0] != want {
		t.Fatalf("extractor input = %#v, want %#v", extractor.inputs[0], want)
	}
	if len(model.messages) != 2 || model.messages[1].role != "assistant" || model.messages[1].content != want.Assistant {
		t.Fatalf("messages = %#v, want assistant content appended before extraction", model.messages)
	}
}

func TestInitialModelViewRendersStartupOverview(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
		{Name: "node-03", Host: "10.0.1.3", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", Nodes: nodes})
	model.selectedNodes = map[string]bool{"node-01": true, "node-03": true}

	view := model.View()

	for _, want := range []string{
		"█████  █████  █   █",
		"Cluster   production",
		"Model     claude-sonnet",
		"Nodes     2/3 selected, 2 online",
		"● node-01  10.0.1.1  Online   selected",
		"○ node-02  10.0.1.2  Offline  unselected",
		"● node-03  10.0.1.3  Online   selected",
		"Type a message or /help",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing startup overview part %q:\n%s", want, view)
		}
	}
}

func TestInitialModelViewUsesCompactStartupOverviewInSmallWindow(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)

	view := model.View()

	if strings.Contains(view, "██████╗ ██████╗") {
		t.Fatalf("small window should not render clipped full logo:\n%s", view)
	}
	for _, want := range []string{
		"CONAN",
		"Cluster   production",
		"Model     claude-sonnet",
		"Type a message or /help",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact startup overview missing %q:\n%s", want, view)
		}
	}
}

func TestStartupOverviewLogoUsesSolidBlockGlyphs(t *testing.T) {
	view := renderStartupOverview("production", "claude-sonnet", nil, nil, uiLanguageEnglish, 80, 20, 0)

	for _, glyph := range []string{"╔", "╗", "╚", "╝", "═", "║"} {
		if strings.Contains(view, glyph) {
			t.Fatalf("startup logo contains box-drawing glyph %q that can render clipped:\n%s", glyph, view)
		}
	}
	if !strings.Contains(view, "█████") {
		t.Fatalf("startup logo should render a solid block wordmark:\n%s", view)
	}
}

func TestStartupOverviewAnimationStaysInsideLogo(t *testing.T) {
	initial := renderStartupOverview("production", "claude-sonnet", nil, nil, uiLanguageEnglish, 80, 20, 0)
	animated := renderStartupOverview("production", "claude-sonnet", nil, nil, uiLanguageEnglish, 80, 20, 1)

	if initial == animated {
		t.Fatalf("startup logo should visibly animate between frames:\n%s", animated)
	}
	for _, line := range strings.Split(animated, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, frame := range startupFrames {
			if trimmed == frame {
				t.Fatalf("startup animation rendered standalone frame %q:\n%s", frame, animated)
			}
		}
	}
}

func TestStartupTickAnimatesInitialOverview(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)

	initial := model.View()
	next, cmd := model.Update(startupTickMsg{})
	model = next.(Model)
	animated := model.View()

	if initial == animated {
		t.Fatalf("startup tick should change rendered frame:\n%s", animated)
	}
	if cmd == nil {
		t.Fatal("startup tick should schedule another tick while overview is visible")
	}

	model.messages = append(model.messages, chatMsg{role: "user", content: "hello"})
	next, cmd = model.Update(startupTickMsg{})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("startup tick should stop after chat starts")
	}
}

func TestInputRendersAsBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)
	model.input = "hello"

	view := model.View()

	for _, want := range []string{"╭──────────────────────────────────────╮", "│ ❯ hello", "╰──────────────────────────────────────╯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing input box part %q:\n%s", want, view)
		}
	}
}

func TestInputRendersCursor(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	model.input = "hello"

	view := model.View()

	if !strings.Contains(view, "❯ hello█") {
		t.Fatalf("view missing input cursor:\n%s", view)
	}
}

func TestChatViewRendersShortcutHintsBelowInputBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)
	model.input = "hello"

	view := model.View()
	inputIndex := strings.LastIndex(view, "│ ❯ hello")
	borderIndex := strings.LastIndex(view, "╯")
	hintIndex := strings.LastIndex(view, "↑/↓ Scroll")

	if inputIndex < 0 || borderIndex < 0 {
		t.Fatalf("view missing input box:\n%s", view)
	}
	if hintIndex < 0 {
		t.Fatalf("view missing shortcut hints:\n%s", view)
	}
	if hintIndex < borderIndex {
		t.Fatalf("shortcut hints should render below input box:\n%s", view)
	}
	if hintIndex < inputIndex {
		t.Fatalf("shortcut hints should not render inside input line:\n%s", view)
	}
	for _, want := range []string{"Ctrl+P/N History", "PgUp/PgDn Page", "Ctrl+O Tool", "Ctrl+A Agents", "Ctrl+L Clear"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing shortcut hint %q:\n%s", want, view)
		}
	}
}

func TestChatViewRendersChineseShortcutHints(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", UILanguage: "zh-CN"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"↑/↓ 滚动", "Ctrl+P/N 历史", "PgUp/PgDn 翻页", "Ctrl+O 工具输出", "Ctrl+A Agents", "Ctrl+L 清屏"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing Chinese shortcut hint %q:\n%s", want, view)
		}
	}
}

func TestMultilinePasteRendersCompactLineCount(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	pasted := "alpha\nbeta\ngamma\n"

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	model = next.(Model)

	if model.input != pasted {
		t.Fatalf("input = %q, want pasted text preserved", model.input)
	}
	view := model.View()
	if !strings.Contains(view, "❯ Pasted 3 lines█") {
		t.Fatalf("view missing compact pasted input:\n%s", view)
	}
	if strings.Contains(view, "│ ❯ alpha") {
		t.Fatalf("view rendered raw pasted input:\n%s", view)
	}
	if model.status != "Pasted 3 lines into input" {
		t.Fatalf("status = %q, want pasted line count", model.status)
	}
}

func TestMultilinePasteSubmitsFullInput(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	pasted := "alpha\nbeta\ngamma"

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if len(model.messages) != 1 || model.messages[0].content != pasted {
		t.Fatalf("messages = %#v, want full pasted input", model.messages)
	}
	if got := conv.Messages()[0].Content; got != pasted {
		t.Fatalf("conversation content = %q, want full pasted input", got)
	}
}

func TestImagePathPasteAttachesImageChip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "screen.png")
	if err := os.WriteFile(path, smallPNG(t), 0600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", ConfigHome: dir})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(path), Paste: true})
	model = next.(Model)

	if len(model.pendingImages) != 1 {
		t.Fatalf("pendingImages = %#v, want one image", model.pendingImages)
	}
	view := model.View()
	if !strings.Contains(view, "[Image #1]") {
		t.Fatalf("view missing image chip:\n%s", view)
	}
	if model.input != "" {
		t.Fatalf("input = %q, want image path paste consumed", model.input)
	}
}

func TestEmptyClipboardPasteIsNoop(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.status = "Ready"

	next, _ := model.Update(clipboardPasteMsg{})
	model = next.(Model)

	if model.status != "Ready" {
		t.Fatalf("status = %q, want unchanged", model.status)
	}
	if model.input != "" || len(model.pendingImages) != 0 {
		t.Fatalf("input = %q pendingImages = %#v, want unchanged", model.input, model.pendingImages)
	}
}

func TestImageSubmitExposesAnalyzeToolToMainModel(t *testing.T) {
	dir := t.TempDir()
	provider := &captureStreamProvider{}
	vision := &stubVisionProvider{summary: "Image #1: terminal screenshot with error E_CONN."}
	conv := conversation.New("test", nil, "model")
	attachment, err := saveImageAttachment(smallPNG(t), "screen.png", "image/png", filepath.Join(dir, "attachments"), 1)
	if err != nil {
		t.Fatalf("save image: %v", err)
	}
	model := NewModel(ModelConfig{
		Cluster:        "test",
		Model:          "m",
		ConfigHome:     dir,
		Provider:       provider,
		VisionProvider: vision,
		Conv:           conv,
	})
	model.pendingImages = []imageAttachment{attachment}

	next, cmd := model.submitMessage("what failed?", nil)
	model = next.(Model)
	if cmd == nil {
		t.Fatal("image submit should start main model stream")
	}
	execMaybeBatch(t, cmd)

	if vision.req != nil {
		t.Fatalf("vision should not run before image_analyze tool call: %#v", vision.req)
	}
	if provider.req == nil {
		t.Fatal("main provider request was not started")
	}
	got := provider.req.Messages[0].Content
	if !strings.Contains(got, "what failed?") || !strings.Contains(got, "image_analyze") || !strings.Contains(got, "[Image #1]") {
		t.Fatalf("main provider input missing image tool context:\n%s", got)
	}
	if strings.Contains(got, "data:image") || strings.Contains(got, "E_CONN") {
		t.Fatalf("main provider input leaked image data:\n%s", got)
	}
	foundTool := false
	for _, tool := range provider.req.Tools {
		if tool.Name == metaToolImageAnalyze {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("tools = %#v, want image_analyze", provider.req.Tools)
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].content, "[Image #1]") {
		t.Fatalf("visible messages = %#v, want one user message with image chip", model.messages)
	}
	if len(conv.Messages()) != 1 {
		t.Fatalf("conversation messages = %d, want one", len(conv.Messages()))
	}

	msg := execCmd(t, model.dispatchTool(0, llm.ToolCall{
		ID:        "img1",
		Name:      metaToolImageAnalyze,
		Arguments: json.RawMessage(`{"image_id":1,"question":"what failed?"}`),
	}))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if vision.req == nil || len(vision.req.Images) != 1 {
		t.Fatalf("vision request = %#v, want one image after tool call", vision.req)
	}
	if !strings.Contains(result.Results[0].Output, "E_CONN") {
		t.Fatalf("tool output = %q, want vision summary", result.Results[0].Output)
	}
}

func TestAtImageReferenceExposesAnalyzeTool(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "screen.png"), smallPNG(t), 0600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:            "test",
		Model:              "m",
		Provider:           provider,
		VisionProvider:     &stubVisionProvider{summary: "ok"},
		LocalWorkspaceRoot: workspace,
		ConfigHome:         workspace,
		Conv:               conversation.New("test", nil, "model"),
	})

	next, cmd := model.submitMessage("inspect @screen.png", nil)
	model = next.(Model)
	execMaybeBatch(t, cmd)

	if len(model.attachedImages) != 1 {
		t.Fatalf("attachedImages = %#v, want one", model.attachedImages)
	}
	if provider.req == nil {
		t.Fatal("main provider request was not started")
	}
	got := provider.req.Messages[0].Content
	if !strings.Contains(got, "image_analyze") || !strings.Contains(got, "[Image #1]") {
		t.Fatalf("main provider input missing image tool context:\n%s", got)
	}
	if strings.Contains(got, "\x89PNG") {
		t.Fatalf("main provider input included raw png bytes:\n%s", got)
	}
}

func TestAutocompleteRendersBelowInputBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = next.(Model)
	for _, r := range "/cl" {
		next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}

	view := model.View()
	inputIndex := strings.Index(view, "│ ❯ /cl")
	acIndex := strings.Index(view, "▸ /clear")
	if inputIndex == -1 || acIndex == -1 {
		t.Fatalf("view missing input or autocomplete:\n%s", view)
	}
	if inputIndex > acIndex {
		t.Fatalf("autocomplete rendered above input:\n%s", view)
	}
	if !strings.Contains(view, "╭──────────────────────────────────────╮") {
		t.Fatalf("view missing full-width autocomplete border:\n%s", view)
	}
}

func TestEnterCompletesVisibleAutocompleteBeforeSubmit(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	for _, r := range "/ex" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("Enter should complete autocomplete before submitting")
	}
	if model.input != "/exit " {
		t.Fatalf("input = %q, want completed /exit", model.input)
	}
	if model.ac.visible {
		t.Fatal("autocomplete should hide after Enter completion")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("second Enter should submit completed command")
	}
}

func TestTabCompletesFileReferenceAutocomplete(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", LocalWorkspaceRoot: workspace})
	model = typeRunes(t, model, "read @RE")
	if !model.ac.visible {
		t.Fatal("file reference autocomplete should be visible")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(Model)

	if model.input != "read @README.md " {
		t.Fatalf("input = %q, want file reference completion", model.input)
	}
	if model.ac.visible {
		t.Fatal("autocomplete should hide after file completion")
	}
}

func TestAssistantMessageRendersElapsedAfterOutput(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.messages = []chatMsg{
		{role: "user", content: "hello"},
		{role: "assistant", content: "hi", elapsed: 1200 * time.Millisecond},
	}

	view := model.View()

	if strings.Contains(view, "── 1.2s ──") {
		t.Fatalf("view should not render elapsed as divider:\n%s", view)
	}
	if !strings.Contains(view, "hi\n\n✱ Took 1.2s") {
		t.Fatalf("view missing elapsed footer after assistant output:\n%s", view)
	}
}

func TestToolMessageRendersElapsedAfterOutput(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.messages = []chatMsg{
		{
			role:       "tool",
			toolName:   "shell_run",
			toolOutput: "exit_code: 0\nstdout:\nok\n",
			elapsed:    3200 * time.Millisecond,
		},
	}

	view := model.View()

	if strings.Contains(view, "── 3.2s ──") {
		t.Fatalf("view should not render elapsed as divider:\n%s", view)
	}
	outputIndex := strings.Index(view, "ok")
	elapsedIndex := strings.Index(view, "✱ Took 3.2s")
	if outputIndex == -1 || elapsedIndex == -1 || elapsedIndex < outputIndex {
		t.Fatalf("view missing elapsed footer after tool output:\n%s", view)
	}
}

func TestUserMessageRendersFullWidthHighlight(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)
	model.messages = []chatMsg{{role: "user", content: "hello"}}

	view := model.View()

	line := findLineContaining(view, "❯ hello")
	if line == "" {
		t.Fatalf("view missing user message:\n%s", view)
	}
	if got := lipgloss.Width(line); got != 40 {
		t.Fatalf("user message width = %d, want 40:\n%q\nfull view:\n%s", got, line, view)
	}
}

func TestStreamingThinkingRendersInBodyOnly(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model = next.(Model)
	model.messages = []chatMsg{{role: "user", content: "hello"}}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.status = "Thinking..."

	view := model.View()

	if !strings.Contains(view, "Thinking...") {
		t.Fatalf("view missing body thinking indicator:\n%s", view)
	}
	if count := strings.Count(view, "Thinking..."); count != 1 {
		t.Fatalf("Thinking... rendered %d times, want body-only once:\n%s", count, view)
	}
	thinkingIndex := strings.Index(view, "Thinking...")
	inputIndex := strings.Index(view, "│ ❯")
	if inputIndex == -1 || thinkingIndex > inputIndex {
		t.Fatalf("thinking indicator should render in body before input:\n%s", view)
	}
}

func TestSubagentStatusRendersInFooterStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.subagentStatus = "Subagents running: 2 active"

	view := model.View()

	statusIndex := strings.Index(view, "Subagents running: 2 active")
	inputIndex := strings.Index(view, "│ ❯")
	if statusIndex == -1 {
		t.Fatalf("view missing subagent status:\n%s", view)
	}
	if inputIndex == -1 || statusIndex > inputIndex {
		t.Fatalf("subagent status should render before input footer:\n%s", view)
	}
}

func TestManualSubagentStatusUpdatesOnStartAndCompletion(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}})

	model, _ = model.startManualSubagent("reviewer check config")
	if !strings.Contains(model.View(), "Subagent reviewer running") {
		t.Fatalf("view missing running subagent status:\n%s", model.View())
	}

	next, _ := model.Update(subagentCommandResultMsg{result: subagent.Result{
		Role:    subagent.RoleReviewer,
		Summary: "ok",
		Elapsed: 1500 * time.Millisecond,
	}})
	model = next.(Model)

	if !strings.Contains(model.View(), "Subagent reviewer completed in 1.5s") {
		t.Fatalf("view missing completed subagent status:\n%s", model.View())
	}
}

func TestManualSubagentRendersPromptPreviewWithShortID(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}})

	model, _ = model.startManualSubagent("reviewer check config")

	if len(model.subagentRuns) != 1 {
		t.Fatalf("subagent runs = %#v, want one", model.subagentRuns)
	}
	id := model.subagentRuns[0].ID
	if len(id) != 8 {
		t.Fatalf("subagent id = %q, want 8-char short id", id)
	}

	chatView := model.View()
	for _, want := range []string{"subagent " + id, "prompt:", "check config"} {
		if strings.Contains(chatView, want) {
			t.Fatalf("chat view should not leak inline subagent preview (%q):\n%s", want, chatView)
		}
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	pageView := model.View()
	for _, want := range []string{id, "reviewer", "receivin", "check config"} {
		if !strings.Contains(pageView, want) {
			t.Fatalf("subagent page missing %q:\n%s", want, pageView)
		}
	}
	if !strings.Contains(pageView, subagentSpinnerGlyph(0)) {
		t.Fatalf("subagent page missing spinner glyph for active run:\n%s", pageView)
	}
}

func TestCtrlAOpensSubagentListPage(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}})
	model, _ = model.startManualSubagent("reviewer check config")

	chatView := model.View()
	if strings.Contains(chatView, "Role: reviewer\nTask:") {
		t.Fatalf("chat view should not leak full subagent prompt:\n%s", chatView)
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	if model.mode != modeSubagentList {
		t.Fatalf("mode = %v, want modeSubagentList", model.mode)
	}
	listView := model.View()
	for _, want := range []string{"Subagents", "reviewer"} {
		if !strings.Contains(listView, want) {
			t.Fatalf("list view missing %q:\n%s", want, listView)
		}
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	detail := model.View()
	for _, want := range []string{"Conversation", "Role: reviewer", "receiving"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail view missing %q:\n%s", want, detail)
		}
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.subagentDetailVisible {
		t.Fatalf("Esc from detail should return to list")
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("Esc from list should return to chat, got mode %v", model.mode)
	}
}

func TestSubagentsRunStatusSummarizesBatchResults(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	call := llm.ToolCall{ID: "sa1", Name: metaToolSubagentsRun, Arguments: json.RawMessage(`{"tasks":[{"task":"a"},{"task":"b"}]}`)}

	next, _ := model.Update(multiToolResultMsg{
		Call: call,
		Results: []nodeToolResult{{
			Node:    "local",
			Output:  "[investigator:local:ok] a\n[reviewer:local:error: failed] (no summary)",
			Success: false,
		}},
	})
	model = next.(Model)

	if !strings.Contains(model.View(), "Subagents completed: 1 ok, 1 error") {
		t.Fatalf("view missing subagent batch summary:\n%s", model.View())
	}
}

func TestSubagentsRunStatusUpdatesWhenToolCallStarts(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster:   "test",
		Model:     "m",
		Subagents: configschema.SubagentConfig{Enabled: true},
	})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCtx = context.Background()

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID:        "sa1",
		Name:      metaToolSubagentsRun,
		Arguments: json.RawMessage(`{"tasks":[{"task":"a"},{"task":"b"}]}`),
	}})
	model = next.(Model)

	if !strings.Contains(model.View(), "Subagents running: 0/2 active") {
		t.Fatalf("view missing running subagents status:\n%s", model.View())
	}
	if !strings.Contains(model.View(), subagentSpinnerGlyph(model.subagentAnimFrame)) {
		t.Fatalf("chat view missing spinner for active subagents:\n%s", model.View())
	}
}

func TestSubmittingMessageSchedulesThinkingTick(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if !model.streaming {
		t.Fatal("model should be streaming after submit")
	}
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("submit command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want stream start and thinking tick", len(batch))
	}
}

func TestThinkingTickAnimatesWhileWaitingForFirstToken(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	initial := model.View()
	next, cmd := model.Update(thinkingTickMsg{streamID: 1})
	model = next.(Model)
	animated := model.View()

	if initial == animated {
		t.Fatalf("thinking tick should change rendered frame:\n%s", animated)
	}
	if cmd == nil {
		t.Fatal("thinking tick should schedule another tick while waiting")
	}

	model.streamBuf = "hello"
	next, cmd = model.Update(thinkingTickMsg{streamID: 1})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("thinking tick should stop after first token arrives")
	}
}

func TestThinkingTickContinuesWhileReasoningIsVisible(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamStartedAt = time.Now().Add(-1500 * time.Millisecond)
	model.streamReasoningBuf = "checking cluster state"

	next, cmd := model.Update(thinkingTickMsg{streamID: 1})
	model = next.(Model)
	view := model.View()

	if cmd == nil {
		t.Fatal("thinking tick should continue while reasoning is visible")
	}
	if !strings.Contains(view, "Thinking: checking cluster state") {
		t.Fatalf("view missing reasoning line:\n%s", view)
	}
	if !strings.Contains(view, "Esc to interrupt") || !strings.Contains(view, "1.") {
		t.Fatalf("reasoning view should keep elapsed interrupt meta updating:\n%s", view)
	}
}

func TestThinkingRendersElapsedAndEscInterruptHint(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamStartedAt = time.Now().Add(-2300 * time.Millisecond)
	model.status = "Thinking..."

	view := model.View()

	for _, want := range []string{"Thinking...", "2.", "Esc to interrupt"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if count := strings.Count(view, "Thinking..."); count != 1 {
		t.Fatalf("Thinking... rendered %d times, want body-only once:\n%s", count, view)
	}
}

func TestStreamingReasoningRendersOnlyLastLineInLightStyle(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamReasoningBuf = "first line\nsecond line\nfinal thought"

	view := model.View()

	if !strings.Contains(view, "Thinking: final thought") {
		t.Fatalf("view missing last reasoning line:\n%s", view)
	}
	if strings.Contains(view, "first line") || strings.Contains(view, "second line") {
		t.Fatalf("view should only render last reasoning line:\n%s", view)
	}
	if strings.Contains(view, "final thought▌") {
		t.Fatalf("reasoning should not use normal streaming cursor style:\n%s", view)
	}
}

func findLineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func typeRunes(t *testing.T, model Model, input string) Model {
	t.Helper()
	for _, r := range input {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	return model
}

func typeAndEnter(t *testing.T, model Model, input string) Model {
	t.Helper()
	model = typeRunes(t, model, input)
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model)
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestTypingAndEnterAddsUserMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "❯ hello") {
		t.Fatalf("view missing submitted message:\n%s", view)
	}
	if model.input != "" {
		t.Fatalf("input = %q, want empty", model.input)
	}
	if !model.streaming {
		t.Fatal("should be streaming after submit")
	}
	if cmd == nil {
		t.Fatal("expected a Cmd to be returned after submit")
	}
}

func TestTypingSpaceAddsInputSpace(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'i'}},
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'t'}},
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'r'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
	} {
		next, _ := model.Update(key)
		model = next.(Model)
	}

	if model.input != "hi there" {
		t.Fatalf("input = %q, want space preserved", model.input)
	}
}

func TestTypingCInChatInputWorksWhenSubagentRunsExist(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "m", Status: "running"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)

	if model.input != "c" {
		t.Fatalf("input = %q, want typed c preserved", model.input)
	}
}

func TestCtrlPNNavigatesInputHistoryAndRestoresDraft(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model = typeAndEnter(t, model, "first")
	model = typeAndEnter(t, model, "second")
	model = typeRunes(t, model, "draft")

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	model = next.(Model)
	if model.input != "second" {
		t.Fatalf("after first Ctrl+P input = %q, want second", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	model = next.(Model)
	if model.input != "first" {
		t.Fatalf("after second Ctrl+P input = %q, want first", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	model = next.(Model)
	if model.input != "first" {
		t.Fatalf("extra Ctrl+P input = %q, want first", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	model = next.(Model)
	if model.input != "second" {
		t.Fatalf("after first Ctrl+N input = %q, want second", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	model = next.(Model)
	if model.input != "draft" {
		t.Fatalf("after second Ctrl+N input = %q, want restored draft", model.input)
	}
}

func TestThinkingCommandSendsMessageWithThinkingOverride(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: provider, Conv: conv})
	model = typeRunes(t, model, "/thinking 你好")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("/thinking should start a stream")
	}
	execMaybeBatch(t, cmd)

	if provider.req == nil {
		t.Fatal("provider did not receive request")
	}
	if provider.req.Thinking == nil || !*provider.req.Thinking {
		t.Fatalf("request Thinking = %#v, want true", provider.req.Thinking)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "你好" {
		t.Fatalf("conversation messages = %#v, want /thinking arg as user message", msgs)
	}
	if len(model.messages) != 1 || model.messages[0].content != "你好" {
		t.Fatalf("ui messages = %#v, want /thinking arg as user message", model.messages)
	}
}

func TestClearCommandClearsMessages(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	for _, r := range "/clear" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "❯ hello") {
		t.Fatalf("clear did not remove message:\n%s", view)
	}
	if !strings.Contains(view, "Conversation cleared") {
		t.Fatalf("clear status missing:\n%s", view)
	}
}

func TestCompactCommandReplacesContextWithSummaryTailAndArchivesFullHistory(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	conv := conversation.New("test", nil, "model")
	conv.AddUser("old user message")
	conv.AddAssistant("old assistant reply")
	conv.AddUser("middle user message")
	conv.AddAssistant("middle assistant reply")
	conv.AddUser("new user message")
	conv.AddAssistant("new assistant reply")

	provider := &compactCaptureProvider{content: "The deployment design is approved. Next step: implement compact."}
	model := NewModel(ModelConfig{Cluster: "test", Model: "model", Provider: provider, Conv: conv, MemoryStore: store})

	next, cmd := model.applyCommand(SlashCommand{Kind: CommandCompact, Arg: "focus on deployment decisions"})
	model = next
	if cmd == nil {
		t.Fatal("compact command should return a command")
	}
	if !model.compacting {
		t.Fatal("compact should show progress while running")
	}
	if !strings.Contains(model.status, "Compacting context") {
		t.Fatalf("compact status = %q, want progress", model.status)
	}
	msg := compactResultFromCmd(t, cmd)
	nextModel, _ := model.Update(msg)
	model = nextModel.(Model)

	if provider.req == nil {
		t.Fatal("compact should call provider.Chat")
	}
	if len(provider.req.Messages) != 6 {
		t.Fatalf("compact request messages = %d, want 6", len(provider.req.Messages))
	}
	if !strings.Contains(provider.req.SystemPrompt, "focus on deployment decisions") {
		t.Fatalf("compact prompt missing focus: %q", provider.req.SystemPrompt)
	}

	msgs := model.conv.Messages()
	if len(msgs) != 5 {
		t.Fatalf("compacted messages len = %d, want summary + 4 tail messages", len(msgs))
	}
	msgData, _ := json.Marshal(msgs)
	msgText := string(msgData)
	if msgs[0].Role != "user" || !strings.Contains(msgs[0].Content, "Previous conversation compacted") || !strings.Contains(msgs[0].Content, "deployment design is approved") {
		t.Fatalf("first compacted message = %#v", msgs[0])
	}
	if strings.Contains(msgText, "old user message") {
		t.Fatalf("compacted context still contains oldest message: %s", msgText)
	}
	if !strings.Contains(msgText, "new assistant reply") {
		t.Fatalf("compacted context should keep recent tail: %s", msgText)
	}
	view := model.View()
	if strings.Contains(view, "Previous conversation compacted") || strings.Contains(view, "deployment design is approved") {
		t.Fatalf("view should not display compacted context:\n%s", view)
	}
	if !strings.Contains(view, "Compact complete") {
		t.Fatalf("view missing compact completion:\n%s", view)
	}

	archives, err := filepath.Glob(filepath.Join(store.Dir(), "archives", conv.ID(), "compact-*.json"))
	if err != nil {
		t.Fatalf("glob archive: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archives = %#v, want one archive", archives)
	}
	archiveData, err := os.ReadFile(archives[0])
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !strings.Contains(string(archiveData), "old user message") {
		t.Fatalf("archive missing full pre-compact history: %s", archiveData)
	}

	model.saveCurrentConversation()
	rec, err := store.LoadConversation(conv.ID())
	if err != nil {
		t.Fatalf("load saved conversation: %v", err)
	}
	if strings.Contains(rec.Messages, "old user message") {
		t.Fatalf("saved resumable conversation should be compacted, got: %s", rec.Messages)
	}
	if !strings.Contains(rec.Messages, "Previous conversation compacted") {
		t.Fatalf("saved resumable conversation missing summary: %s", rec.Messages)
	}
}

func TestContextLimitLookupIsCaseInsensitiveAndUsesConfiguredModelID(t *testing.T) {
	if limit, ok := lookupModelContextLimit("GPT-4O"); !ok || limit != 128000 {
		t.Fatalf("lookupModelContextLimit(GPT-4O) = %d, %v; want 128000, true", limit, ok)
	}
	if limit, ok := lookupModelContextLimit("GPT-5.5"); !ok || limit != 1050000 {
		t.Fatalf("lookupModelContextLimit(GPT-5.5) = %d, %v; want 1050000, true", limit, ok)
	}

	model := NewModel(ModelConfig{
		Model: "alias",
		ModelConfigs: []configschema.ModelConfig{
			{Name: "alias", Model: "gPt-4O"},
		},
	})
	meter := model.currentContextMeter()
	if !meter.Known || meter.Limit != 128000 {
		t.Fatalf("currentContextMeter() = %+v, want known 128000", meter)
	}
}

func TestContextMeterRendersKnownAndUnknownLimits(t *testing.T) {
	known := renderContextMeter(contextMeter{Used: 1200, Limit: 128000, Known: true}, uiLanguageEnglish, 80)
	if !strings.Contains(known, "Context 1,200/128,000") {
		t.Fatalf("known meter missing context counts:\n%s", known)
	}
	if !strings.Contains(known, "(0%)") {
		t.Fatalf("known meter missing percent:\n%s", known)
	}
	if !strings.Contains(known, "[") {
		t.Fatalf("known meter missing progress bar:\n%s", known)
	}

	unknown := renderContextMeter(contextMeter{Used: 1200, Known: false}, uiLanguageEnglish, 80)
	if !strings.Contains(unknown, "Context 1,200/unknown context limit") {
		t.Fatalf("unknown meter missing label:\n%s", unknown)
	}
}

func TestViewRendersFooterMetaBelowInputBox(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "gpt-4o", Conv: conversation.New("test", nil, "gpt-4o")})
	model.width = 80
	model.input = "hello"

	view := model.View()
	contextIndex := strings.LastIndex(view, "Context ")
	modelIndex := strings.LastIndex(view, "gpt-4o")
	clusterIndex := strings.LastIndex(view, "test")
	borderIndex := strings.LastIndex(view, "╯")
	if contextIndex < 0 {
		t.Fatalf("view missing context meter:\n%s", view)
	}
	if modelIndex < 0 || clusterIndex < 0 || !strings.Contains(view, " · ") {
		t.Fatalf("view missing cluster/model footer meta:\n%s", view)
	}
	if strings.Contains(view, "Cluster test") || strings.Contains(view, "Model gpt-4o") {
		t.Fatalf("footer meta should not render Cluster/Model labels:\n%s", view)
	}
	if borderIndex < 0 || contextIndex < borderIndex || modelIndex < borderIndex || clusterIndex < borderIndex {
		t.Fatalf("footer meta should render after input box border:\n%s", view)
	}
}

func TestSubmitAutoCompactsAtKnownContextThresholdThenStartsStream(t *testing.T) {
	conv := conversation.New("test", nil, "gpt-4o")
	large := strings.Repeat("a", 120000)
	conv.AddUser(large)
	conv.AddAssistant(large)
	conv.AddUser(large)
	conv.AddAssistant(large)
	conv.AddUser(large)

	provider := &compactCaptureProvider{content: "compressed history"}
	model := NewModel(ModelConfig{Cluster: "test", Model: "gpt-4o", Provider: provider, Conv: conv})

	next, cmd := model.submitMessage("new request", nil)
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submit should start auto compact command")
	}
	if !model.compacting || !model.autoCompactResume {
		t.Fatalf("model compacting=%v autoCompactResume=%v, want true true", model.compacting, model.autoCompactResume)
	}

	result := compactResultFromCmd(t, cmd)
	nextModel, streamCmd := model.Update(result)
	model = nextModel.(Model)
	if streamCmd == nil {
		t.Fatal("auto compact result should start stream")
	}
	if !model.streaming {
		t.Fatal("model should be streaming after auto compact")
	}
	msgs := model.conv.Messages()
	if len(msgs) == 0 || !strings.Contains(msgs[0].Content, "compressed history") {
		t.Fatalf("conversation was not compacted before streaming: %#v", msgs)
	}
}

func TestContextLimitErrorTriggersAutoCompactForUnknownModel(t *testing.T) {
	conv := conversation.New("test", nil, "custom-model")
	for i := 0; i < compactTailMessages+1; i++ {
		conv.AddUser(fmt.Sprintf("message %d", i))
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "custom-model", Provider: &compactCaptureProvider{}, Conv: conv})
	model.streaming = true
	model.activeStreamID = 1

	next, cmd := model.Update(streamReadyMsg{streamID: 1, err: errors.New("maximum context length exceeded")})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("context limit error should start auto compact")
	}
	if !model.compacting || !model.autoCompactResume {
		t.Fatalf("model compacting=%v autoCompactResume=%v, want true true", model.compacting, model.autoCompactResume)
	}
}

func TestResumeRestoresSavedCompactedConversationMessages(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	saved := []models.Message{
		{ID: "m1", ConversationID: "conv-compact", Role: "user", Content: "Previous conversation compacted.\n\nSummary:\nKeep the agent transfer design."},
		{ID: "m2", ConversationID: "conv-compact", Role: "user", Content: "continue implementation"},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-compact",
		Cluster:  "prod",
		Model:    "model",
		Summary:  "Compacted: Keep the agent transfer design.",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, cmd := model.applyCommand(SlashCommand{Kind: CommandResume, Arg: "conv-compact"})
	model = next
	msg := execCmd(t, cmd)
	nextModel, _ := model.Update(msg)
	model = nextModel.(Model)

	if model.conv.ID() != "conv-compact" {
		t.Fatalf("conversation ID = %q, want conv-compact", model.conv.ID())
	}
	if model.cluster != "prod" || model.model != "model" {
		t.Fatalf("cluster/model = %s/%s, want prod/model", model.cluster, model.model)
	}
	got := model.conv.Messages()
	if len(got) != 2 || got[0].Content != saved[0].Content || got[1].Content != saved[1].Content {
		t.Fatalf("restored messages = %#v, want saved compacted messages", got)
	}
}

func TestResumeWithoutIDRendersRecentSessionList(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("conv-%02d", i)
		if err := store.SaveConversation(memory.ConversationRecord{
			ID:      id,
			Cluster: "prod",
			Model:   "model",
			Summary: "session " + id,
		}); err != nil {
			t.Fatalf("save conversation %s: %v", id, err)
		}
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, cmd := model.applyCommand(SlashCommand{Kind: CommandResume})
	if cmd != nil {
		t.Fatal("resume without id returned command, want synchronous list")
	}
	model = next

	if model.mode != modeSession {
		t.Fatalf("mode = %v, want modeSession", model.mode)
	}
	if got := len(model.sessionList.sessions); got != 20 {
		t.Fatalf("session list length = %d, want 20", got)
	}
	view := model.View()
	if !strings.Contains(view, "Historical Sessions") {
		t.Fatalf("resume view missing session list:\n%s", view)
	}
}

func TestResumeSessionListEnterLoadsCurrentSession(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	saved := []models.Message{{ID: "m1", ConversationID: "conv-enter", Role: conversation.RoleUser, Content: "resume this session"}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-enter",
		Cluster:  "prod",
		Model:    "model",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandResume})
	model = next

	nextModel, cmd := model.handleSessionSelectKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = nextModel.(Model)
	if cmd == nil {
		t.Fatalf("enter returned nil command, status = %q", model.status)
	}
	msg := execCmd(t, cmd)
	nextModel, _ = model.Update(msg)
	model = nextModel.(Model)

	if model.conv.ID() != "conv-enter" {
		t.Fatalf("conversation ID = %q, want conv-enter", model.conv.ID())
	}
	if model.status == "No session selected" || model.status == "未选择会话" {
		t.Fatalf("status = %q, want loaded session", model.status)
	}
}

func TestResumeWithoutIDDisplaysLastUserMessage(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	saved := []models.Message{
		{
			ID:             "m1",
			ConversationID: "conv-summary",
			Role:           conversation.RoleUser,
			Content:        "older user message",
		},
		{
			ID:             "m2",
			ConversationID: "conv-summary",
			Role:           conversation.RoleUser,
			Content:        "latest user request",
		},
		{
			ID:             "m3",
			ConversationID: "conv-summary",
			Role:           conversation.RoleAssistant,
			Content:        "assistant final answer should not be listed",
		},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-summary",
		Cluster:  "prod",
		Model:    "model",
		Summary:  "stored summary should not be listed",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandResume})
	model = next

	view := model.View()
	if !strings.Contains(view, "latest user request") {
		t.Fatalf("resume view missing last user message:\n%s", view)
	}
	for _, unwanted := range []string{"stored summary should not be listed", "older user message", "assistant final answer should not be listed"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("resume view contains %q, want only last user message:\n%s", unwanted, view)
		}
	}
}

func TestResumeWithoutIDElidesMultilineLongUserMessage(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	longLine := strings.Repeat("very long request ", 20)
	saved := []models.Message{{
		ID:             "m1",
		ConversationID: "conv-long",
		Role:           conversation.RoleUser,
		Content:        "first line\nsecond line\r\n" + longLine,
	}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-long",
		Cluster:  "prod",
		Model:    "model",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandResume})
	model = next

	view := model.View()
	var sessionLines []string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "conv-long") || strings.Contains(line, "second line") || strings.Contains(line, "very long request") {
			sessionLines = append(sessionLines, line)
		}
	}
	if len(sessionLines) != 1 {
		t.Fatalf("resume view should render user preview on one line, got %d matching lines:\n%s", len(sessionLines), view)
	}
	if !strings.Contains(sessionLines[0], "first line second line") {
		t.Fatalf("resume line did not collapse newlines:\n%s", view)
	}
	if !strings.Contains(sessionLines[0], "...") {
		t.Fatalf("resume line did not elide long message:\n%s", view)
	}
}

func TestResumeDisplaysOnlyRecentTwentyMessages(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	var saved []models.Message
	for i := 0; i < 25; i++ {
		saved = append(saved, models.Message{
			ID:             fmt.Sprintf("m-%02d", i),
			ConversationID: "conv-many",
			Role:           conversation.RoleUser,
			Content:        fmt.Sprintf("message-%02d", i),
		})
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-many",
		Cluster:  "prod",
		Model:    "model",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{Cluster: "test", Model: "old", Conv: conversation.New("test", nil, "old"), MemoryStore: store})
	next, cmd := model.applyCommand(SlashCommand{Kind: CommandResume, Arg: "conv-many"})
	model = next
	msg := execCmd(t, cmd)
	nextModel, _ := model.Update(msg)
	model = nextModel.(Model)

	if got := len(model.conv.Messages()); got != 25 {
		t.Fatalf("conversation message length = %d, want full 25", got)
	}
	if got := len(model.messages); got != 20 {
		t.Fatalf("visible message length = %d, want 20", got)
	}
	view := model.View()
	if strings.Contains(view, "message-04") {
		t.Fatalf("view contains old message:\n%s", view)
	}
	if !strings.Contains(view, "message-05") || !strings.Contains(view, "message-24") {
		t.Fatalf("view missing recent messages:\n%s", view)
	}
}

func TestInitLoadsInitialSessionID(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	saved := []models.Message{{ID: "m1", ConversationID: "conv-init", Role: "user", Content: "initial resume context"}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-init",
		Cluster:  "prod",
		Model:    "model",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	model := NewModel(ModelConfig{
		Cluster:          "test",
		Model:            "old",
		Conv:             conversation.New("test", nil, "old"),
		MemoryStore:      store,
		InitialSessionID: "conv-init",
	})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, want load session command")
	}
	msg := execCmd(t, cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			next, _ := model.Update(execCmd(t, c))
			model = next.(Model)
		}
	} else {
		next, _ := model.Update(msg)
		model = next.(Model)
	}

	if model.conv.ID() != "conv-init" {
		t.Fatalf("conversation ID = %q, want conv-init", model.conv.ID())
	}
	if got := model.conv.Messages(); len(got) != 1 || got[0].Content != "initial resume context" {
		t.Fatalf("messages = %#v, want initial resume context", got)
	}
}

func TestViewportScrollsMessagesAddedAfterWindowSize(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)

	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	if model.vp.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", model.vp.YOffset)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = next.(Model)

	if model.vp.YOffset == 0 {
		t.Fatal("PageUp did not scroll messages added after window sizing")
	}
}

func TestViewportScrollsWhileStreaming(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)
	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = next.(Model)

	if model.vp.YOffset == 0 {
		t.Fatal("PageUp should scroll while a response is streaming")
	}
}

func TestViewportFollowsStreamingDeltasWhenAtBottom(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = next.(Model)
	for i := 0; i < 8; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.updateViewportContent()
	model.vp.GotoBottom()
	before := model.vp.YOffset
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: strings.Repeat("new line\n", 8)}})
	model = next.(Model)

	if model.vp.YOffset <= before {
		t.Fatalf("viewport did not follow streaming output: before=%d after=%d", before, model.vp.YOffset)
	}
	if !model.vp.AtBottom() {
		t.Fatalf("viewport should remain at bottom while streaming, offset=%d", model.vp.YOffset)
	}
}

func TestViewportFollowsSubmittedMessageWhenAtBottom(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet", Provider: &fakeProvider{}, Conv: conv})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = next.(Model)
	for i := 0; i < 8; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.updateViewportContent()
	model.vp.GotoBottom()
	before := model.vp.YOffset
	model = typeRunes(t, model, "hello")

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.vp.YOffset <= before {
		t.Fatalf("viewport did not follow submitted message: before=%d after=%d", before, model.vp.YOffset)
	}
	if !model.vp.AtBottom() {
		t.Fatalf("viewport should remain at bottom after submit, offset=%d", model.vp.YOffset)
	}
}

func TestViewportFollowsThinkingTickWhenAtBottom(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = next.(Model)
	for i := 0; i < 8; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.updateViewportContent()
	model.vp.GotoBottom()
	before := model.vp.YOffset
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.status = "Thinking..."

	next, _ = model.Update(thinkingTickMsg{streamID: 1})
	model = next.(Model)

	if model.vp.YOffset <= before {
		t.Fatalf("viewport did not follow thinking tick: before=%d after=%d", before, model.vp.YOffset)
	}
	if !model.vp.AtBottom() {
		t.Fatalf("viewport should remain at bottom during thinking, offset=%d", model.vp.YOffset)
	}
}

func TestMouseWheelScrollsViewportWithoutNavigatingInputHistory(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)
	model = typeAndEnter(t, model, "first")
	model = typeAndEnter(t, model, "second")
	model.input = "draft"
	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.updateViewportContent()
	model.vp.GotoBottom()
	bottomOffset := model.vp.YOffset

	next, _ = model.Update(tea.MouseMsg{
		Type:   tea.MouseWheelUp,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	model = next.(Model)

	if model.input != "draft" {
		t.Fatalf("mouse wheel changed input to %q, want draft", model.input)
	}
	if model.vp.YOffset >= bottomOffset {
		t.Fatalf("mouse wheel did not scroll viewport up: before=%d after=%d", bottomOffset, model.vp.YOffset)
	}
}

func TestUpDownScrollViewportWithoutNavigatingInputHistory(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude-sonnet"})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = next.(Model)
	model = typeAndEnter(t, model, "first")
	model = typeAndEnter(t, model, "second")
	model.input = "draft"
	for i := 0; i < 30; i++ {
		model.messages = append(model.messages, chatMsg{
			role:    "assistant",
			content: strings.Repeat("line\n", 3) + "message",
		})
	}
	model.updateViewportContent()
	model.vp.GotoBottom()
	bottomOffset := model.vp.YOffset

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)

	if model.input != "draft" {
		t.Fatalf("Up changed input to %q, want draft", model.input)
	}
	if model.vp.YOffset >= bottomOffset {
		t.Fatalf("Up did not scroll viewport up: before=%d after=%d", bottomOffset, model.vp.YOffset)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)

	if model.input != "draft" {
		t.Fatalf("Down changed input to %q, want draft", model.input)
	}
	if model.vp.YOffset <= bottomOffset-1 {
		t.Fatalf("Down did not scroll viewport down: before=%d after=%d", bottomOffset-1, model.vp.YOffset)
	}
}

func TestExitCommandQuits(t *testing.T) {
	model := NewModel(ModelConfig{})
	for _, r := range "/exit" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("exit command did not return quit command")
	}
}

func TestVersionCheckWarningListsMismatchedAndUnreachableNodes(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.status = "Thinking..."

	next, _ := model.Update(versionCheckMsg{mismatches: []mcp.Mismatch{
		{Node: "node-a", Got: "1.2.2", Expected: "1.2.3"},
		{Node: "node-b", Got: "connection refused", Expected: "1.2.3", IsError: true},
	}})
	model = next.(Model)

	if model.status != "Thinking..." {
		t.Fatalf("status = %q, want original status preserved", model.status)
	}
	for _, want := range []string{"Version warning", "node-a: 1.2.2 (expected 1.2.3)", "node-b: unreachable (connection refused)"} {
		if !strings.Contains(model.versionWarning, want) {
			t.Fatalf("versionWarning missing %q: %q", want, model.versionWarning)
		}
	}

	view := model.View()
	for _, want := range []string{"Thinking...", "Version warning", "node-a: 1.2.2 (expected 1.2.3)", "node-b: unreachable (connection refused)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInitVersionCheckCommandRendersWarning(t *testing.T) {
	client := newInitializeVersionTestClient(t, "1.2.2")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "1.2.3", Clients: map[string]*mcp.Client{"node-a": client}})
	model.status = "Ready"

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, want version check command")
	}

	versionMsg := execVersionCheckFromBatch(t, cmd)
	if len(versionMsg.mismatches) != 1 {
		t.Fatalf("len(mismatches) = %d, want 1", len(versionMsg.mismatches))
	}

	next, _ := model.Update(versionMsg)
	model = next.(Model)
	if model.status != "Ready" {
		t.Fatalf("status = %q, want original status preserved", model.status)
	}
	view := model.View()
	for _, want := range []string{"Ready", "Version warning", "node-a: 1.2.2 (expected 1.2.3)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInitCommandUsesMCPInitializeResponseForVersionCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "conan-agent",
						"version": "1.2.2",
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]interface{}{},
			})
		}
	}))
	defer srv.Close()

	client := mcp.NewClient(mcp.Config{BaseURL: srv.URL})
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "1.2.3", Clients: map[string]*mcp.Client{"node-a": client}})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, want version check command")
	}

	versionMsg := execVersionCheckFromBatch(t, cmd)
	next, _ := model.Update(versionMsg)
	model = next.(Model)
	if model.status != "Ready" {
		t.Fatalf("status = %q, want Ready", model.status)
	}
	if !strings.Contains(model.View(), "Version warning") {
		t.Fatalf("view missing version warning:\n%s", model.View())
	}
}

func newInitializeVersionTestClient(t *testing.T, version string) *mcp.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo:      mcpproto.ServerInfo{Name: "conan-agent", Version: version},
			}))
		default:
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]interface{}{}))
		}
	}))
	t.Cleanup(srv.Close)
	return mcp.NewClient(mcp.Config{BaseURL: srv.URL})
}

func TestInitSkipsVersionCheckForDevVersion(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Version: "dev"})

	msg := execCmd(t, model.Init())
	if _, ok := msg.(startupTickMsg); !ok {
		t.Fatalf("Init() returned %T, want startup tick only", msg)
	}
}

func TestNoProviderShowsStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	for _, r := range "hello" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !strings.Contains(model.status, "No LLM provider") {
		t.Fatalf("status = %q", model.status)
	}
}

func TestStreamingUpdatesAccumulate(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "Hello "}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "world"}})
	model = next.(Model)
	if model.streamBuf != "Hello world" {
		t.Fatalf("streamBuf = %q", model.streamBuf)
	}

	next, _ = model.Update(streamDoneMsg{streamID: 1})
	model = next.(Model)
	if model.streaming {
		t.Fatal("should not be streaming after done")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Hello world") {
		t.Fatalf("view missing streamed text:\n%s", view)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Hello world" {
		t.Fatalf("conversation messages = %#v, want one assistant message with streamed content", msgs)
	}
	if !strings.Contains(model.status, "Stream ended") {
		t.Fatalf("status = %q, want stream ended status", model.status)
	}
}

func TestStreamReadyWithNilChannelStopsWithError(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamReadyMsg{streamID: 1, ch: nil})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("nil stream channel should not return a wait command")
	}
	if model.streaming {
		t.Fatal("nil stream channel should stop streaming")
	}
	if !strings.Contains(model.status, "Stream error") || !strings.Contains(model.status, "nil") {
		t.Fatalf("status = %q, want nil stream error", model.status)
	}
}

func TestStopEventWithEmptyResponseShowsVisibleError(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("empty stop should not return a command")
	}
	if model.streaming {
		t.Fatal("empty stop should end streaming")
	}
	if !strings.Contains(model.status, "empty response") {
		t.Fatalf("status = %q, want empty response error", model.status)
	}
	view := model.View()
	if !strings.Contains(view, "Model returned an empty response") {
		t.Fatalf("view missing visible empty response error:\n%s", view)
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want no synthetic assistant error", conv.Messages())
	}
}

func TestReasoningOnlyStreamIsTreatedAsEmptyResponse(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ReasoningDeltaEvent{Delta: "Hello"}})
	model = next.(Model)
	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("reasoning-only stop should not return a command")
	}
	if model.streaming {
		t.Fatal("reasoning-only stop should end streaming")
	}
	if !strings.Contains(model.status, "empty response") {
		t.Fatalf("status = %q, want empty response status", model.status)
	}
	for _, msg := range model.messages {
		if strings.Contains(msg.content, "Hello") {
			t.Fatalf("reasoning leaked into assistant message: %#v", msg)
		}
	}
	msgs := conv.Messages()
	if len(msgs) != 0 {
		t.Fatalf("conversation messages = %#v, want no reasoning content", msgs)
	}
}

func TestStreamDoneWithEmptyResponseShowsVisibleError(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamDoneMsg{streamID: 1})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("empty stream done should not return a command")
	}
	if model.streaming {
		t.Fatal("empty stream done should end streaming")
	}
	if !strings.Contains(model.status, "empty response") {
		t.Fatalf("status = %q, want empty response error", model.status)
	}
	if !strings.Contains(model.View(), "Model returned an empty response") {
		t.Fatalf("view missing visible empty response error:\n%s", model.View())
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want no synthetic assistant error", conv.Messages())
	}
}

func TestDebugLoggingRecordsLLMRequestAndStreamEvents(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	conv := conversation.New("test", nil, "model")
	conv.AddUser("hello")
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Provider: &fakeProvider{},
		Conv:     conv,
		Tools: []llm.ToolDef{{
			Name:        "log_read",
			Description: "Read logs",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})

	model, _ = model.startStream()
	next, _ := model.Update(streamEventMsg{streamID: model.activeStreamID, Event: llm.TextDeltaEvent{Delta: "Hi"}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: model.activeStreamID, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)
	logging.Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(contents)
	for _, want := range []string{
		"llm request",
		"system_prompt_len",
		"messages_count",
		"tools_count",
		"llm stream text_delta",
		"delta_len",
		"llm stream stop",
		"end_turn",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logText)
		}
	}
	for _, unwanted := range []string{"tool_search", "call_tool", "Read logs", "Hi"} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("debug log should not contain verbose LLM content %q:\n%s", unwanted, logText)
		}
	}
}

func TestDebugLoggingRecordsEmptyResponse(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)
	logging.Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(contents)
	for _, want := range []string{
		"llm stream stop",
		"llm empty response",
		"end_turn",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logText)
		}
	}
}

func TestStreamTimeoutStopsWaitingWithError(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEventSeq = 3
	cancelled := false
	model.streamCancel = func() { cancelled = true }

	next, cmd := model.Update(streamTimeoutMsg{streamID: 1, eventSeq: 3})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("stream timeout should not return a command")
	}
	if !cancelled {
		t.Fatal("stream timeout should cancel active stream")
	}
	if model.streaming {
		t.Fatal("stream timeout should stop streaming")
	}
	if !strings.Contains(model.status, "Stream timeout") {
		t.Fatalf("status = %q, want stream timeout", model.status)
	}
}

func TestStaleStreamTimeoutIsIgnoredAfterEventProgress(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEventSeq = 4

	next, cmd := model.Update(streamTimeoutMsg{streamID: 1, eventSeq: 3})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("stale stream timeout should not return a command")
	}
	if !model.streaming {
		t.Fatal("stale stream timeout should not stop streaming")
	}
}

func TestStreamErrorPreservesPartialContent(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "Partial response"
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream closed unexpectedly")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Partial response") {
		t.Fatalf("view missing preserved content:\n%s", view)
	}
	if !strings.Contains(model.status, "Stream error") || !strings.Contains(model.status, "preserved") {
		t.Fatalf("status = %q, want stream error with preserved content", model.status)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Partial response" {
		t.Fatalf("conversation messages = %#v, want preserved assistant message", msgs)
	}
}

func TestToolCallReturnsCommandThatContinuesStreamWaiting(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ch := make(chan llm.ChatEvent)
	model.streamCh = ch
	model.streamCtx = context.Background()

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "fs_read", Arguments: []byte(`{"path":"/tmp/a"}`),
	}})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("tool call returned nil command, want tool work batched with continued stream waiting")
	}
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("tool call command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 3 {
		t.Fatalf("batch has %d commands, want tool work, stream wait, and stream timeout", len(batch))
	}

	go func() {
		ch <- llm.ToolCallEvent{ID: "tc2", Name: "fs_stat", Arguments: []byte(`{"path":"/tmp/b"}`)}
	}()
	continuedMsg := execCmd(t, batch[1])
	if _, ok := continuedMsg.(streamEventMsg); !ok {
		t.Fatalf("continued wait command returned %T, want streamEventMsg", continuedMsg)
	}
}

func TestMemoryToolCallIsHiddenFromNormalChat(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", MemoryStore: store})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "mem1",
			Name:      "memory_patch",
			Arguments: json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User name is Alice."}`),
		},
	})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "memory_patch") || strings.Contains(view, "User name is Alice") {
		t.Fatalf("memory tool leaked into normal chat:\n%s", view)
	}
}

func TestInternalToolSearchAndCallToolAreHiddenFromNormalChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "search1",
			Name:      metaToolToolSearch,
			Arguments: json.RawMessage(`{"query":"cpu process"}`),
		},
	})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "tool_search") || strings.Contains(view, "cpu process") {
		t.Fatalf("tool_search leaked into normal chat:\n%s", view)
	}
	if model.status != "Inspecting..." {
		t.Fatalf("status = %q, want generic inspecting status", model.status)
	}

	next, _ = model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     llm.ToolCall{ID: "search1", Name: metaToolToolSearch, Arguments: json.RawMessage(`{"query":"cpu process"}`)},
		Results:  []nodeToolResult{{Node: "-", Output: `[{"name":"sys_processes"}]`, Success: true}},
	})
	model = next.(Model)

	next, _ = model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "call1",
			Name:      metaToolCallTool,
			Arguments: json.RawMessage(`{"node":"node-01","tool":"sys_processes","arguments":{}}`),
		},
	})
	model = next.(Model)

	next, _ = model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     llm.ToolCall{ID: "call1", Name: metaToolCallTool, Arguments: json.RawMessage(`{"node":"node-01","tool":"sys_processes","arguments":{}}`)},
		Results:  []nodeToolResult{{Node: "node-01", Output: "postgres 86%", Success: true}},
	})
	model = next.(Model)

	view = model.View()
	for _, leaked := range []string{"tool_search", "call_tool", "sys_processes", "postgres 86%"} {
		if strings.Contains(view, leaked) {
			t.Fatalf("internal tool detail %q leaked into normal chat:\n%s", leaked, view)
		}
	}
}

func TestHiddenInternalToolStopToolUseDoesNotShowRunningToolStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCtx = context.Background()

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "search1",
			Name:      metaToolToolSearch,
			Arguments: json.RawMessage(`{"query":"logs"}`),
		},
	})
	model = next.(Model)

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopToolUse}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("StopToolUse should wait for hidden internal tool result before continuing")
	}
	if strings.Contains(model.status, "Running tool") {
		t.Fatalf("status leaked hidden internal tool activity: %q", model.status)
	}
	if model.status != "Inspecting..." {
		t.Fatalf("status = %q, want Inspecting...", model.status)
	}
}

func TestExecToolCallRemainsVisibleInNormalChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "exec1",
			Name:      metaToolExec,
			Arguments: json.RawMessage(`{"command":"systemctl restart nginx"}`),
		},
	})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, metaToolExec) || !strings.Contains(view, "systemctl restart nginx") {
		t.Fatalf("exec tool should remain visible:\n%s", view)
	}
}

func TestMemoryToolFallbackPlaceholderIsHiddenFromNormalChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	call := llm.ToolCall{
		ID:        "mem1",
		Name:      "memory_patch",
		Arguments: json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User name is Alice."}`),
	}

	next, _ := model.Update(multiToolResultMsg{
		Call:    call,
		Results: []nodeToolResult{{Node: "local", Output: "patched profile with User name is Alice.", Success: true}},
	})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "memory_patch") || strings.Contains(view, "User name is Alice") {
		t.Fatalf("fallback memory tool leaked into normal chat:\n%s", view)
	}
}

func TestMemoryToolStopToolUseDoesNotShowRunningToolStatus(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := &captureStreamProvider{}
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: provider, Conv: conv, MemoryStore: store})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCtx = context.Background()

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "mem1",
			Name:      "memory_patch",
			Arguments: json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User name is Alice."}`),
		},
	})
	model = next.(Model)

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopToolUse}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("StopToolUse should wait for hidden memory tool result before continuing")
	}
	if strings.Contains(model.status, "Running tool") {
		t.Fatalf("status leaked hidden memory tool activity: %q", model.status)
	}

	next, cmd = model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     llm.ToolCall{ID: "mem1", Name: "memory_patch", Arguments: json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User name is Alice."}`)},
		Results:  []nodeToolResult{{Node: "local", Output: "patched profile with User name is Alice.", Success: true}},
	})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("hidden memory tool result should continue the stream after StopToolUse")
	}
	execMaybeBatch(t, cmd)
	if provider.req == nil {
		t.Fatal("hidden memory tool did not continue the stream")
	}
}

func TestToolCallPreservesPrecedingAssistantText(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "Before the tool."}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`),
	}})
	model = next.(Model)

	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty after tool call", model.streamBuf)
	}
	if len(model.messages) < 2 {
		t.Fatalf("messages = %#v, want assistant text followed by tool call", model.messages)
	}
	if model.messages[0].role != "assistant" || model.messages[0].content != "Before the tool." {
		t.Fatalf("first message = %#v, want preserved assistant text", model.messages[0])
	}
	if model.messages[1].role != "tool" || model.messages[1].toolName != "shell_run" {
		t.Fatalf("second message = %#v, want tool call placeholder", model.messages[1])
	}
	view := model.View()
	if !strings.Contains(view, "Before the tool.") {
		t.Fatalf("view missing preserved assistant text:\n%s", view)
	}
	msgs := conv.Messages()
	if len(msgs) != 2 {
		t.Fatalf("conversation messages = %#v, want assistant text and tool call", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "Before the tool." {
		t.Fatalf("conversation first message = %#v, want preserved assistant text", msgs[0])
	}
	if msgs[1].ToolCallID != "tc1" || msgs[1].ToolName != "shell_run" {
		t.Fatalf("conversation second message = %#v, want tool call", msgs[1])
	}
}

func TestReasoningOnlyStopDoesNotBecomeAssistantMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ReasoningDeltaEvent{Delta: "private chain"}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after stop")
	}
	if model.streamReasoningBuf != "" {
		t.Fatalf("streamReasoningBuf = %q, want cleared", model.streamReasoningBuf)
	}
	for _, msg := range model.messages {
		if strings.Contains(msg.content, "private chain") {
			t.Fatalf("reasoning leaked into assistant message: %#v", msg)
		}
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none", got)
	}
	if !strings.Contains(model.status, "empty response") {
		t.Fatalf("status = %q, want empty response status", model.status)
	}
}

func TestReasoningBeforeToolCallIsNotPersistedOrRendered(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamReasoningBuf = "I should upload the file"

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID:        "tc1",
		Name:      metaToolFilePut,
		Arguments: []byte(`{"node":"node-a","local_path":"tmp/a.sh","remote_path":"/tmp/a.sh"}`),
	}})
	model = next.(Model)

	if model.streamReasoningBuf != "" {
		t.Fatalf("streamReasoningBuf = %q, want cleared after tool call", model.streamReasoningBuf)
	}
	view := model.View()
	if strings.Contains(view, "I should upload the file") {
		t.Fatalf("reasoning leaked into view:\n%s", view)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("conversation messages = %#v, want only tool call", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].ToolCallID != "tc1" || msgs[0].ToolName != metaToolFilePut {
		t.Fatalf("conversation message = %#v, want tool call only", msgs[0])
	}
}

func TestStreamErrorWithEmptyBufferDoesNotAddAssistantMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream failed")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	if !strings.Contains(model.status, "Stream error") {
		t.Fatalf("status = %q, want stream error status", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none", got)
	}
}

func TestInterruptedStreamIgnoresLateEvents(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "partial"
	model.streamCh = make(chan llm.ChatEvent)
	cancelled := false
	model.streamCancel = func() { cancelled = true }
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	if !cancelled {
		t.Fatal("interrupt did not call stream cancel")
	}

	if model.streaming {
		t.Fatal("should not be streaming after interrupt")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	if model.streamCh != nil {
		t.Fatalf("streamCh = %#v, want nil", model.streamCh)
	}
	if model.streamCancel != nil {
		t.Fatalf("streamCancel = %#v, want nil", model.streamCancel)
	}
	if model.activeStreamID != 0 {
		t.Fatalf("activeStreamID = %d, want 0", model.activeStreamID)
	}
	if !strings.Contains(model.status, "Interrupted") {
		t.Fatalf("status = %q, want interrupted status", model.status)
	}

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "late text"}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("stale text delta returned a command")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty after stale text", model.streamBuf)
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale text", conv.Messages())
	}

	next, cmd = model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)
	if cmd != nil {
		t.Fatal("stale stop returned a command")
	}
	if model.streaming {
		t.Fatal("stale stop should not resume streaming")
	}
	if len(conv.Messages()) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale stop", conv.Messages())
	}
}

func TestEscInterruptsStreaming(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}})
	model.streaming = true
	model.status = "Thinking..."
	model.streamCh = make(chan llm.ChatEvent)
	cancelled := false
	model.streamCancel = func() { cancelled = true }
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("Esc interrupt should not return a command")
	}
	if !cancelled {
		t.Fatal("Esc interrupt did not call stream cancel")
	}
	if model.streaming {
		t.Fatal("should not be streaming after Esc interrupt")
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
}

func TestStreamErrorWithPartialBufferPreservesContent(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.status = "Thinking..."
	model.streamBuf = "Partial response"
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream closed unexpectedly")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("should not be streaming after error")
	}
	if model.streamBuf != "" {
		t.Fatalf("streamBuf = %q, want empty", model.streamBuf)
	}
	view := model.View()
	if !strings.Contains(view, "Partial response") {
		t.Fatalf("view missing preserved content:\n%s", view)
	}
	if !strings.Contains(model.status, "Stream error") || !strings.Contains(model.status, "preserved") {
		t.Fatalf("status = %q, want stream error with preserved content", model.status)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != "Partial response" {
		t.Fatalf("conversation messages = %#v, want preserved assistant message", msgs)
	}
}

func TestToolResultMessage(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	call := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
	}
	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell_run") {
		t.Fatalf("view missing tool name:\n%s", view)
	}
}

func TestLateRiskAssessmentAfterInterruptIsIgnored(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell_run", toolInput: `{"command":"rm -rf /"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`)}
	next, cmd := model.Update(riskAssessmentMsg{
		streamID:   1,
		call:       call,
		assessment: security.RiskAssessment{Level: security.RiskDeny, Reason: "Destructive"},
	})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("late risk assessment returned command, want ignored")
	}
	if model.streaming {
		t.Fatal("late risk assessment should not restart streaming")
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale risk assessment", got)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages = %#v, want stale risk assessment not to mutate tool output", model.messages)
	}
}

func TestCtrlCCancelsInFlightRiskAssessment(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{
			started: started,
			block:   make(chan struct{}),
			done:    done,
		},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ctx, cancel := context.WithCancel(context.Background())
	model.streamCtx = ctx
	model.streamCancel = cancel

	cmd := model.assessToolRisk(1, llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`)})
	cmdDone := make(chan tea.Msg, 1)
	go func() { cmdDone <- execCmd(t, cmd) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("risk assessment did not start")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("risk assessment did not observe stream context cancellation")
	}
	select {
	case msg := <-cmdDone:
		riskMsg, ok := msg.(riskAssessmentMsg)
		if !ok {
			t.Fatalf("command returned %T, want riskAssessmentMsg", msg)
		}
		if riskMsg.err == nil {
			t.Fatal("risk assessment message err is nil, want context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("risk assessment command did not return after cancellation")
	}
}

func TestCtrlCCancelsInFlightToolDispatch(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	client := mcp.NewClient(mcp.Config{BaseURL: "http://node-01", Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		close(done)
		return nil, req.Context().Err()
	})}})
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Nodes:   []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
		Clients: map[string]*mcp.Client{"node-01": client},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	ctx, cancel := context.WithCancel(context.Background())
	model.streamCtx = ctx
	model.streamCancel = cancel

	cmd := model.dispatchTool(1, llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)})
	cmdDone := make(chan tea.Msg, 1)
	go func() { cmdDone <- execCmd(t, cmd) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool dispatch did not start")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tool dispatch did not observe stream context cancellation")
	}
	select {
	case msg := <-cmdDone:
		if _, ok := msg.(multiToolResultMsg); !ok {
			t.Fatalf("command returned %T, want multiToolResultMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("tool dispatch command did not return after cancellation")
	}
}

func TestLateToolResultAfterInterruptIsIgnored(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell_run", toolInput: `{"command":"uptime"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)}
	next, cmd := model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     call,
		Results: []nodeToolResult{
			{Node: "node-01", Output: "late output", Success: true},
		},
	})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("late tool result returned command, want ignored")
	}
	if model.streaming {
		t.Fatal("late tool result should not restart streaming")
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
	if got := conv.Messages(); len(got) != 0 {
		t.Fatalf("conversation messages = %#v, want none after stale tool result", got)
	}
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages = %#v, want stale tool result not to mutate tool output", model.messages)
	}
}

func TestNodesCommandOpensSelector(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeNodeSelect {
		t.Fatal("should be in node select mode after /nodes")
	}
	view := model.View()
	if !strings.Contains(view, "Select Target Nodes") {
		t.Fatalf("view should show node selector:\n%s", view)
	}
}

func TestNodesCommandOpensSelectorWithOnlyAddOptionWhenEmpty(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})

	next, cmd := model.applyCommand(SlashCommand{Kind: CommandNodes})
	model = next

	if cmd != nil {
		t.Fatal("/nodes with no nodes should not ping")
	}
	if model.mode != modeNodeSelect {
		t.Fatalf("mode = %v, want node select", model.mode)
	}
	if !model.nodeSelector.AddSelected() {
		t.Fatal("empty selector should select add row")
	}
	if !strings.Contains(model.View(), "Add new node") {
		t.Fatalf("view missing add node option:\n%s", model.View())
	}
}

func TestNodeSelectorEnterOnAddRowOpensNodeAddForm(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Nodes: []NodeInfo{
			{Name: "node-01", Host: "10.0.1.1", Online: true},
		},
	})
	model.mode = modeNodeSelect
	model.nodeSelector = newNodeSelector(model.nodes, model.selectedNodes)
	model.nodeSelector.cursor = len(model.nodes)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("opening node add form returned command")
	}
	if model.mode != modeNodeAddForm {
		t.Fatalf("mode = %v, want node add form", model.mode)
	}
	if !strings.Contains(model.View(), "Add New Node") {
		t.Fatalf("view missing node add form:\n%s", model.View())
	}
}

func TestNodeAddFormSubmitCallsRunnerAndRefreshesSelector(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.yaml"), "default_cluster: test\n")
	writeTestFile(t, filepath.Join(home, "clusters", "test", "cluster.yaml"), "name: test\n")

	var gotReq nodeadd.Request
	model := NewModel(ModelConfig{
		Cluster:    "test",
		Model:      "m",
		ConfigHome: home,
		NodeAddRunner: nodeAddRunnerFunc(func(_ context.Context, req nodeadd.Request) (nodeadd.Result, error) {
			gotReq = req
			return nodeadd.Result{
				Node: configschema.NodeConfig{
					Name: req.Name,
					Host: req.Input,
					Agent: &configschema.NodeAgentOverride{
						Port:  req.AgentPort,
						Token: "agent-token",
					},
				},
				Deployed: true,
			}, nil
		}),
	})
	model.mode = modeNodeAddForm
	model.prevSelected = map[string]bool{}
	model.nodeAddForm = newNodeAddForm(uiLanguageEnglish).
		withValue(nodeAddFormFieldName, "web-1").
		withValue(nodeAddFormFieldHost, "10.0.0.12").
		withValue(nodeAddFormFieldPort, "9281").
		withValue(nodeAddFormFieldUser, "deploy").
		withValue(nodeAddFormFieldPassword, "secret")
	model.nodeAddForm.cursor = nodeAddFormFieldPassword

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submitting node add form returned nil command")
	}
	msg := execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	if gotReq.ClusterName != "test" || gotReq.Name != "web-1" || gotReq.Input != "10.0.0.12" {
		t.Fatalf("request identity = %#v", gotReq)
	}
	if gotReq.AgentPort != 9281 || gotReq.Username != "deploy" || gotReq.Password != "secret" {
		t.Fatalf("request deploy fields = %#v", gotReq)
	}
	if model.mode != modeNodeSelect {
		t.Fatalf("mode = %v, want node select", model.mode)
	}
	if !model.selectedNodes["web-1"] {
		t.Fatalf("selected nodes = %#v, want web-1 selected", model.selectedNodes)
	}
	if len(model.nodes) != 1 || model.nodes[0].Name != "web-1" {
		t.Fatalf("nodes = %#v, want web-1", model.nodes)
	}
	if !strings.Contains(model.View(), "web-1") || !strings.Contains(model.View(), "10.0.0.12") {
		t.Fatalf("selector view missing new node:\n%s", model.View())
	}
}

func TestNodeAddFormSubmitFailureKeepsFormOpen(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.yaml"), "default_cluster: test\n")
	writeTestFile(t, filepath.Join(home, "clusters", "test", "cluster.yaml"), "name: test\n")

	model := NewModel(ModelConfig{
		Cluster:    "test",
		Model:      "m",
		ConfigHome: home,
		NodeAddRunner: nodeAddRunnerFunc(func(context.Context, nodeadd.Request) (nodeadd.Result, error) {
			return nodeadd.Result{}, errors.New("deploy failed")
		}),
	})
	model.mode = modeNodeAddForm
	model.nodeAddForm = newNodeAddForm(uiLanguageEnglish).
		withValue(nodeAddFormFieldName, "web-1").
		withValue(nodeAddFormFieldHost, "10.0.0.12").
		withValue(nodeAddFormFieldPort, "9281").
		withValue(nodeAddFormFieldUser, "deploy").
		withValue(nodeAddFormFieldPassword, "secret")
	model.nodeAddForm.cursor = nodeAddFormFieldPassword

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("submitting node add form returned nil command")
	}
	msg := execCmd(t, cmd)
	next, _ = model.Update(msg)
	model = next.(Model)

	if model.mode != modeNodeAddForm {
		t.Fatalf("mode = %v, want node add form", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "deploy failed") {
		t.Fatalf("view missing error:\n%s", view)
	}
	if !strings.Contains(view, "web-1") || !strings.Contains(view, "10.0.0.12") {
		t.Fatalf("form did not preserve values:\n%s", view)
	}
}

func TestNodeCommandEnablesNodeToolsForNextResponse(t *testing.T) {
	model := NewModel(ModelConfig{})
	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode})

	if !next.nodeToolsEnabled {
		t.Fatal("/node should enable node tools")
	}
	if next.status != "Node management enabled for next model response" {
		t.Fatalf("status = %q", next.status)
	}
}

func TestNodeCommandOffDisablesNodeTools(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true

	next, _ := model.applyCommand(SlashCommand{Kind: CommandNode, Arg: "off"})

	if next.nodeToolsEnabled {
		t.Fatal("/node off should disable node tools")
	}
	if next.status != "Node management disabled" {
		t.Fatalf("status = %q", next.status)
	}
}

func TestNodeToolExposureDispatchesNodeAddLocally(t *testing.T) {
	model := NewModel(ModelConfig{})
	rawArgs := json.RawMessage(`{"host":"10.0.0.5","user":"deploy","password":"secret"}`)
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: rawArgs}

	msg := execCmd(t, model.dispatchTool(7, call))

	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if result.streamID != 7 {
		t.Fatalf("streamID = %d, want 7", result.streamID)
	}
	if string(result.Call.Arguments) != string(rawArgs) {
		t.Fatalf("dispatch should preserve raw arguments, got %s", string(result.Call.Arguments))
	}
	if len(result.Results) != 1 || result.Results[0].Node != "local" {
		t.Fatalf("results = %#v, want one local result", result.Results)
	}
	if result.Results[0].Success {
		t.Fatal("node_add dispatch should fail when node tools are disabled")
	}
	if !strings.Contains(result.Results[0].Output, "node_add is not enabled") {
		t.Fatalf("output = %q, want authorization error", result.Results[0].Output)
	}
}

func TestFilePutDispatchesManagedUpload(t *testing.T) {
	workspace := t.TempDir()
	localPath := filepath.Join(workspace, "payload.txt")
	if err := os.WriteFile(localPath, []byte("payload data"), 0644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/files/upload" {
			t.Fatalf("request = %s %s, want PUT /files/upload", r.Method, r.URL.Path)
		}
		gotPath = r.URL.Query().Get("path")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("X-Conan-Bytes-Written", "12")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	model := NewModel(ModelConfig{
		LocalWorkspaceRoot: workspace,
		Clients: map[string]*mcp.Client{
			"node-a": mcp.NewClient(mcp.Config{BaseURL: server.URL}),
		},
	})
	call := llm.ToolCall{
		ID:        "put1",
		Name:      metaToolFilePut,
		Arguments: json.RawMessage(`{"node":"node-a","local_path":"payload.txt","remote_path":"/tmp/payload.txt"}`),
	}

	msg := execCmd(t, model.dispatchTool(7, call))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if gotPath != "/tmp/payload.txt" {
		t.Fatalf("upload path = %q, want remote path", gotPath)
	}
	if gotBody != "payload data" {
		t.Fatalf("upload body = %q", gotBody)
	}
	if len(result.Results) != 1 || !result.Results[0].Success || result.Results[0].Node != "node-a" {
		t.Fatalf("results = %#v, want successful node-a upload", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "uploaded payload.txt to node-a:/tmp/payload.txt") {
		t.Fatalf("output = %q", result.Results[0].Output)
	}
}

func TestFileGetDispatchesManagedDownload(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files/download" {
			t.Fatalf("request = %s %s, want GET /files/download", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "/var/log/app.log" {
			t.Fatalf("download path = %q", got)
		}
		_, _ = w.Write([]byte("remote log"))
	}))
	defer server.Close()

	model := NewModel(ModelConfig{
		LocalWorkspaceRoot: workspace,
		Clients: map[string]*mcp.Client{
			"node-a": mcp.NewClient(mcp.Config{BaseURL: server.URL}),
		},
	})
	call := llm.ToolCall{
		ID:        "get1",
		Name:      metaToolFileGet,
		Arguments: json.RawMessage(`{"node":"node-a","remote_path":"/var/log/app.log","local_path":"downloads/app.log"}`),
	}

	msg := execCmd(t, model.dispatchTool(7, call))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "downloads", "app.log"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "remote log" {
		t.Fatalf("downloaded data = %q", data)
	}
	if len(result.Results) != 1 || !result.Results[0].Success || result.Results[0].Node != "node-a" {
		t.Fatalf("results = %#v, want successful node-a download", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "downloaded node-a:/var/log/app.log to downloads/app.log") {
		t.Fatalf("output = %q", result.Results[0].Output)
	}
}

func TestFileTransferRequiresConfirmationWithoutReviewer(t *testing.T) {
	model := NewModel(ModelConfig{})
	call := llm.ToolCall{
		ID:        "put1",
		Name:      metaToolFilePut,
		Arguments: json.RawMessage(`{"node":"node-a","local_path":"payload.txt","remote_path":"/tmp/payload.txt"}`),
	}

	msg := execCmd(t, model.assessToolRisk(7, call))
	result, ok := msg.(riskAssessmentMsg)
	if !ok {
		t.Fatalf("assessToolRisk returned %T, want riskAssessmentMsg", msg)
	}
	if result.assessment.Level != security.RiskConfirm {
		t.Fatalf("risk level = %v, want confirm", result.assessment.Level)
	}
}

func TestNodeToolExposureClearedOnFinishStream(t *testing.T) {
	model := NewModel(ModelConfig{})
	model.nodeToolsEnabled = true

	model.finishStream(false)

	if model.nodeToolsEnabled {
		t.Fatal("finishStream should clear node tool exposure")
	}
}

func TestNodeAddAuditLogsRedactPassword(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()

	model := NewModel(ModelConfig{AuditLogger: auditLog})
	call := llm.ToolCall{
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`),
	}

	model.logAuditDecision(call, security.RiskAssessment{Level: security.RiskConfirm, Reason: "node add"}, "denied")
	model.logAuditExecution(call, []nodeToolResult{{Node: "local", Output: "not implemented", Success: false}})

	if err := auditLog.Close(); err != nil {
		t.Fatalf("close audit log: %v", err)
	}
	contents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	text := string(contents)
	if strings.Contains(text, "secret") {
		t.Fatalf("audit log should not contain raw password: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("audit log should contain redacted password marker: %s", text)
	}
	if !strings.Contains(text, "10.0.0.5") {
		t.Fatalf("audit log should preserve host: %s", text)
	}
}

func TestNodeAddRiskReviewRedactsPasswordAndPreservesRawCall(t *testing.T) {
	provider := &stubRiskProvider{response: `{"risk_level":"confirm","reason":"node add"}`}
	reviewer := security.NewReviewer(security.ReviewerConfig{Provider: provider})
	model := NewModel(ModelConfig{Reviewer: reviewer})
	rawArgs := json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`)
	call := llm.ToolCall{ID: "node-add-1", Name: metaToolNodeAdd, Arguments: rawArgs}

	msg := execCmd(t, model.assessToolRisk(7, call))
	result, ok := msg.(riskAssessmentMsg)
	if !ok {
		t.Fatalf("assessToolRisk returned %T, want riskAssessmentMsg", msg)
	}
	if result.err != nil {
		t.Fatalf("risk assessment error: %v", result.err)
	}
	if provider.req == nil {
		t.Fatal("risk provider was not called")
	}
	if strings.Contains(provider.req.SystemPrompt, "secret") {
		t.Fatalf("risk prompt should not contain raw password: %s", provider.req.SystemPrompt)
	}
	if !strings.Contains(provider.req.SystemPrompt, "[REDACTED]") {
		t.Fatalf("risk prompt should contain redacted password marker: %s", provider.req.SystemPrompt)
	}
	if string(result.call.Arguments) != string(rawArgs) {
		t.Fatalf("risk assessment call args = %s, want raw args", result.call.Arguments)
	}
}

func TestDebugLogStreamEventLogsToolCallSummaryOnly(t *testing.T) {
	var buf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	model := NewModel(ModelConfig{})
	model.debugLogStreamEvent(llm.ToolCallEvent{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: json.RawMessage(`{"host":"10.0.0.5","password":"secret"}`),
	})

	logText := buf.String()
	for _, unwanted := range []string{"secret", "10.0.0.5", "password"} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("debug log should not contain raw tool argument content %q: %s", unwanted, logText)
		}
	}
	for _, want := range []string{"llm stream tool_call", "node-add-1", metaToolNodeAdd, "arguments_len"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q: %s", want, logText)
		}
	}
}

func TestDebugLogLLMRequestLogsSummaryOnly(t *testing.T) {
	var buf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	model := NewModel(ModelConfig{Cluster: "prod", Model: "gpt-test"})
	model.debugLogLLMRequest(&llm.ChatRequest{
		SystemPrompt: "full system prompt should not be logged",
		Messages: []models.Message{
			{Role: "user", Content: "complex request should not be logged"},
			{Role: "tool", ToolName: "shell_run", ToolInput: "secret tool output should not be logged"},
		},
		Tools: []llm.ToolDef{{
			Name:        "shell_run",
			Description: "run shell command",
			InputSchema: json.RawMessage(`{"properties":{"command":{"type":"string"}}}`),
		}},
	})

	logText := buf.String()
	for _, unwanted := range []string{
		"full system prompt",
		"complex request",
		"secret tool output",
		"run shell command",
		"properties",
	} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("debug log should not contain request content %q: %s", unwanted, logText)
		}
	}
	for _, want := range []string{"llm request", "prod", "gpt-test", "system_prompt_len", "messages_count", "tools_count"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q: %s", want, logText)
		}
	}
}

func TestNodeSelectConfirm(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})
	if len(model.selectedNodes) != 2 {
		t.Fatalf("expected 2 default selected, got %d", len(model.selectedNodes))
	}

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatal("should be back in chat mode after confirm")
	}
	if model.selectedNodes["node-02"] {
		t.Fatal("node-02 should be deselected")
	}
	if !model.selectedNodes["node-01"] {
		t.Fatal("node-01 should still be selected")
	}
}

func TestNodeSelectCancel(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatal("should be back in chat mode after cancel")
	}
	if !model.selectedNodes["node-01"] {
		t.Fatal("cancel should restore original selection")
	}
}

func TestPingResultUpdatesNodeStatus(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: false},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Nodes: nodes})

	next, _ := model.Update(pingResultMsg{node: "node-01", online: true})
	model = next.(Model)

	if !model.nodes[0].Online {
		t.Fatal("node-01 should be online after ping")
	}
	if model.nodes[1].Online {
		t.Fatal("node-02 should still be offline")
	}
}

func TestInitPingsNodesAndUpdatesStartupOverviewStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/rpc":
			var req mcpproto.JSONRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]interface{}{"tools": []mcpproto.ToolDefinition{}}))
		default:
			t.Fatalf("request path = %s, want /health or /rpc", r.URL.Path)
		}
	}))
	defer srv.Close()

	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Version: "dev",
		Clients: map[string]*mcp.Client{
			"node-01": mcp.NewClient(mcp.Config{BaseURL: srv.URL}),
		},
		Nodes: []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: false}},
	})

	pingMsg := execPingResultFromBatch(t, model.Init())
	next, _ := model.Update(pingMsg)
	model = next.(Model)

	if !model.nodes[0].Online {
		t.Fatal("node-01 should be online after startup ping")
	}
	view := model.View()
	for _, want := range []string{"Nodes     1/1 selected, 1 online", "Online"} {
		if !strings.Contains(view, want) {
			t.Fatalf("startup overview missing %q:\n%s", want, view)
		}
	}
}

func TestNodesNoNodesConfigured(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})

	for _, r := range "/nodes" {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = next.(Model)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeNodeSelect {
		t.Fatal("should enter node selector with no nodes")
	}
	if !model.nodeSelector.AddSelected() {
		t.Fatal("add row should be selected when no nodes are configured")
	}
	if !strings.Contains(model.View(), "Add new node") {
		t.Fatalf("view should show add option:\n%s", model.View())
	}
}

func TestMultiNodeDispatch(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Conv:    conv,
		Nodes:   nodes,
	})

	call := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "load average: 0.52", Success: true},
		{Node: "node-02", Output: "load average: 0.31", Success: true},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell_run on 2 node(s)") {
		t.Fatalf("view missing multi-node header:\n%s", view)
	}
	if !strings.Contains(view, "node-01") || !strings.Contains(view, "node-02") {
		t.Fatalf("view missing node names:\n%s", view)
	}
}

func TestToolOutputCollapsesAndTogglesLastToolWithCtrlO(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	firstCall := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"seq 1 6"}`)}
	firstOutput := "first 1\nfirst 2\nfirst 3\nfirst 4\nfirst 5\nfirst 6"

	next, _ := model.Update(multiToolResultMsg{Call: firstCall, Results: []nodeToolResult{{Node: "node-01", Output: firstOutput, Success: true}}})
	model = next.(Model)

	secondCall := llm.ToolCall{ID: "c2", Name: "shell_run", Arguments: []byte(`{"command":"seq 1 6"}`)}
	secondOutput := "second 1\nsecond 2\nsecond 3\nsecond 4\nsecond 5\nsecond 6"
	next, _ = model.Update(multiToolResultMsg{Call: secondCall, Results: []nodeToolResult{{Node: "node-01", Output: secondOutput, Success: true}}})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"first 4", "second 4", "2 more line(s)", "Ctrl+O"} {
		if !strings.Contains(view, want) {
			t.Fatalf("collapsed view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "first 6") || strings.Contains(view, "second 6") {
		t.Fatalf("collapsed view showed hidden lines:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = next.(Model)
	view = model.View()
	if strings.Contains(view, "first 6") {
		t.Fatalf("Ctrl+O expanded an older tool message:\n%s", view)
	}
	if !strings.Contains(view, "second 6") {
		t.Fatalf("expanded view missing last tool final line:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = next.(Model)
	view = model.View()
	if strings.Contains(view, "second 6") || !strings.Contains(view, "2 more line(s)") {
		t.Fatalf("re-collapsed view did not hide final lines:\n%s", view)
	}
}

func TestToolOutputToggleSkipsHiddenMemoryTools(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	visibleOutput := "visible 1\nvisible 2\nvisible 3\nvisible 4\nvisible 5\nvisible 6"
	model.messages = []chatMsg{
		{
			role:       "tool",
			toolCallID: "tc1",
			toolName:   "shell_run",
			toolOutput: visibleOutput,
			nodeResults: []nodeToolResult{{
				Node:    "node-01",
				Output:  visibleOutput,
				Success: true,
			}},
		},
		{
			role:       "tool",
			toolCallID: "mem1",
			toolName:   "memory_patch",
			toolOutput: "hidden memory output: User name is Alice.",
			nodeResults: []nodeToolResult{{
				Node:    "local",
				Output:  "hidden memory output: User name is Alice.",
				Success: true,
			}},
			hidden: true,
		},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "visible 6") {
		t.Fatalf("Ctrl+O did not expand visible tool output:\n%s", view)
	}
	if strings.Contains(view, "memory_patch") || strings.Contains(view, "User name is Alice") {
		t.Fatalf("hidden memory details leaked after Ctrl+O:\n%s", view)
	}
	if model.status != "Last tool output expanded" {
		t.Fatalf("status = %q, want visible tool expanded status", model.status)
	}
}

func TestShellRunOutputShowsStatusWithoutStdoutStderrLabels(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	call := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"pwd"}`)}
	output := "exit_code: 0\nstdout:\n/home/app\nstderr:\n"

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: []nodeToolResult{{Node: "node-01", Output: output, Success: true}}})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"status: 0", "/home/app"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"stdout:", "stderr:"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view contains %q:\n%s", unwanted, view)
		}
	}
}

func TestMultiNodeDispatchWithFailure(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})

	call := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
		{Node: "node-02", Output: "Connection timeout", Success: false},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "node-01") {
		t.Fatalf("view missing success node:\n%s", view)
	}
	if !strings.Contains(view, "node-02") {
		t.Fatalf("view missing failure node:\n%s", view)
	}
}

func TestDispatchExecCallsShellRunTool(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method != "tools/call" {
			t.Fatalf("method = %q, want tools/call", req.Method)
		}
		var params mcpproto.ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.Name != "shell_run" {
			t.Fatalf("tool name = %q, want shell_run", params.Name)
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			t.Fatalf("decode arguments: %v", err)
		}
		if args.Command != "uptime" {
			t.Fatalf("command = %q, want uptime", args.Command)
		}
		called = true
		_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("ok")}}))
	}))
	t.Cleanup(srv.Close)

	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Clients: map[string]*mcp.Client{"node-01": mcp.NewClient(mcp.Config{BaseURL: srv.URL})},
	})
	call := llm.ToolCall{ID: "c1", Name: "exec", Arguments: []byte(`{"node":"node-01","command":"uptime"}`)}

	msg := execCmd(t, model.dispatchTool(0, call))
	resultMsg, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	if !called {
		t.Fatal("agent tools/call was not called")
	}
	if len(resultMsg.Results) != 1 || !resultMsg.Results[0].Success {
		t.Fatalf("results = %#v, want one successful result", resultMsg.Results)
	}
}

func TestDispatchToolConnectionLostMarksOnlyFailedNodeOffline(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	okClient := mcp.NewClient(mcp.Config{BaseURL: "http://node-01", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`)),
			Header:     make(http.Header),
		}, nil
	})}})
	connectionLostClient := mcp.NewClient(mcp.Config{BaseURL: "http://node-02", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("read tcp 10.0.0.2:443->10.0.0.1:54832: read: connection reset by peer")
	})}})
	model := NewModel(ModelConfig{
		Cluster: "test",
		Model:   "m",
		Nodes:   nodes,
		Clients: map[string]*mcp.Client{
			"node-01": okClient,
			"node-02": connectionLostClient,
		},
	})
	call := llm.ToolCall{ID: "c1", Name: "shell_run", Arguments: []byte(`{"command":"ls"}`)}

	msg := execCmd(t, model.dispatchTool(0, call))
	resultMsg, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("dispatchTool returned %T, want multiToolResultMsg", msg)
	}
	next, _ := model.Update(resultMsg)
	model = next.(Model)

	if !model.nodes[0].Online {
		t.Fatal("node-01 should remain online")
	}
	if model.nodes[1].Online {
		t.Fatal("node-02 should be marked offline after connection loss")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// execCmd executes a tea.Cmd synchronously and returns its message.
func execCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func execMaybeBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return
	}
	for _, c := range batch {
		execCmd(t, c)
	}
}

func compactResultFromCmd(t *testing.T, cmd tea.Cmd) compactResultMsg {
	t.Helper()
	msg := execCmd(t, cmd)
	if result, ok := msg.(compactResultMsg); ok {
		return result
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("compact command returned %T, want compactResultMsg or tea.BatchMsg", msg)
	}
	for _, c := range batch {
		next := execCmd(t, c)
		if result, ok := next.(compactResultMsg); ok {
			return result
		}
	}
	t.Fatal("compact batch did not produce compactResultMsg")
	return compactResultMsg{}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func execVersionCheckFromBatch(t *testing.T, cmd tea.Cmd) versionCheckMsg {
	t.Helper()
	msg := execCmd(t, cmd)
	if vm, ok := msg.(versionCheckMsg); ok {
		return vm
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want versionCheckMsg or tea.BatchMsg", msg)
	}
	for _, c := range batch {
		inner := execCmd(t, c)
		if vm, ok := inner.(versionCheckMsg); ok {
			return vm
		}
	}
	t.Fatal("no versionCheckMsg in batch")
	return versionCheckMsg{}
}

func execPingResultFromBatch(t *testing.T, cmd tea.Cmd) pingResultMsg {
	t.Helper()
	msg := execCmd(t, cmd)
	if pm, ok := msg.(pingResultMsg); ok {
		return pm
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want pingResultMsg or tea.BatchMsg", msg)
	}
	for _, c := range batch {
		inner := execCmd(t, c)
		if pm, ok := inner.(pingResultMsg); ok {
			return pm
		}
	}
	t.Fatal("no pingResultMsg in batch")
	return pingResultMsg{}
}

type stubRiskProvider struct {
	response string
	err      error
	req      *llm.ChatRequest
	started  chan struct{}
	block    chan struct{}
	done     chan struct{}
}

func (s *stubRiskProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	s.req = req
	if s.started != nil {
		close(s.started)
	}
	if s.block != nil {
		select {
		case <-ctx.Done():
			if s.done != nil {
				close(s.done)
			}
			return nil, ctx.Err()
		case <-s.block:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: s.response},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (s *stubRiskProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	return nil, nil
}

type countingStreamProvider struct {
	calls int
}

func (p *countingStreamProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}

func (p *countingStreamProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.calls++
	ch := make(chan llm.ChatEvent)
	close(ch)
	return ch, nil
}

func TestRiskDenyFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"deny","reason":"Destructive"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages after tool call = %#v, want one empty placeholder", model.messages)
	}

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want denial to fill existing placeholder only", model.messages)
	}
	if model.messages[0].toolOutput == "" || !strings.Contains(model.messages[0].toolOutput, "BLOCKED") {
		t.Fatalf("placeholder output = %q, want BLOCKED", model.messages[0].toolOutput)
	}
}

func TestToolCallDeniedBySecurity(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"deny","reason":"Destructive"}`},
	})
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()
	model := NewModel(ModelConfig{
		Cluster:     "test",
		Model:       "m",
		Conv:        conv,
		Reviewer:    reviewer,
		AuditLogger: auditLog,
		Nodes:       []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	view := model.View()
	if !strings.Contains(view, "BLOCKED") {
		t.Fatalf("denied tool should show BLOCKED in view:\n%s", view)
	}
	auditContents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditContents), "[DENY]") || !strings.Contains(string(auditContents), "node-01") {
		t.Fatalf("audit log missing denied decision: %s", auditContents)
	}
}

func TestRiskAssessmentErrorFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{err: errors.New("review unavailable")},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want error to fill existing placeholder only", model.messages)
	}
	if !strings.Contains(model.messages[0].toolOutput, "Risk assessment error") {
		t.Fatalf("placeholder output = %q, want risk assessment error", model.messages[0].toolOutput)
	}
}

func TestRiskAssessmentErrorRecordsConversationToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{err: errors.New("review unavailable")},
	})
	provider := &countingStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Provider: provider,
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"rm -rf /"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, cmd = model.Update(msg)
	model = result.(Model)

	if cmd == nil {
		t.Fatal("risk assessment error should resume after recording tool result")
	}
	if provider.calls != 0 {
		t.Fatalf("ChatStream called before command execution: %d", provider.calls)
	}
	msgs := conv.Messages()
	if len(msgs) != 2 || msgs[0].ToolCallID != "tc1" || msgs[1].Role != conversation.RoleTool || msgs[1].ToolCallID != "tc1" {
		t.Fatalf("conversation messages = %#v, want tool call followed by matching tool result", msgs)
	}
	if !strings.Contains(msgs[1].Content, "Risk assessment error") {
		t.Fatalf("tool result content = %q, want risk assessment error", msgs[1].Content)
	}
}

func TestMultipleToolCallsResumeOnlyAfterAllResults(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	provider := &countingStreamProvider{}
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: provider, Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	for _, call := range []llm.ToolCallEvent{
		{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)},
		{ID: "tc2", Name: "shell_run", Arguments: []byte(`{"command":"date"}`)},
	} {
		result, _ := model.Update(streamEventMsg{streamID: 1, Event: call})
		model = result.(Model)
	}
	result, _ := model.Update(streamDoneMsg{streamID: 1})
	model = result.(Model)

	result, cmd := model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "up", Success: true}}})
	model = result.(Model)
	if cmd != nil {
		t.Fatal("first tool result resumed stream before second result")
	}
	if provider.calls != 0 {
		t.Fatalf("ChatStream calls = %d, want none before all tool results", provider.calls)
	}

	result, cmd = model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc2", Name: "shell_run", Arguments: []byte(`{"command":"date"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "today", Success: true}}})
	model = result.(Model)
	if cmd == nil {
		t.Fatal("second tool result should resume stream")
	}
	execMaybeBatch(t, cmd)
	if provider.calls != 1 {
		t.Fatalf("ChatStream calls = %d, want one resume after all tool results", provider.calls)
	}
}

func TestIdenticalToolCallsFillPlaceholderByID(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	args := []byte(`{"command":"uptime"}`)
	model.messages = []chatMsg{
		{role: "tool", toolCallID: "tc1", toolName: "shell_run", toolInput: string(args)},
		{role: "tool", toolCallID: "tc2", toolName: "shell_run", toolInput: string(args)},
	}

	model.fillToolPlaceholder(llm.ToolCall{ID: "tc1", Name: "shell_run", Arguments: args}, "first", nil)

	if model.messages[0].toolOutput != "first" {
		t.Fatalf("first output = %q, want first", model.messages[0].toolOutput)
	}
	if model.messages[1].toolOutput != "" {
		t.Fatalf("second output = %q, want empty", model.messages[1].toolOutput)
	}
}

func TestIncidentCommandLifecycleAndExport(t *testing.T) {
	dir := t.TempDir()
	model := NewModel(ModelConfig{
		Cluster:     "prod",
		Model:       "model",
		IncidentDir: filepath.Join(dir, "incidents"),
		Nodes:       []NodeInfo{{Name: "web-1", Host: "10.0.0.1"}},
	})

	var cmd tea.Cmd
	model, cmd = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start API latency"})
	if cmd != nil {
		t.Fatal("incident start should not return command")
	}
	if model.incidentRecorder == nil || model.incidentRecorder.Current() == nil {
		t.Fatal("incident recorder should have current incident")
	}
	if !strings.Contains(model.status, "Incident started") {
		t.Fatalf("status = %q", model.status)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "note checked nginx logs"})
	if events := model.incidentRecorder.Events(); len(events) != 1 || events[0].Summary != "checked nginx logs" {
		t.Fatalf("note events = %#v", events)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "export"})
	if !strings.Contains(model.status, "incidents/") {
		t.Fatalf("export status missing path: %q", model.status)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "incidents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("exported reports = %#v, want one", matches)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "close"})
	if model.incidentRecorder.Current() != nil {
		t.Fatal("incident should be closed")
	}
}

func TestIncidentStartDoesNotDiscardOpenIncident(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(t.TempDir(), "incidents")})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start First incident"})
	model.incidentRecorder.Note("first note")
	first := model.incidentRecorder.Current()

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start Second incident"})

	current := model.incidentRecorder.Current()
	if current == nil || current.ID != first.ID || current.Title != "First incident" {
		t.Fatalf("open incident was replaced: before=%#v after=%#v", first, current)
	}
	if events := model.incidentRecorder.Events(); len(events) != 1 || events[0].Summary != "first note" {
		t.Fatalf("existing incident events lost: %#v", events)
	}
}

func TestIncidentCloseExportsClosedReport(t *testing.T) {
	dir := t.TempDir()
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(dir, "incidents")})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start API latency"})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "close"})

	matches, err := filepath.Glob(filepath.Join(dir, "incidents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("reports = %#v, want one", matches)
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status: closed", "closed_at:"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("closed report missing %q:\n%s", want, string(content))
		}
	}
}

func TestIncidentNoteRequiresOpenIncident(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(t.TempDir(), "incidents")})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "note checked nginx"})

	if !strings.Contains(model.status, "no open incident") {
		t.Fatalf("status = %q, want no open incident", model.status)
	}
}

func TestIncidentRecordsUserToolRiskAndAssistantEvents(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster:     "prod",
		Model:       "model",
		IncidentDir: filepath.Join(t.TempDir(), "incidents"),
		Nodes:       []NodeInfo{{Name: "web-1", Host: "10.0.0.1"}},
	})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start API latency"})

	updated, _ := model.startSubmittedMessage("check api", "check api", nil)
	model = updated.(Model)
	model.recordAssistantEvidence("api recovered")
	updated, _ = model.Update(riskAssessmentMsg{
		call:       llm.ToolCall{ID: "risk-1", Name: "svc_restart", Arguments: json.RawMessage(`{"service":"nginx"}`)},
		assessment: security.RiskAssessment{Level: security.RiskConfirm, Reason: "restart service"},
	})
	model = updated.(Model)
	updated, _ = model.Update(multiToolResultMsg{
		Call:    llm.ToolCall{ID: "tool-1", Name: "svc_status", Arguments: json.RawMessage(`{"service":"nginx"}`)},
		Results: []nodeToolResult{{Node: "web-1", Output: "active", Success: true}},
	})
	model = updated.(Model)

	var sources []string
	for _, event := range model.incidentRecorder.Events() {
		sources = append(sources, string(event.Source))
	}
	joined := strings.Join(sources, ",")
	for _, want := range []string{"user", "assistant", "risk", "tool"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("events missing %s: %#v", want, model.incidentRecorder.Events())
		}
	}
}

func TestRunbookDraftPreviewAndRunCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "incidents"), 0700); err != nil {
		t.Fatal(err)
	}
	incidentPath := filepath.Join(root, "incidents", "2026-05-23-api.md")
	incident := `# API latency incident

incident_id: incident-abc123
cluster: prod

## 摘要

- API latency recovered.

## 影响范围

- nodes: web-1

## 证据

- 2026-05-23T10:00:00Z tool=svc_status success=true nginx active

## 执行动作

- 2026-05-23T10:05:00Z tool=svc_restart risk=confirm outcome=approved restart nginx

## 验证结果

- Latency recovered.
`
	if err := os.WriteFile(incidentPath, []byte(incident), 0600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{
		Cluster:     "prod",
		Model:       "model",
		IncidentDir: filepath.Join(root, "incidents"),
	})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "draft incidents/2026-05-23-api.md"})
	matches, err := filepath.Glob(filepath.Join(root, "runbooks", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("runbook drafts = %#v, want one", matches)
	}
	relRunbook := filepath.ToSlash(filepath.Join("runbooks", filepath.Base(matches[0])))

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "preview " + relRunbook})
	if len(model.messages) == 0 || !strings.Contains(model.messages[len(model.messages)-1].content, "Runbook preview") {
		t.Fatalf("preview message missing: %#v", model.messages)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "run " + relRunbook})
	if len(model.messages) == 0 || !strings.Contains(model.messages[len(model.messages)-1].content, "Execute this Conan runbook") {
		t.Fatalf("run injection missing: %#v", model.messages)
	}
	if !strings.Contains(model.messages[len(model.messages)-1].content, "existing risk review and confirmation flow") {
		t.Fatalf("run injection should preserve confirmations:\n%s", model.messages[len(model.messages)-1].content)
	}
	if !strings.Contains(model.messages[len(model.messages)-1].content, "ask whether to append the outcome") {
		t.Fatalf("run injection should ask about promotion back into runbook:\n%s", model.messages[len(model.messages)-1].content)
	}
}

func TestRunbookCommandsRejectWrongPathCategories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "incidents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runbooks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "incidents", "incident.md"), []byte("# Incident\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runbooks", "runbook.md"), []byte("# Runbook\n"), 0600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(root, "incidents")})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "draft runbooks/runbook.md"})
	if !strings.Contains(model.status, "incident path must be under incidents/") {
		t.Fatalf("draft status = %q", model.status)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "preview incidents/incident.md"})
	if !strings.Contains(model.status, "runbook path must be under runbooks/") {
		t.Fatalf("preview status = %q", model.status)
	}

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "run incidents/incident.md"})
	if !strings.Contains(model.status, "runbook path must be under runbooks/") {
		t.Fatalf("run status = %q", model.status)
	}
}

func TestRunbookDraftCreatesUniqueFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "incidents"), 0700); err != nil {
		t.Fatal(err)
	}
	incident := "# API latency incident\n\ncluster: prod\n\n## 摘要\n\nok\n"
	if err := os.WriteFile(filepath.Join(root, "incidents", "incident.md"), []byte(incident), 0600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(root, "incidents")})

	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "draft incidents/incident.md"})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandRunbook, Arg: "draft incidents/incident.md"})

	matches, err := filepath.Glob(filepath.Join(root, "runbooks", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("runbook files = %#v, want two unique files", matches)
	}
}

func TestToolCallNeedsConfirmation(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Restarts service","suggestion":"Rolling restart"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Security Review") {
		t.Fatalf("confirm mode should show security review:\n%s", view)
	}
	if !strings.Contains(view, "▶ Allow") || !strings.Contains(view, "Deny") {
		t.Fatalf("confirm mode should show selectable allow/deny options:\n%s", view)
	}
	if strings.Contains(view, "▶ Allow    Deny") {
		t.Fatalf("confirm options should be stacked vertically:\n%s", view)
	}
	if strings.Contains(view, "yes") || strings.Contains(view, "Confirm?") {
		t.Fatalf("confirm mode should not require typed confirmation:\n%s", view)
	}
	if !strings.Contains(view, "Restarts service") {
		t.Fatalf("confirm mode should show risk reason:\n%s", view)
	}
	if !strings.Contains(view, "shell_run") {
		t.Fatalf("confirm mode should keep the tool placeholder visible:\n%s", view)
	}
	if !strings.Contains(view, "Command") || !strings.Contains(view, "systemctl restart nginx") {
		t.Fatalf("confirm mode should show the command being reviewed:\n%s", view)
	}
	if strings.Contains(view, "╭") || strings.Contains(view, "╰") {
		t.Fatalf("confirm mode should render inline at the bottom, not as a separate panel:\n%s", view)
	}
}

func TestAskChoiceToolCallEntersChoiceMode(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	ch := make(chan llm.ChatEvent)
	model.streamCh = ch
	model.streamCtx = context.Background()

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{
			"question":"Pick a path",
			"options":[
				{"label":"Continue","value":"continue"},
				{"label":"Revise","value":"revise"}
			],
			"default_value":"revise",
			"allow_cancel":true
		}`),
	}})
	model = next.(Model)

	if cmd == nil {
		t.Fatal("ask_choice should keep stream reader active while waiting for user input")
	}
	msg := execCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("ask_choice command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want stream wait and stream timeout", len(batch))
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if model.choice.question != "Pick a path" || model.choice.selected != 1 {
		t.Fatalf("choice state = %#v", model.choice)
	}
	if len(model.messages) != 1 || model.messages[0].toolCallID != "choice-1" || model.messages[0].toolName != metaToolAskChoice {
		t.Fatalf("messages = %#v, want recorded ask_choice placeholder", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].ToolCallID != "choice-1" || msgs[0].ToolName != metaToolAskChoice {
		t.Fatalf("conversation messages = %#v, want recorded ask_choice tool call", msgs)
	}

	go func() {
		ch <- llm.StopEvent{Reason: llm.StopToolUse}
	}()
	continuedMsg := execCmd(t, batch[0])
	if _, ok := continuedMsg.(streamEventMsg); !ok {
		t.Fatalf("continued wait command returned %T, want streamEventMsg", continuedMsg)
	}
	if model.mode != modeChoice {
		t.Fatalf("mode after executing wait command = %v, want modeChoice", model.mode)
	}
}

func TestAskChoiceStopToolUseKeepsChoiceModeAndStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCtx = context.Background()

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{
			"question":"Pick a path",
			"options":[
				{"label":"Continue","value":"continue"},
				{"label":"Revise","value":"revise"}
			]
		}`),
	}})
	model = next.(Model)
	choiceStatus := model.status

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopToolUse}})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("StopToolUse should wait for ask_choice result before continuing")
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if !model.streamEnded {
		t.Fatal("StopToolUse should mark stream ended")
	}
	if model.status != choiceStatus || !strings.Contains(model.status, "Use ↑↓ to choose") {
		t.Fatalf("status = %q, want choice guidance %q", model.status, choiceStatus)
	}
}

func TestAskChoiceEnterReturnsSelectedToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick one",
		options: []choiceOption{
			{Label: "Continue", Value: "continue"},
			{Label: "Revise", Value: "revise"},
		},
		selected:    0,
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.choice.selected != 1 {
		t.Fatalf("selected = %d, want 1", model.choice.selected)
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.choice.call.ID != "" {
		t.Fatalf("choice state should be cleared: %#v", model.choice)
	}
	if cmd == nil {
		t.Fatal("enter should continue after tool result")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].toolOutput, `"value":"revise"`) {
		t.Fatalf("messages = %#v, want selected tool output", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != conversation.RoleTool || !strings.Contains(msgs[0].Content, `"value":"revise"`) {
		t.Fatalf("conversation messages = %#v, want selected tool result", msgs)
	}
}

func TestAskChoiceSpaceTogglesMultiSelectionAndEnterReturnsValues(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick targets",
		options: []choiceOption{
			{Label: "Logs", Value: "logs"},
			{Label: "Metrics", Value: "metrics"},
		},
		multiple:       true,
		selectedValues: map[string]bool{"logs": true},
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if !model.choice.selectedValues["metrics"] {
		t.Fatalf("selectedValues = %#v, want metrics selected", model.choice.selectedValues)
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if cmd == nil {
		t.Fatal("enter should continue after multi-choice tool result")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].toolOutput, `"values":["logs","metrics"]`) {
		t.Fatalf("messages = %#v, want multi-choice values", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, `"values":["logs","metrics"]`) {
		t.Fatalf("conversation messages = %#v, want multi-choice values", msgs)
	}
}

func TestAskChoiceStreamTimeoutKeepsWaitingForChoice(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.streamEventSeq = 3
	model.mode = modeChoice
	model.status = "Use ↑↓ to choose, Enter to confirm"
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick one",
		options: []choiceOption{
			{Label: "Continue", Value: "continue"},
			{Label: "Revise", Value: "revise"},
		},
		selected:    0,
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}
	choiceStatus := model.status

	next, cmd := model.Update(streamTimeoutMsg{streamID: 1, eventSeq: 3})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("timeout while choosing returned cmd %T, want nil", cmd)
	}
	if !model.streaming {
		t.Fatal("timeout while choosing should keep stream state active")
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if model.activeStreamID != 1 || model.streamID != 1 || !model.streamEnded || model.streamToolExpected != 1 {
		t.Fatalf("stream state changed unexpectedly: active=%d id=%d ended=%v expected=%d", model.activeStreamID, model.streamID, model.streamEnded, model.streamToolExpected)
	}
	if model.status != choiceStatus {
		t.Fatalf("status = %q, want unchanged choice guidance %q", model.status, choiceStatus)
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if cmd == nil {
		t.Fatal("enter after timeout should continue after tool result")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].toolOutput, `"value":"continue"`) {
		t.Fatalf("messages = %#v, want selected tool output", model.messages)
	}
}

func TestAskChoiceStreamTimeoutBeforeStopInterruptsChoice(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = false
	model.streamToolExpected = 1
	model.streamEventSeq = 3
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick one",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, cmd := model.Update(streamTimeoutMsg{streamID: 1, eventSeq: 3})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("timeout returned cmd %T, want nil", cmd)
	}
	if model.streaming {
		t.Fatal("timeout before StopToolUse should interrupt streaming")
	}
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.choice.call.ID != "" {
		t.Fatalf("choice state should be cleared: %#v", model.choice)
	}
	if len(model.messages) != 1 || !strings.Contains(strings.ToLower(model.messages[0].toolOutput), "timeout") {
		t.Fatalf("messages = %#v, want timeout tool output", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != conversation.RoleTool || msgs[0].ToolCallID != "choice-1" || !strings.Contains(strings.ToLower(msgs[0].Content), "timeout") {
		t.Fatalf("conversation messages = %#v, want timeout tool result", msgs)
	}
}

func TestAskChoiceRejectsParallelToolCallWhilePending(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "First choice",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-2", Name: metaToolAskChoice, Arguments: []byte(`{
			"question":"Second choice",
			"options":[{"label":"C","value":"c"},{"label":"D","value":"d"}]
		}`),
	}})
	model = next.(Model)

	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if model.choice.call.ID != "choice-1" || model.choice.question != "First choice" {
		t.Fatalf("choice was overwritten: %#v", model.choice)
	}
	result := askChoiceResultFromCmd(t, cmd)
	if len(result.Results) != 1 || result.Results[0].Success {
		t.Fatalf("parallel result = %#v, want failed result", result.Results)
	}
	if !strings.Contains(strings.ToLower(result.Results[0].Output), "choice already pending") {
		t.Fatalf("parallel output = %q, want pending-choice error", result.Results[0].Output)
	}
}

func TestAskChoiceEscCancelsWhenAllowed(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID:    1,
		call:        llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question:    "Pick one",
		options:     []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if cmd == nil {
		t.Fatal("cancel should continue after tool result")
	}
	if !strings.Contains(model.messages[0].toolOutput, `"cancelled":true`) {
		t.Fatalf("tool output = %q, want cancellation JSON", model.messages[0].toolOutput)
	}
}

func TestAskChoiceCtrlCRecordsInterruptedToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID:    1,
		call:        llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question:    "Pick one",
		options:     []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("ctrl-c returned cmd %T, want nil", cmd)
	}
	if model.streaming {
		t.Fatal("ctrl-c should interrupt streaming")
	}
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.status != "Interrupted" {
		t.Fatalf("status = %q, want Interrupted", model.status)
	}
	if model.choice.call.ID != "" {
		t.Fatalf("choice state should be cleared: %#v", model.choice)
	}
	if len(model.messages) != 1 || !strings.Contains(strings.ToLower(model.messages[0].toolOutput), "interrupted") {
		t.Fatalf("messages = %#v, want interrupted tool output", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != conversation.RoleTool || msgs[0].ToolCallID != "choice-1" {
		t.Fatalf("conversation messages = %#v, want tool result for choice-1", msgs)
	}
	if !strings.Contains(strings.ToLower(msgs[0].Content), "interrupted") {
		t.Fatalf("conversation tool result = %q, want interrupted output", msgs[0].Content)
	}
}

func TestAskChoiceEscBlockedWhenCancelDisabled(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.mode = modeChoice
	model.choice = choiceState{
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice},
		question: "Pick one",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("cmd = %T, want nil when cancel is disabled", cmd)
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if !strings.Contains(model.status, "Choose an option") && !strings.Contains(model.status, "请选择") {
		t.Fatalf("status = %q, want choose-option guidance", model.status)
	}
}

func TestAskChoiceViewRendersOptions(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.width = 80
	model.mode = modeChoice
	model.choice = choiceState{
		question: "Pick a path",
		options: []choiceOption{
			{Label: "Continue", Value: "continue", Description: "Run the planned command"},
			{Label: "Revise", Value: "revise"},
		},
		selected:    0,
		allowCancel: true,
	}

	view := model.View()
	for _, want := range []string{"Pick a path", "▶ ○ Continue", "└─ Run the planned command", "○ Revise", "Enter to choose", "Esc to cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestAskChoiceViewRendersMultiSelectStateAndHelp(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.width = 80
	model.mode = modeChoice
	model.choice = choiceState{
		question: "Pick targets",
		options: []choiceOption{
			{Label: "Logs", Value: "logs", Description: "Collect application logs"},
			{Label: "Metrics", Value: "metrics"},
		},
		multiple:       true,
		selectedValues: map[string]bool{"logs": true},
	}

	view := model.View()
	for _, want := range []string{"Pick targets", "▶ ☑ Logs", "└─ Collect application logs", "☐ Metrics", "Space to toggle", "Enter to choose"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestAskChoiceSelectionRecordsToolResultEvidence(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "prod", Model: "model", IncidentDir: filepath.Join(t.TempDir(), "incidents")})
	model, _ = model.applyCommand(SlashCommand{Kind: CommandIncident, Arg: "start Ask choice"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick one",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	found := false
	for _, event := range model.incidentRecorder.Events() {
		if event.ToolName == metaToolAskChoice && strings.Contains(event.Summary, `"value":"a"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("events missing ask_choice tool result: %#v", model.incidentRecorder.Events())
	}
}

func TestAskChoiceInvalidArgumentsReturnToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{"question":"Pick","options":[]}`),
	}})
	model = next.(Model)

	if model.mode == modeChoice {
		t.Fatal("invalid ask_choice arguments should not open choice mode")
	}
	if cmd == nil {
		t.Fatal("invalid ask_choice arguments should return a tool result command")
	}
	result := askChoiceResultFromCmd(t, cmd)
	if len(result.Results) != 1 || result.Results[0].Success {
		t.Fatalf("results = %#v, want one failed result", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "at least 2 options") {
		t.Fatalf("output = %q, want validation error", result.Results[0].Output)
	}
}

func TestAskChoiceInvalidArgumentsDoNotWriteAuditExecution(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()

	model := NewModel(ModelConfig{Cluster: "test", Model: "m", AuditLogger: auditLog})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{"question":"Pick","options":[]}`),
	}})
	model = next.(Model)

	result := askChoiceResultFromCmd(t, cmd)
	next, _ = model.Update(result)
	model = next.(Model)
	if model.mode == modeChoice {
		t.Fatal("invalid ask_choice arguments should not open choice mode")
	}
	if err := auditLog.Close(); err != nil {
		t.Fatalf("close audit log: %v", err)
	}
	contents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if strings.Contains(string(contents), metaToolAskChoice) || strings.Contains(string(contents), "EXECUTE") {
		t.Fatalf("ask_choice invalid result should not write audit execution: %s", contents)
	}
}

func askChoiceResultFromCmd(t *testing.T, cmd tea.Cmd) multiToolResultMsg {
	t.Helper()
	msg := execCmd(t, cmd)
	if result, ok := msg.(multiToolResultMsg); ok {
		return result
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want multiToolResultMsg or tea.BatchMsg", msg)
	}
	for _, c := range batch {
		inner := execCmd(t, c)
		if result, ok := inner.(multiToolResultMsg); ok {
			return result
		}
	}
	t.Fatal("command did not produce multiToolResultMsg")
	return multiToolResultMsg{}
}

func TestConfirmationSummaryShowsFileTransferImpact(t *testing.T) {
	call := llm.ToolCall{
		Name:      metaToolFilePut,
		Arguments: json.RawMessage(`{"node":"web-1","local_path":"README.md","remote_path":"/tmp/README.md"}`),
	}

	lines := confirmationSummary(call, []string{"web-1"})
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"Tool: file_put",
		"Safety: mutating",
		"Scope: node",
		"Node: web-1",
		"local_path: README.md",
		"remote_path: /tmp/README.md",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("summary missing %q:\n%s", want, joined)
		}
	}
}

func TestConfirmationSummaryShowsCallToolInnerImpact(t *testing.T) {
	call := llm.ToolCall{
		Name:      metaToolCallTool,
		Arguments: json.RawMessage(`{"node":"web-1","tool":"svc_restart","arguments":{"service":"nginx"}}`),
	}

	lines := confirmationSummary(call, []string{"web-1"})
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"Tool: call_tool",
		"Inner tool: svc_restart",
		"Node: web-1",
		"service: nginx",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("summary missing %q:\n%s", want, joined)
		}
	}
}

func TestNodeAddRequiresConfirmationEvenWhenRiskAllows(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"allow","reason":"allowed"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
	})
	model.nodeToolsEnabled = true
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID:        "node-add-1",
		Name:      metaToolNodeAdd,
		Arguments: []byte(`{"host":"10.0.0.12","user":"deploy","password":"secret"}`),
	}})
	model = result.(Model)

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", model.mode)
	}
	if model.pendingRisk == nil || model.pendingRisk.Level != security.RiskConfirm {
		t.Fatalf("pendingRisk = %#v, want forced confirmation", model.pendingRisk)
	}
	if !strings.Contains(model.View(), "node_add requires confirmation") {
		t.Fatalf("view missing forced confirmation reason:\n%s", model.View())
	}
}

func TestConfirmEnterOnAllowDispatchesTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditLog, err := security.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer auditLog.Close()
	model := NewModel(ModelConfig{
		Cluster:     "test",
		Model:       "m",
		Conv:        conv,
		Reviewer:    reviewer,
		AuditLogger: auditLog,
		Nodes:       []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatal("should be in confirm mode")
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after confirm", model.mode)
	}
	if cmd == nil {
		t.Fatal("confirming should dispatch the tool")
	}
	auditContents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditContents), "[CONFIRM]") || !strings.Contains(string(auditContents), `outcome="approved"`) {
		t.Fatalf("audit log missing approved confirmation: %s", auditContents)
	}
}

func TestConfirmNoFillsExistingToolPlaceholder(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)
	if len(model.messages) != 1 || model.messages[0].toolOutput != "" {
		t.Fatalf("messages after tool call = %#v, want one empty placeholder", model.messages)
	}

	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = result.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want deny selected", model.confirmChoice)
	}
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = result.(Model)

	if len(model.messages) != 1 {
		t.Fatalf("messages = %#v, want cancellation to fill existing placeholder only", model.messages)
	}
	if model.messages[0].toolOutput != "Cancelled by user" {
		t.Fatalf("placeholder output = %q, want cancellation", model.messages[0].toolOutput)
	}
}

func TestConfirmAllowAndAddWritesNodeAllowlist(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "clusters", "test", "cluster.yaml"), `name: test
`)
	writeTestFile(t, filepath.Join(home, "clusters", "test", "nodes.yaml"), `nodes:
  - name: node-01
    host: 10.0.0.11
`)
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:    "test",
		Model:      "m",
		Conv:       conv,
		Reviewer:   reviewer,
		ConfigHome: home,
		Nodes:      []NodeInfo{{Name: "node-01", Host: "10.0.0.11", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "exec", Arguments: []byte(`{"command":"systemctl restart nginx","node":"node-01"}`),
	}})
	model = result.(Model)
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	view := model.View()
	if !strings.Contains(view, "Allow and add to allowlist") {
		t.Fatalf("confirm view missing allowlist option:\n%s", view)
	}
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = result.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want allow and add selected", model.confirmChoice)
	}
	result, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = result.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want chat", model.mode)
	}
	if cmd == nil {
		t.Fatal("allow and add should dispatch the tool")
	}
	data, err := os.ReadFile(filepath.Join(home, "clusters", "test", "nodes.yaml"))
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	if !strings.Contains(string(data), "systemctl restart nginx") {
		t.Fatalf("nodes.yaml missing command allowlist:\n%s", data)
	}

	assessment, err := reviewer.Review(context.Background(), "exec", `{"command":"systemctl restart nginx","node":"node-01"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Level != security.RiskAllow {
		t.Fatalf("updated reviewer should allow command, got %#v", assessment)
	}
}

func TestConfirmAllowAndAddWritesLocalFileAllowlist(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.yaml"), `default_cluster: test
`)
	writeTestFile(t, filepath.Join(workspace, "README.md"), "old content")

	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{})
	model := NewModel(ModelConfig{
		Cluster:            "test",
		Model:              "m",
		Conv:               conv,
		Reviewer:           reviewer,
		ConfigHome:         home,
		LocalWorkspaceRoot: workspace,
	})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "local_fs_write", Arguments: []byte(`{"path":"README.md","content":"new content"}`),
	}})
	model = result.(Model)
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want confirm", model.mode)
	}
	if !strings.Contains(model.View(), "Allow and add to allowlist") {
		t.Fatalf("confirm view missing local file allowlist option:\n%s", model.View())
	}
	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = result.(Model)
	result, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = result.(Model)
	if cmd == nil {
		t.Fatal("allow and add should dispatch local file tool")
	}

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "local_file_whitelist") || !strings.Contains(string(data), "README.md") {
		t.Fatalf("config missing local file allowlist:\n%s", data)
	}

	assessment, err := reviewer.Review(context.Background(), "local_fs_write", `{"path":"README.md","content":"again"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Level != security.RiskAllow {
		t.Fatalf("updated reviewer should allow local file write, got %#v", assessment)
	}
}

func TestConfirmEscCancelsTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	result, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = result.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after esc", model.mode)
	}
	if model.messages[0].toolOutput != "Cancelled by user" {
		t.Fatalf("placeholder output = %q, want cancellation", model.messages[0].toolOutput)
	}
}

func TestConfirmNoCancelsTool(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"Risky"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}

	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	result, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
	}})
	model = result.(Model)

	// Execute the assessToolRisk command to get riskAssessmentMsg
	msg := execCmd(t, cmd)
	result, _ = model.Update(msg)
	model = result.(Model)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.confirmChoice != 1 {
		t.Fatalf("confirmChoice = %d, want deny selected", model.confirmChoice)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat after cancel", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Cancelled") {
		t.Fatalf("cancelled tool should show cancelled:\n%s", view)
	}
}

func TestParallelRiskConfirmationsAreQueued(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"multi"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	calls := []llm.ToolCall{
		{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)},
		{ID: "tc2", Name: "shell_run", Arguments: []byte(`{"command":"date"}`)},
		{ID: "tc3", Name: "shell_run", Arguments: []byte(`{"command":"whoami"}`)},
		{ID: "tc4", Name: "shell_run", Arguments: []byte(`{"command":"hostname"}`)},
	}
	for _, c := range calls {
		next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
			ID: c.ID, Name: c.Name, Arguments: c.Arguments,
		}})
		model = next.(Model)
		riskMsg := execCmd(t, cmd)
		if riskMsg == nil {
			t.Fatalf("call %s: risk cmd was nil", c.ID)
		}
		next, _ = model.Update(riskMsg)
		model = next.(Model)
	}

	if model.streamToolExpected != len(calls) {
		t.Fatalf("streamToolExpected = %d, want %d", model.streamToolExpected, len(calls))
	}
	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm after first risk", model.mode)
	}
	if model.pendingToolCall == nil || model.pendingToolCall.ID != "tc1" {
		t.Fatalf("first pending = %#v, want tc1", model.pendingToolCall)
	}
	if len(model.pendingToolQueue) != len(calls)-1 {
		t.Fatalf("queue len = %d, want %d", len(model.pendingToolQueue), len(calls)-1)
	}

	for i, c := range calls {
		if model.pendingToolCall == nil || model.pendingToolCall.ID != c.ID {
			t.Fatalf("round %d: pending = %#v, want %s", i, model.pendingToolCall, c.ID)
		}
		if model.mode != modeConfirm {
			t.Fatalf("round %d: mode = %v, want modeConfirm", i, model.mode)
		}
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = next.(Model)
	}

	if model.mode == modeConfirm {
		t.Fatalf("mode should leave modeConfirm once queue is drained, still in %v with pending=%#v queue=%#v", model.mode, model.pendingToolCall, model.pendingToolQueue)
	}
	if model.pendingToolCall != nil {
		t.Fatalf("pending should be nil after queue drain, got %#v", model.pendingToolCall)
	}
	if len(model.pendingToolQueue) != 0 {
		t.Fatalf("queue should be empty, got %d entries", len(model.pendingToolQueue))
	}
}

func TestParallelRiskConfirmationsEscCancelsAll(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	reviewer := security.NewReviewer(security.ReviewerConfig{
		Provider: &stubRiskProvider{response: `{"risk_level":"confirm","reason":"multi"}`},
	})
	model := NewModel(ModelConfig{
		Cluster:  "test",
		Model:    "m",
		Conv:     conv,
		Reviewer: reviewer,
		Nodes:    []NodeInfo{{Name: "node-01", Host: "10.0.1.1", Online: true}},
	})
	model.selectedNodes = map[string]bool{"node-01": true}
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	calls := []llm.ToolCall{
		{ID: "tc1", Name: "shell_run", Arguments: []byte(`{"command":"uptime"}`)},
		{ID: "tc2", Name: "shell_run", Arguments: []byte(`{"command":"date"}`)},
	}
	for _, c := range calls {
		next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
			ID: c.ID, Name: c.Name, Arguments: c.Arguments,
		}})
		model = next.(Model)
		riskMsg := execCmd(t, cmd)
		next, _ = model.Update(riskMsg)
		model = next.(Model)
	}

	for i := 0; i < len(calls); i++ {
		if model.pendingToolCall == nil {
			t.Fatalf("round %d: pending is nil", i)
		}
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		model = next.(Model)
	}
	if model.mode == modeConfirm {
		t.Fatalf("Esc should have drained queue, still in modeConfirm")
	}
	if model.pendingToolCall != nil || len(model.pendingToolQueue) != 0 {
		t.Fatalf("queue not drained: pending=%#v queue=%#v", model.pendingToolCall, model.pendingToolQueue)
	}
}
