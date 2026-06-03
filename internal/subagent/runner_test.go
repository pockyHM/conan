package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

func drainEvents(ch <-chan Event) []Event {
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestRunnerEmitsEventsAndResult(t *testing.T) {
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

	events, results := runner.Run(context.Background(), Request{
		Role:         RoleInvestigator,
		Task:         "check cpu",
		Cluster:      "production",
		Nodes:        []string{"node-01"},
		MaxTurns:     4,
		MaxToolCalls: 4,
	})

	evs := drainEvents(events)
	if len(evs) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %#v", len(evs), evs)
	}
	last := evs[len(evs)-1]
	if last.Kind != EventDone {
		t.Errorf("last event Kind = %v, want EventDone", last.Kind)
	}

	var res Result
	select {
	case res = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not deliver")
	}
	if res.Err != nil {
		t.Fatalf("result.Err = %v", res.Err)
	}
	if res.Summary != "CPU is normal." {
		t.Errorf("Summary = %q, want %q", res.Summary, "CPU is normal.")
	}
}

func TestRunnerRespectsMaxTurns(t *testing.T) {
	provider := &endlessToolCallProvider{}
	executor := &fakeExecutor{}
	runner := Runner{Provider: provider, Executor: executor}

	events, results := runner.Run(context.Background(), Request{
		Role:         RoleInvestigator,
		Task:         "loop",
		MaxTurns:     2,
		MaxToolCalls: 100,
	})

	_ = drainEvents(events)
	res := <-results
	if res.Err == nil || !strings.Contains(res.Err.Error(), "turn limit") {
		t.Errorf("expected turn limit error, got %v", res.Err)
	}
	if provider.calls != 2 {
		t.Errorf("provider calls = %d, want 2", provider.calls)
	}
}

func TestRunnerRespectsMaxToolCalls(t *testing.T) {
	provider := &endlessToolCallProvider{}
	executor := &fakeExecutor{}
	runner := Runner{Provider: provider, Executor: executor}

	events, results := runner.Run(context.Background(), Request{
		Role:         RoleInvestigator,
		Task:         "loop",
		MaxTurns:     100,
		MaxToolCalls: 3,
	})

	_ = drainEvents(events)
	res := <-results
	if res.Err == nil || !strings.Contains(res.Err.Error(), "tool call limit") {
		t.Errorf("expected tool call limit error, got %v", res.Err)
	}
}

func TestRunnerReturnsContextCanceledWhenCtxDone(t *testing.T) {
	provider := &blockingProvider{}
	executor := &fakeExecutor{}
	runner := Runner{Provider: provider, Executor: executor}

	ctx, cancel := context.WithCancel(context.Background())
	events, results := runner.Run(ctx, Request{
		Role:         RoleInvestigator,
		Task:         "loop",
		MaxTurns:     10,
		MaxToolCalls: 10,
	})

	cancel()
	_ = drainEvents(events)
	res := <-results
	if !errors.Is(res.Err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", res.Err)
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
			t.Errorf("allowed tools missing %s: %#v", want, allowed)
		}
	}
	for _, blocked := range []string{"file_put", "node_add", "memory_patch", "exec"} {
		if names[blocked] {
			t.Errorf("%s should be blocked for investigator: %#v", blocked, allowed)
		}
	}
}

func TestParseTasksValidatesTask(t *testing.T) {
	_, err := ParseTasks(json.RawMessage(`{"tasks":[{"role":"reviewer","task":"  "}]} `))
	if err == nil {
		t.Fatal("expected empty task error")
	}
}

type endlessToolCallProvider struct {
	calls int
}

func (p *endlessToolCallProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls++
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{
			ID:        "tool-1",
			Name:      "tool_search",
			Arguments: json.RawMessage(`{"query":"x"}`),
		}},
		StopReason: llm.StopToolUse,
	}, nil
}

func (p *endlessToolCallProvider) ChatStream(context.Context, *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	panic("not used")
}

type blockingProvider struct{}

func (p *blockingProvider) Chat(ctx context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *blockingProvider) ChatStream(context.Context, *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	panic("not used")
}
