package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

type fakeProvider struct {
	calls int
	reqs  []*llm.ChatRequest
}

func (p *fakeProvider) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls++
	p.reqs = append(p.reqs, req)
	if p.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{
				ID:        "tool-1",
				Name:      "tool_search",
				Arguments: json.RawMessage(`{"query":"cpu"}`),
			}},
			StopReason: llm.StopToolUse,
		}, nil
	}
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: "CPU is normal."},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *fakeProvider) ChatStream(context.Context, *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	panic("not used")
}

type fakeExecutor struct {
	calls []llm.ToolCall
}

func (e *fakeExecutor) ExecuteSubagentTool(_ context.Context, call llm.ToolCall) (string, bool) {
	e.calls = append(e.calls, call)
	return "sys_cpu", true
}

func TestRunnerLoopsThroughReadOnlyToolCalls(t *testing.T) {
	provider := &fakeProvider{}
	executor := &fakeExecutor{}
	runner := Runner{
		Provider: provider,
		Executor: executor,
		Tools: []llm.ToolDef{{
			Name:        "tool_search",
			Description: "search tools",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}

	result := runner.Run(context.Background(), Request{
		Role:    RoleInvestigator,
		Task:    "check cpu",
		Cluster: "production",
		Nodes:   []string{"node-01"},
	})

	if result.Err != nil {
		t.Fatalf("Run error: %v", result.Err)
	}
	if result.Summary != "CPU is normal." {
		t.Fatalf("summary = %q", result.Summary)
	}
	if len(executor.calls) != 1 || executor.calls[0].Name != "tool_search" {
		t.Fatalf("executor calls = %#v", executor.calls)
	}
	if len(provider.reqs) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.reqs))
	}
	if !strings.Contains(provider.reqs[0].SystemPrompt, "Role: investigator") {
		t.Fatalf("prompt missing role:\n%s", provider.reqs[0].SystemPrompt)
	}
	if len(provider.reqs[1].Messages) < 3 {
		t.Fatalf("second request should include tool call/result messages: %#v", provider.reqs[1].Messages)
	}
}

func TestAllowedToolsUsesMetadataForReadOnlyFiltering(t *testing.T) {
	tools := []llm.ToolDef{
		{Name: "tool_search"},
		{Name: "call_tool"},
		{Name: "svc_status"},
		{Name: "log_read"},
		{Name: "memory_search"},
		{Name: "memory_read"},
		{Name: "file_put"},
		{Name: "node_add"},
		{Name: "memory_patch"},
		{Name: "exec"},
	}

	allowed := allowedTools(RoleInvestigator, tools)
	names := map[string]bool{}
	for _, tool := range allowed {
		names[tool.Name] = true
	}

	for _, want := range []string{"tool_search", "call_tool", "svc_status", "log_read", "memory_search", "memory_read"} {
		if !names[want] {
			t.Fatalf("allowed tools missing %s: %#v", want, allowed)
		}
	}
	for _, blocked := range []string{"file_put", "node_add", "memory_patch", "exec"} {
		if names[blocked] {
			t.Fatalf("%s should be blocked for investigator: %#v", blocked, allowed)
		}
	}
}

func TestParseTasksValidatesTask(t *testing.T) {
	_, err := ParseTasks(json.RawMessage(`{"tasks":[{"role":"reviewer","task":"  "}]} `))
	if err == nil {
		t.Fatal("expected empty task error")
	}
}
