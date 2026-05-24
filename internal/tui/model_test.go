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
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/internal/skills"
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
	candidates []memory.MemoryCandidate
	err        error
	inputs     []MemoryExtractionInput
}

func (s *stubMemoryExtractor) ExtractMemory(_ context.Context, input MemoryExtractionInput) ([]memory.MemoryCandidate, error) {
	s.inputs = append(s.inputs, input)
	return s.candidates, s.err
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
	for _, legacy := range []string{"memory/save", "memory/search"} {
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
		"██████╗ ██████╗ ███╗   ██╗ █████╗ ███╗   ██╗",
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
			toolName:   "shell/run",
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

func TestUpDownNavigatesInputHistoryAndRestoresDraft(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model = typeAndEnter(t, model, "first")
	model = typeAndEnter(t, model, "second")
	model = typeRunes(t, model, "draft")

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.input != "second" {
		t.Fatalf("after first Up input = %q, want second", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.input != "first" {
		t.Fatalf("after second Up input = %q, want first", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.input != "first" {
		t.Fatalf("extra Up input = %q, want first", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.input != "second" {
		t.Fatalf("after first Down input = %q, want second", model.input)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.input != "draft" {
		t.Fatalf("after second Down input = %q, want restored draft", model.input)
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
	msg := execCmd(t, cmd)
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

	if cmd := model.Init(); cmd != nil {
		t.Fatal("Init() returned command for dev version with no clients, want nil")
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
			Name:        "log/read",
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
		"system_prompt",
		"messages",
		"tools",
		"tool_search",
		"call_tool",
		"llm stream text_delta",
		"Hi",
		"llm stream stop",
		"end_turn",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logText)
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
		ID: "tc1", Name: "fs/read", Arguments: []byte(`{"path":"/tmp/a"}`),
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
		ch <- llm.ToolCallEvent{ID: "tc2", Name: "fs/stat", Arguments: []byte(`{"path":"/tmp/b"}`)}
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
		Results:  []nodeToolResult{{Node: "-", Output: `[{"name":"sys/processes"}]`, Success: true}},
	})
	model = next.(Model)

	next, _ = model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "call1",
			Name:      metaToolCallTool,
			Arguments: json.RawMessage(`{"node":"node-01","tool":"sys/processes","arguments":{}}`),
		},
	})
	model = next.(Model)

	next, _ = model.Update(multiToolResultMsg{
		streamID: 1,
		Call:     llm.ToolCall{ID: "call1", Name: metaToolCallTool, Arguments: json.RawMessage(`{"node":"node-01","tool":"sys/processes","arguments":{}}`)},
		Results:  []nodeToolResult{{Node: "node-01", Output: "postgres 86%", Success: true}},
	})
	model = next.(Model)

	view = model.View()
	for _, leaked := range []string{"tool_search", "call_tool", "sys/processes", "postgres 86%"} {
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`),
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
	if model.messages[1].role != "tool" || model.messages[1].toolName != "shell/run" {
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
	if msgs[1].ToolCallID != "tc1" || msgs[1].ToolName != "shell/run" {
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
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "file1\nfile2", Success: true},
	}
	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("view missing tool name:\n%s", view)
	}
}

func TestLateRiskAssessmentAfterInterruptIsIgnored(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell/run", toolInput: `{"command":"rm -rf /"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`)}
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

	cmd := model.assessToolRisk(1, llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`)})
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

	cmd := model.dispatchTool(1, llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)})
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
	model.messages = append(model.messages, chatMsg{role: "tool", toolName: "shell/run", toolInput: `{"command":"uptime"}`})
	model.streaming = true
	model.status = "Thinking..."
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCancel = func() {}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	call := llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}
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

func TestDebugLogStreamEventRedactsNodeAddPassword(t *testing.T) {
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
	if strings.Contains(logText, "secret") {
		t.Fatalf("debug log should not contain raw password: %s", logText)
	}
	if !strings.Contains(logText, "[REDACTED]") {
		t.Fatalf("debug log should contain redacted password marker: %s", logText)
	}
	if !strings.Contains(logText, "10.0.0.5") {
		t.Fatalf("debug log should preserve host: %s", logText)
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

	if model.mode != modeChat {
		t.Fatal("should stay in chat mode with no nodes")
	}
	if !strings.Contains(model.status, "No nodes") {
		t.Fatalf("status = %q, want no nodes message", model.status)
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

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}
	results := []nodeToolResult{
		{Node: "node-01", Output: "load average: 0.52", Success: true},
		{Node: "node-02", Output: "load average: 0.31", Success: true},
	}

	next, _ := model.Update(multiToolResultMsg{Call: call, Results: results})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "shell/run on 2 node(s)") {
		t.Fatalf("view missing multi-node header:\n%s", view)
	}
	if !strings.Contains(view, "node-01") || !strings.Contains(view, "node-02") {
		t.Fatalf("view missing node names:\n%s", view)
	}
}

func TestToolOutputCollapsesAndTogglesLastToolWithCtrlO(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	firstCall := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"seq 1 6"}`)}
	firstOutput := "first 1\nfirst 2\nfirst 3\nfirst 4\nfirst 5\nfirst 6"

	next, _ := model.Update(multiToolResultMsg{Call: firstCall, Results: []nodeToolResult{{Node: "node-01", Output: firstOutput, Success: true}}})
	model = next.(Model)

	secondCall := llm.ToolCall{ID: "c2", Name: "shell/run", Arguments: []byte(`{"command":"seq 1 6"}`)}
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
			toolName:   "shell/run",
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
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"pwd"}`)}
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

	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}
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
		if params.Name != "shell/run" {
			t.Fatalf("tool name = %q, want shell/run", params.Name)
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
	call := llm.ToolCall{ID: "c1", Name: "shell/run", Arguments: []byte(`{"command":"ls"}`)}

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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"rm -rf /"}`),
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
		{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)},
		{ID: "tc2", Name: "shell/run", Arguments: []byte(`{"command":"date"}`)},
	} {
		result, _ := model.Update(streamEventMsg{streamID: 1, Event: call})
		model = result.(Model)
	}
	result, _ := model.Update(streamDoneMsg{streamID: 1})
	model = result.(Model)

	result, cmd := model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"uptime"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "up", Success: true}}})
	model = result.(Model)
	if cmd != nil {
		t.Fatal("first tool result resumed stream before second result")
	}
	if provider.calls != 0 {
		t.Fatalf("ChatStream calls = %d, want none before all tool results", provider.calls)
	}

	result, cmd = model.Update(multiToolResultMsg{streamID: 1, Call: llm.ToolCall{ID: "tc2", Name: "shell/run", Arguments: []byte(`{"command":"date"}`)}, Results: []nodeToolResult{{Node: "node-01", Output: "today", Success: true}}})
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
		{role: "tool", toolCallID: "tc1", toolName: "shell/run", toolInput: string(args)},
		{role: "tool", toolCallID: "tc2", toolName: "shell/run", toolInput: string(args)},
	}

	model.fillToolPlaceholder(llm.ToolCall{ID: "tc1", Name: "shell/run", Arguments: args}, "first", nil)

	if model.messages[0].toolOutput != "first" {
		t.Fatalf("first output = %q, want first", model.messages[0].toolOutput)
	}
	if model.messages[1].toolOutput != "" {
		t.Fatalf("second output = %q, want empty", model.messages[1].toolOutput)
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
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
	if !strings.Contains(view, "shell/run") {
		t.Fatalf("confirm mode should keep the tool placeholder visible:\n%s", view)
	}
	if !strings.Contains(view, "Command") || !strings.Contains(view, "systemctl restart nginx") {
		t.Fatalf("confirm mode should show the command being reviewed:\n%s", view)
	}
	if strings.Contains(view, "╭") || strings.Contains(view, "╰") {
		t.Fatalf("confirm mode should render inline at the bottom, not as a separate panel:\n%s", view)
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
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
		ID: "tc1", Name: "local/fs/write", Arguments: []byte(`{"path":"README.md","content":"new content"}`),
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

	assessment, err := reviewer.Review(context.Background(), "local/fs/write", `{"path":"README.md","content":"again"}`, nil)
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
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
		ID: "tc1", Name: "shell/run", Arguments: []byte(`{"command":"systemctl restart nginx"}`),
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
