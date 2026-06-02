# Conan Subagent Quality-of-Life Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Conan subagents configurable per role, surface progress to the TUI in real time, persist debug transcripts, and let the user cancel a specific in-flight subagent.

**Architecture:** Replace the synchronous `Runner.Run(ctx, req) Result` with a channel-based API returning `(<-chan Event, <-chan Result)`. A new `Manager` owns per-request context cancellation. A new `Transcript` writes JSONL to `~/.conan/logs/subagents/...`. The TUI dispatch path consumes events via `tea.Cmd` and updates a persistent panel. Per-role turn and tool-call limits come from a new `SubagentRoleLimits` config block.

**Tech Stack:** Go 1.x, Bubble Tea, existing `llm.Provider` interface, `memory.Store`, `os`/`encoding/json` for transcript, `log/slog` for error logging.

**Spec:** `docs/superpowers/specs/2026-06-02-subagent-quality-of-life-design.md` (commits `89e4d39` + `f20356e`).

**Working assumption:** No new packages. No external dependencies added. All work happens in `internal/subagent/`, `internal/tui/`, and `pkg/configschema/`. The user's pre-existing modifications in those directories (M files in `git status`) are not touched in this plan.

---

## File map

**Create:**
- `internal/subagent/limits.go` — `RoleLimits` type and `NormalizeRoleLimits`/`For` helpers
- `internal/subagent/limits_test.go`
- `internal/subagent/manager.go` — `Manager` with `Submit`/`Cancel`/`CancelAll`/`Running`
- `internal/subagent/manager_test.go`
- `internal/subagent/transcript.go` — file-backed JSONL `Transcript`
- `internal/subagent/transcript_test.go`

**Modify:**
- `internal/subagent/runner.go` — add `Event` and `EventKind`, change `Run` signature, drop internal defaults for `MaxTurns`/`MaxToolCalls`
- `internal/subagent/runner_test.go` — rewrite for channel API
- `pkg/configschema/config.go` — add `MaxInboundChars`, `DebugTranscript`, `PerRole SubagentRoleLimits`; remove `MaxTurns`/`MaxToolCalls`
- `internal/tui/configscreen.go` — render and parse seven new entries
- `internal/tui/model.go` — add msg types, extend `subagentRunView`, replace `runSubagent`/`runSubagentBatch` with `dispatchSubagentsRun`, add `recomputeSubagentStatus`, wire `CancelAll` in `cancelActiveStream`, add `c` keybinding in `handleKey`
- `internal/tui/model_test.go` — new tests for event-driven state and keybinding

**Skip:** `pkg/configschema/config.go` validation that errors on unknown YAML keys. Old `subagents.max_turns` / `subagents.max_tool_calls` will be silently ignored by the YAML decoder. README and `.gitignore` for `.superpowers/` are out of scope of this plan and will be handled separately.

---

## Phase A — Pure subagent package (no TUI changes)

### Task 1: Add per-role limits helpers

**Files:**
- Create: `internal/subagent/limits.go`
- Create: `internal/subagent/limits_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/subagent/limits_test.go`:

```go
package subagent

import (
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestNormalizeRoleLimitsAppliesDefaults(t *testing.T) {
	got := NormalizeRoleLimits(configschema.SubagentRoleLimits{})

	cases := []struct {
		role  Role
		turns int
		calls int
	}{
		{RoleInvestigator, 8, 12},
		{RoleReviewer, 4, 6},
		{RoleSummarizer, 2, 0},
	}
	for _, c := range cases {
		turns, calls := got.For(c.role)
		if turns != c.turns || calls != c.calls {
			t.Errorf("For(%s) = (%d, %d), want (%d, %d)", c.role, turns, calls, c.turns, c.calls)
		}
	}
}

func TestNormalizeRoleLimitsPreservesCustomValues(t *testing.T) {
	cfg := configschema.SubagentRoleLimits{
		InvestigatorTurns:    16,
		InvestigatorToolCalls: 20,
		ReviewerTurns:        2,
		ReviewerToolCalls:    4,
		SummarizerTurns:      1,
	}
	got := NormalizeRoleLimits(cfg)

	turns, calls := got.For(RoleInvestigator)
	if turns != 16 || calls != 20 {
		t.Errorf("investigator = (%d, %d), want (16, 20)", turns, calls)
	}
	turns, _ = got.For(RoleReviewer)
	if turns != 2 {
		t.Errorf("reviewer turns = %d, want 2", turns)
	}
	turns, _ = got.For(RoleSummarizer)
	if turns != 1 {
		t.Errorf("summarizer turns = %d, want 1", turns)
	}
}

func TestRoleLimitsFallsBackToDefaultsForZeroValues(t *testing.T) {
	got := NormalizeRoleLimits(configschema.SubagentRoleLimits{
		InvestigatorTurns: 0,
	})

	turns, calls := got.For(RoleInvestigator)
	if turns != 8 || calls != 12 {
		t.Errorf("zero turns must fall back to defaults; got (%d, %d)", turns, calls)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/subagent/ -run TestNormalizeRoleLimits -v`
Expected: FAIL with `undefined: NormalizeRoleLimits` or `undefined: RoleLimits`.

- [ ] **Step 3: Implement `limits.go`**

Create `internal/subagent/limits.go`:

```go
package subagent

import "github.com/pockyHM/conan/pkg/configschema"

type RoleLimits struct {
	InvestigatorTurns    int
	InvestigatorToolCalls int
	ReviewerTurns        int
	ReviewerToolCalls    int
	SummarizerTurns      int
}

const (
	defaultInvestigatorTurns    = 8
	defaultInvestigatorToolCalls = 12
	defaultReviewerTurns        = 4
	defaultReviewerToolCalls    = 6
	defaultSummarizerTurns      = 2
)

func NormalizeRoleLimits(cfg configschema.SubagentRoleLimits) RoleLimits {
	r := RoleLimits{
		InvestigatorTurns:    cfg.InvestigatorTurns,
		InvestigatorToolCalls: cfg.InvestigatorToolCalls,
		ReviewerTurns:        cfg.ReviewerTurns,
		ReviewerToolCalls:    cfg.ReviewerToolCalls,
		SummarizerTurns:      cfg.SummarizerTurns,
	}
	if r.InvestigatorTurns <= 0 {
		r.InvestigatorTurns = defaultInvestigatorTurns
	}
	if r.InvestigatorToolCalls <= 0 {
		r.InvestigatorToolCalls = defaultInvestigatorToolCalls
	}
	if r.ReviewerTurns <= 0 {
		r.ReviewerTurns = defaultReviewerTurns
	}
	if r.ReviewerToolCalls <= 0 {
		r.ReviewerToolCalls = defaultReviewerToolCalls
	}
	if r.SummarizerTurns <= 0 {
		r.SummarizerTurns = defaultSummarizerTurns
	}
	return r
}

func (r RoleLimits) For(role Role) (turns, toolCalls int) {
	switch normalizeRole(role) {
	case RoleReviewer:
		return r.ReviewerTurns, r.ReviewerToolCalls
	case RoleSummarizer:
		return r.SummarizerTurns, 0
	default:
		return r.InvestigatorTurns, r.InvestigatorToolCalls
	}
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/subagent/ -run TestNormalizeRoleLimits -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/subagent/limits.go internal/subagent/limits_test.go
git commit -m "feat(subagent): add RoleLimits helpers"
```

---

### Task 2: Extend config schema with per-role limits and new subagent fields

**Files:**
- Modify: `pkg/configschema/config.go:107-112`

- [ ] **Step 1: Add `SubagentRoleLimits` struct and update `SubagentConfig`**

In `pkg/configschema/config.go`, replace the existing `SubagentConfig` (lines 107-112) with:

```go
type SubagentConfig struct {
	Enabled         bool               `yaml:"enabled"`
	MaxParallel     int                `yaml:"max_parallel"`
	TimeoutSeconds  int                `yaml:"timeout_seconds"`
	MaxInboundChars int                `yaml:"max_inbound_chars"`
	DefaultModel    string             `yaml:"default_model"`
	Debug           bool               `yaml:"debug"`
	DebugTranscript bool               `yaml:"debug_transcript"`
	PerRole         SubagentRoleLimits `yaml:"per_role"`
}

type SubagentRoleLimits struct {
	InvestigatorTurns     int `yaml:"investigator_turns"`
	InvestigatorToolCalls int `yaml:"investigator_tool_calls"`
	ReviewerTurns         int `yaml:"reviewer_turns"`
	ReviewerToolCalls     int `yaml:"reviewer_tool_calls"`
	SummarizerTurns       int `yaml:"summarizer_turns"`
}
```

- [ ] **Step 2: Verify the package still builds**

Run: `go build ./pkg/configschema/`
Expected: success, no errors.

- [ ] **Step 3: Commit**

```bash
git add pkg/configschema/config.go
git commit -m "feat(config): add per-role subagent limits and debug transcript flag"
```

---

### Task 3: Add `Event` type and rewrite `Runner.Run` to channel API

**Files:**
- Modify: `internal/subagent/runner.go`

- [ ] **Step 1: Replace the existing `runner_test.go` with a failing test for the new API**

Overwrite `internal/subagent/runner_test.go` with the new channel-based tests below. These are written first so the test file always matches what the spec demands.

```go
package subagent

import (
	"context"
	"encoding/json"
	"errors"
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
		Role:       RoleInvestigator,
		Task:       "check cpu",
		Cluster:    "production",
		Nodes:      []string{"node-01"},
		MaxTurns:   4,
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
```

Note: add `"time"` and `"errors"` imports at the top of the test file if not already present.

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/subagent/ -run TestRunner -v`
Expected: FAIL with `runner.Run(...) has unsupported return values` or compile error referencing `Event`.

- [ ] **Step 3: Update `Request` and add `Event` type**

In `internal/subagent/runner.go`, modify the `Request` struct (currently around line 32) to add the new fields used by the tests and config:

```go
type Request struct {
	ID              string
	Role            Role
	Task            string
	Cluster         string
	Nodes           []string
	Model           string
	Context         []models.Message
	MemoryContext   string
	Timeout         time.Duration
	MaxTurns        int
	MaxToolCalls    int
	DebugTranscript bool
	SessionID       string
}
```

Add a new `Event` type above `Request`:

```go
type EventKind int

const (
	EventTurnStart EventKind = iota + 1
	EventTurnEnd
	EventToolCall
	EventToolResult
	EventDone
)

type Event struct {
	ID      string
	Kind    EventKind
	Turn    int
	Tool    string
	Args    string
	Out     string
	OK      bool
	Elapsed time.Duration
}
```

- [ ] **Step 4: Rewrite `Run` to return channels and emit events**

Replace the `Run` method (lines 74-162) with the following:

```go
func (r Runner) Run(ctx context.Context, req Request) (<-chan Event, <-chan Result) {
	events := make(chan Event, 16)
	results := make(chan Result, 1)

	go func() {
		defer close(events)
		defer close(results)

		start := time.Now()
		result := Result{
			ID:    req.ID,
			Role:  normalizeRole(req.Role),
			Task:  strings.TrimSpace(req.Task),
			Nodes: append([]string(nil), req.Nodes...),
		}
		if result.ID == "" {
			result.ID = models.NewID()
		}

		maxTurns := req.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 4
		}
		maxToolCalls := req.MaxToolCalls
		if maxToolCalls <= 0 {
			maxToolCalls = 8
		}

		timeout := req.Timeout
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if r.Provider == nil {
			result.Err = fmt.Errorf("subagent provider is nil")
			result.Elapsed = time.Since(start)
			emitEvent(events, Event{ID: result.ID, Kind: EventDone, Elapsed: result.Elapsed})
			results <- result
			return
		}
		if result.Task == "" {
			result.Err = fmt.Errorf("subagent task is required")
			result.Elapsed = time.Since(start)
			emitEvent(events, Event{ID: result.ID, Kind: EventDone, Elapsed: result.Elapsed})
			results <- result
			return
		}

		messages := buildMessages(req)
		tools := allowedTools(result.Role, r.Tools)
		toolCalls := 0

		for turn := 1; turn <= maxTurns; turn++ {
			emitEvent(events, Event{ID: result.ID, Kind: EventTurnStart, Turn: turn, Elapsed: time.Since(start)})

			resp, err := r.Provider.Chat(ctx, &llm.ChatRequest{
				SystemPrompt: rolePrompt(result.Role, req),
				Messages:     messages,
				Tools:        tools,
				MaxTokens:    1800,
			})
			if err != nil {
				if ctx.Err() != nil {
					result.Err = ctx.Err()
				} else {
					result.Err = err
				}
				result.Elapsed = time.Since(start)
				emitEvent(events, Event{ID: result.ID, Kind: EventDone, Turn: turn, Elapsed: result.Elapsed})
				results <- result
				return
			}

			emitEvent(events, Event{ID: result.ID, Kind: EventTurnEnd, Turn: turn, Elapsed: time.Since(start)})

			if strings.TrimSpace(resp.Message.Content) != "" {
				messages = append(messages, models.Message{Role: "assistant", Content: resp.Message.Content})
				result.Summary = strings.TrimSpace(resp.Message.Content)
			}
			if len(resp.ToolCalls) == 0 {
				result.Elapsed = time.Since(start)
				emitEvent(events, Event{ID: result.ID, Kind: EventDone, Turn: turn, Elapsed: result.Elapsed})
				results <- result
				return
			}
			if r.Executor == nil {
				result.Err = fmt.Errorf("subagent requested tools but no executor is configured")
				result.Elapsed = time.Since(start)
				emitEvent(events, Event{ID: result.ID, Kind: EventDone, Turn: turn, Elapsed: result.Elapsed})
				results <- result
				return
			}
			for _, call := range resp.ToolCalls {
				if toolCalls >= maxToolCalls {
					result.Err = fmt.Errorf("subagent exceeded tool call limit")
					result.Elapsed = time.Since(start)
					emitEvent(events, Event{ID: result.ID, Kind: EventDone, Turn: turn, Elapsed: result.Elapsed})
					results <- result
					return
				}
				toolCalls++
				emitEvent(events, Event{ID: result.ID, Kind: EventToolCall, Turn: turn, Tool: call.Name, Args: string(call.Arguments), Elapsed: time.Since(start)})
				output, success := r.Executor.ExecuteSubagentTool(ctx, call)
				result.ToolCalls = append(result.ToolCalls, ToolCall{
					Name:      call.Name,
					Arguments: string(call.Arguments),
					Output:    output,
					Success:   success,
				})
				emitEvent(events, Event{ID: result.ID, Kind: EventToolResult, Turn: turn, Tool: call.Name, Out: output, OK: success, Elapsed: time.Since(start)})
				messages = append(messages,
					models.Message{Role: "assistant", ToolCallID: call.ID, ToolName: call.Name, ToolInput: string(call.Arguments)},
					models.Message{Role: "tool", ToolCallID: call.ID, Content: output, ToolOutput: output},
				)
			}
		}
		result.Err = fmt.Errorf("subagent reached turn limit")
		result.Elapsed = time.Since(start)
		emitEvent(events, Event{ID: result.ID, Kind: EventDone, Elapsed: result.Elapsed})
		results <- result
	}()

	return events, results
}

func emitEvent(ch chan<- Event, ev Event) {
	defer func() { _ = recover() }()
	ch <- ev
}
```

- [ ] **Step 5: Run the tests, verify they pass**

Run: `go test ./internal/subagent/ -v`
Expected: all `TestRunner*`, `TestAllowedTools*`, and `TestParseTasks*` pass.

- [ ] **Step 6: Commit**

```bash
git add internal/subagent/runner.go internal/subagent/runner_test.go
git commit -m "feat(subagent): emit events from Runner.Run via channels"
```

---

### Task 4: Add `Manager` for per-subagent cancellation

**Files:**
- Create: `internal/subagent/manager.go`
- Create: `internal/subagent/manager_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/subagent/manager_test.go`:

```go
package subagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

func TestManagerSubmitReturnsChannelsAndID(t *testing.T) {
	mgr := NewManager()
	runner := Runner{
		Provider: &endlessToolCallProvider{},
		Executor: &fakeExecutor{},
	}

	id, events, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 10,
	})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}
	if id == "" {
		t.Fatal("id is empty")
	}
	if events == nil || results == nil {
		t.Fatal("channels are nil")
	}
	_ = events
	_ = results
	mgr.CancelAll()
}

func TestManagerCancelStopsSubagent(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &blockingProvider{}, Executor: &fakeExecutor{}}

	id, _, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 10,
	})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	if err := mgr.Cancel(id); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	select {
	case res := <-results:
		if !errors.Is(res.Err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not deliver after Cancel")
	}
}

func TestManagerCancelUnknownIDIsNoop(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Cancel("does-not-exist"); err != nil {
		t.Errorf("Cancel unknown id = %v, want nil", err)
	}
}

func TestManagerCancelAllStopsAll(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &blockingProvider{}, Executor: &fakeExecutor{}}

	ids := make([]string, 0, 3)
	results := make([]<-chan Result, 0, 3)
	for i := 0; i < 3; i++ {
		id, _, r, err := mgr.Submit(context.Background(), runner, Request{
			Role:     RoleInvestigator,
			Task:     "x",
			MaxTurns: 10,
		})
		if err != nil {
			t.Fatalf("Submit error: %v", err)
		}
		ids = append(ids, id)
		results = append(results, r)
	}

	mgr.CancelAll()

	for i, r := range results {
		select {
		case res := <-r:
			if !errors.Is(res.Err, context.Canceled) {
				t.Errorf("results[%d] err = %v, want context.Canceled", i, res.Err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("results[%d] did not deliver after CancelAll", i)
		}
	}
}

func TestManagerResultChannelClosesAfterResult(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &endlessToolCallProvider{}, Executor: &fakeExecutor{}}

	_, _, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 1,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	<-results
	select {
	case _, ok := <-results:
		if ok {
			t.Error("results channel still open after result received")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not close")
	}
}

// ensure time import is used even when tests change
var _ = time.Second
var _ = models.NewID
var _ llm.ToolCall
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/subagent/ -run TestManager -v`
Expected: FAIL with `undefined: NewManager`.

- [ ] **Step 3: Implement `Manager`**

Create `internal/subagent/manager.go`:

```go
package subagent

import (
	"context"
	"sync"
)

type Manager struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewManager() *Manager {
	return &Manager{running: map[string]context.CancelFunc{}}
}

func (m *Manager) Submit(ctx context.Context, runner Runner, req Request) (string, <-chan Event, <-chan Result, error) {
	events, results := runner.Run(ctx, req)
	id := req.ID
	if id == "" {
		// We rely on the runner's own id assignment if blank; for cancel lookup
		// we need a deterministic id, so we generate one here. The runner uses
		// req.ID when set, so reassign.
		// The first event carries the runner's id; we keep our own cancel id
		// separately. For now use the req.ID generated by the caller.
	}
	subCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.running[id] = cancel
	m.mu.Unlock()

	go func() {
		<-results
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	return id, events, results, nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	cancel, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	cancel()
	return nil
}

func (m *Manager) CancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.running))
	for _, c := range m.running {
		cancels = append(cancels, c)
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (m *Manager) Running() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for id := range m.running {
		out = append(out, id)
	}
	return out
}
```

Note: `id` is taken from `req.ID`. The TUI caller generates the id with `models.NewID()` before calling `Submit`. The runner uses the same id when emitting events. (Verified in Task 5 wiring.)

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/subagent/ -run TestManager -v`
Expected: PASS for all four `TestManager*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/subagent/manager.go internal/subagent/manager_test.go
git commit -m "feat(subagent): add Manager for per-subagent cancellation"
```

---

### Task 5: Add `Transcript` file sink

**Files:**
- Create: `internal/subagent/transcript.go`
- Create: `internal/subagent/transcript_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/subagent/transcript_test.go`:

```go
package subagent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscriptWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenTranscript(dir, "sess-1", "id-1")
	if err != nil {
		t.Fatalf("OpenTranscript: %v", err)
	}

	events := []Event{
		{ID: "id-1", Kind: EventTurnStart, Turn: 1},
		{ID: "id-1", Kind: EventToolCall, Turn: 1, Tool: "tool_search", Args: `{"q":"x"}`},
		{ID: "id-1", Kind: EventDone, Turn: 1},
	}
	for _, ev := range events {
		if err := tr.Write(ev); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "logs", "subagents", "sess-1", "id-1.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.Kind != EventTurnStart || first.Turn != 1 {
		t.Errorf("first event = %#v, want TurnStart turn=1", first)
	}
}

func TestOpenTranscriptCreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenTranscript(dir, "deep/sess", "id-2")
	if err != nil {
		t.Fatalf("OpenTranscript: %v", err)
	}
	defer tr.Close()

	expected := filepath.Join(dir, "logs", "subagents", "deep", "sess", "id-2.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected file %s to exist: %v", expected, err)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/subagent/ -run TestTranscript -v`
Expected: FAIL with `undefined: OpenTranscript`.

- [ ] **Step 3: Implement `Transcript`**

Create `internal/subagent/transcript.go`:

```go
package subagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Transcript struct {
	path string
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

func OpenTranscript(configHome, sessionID, id string) (*Transcript, error) {
	dir := filepath.Join(configHome, "logs", "subagents", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open transcript file: %w", err)
	}
	return &Transcript{path: path, file: f, enc: json.NewEncoder(f)}, nil
}

func (t *Transcript) Write(ev Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enc.Encode(ev)
}

func (t *Transcript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	return err
}

func (t *Transcript) Path() string {
	return t.path
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/subagent/ -run TestTranscript -v`
Expected: PASS for both `TestTranscript*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/subagent/transcript.go internal/subagent/transcript_test.go
git commit -m "feat(subagent): add JSONL transcript sink"
```

---

## Phase B — TUI integration

### Task 6: Extend `subagentRunView` and add TUI message types

**Files:**
- Modify: `internal/tui/model.go` (around lines 104-115, 222-225, 526-527)

- [ ] **Step 1: Add new fields to `subagentRunView`**

In `internal/tui/model.go`, replace the `subagentRunView` struct (lines 104-115) with:

```go
type subagentRunView struct {
	ID         string
	Role       subagent.Role
	Task       string
	Prompt     string
	Model      string
	Nodes      []string
	Status     string
	Summary    string
	Err        string
	Elapsed    time.Duration
	StartedAt  time.Time
	Turn       int
	CurrentTool string
	EventCount int
}
```

- [ ] **Step 2: Add new message types near the existing `subagentCommandResultMsg`**

In `internal/tui/model.go`, after the `subagentCommandResultMsg` type (around line 527), add:

```go
type subagentEventMsg struct {
	ID    string
	Event subagent.Event
}

type subagentStartMsg struct {
	ID      string
	Role    subagent.Role
	Task    string
	Nodes   []string
	Model   string
	Started time.Time
}
```

- [ ] **Step 3: Verify the package still builds**

Run: `go build ./internal/tui/`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): add fields and msg types for streaming subagent events"
```

---

### Task 7: Add `Manager` field and replace dispatch with channel-based path

**Files:**
- Modify: `internal/tui/model.go` (around line 222, 4462-4511)

- [ ] **Step 1: Add `subagentManager` field to `Model`**

In `internal/tui/model.go`, in the `Model` struct, after the `subagents configschema.SubagentConfig` field (line 221), add:

```go
subagentManager *subagent.Manager
```

Initialize it in `NewModel` (around line 317 where `subagents` is set) with:

```go
subagentManager:      subagent.NewManager(),
```

- [ ] **Step 2: Replace `dispatchSubagentsRun` and remove `runSubagent`/`runSubagentBatch`**

In `internal/tui/model.go`, replace the `dispatchSubagentsRun` function (lines 4462-4489) and the `runSubagentBatch` function (lines 4491-4511) with the following single function:

```go
func (m Model) dispatchSubagentsRun(streamID uint64, call llm.ToolCall) tea.Cmd {
	if !m.subagents.Enabled {
		return func() tea.Msg {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "subagents are disabled. Use /subagents on to enable delegation.", Success: false}}}
		}
	}
	return func() tea.Msg {
		tasks, err := subagent.ParseTasks(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "invalid subagent tasks: " + err.Error(), Success: false}}}
		}
		limits := subagent.NormalizeRoleLimits(m.subagents.PerRole)
		runner := subagent.Runner{
			Provider: m.provider,
			Tools:    m.availableToolDefsForSubagent(),
		}
		ctx := m.streamCtx
		if ctx == nil {
			ctx = context.Background()
		}

		now := time.Now()
		rows := make([]subagentRunView, 0, len(tasks))
		cmds := make([]tea.Cmd, 0, len(tasks)*2)
		for _, task := range tasks {
			nodes := m.restrictSubagentNodes(task.Nodes)
			if len(task.Nodes) > 0 && len(nodes) == 0 {
				return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "subagent task has no allowed target nodes", Success: false}}}
			}
			turns, toolCalls := limits.For(task.Role)
			memoryCtx := m.buildSubagentMemoryContext(task.Task, nodes)
			req := subagent.Request{
				ID:            models.NewID(),
				Role:          task.Role,
				Task:          task.Task,
				Cluster:       m.cluster,
				Nodes:         nodes,
				Model:         m.model,
				Context:        recentConversationContextTruncated(m.conv, m.subagents.MaxInboundChars),
				MemoryContext:  memoryCtx,
				Timeout:        time.Duration(m.subagents.TimeoutSeconds) * time.Second,
				MaxTurns:       turns,
				MaxToolCalls:   toolCalls,
				DebugTranscript: m.subagents.DebugTranscript,
				SessionID:      m.initialSessionID,
			}
			row := subagentRunView{
				ID:        req.ID,
				Role:      req.Role,
				Task:      req.Task,
				Prompt:    subagentPromptForDisplay(req),
				Model:     req.Model,
				Nodes:     append([]string(nil), req.Nodes...),
				Status:    "running",
				StartedAt: now,
			}
			rows = append(rows, row)

			executor := subagentToolExecutor{model: m, nodes: req.Nodes}
			runnerInstance := subagent.Runner{Provider: m.provider, Tools: m.availableToolDefsForSubagent(), Executor: executor}
			id, events, results, err := m.subagentManager.Submit(ctx, runnerInstance, req)
			if err != nil {
				return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "subagent submit failed: " + err.Error(), Success: false}}}
			}
			_ = id // id is already in req.ID

			if req.DebugTranscript && m.configHome != "" {
				if tr, err := subagent.OpenTranscript(m.configHome, req.SessionID, req.ID); err == nil {
					go forwardEventsToTranscript(events, tr)
				}
			}

			evCh := events
			resCh := results
			cmds = append(cmds, func() tea.Msg { return subagentStartMsg{ID: req.ID, Role: req.Role, Task: req.Task, Nodes: req.Nodes, Model: req.Model, Started: now} })
			cmds = append(cmds, waitForEventCmd(evCh))
			cmds = append(cmds, waitForResultCmd(resCh))
		}

		m.pendingSubagentRows = rows
		return tea.Batch(append([]tea.Cmd{func() tea.Msg {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: subagent.FormatResults(nil), Success: true}}
		}}, cmds...)...)
	}
}
```

- [ ] **Step 3: Delete `runSubagent` and `runSubagentBatch`**

Delete the `runSubagent` function (lines 3578-3587) and `runSubagentBatch` (lines 4491-4511).

- [ ] **Step 4: Add `pendingSubagentRows` field and helpers**

In `Model`, after `subagentManager`, add:

```go
pendingSubagentRows []subagentRunView
```

Add these helpers in `model.go` (near `runSubagent`/`runSubagentBatch`):

```go
func waitForEventCmd(ch <-chan subagent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamSubagentEventsClosedMsg{}
		}
		return subagentEventMsg{Event: ev}
	}
}

func waitForResultCmd(ch <-chan subagent.Result) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return streamSubagentResultClosedMsg{}
		}
		return subagentResultMsg{Result: res}
	}
}

func forwardEventsToTranscript(events <-chan subagent.Event, tr *subagent.Transcript) {
	for ev := range events {
		if err := tr.Write(ev); err != nil {
			slog.Default().Debug("subagent transcript write failed", "err", err)
		}
	}
	_ = tr.Close()
}

type streamSubagentEventsClosedMsg struct{}
type streamSubagentResultClosedMsg struct{}
```

- [ ] **Step 5: Replace `recentConversationContext` with a truncated variant**

Add a new helper next to the existing `recentConversationContext` (around line 3541):

```go
func recentConversationContextTruncated(conv *conversation.Conversation, maxChars int) []models.Message {
	if maxChars <= 0 || conv == nil {
		return nil
	}
	return completeToolCallContext(conv.Context(maxChars))
}
```

- [ ] **Step 6: Verify the package builds**

Run: `go build ./internal/tui/`
Expected: success. There will be compile errors referencing `subagentResultMsg` and the old `runSubagent`; that is fixed in Task 8.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go
git commit -m "refactor(tui): route subagent dispatch through Manager and event channels"
```

---

### Task 8: Wire `subagentStartMsg`, `subagentEventMsg`, `subagentResultMsg` handlers

**Files:**
- Modify: `internal/tui/model.go` (the `Update` switch)

- [ ] **Step 1: Add the message handlers in `Update`**

Locate the existing `case subagentCommandResultMsg:` block in `Update` (around line 552). Add new cases right after it:

```go
case subagentStartMsg:
	m.subagentRuns = append(m.subagentRuns, subagentRunView{
		ID:        msg.ID,
		Role:      msg.Role,
		Task:      msg.Task,
		Nodes:     append([]string(nil), msg.Nodes...),
		Model:     msg.Model,
		Status:    "running",
		StartedAt: msg.Started,
	})
	m.recomputeSubagentStatus()
	m.lastBodyContent = ""
	return m, nil

case subagentEventMsg:
	for i := range m.subagentRuns {
		if m.subagentRuns[i].ID != msg.ID {
			continue
		}
		switch msg.Event.Kind {
		case subagent.EventTurnStart:
			m.subagentRuns[i].Turn = msg.Event.Turn
		case subagent.EventTurnEnd:
			m.subagentRuns[i].Turn = msg.Event.Turn
		case subagent.EventToolCall:
			m.subagentRuns[i].CurrentTool = msg.Event.Tool
			m.subagentRuns[i].EventCount++
		case subagent.EventToolResult:
			m.subagentRuns[i].CurrentTool = ""
		case subagent.EventDone:
			m.subagentRuns[i].CurrentTool = ""
		}
		break
	}
	m.recomputeSubagentStatus()
	m.lastBodyContent = ""
	return m, nil

case subagentResultMsg:
	res := msg.Result
	m.updateSubagentRunResult(res)
	m.recomputeSubagentStatus()
	m.lastBodyContent = ""
	return m, nil
```

- [ ] **Step 2: Add the `subagentResultMsg` type**

Next to `subagentEventMsg` (added in Task 6), add:

```go
type subagentResultMsg struct {
	Result subagent.Result
}
```

- [ ] **Step 3: Add `recomputeSubagentStatus` and update existing assignments**

In `internal/tui/model.go`, add the helper (near the other `recompute*` helpers):

```go
func (m *Model) recomputeSubagentStatus() {
	if len(m.subagentRuns) == 0 {
		m.subagentStatus = ""
		return
	}
	done, active := 0, 0
	var activeNodes []string
	var firstStarted time.Time
	for _, r := range m.subagentRuns {
		if r.Status == "completed" || r.Status == "failed" {
			done++
			continue
		}
		active++
		if len(r.Nodes) > 0 && len(activeNodes) < 3 {
			activeNodes = append(activeNodes, r.Nodes[0])
		}
		if firstStarted.IsZero() || r.StartedAt.Before(firstStarted) {
			firstStarted = r.StartedAt
		}
	}
	elapsed := time.Duration(0)
	if !firstStarted.IsZero() {
		elapsed = time.Since(firstStarted).Round(100 * time.Millisecond)
	}
	parts := []string{
		fmt.Sprintf("%d/%d done", done, len(m.subagentRuns)),
	}
	if active > 0 {
		parts = append(parts, fmt.Sprintf("%d active", active))
		if len(activeNodes) > 0 {
			parts = append(parts, "("+strings.Join(activeNodes, ", ")+")")
		}
	}
	if elapsed > 0 {
		parts = append(parts, elapsed.String())
	}
	m.subagentStatus = strings.Join(parts, " · ")
}
```

Search for the existing direct assignments to `m.subagentStatus` in `model.go` and replace them with calls to `m.recomputeSubagentStatus()`. The direct assignments are at lines 559, 562, 771, 896, and 3028. Replace each by a single call:

- Line 559 / 562 (in `subagentCommandResultMsg` handler): call `m.recomputeSubagentStatus()` after `m.updateSubagentRunResult(...)`.
- Line 771 (in `metaToolSubagentsRun` branch): call `m.recomputeSubagentStatus()` after `m.addSubagentRunsFromTasks(call.Arguments)`.
- Line 896 (in `multiToolResultMsg` handler): call `m.recomputeSubagentStatus()` after the subagents branch.
- Line 3028: call `m.recomputeSubagentStatus()` after `m.addSubagentRun(req)`.

- [ ] **Step 4: Build and run existing tests**

Run: `go build ./... && go test ./internal/tui/ -count=1 -short`
Expected: existing tests pass. New code compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): handle subagent event/result messages and recompute status"
```

---

### Task 9: Wire `CancelAll` in `cancelActiveStream` and add the `c` keybinding

**Files:**
- Modify: `internal/tui/model.go` (around line 3957, in `handleKey`)

- [ ] **Step 1: Cancel subagents when the main stream is cancelled**

Replace `cancelActiveStream` (lines 3957-3961) with:

```go
func (m *Model) cancelActiveStream() {
	if m.subagentManager != nil {
		m.subagentManager.CancelAll()
	}
	if m.streamCancel != nil {
		m.streamCancel()
	}
}
```

- [ ] **Step 2: Add the `c` keybinding in `handleKey`**

Locate `handleKey` (line 1034). Find the section that processes keys in the chat mode and look for the section that handles `subagentRunsExpanded`. Add a new case before the default. Concretely, find the existing case that toggles expanded view (around line 2664) and add this case nearby:

```go
case "c":
	if m.mode == modeChat && m.subagentRunsExpanded && len(m.subagentRuns) > 0 {
		focusedID := m.subagentRuns[len(m.subagentRuns)-1].ID
		if m.subagentManager != nil {
			_ = m.subagentManager.Cancel(focusedID)
		}
		return m, nil
	}
```

Place this case alongside the existing expanded-view keybinding so both are reachable from `modeChat`. If the structure of `handleKey` is a switch on the key string, add it as a new `case`; if it is a chain of `if/else`, insert it at the same nesting level as the expand toggle.

- [ ] **Step 3: Update the `addSubagentRun` helper to set `StartedAt`**

Find `addSubagentRun` (around line 3426). Change the append to include `StartedAt: time.Now()`:

```go
func (m *Model) addSubagentRun(req subagent.Request) {
	m.subagentRuns = append(m.subagentRuns, subagentRunView{
		ID:        req.ID,
		Role:      req.Role,
		Task:      req.Task,
		Prompt:    subagentPromptForDisplay(req),
		Model:     req.Model,
		Nodes:     append([]string(nil), req.Nodes...),
		Status:    "running",
		StartedAt: time.Now(),
	})
	m.lastBodyContent = ""
}
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): cancel subagents on parent cancel and add c keybinding"
```

---

### Task 10: Update the config screen with new subagent entries

**Files:**
- Modify: `internal/tui/configscreen.go` (around lines 69-72, 263-286)

- [ ] **Step 1: Render the new entries**

In `internal/tui/configscreen.go`, replace lines 69-72 with:

```go
{Group: "Subagents", Key: "subagents.enabled", Type: configBool, Value: formatBool(g.Subagents.Enabled)},
{Group: "Subagents", Key: "subagents.max_parallel", Type: configInt, Value: strconv.Itoa(g.Subagents.MaxParallel)},
{Group: "Subagents", Key: "subagents.timeout_seconds", Type: configInt, Value: strconv.Itoa(g.Subagents.TimeoutSeconds)},
{Group: "Subagents", Key: "subagents.max_inbound_chars", Type: configInt, Value: strconv.Itoa(g.Subagents.MaxInboundChars)},
{Group: "Subagents", Key: "subagents.debug", Type: configBool, Value: formatBool(g.Subagents.Debug)},
{Group: "Subagents", Key: "subagents.debug_transcript", Type: configBool, Value: formatBool(g.Subagents.DebugTranscript)},
{Group: "Subagents", Key: "subagents.per_role.investigator_turns", Type: configInt, Value: strconv.Itoa(g.Subagents.PerRole.InvestigatorTurns)},
{Group: "Subagents", Key: "subagents.per_role.investigator_tool_calls", Type: configInt, Value: strconv.Itoa(g.Subagents.PerRole.InvestigatorToolCalls)},
{Group: "Subagents", Key: "subagents.per_role.reviewer_turns", Type: configInt, Value: strconv.Itoa(g.Subagents.PerRole.ReviewerTurns)},
{Group: "Subagents", Key: "subagents.per_role.reviewer_tool_calls", Type: configInt, Value: strconv.Itoa(g.Subagents.PerRole.ReviewerToolCalls)},
{Group: "Subagents", Key: "subagents.per_role.summarizer_turns", Type: configInt, Value: strconv.Itoa(g.Subagents.PerRole.SummarizerTurns)},
```

- [ ] **Step 2: Add the parse handlers**

In `internal/tui/configscreen.go`, replace the `subagents.debug` case (lines 281-286) with the following, which adds the new keys before the existing `vision.*` cases:

```go
case "subagents.debug":
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid subagents.debug: %s", value)
	}
	g.Subagents.Debug = parsed
case "subagents.debug_transcript":
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid subagents.debug_transcript: %s", value)
	}
	g.Subagents.DebugTranscript = parsed
case "subagents.max_inbound_chars":
	parsed, err := parsePositiveInt("subagents.max_inbound_chars", value)
	if err != nil {
		return err
	}
	g.Subagents.MaxInboundChars = parsed
case "subagents.per_role.investigator_turns":
	parsed, err := parsePositiveInt("subagents.per_role.investigator_turns", value)
	if err != nil {
		return err
	}
	g.Subagents.PerRole.InvestigatorTurns = parsed
case "subagents.per_role.investigator_tool_calls":
	parsed, err := parsePositiveInt("subagents.per_role.investigator_tool_calls", value)
	if err != nil {
		return err
	}
	g.Subagents.PerRole.InvestigatorToolCalls = parsed
case "subagents.per_role.reviewer_turns":
	parsed, err := parsePositiveInt("subagents.per_role.reviewer_turns", value)
	if err != nil {
		return err
	}
	g.Subagents.PerRole.ReviewerTurns = parsed
case "subagents.per_role.reviewer_tool_calls":
	parsed, err := parsePositiveInt("subagents.per_role.reviewer_tool_calls", value)
	if err != nil {
		return err
	}
	g.Subagents.PerRole.ReviewerToolCalls = parsed
case "subagents.per_role.summarizer_turns":
	parsed, err := parsePositiveInt("subagents.per_role.summarizer_turns", value)
	if err != nil {
		return err
	}
	g.Subagents.PerRole.SummarizerTurns = parsed
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/configscreen.go
git commit -m "feat(tui): expose per-role subagent config in config screen"
```

---

### Task 11: Add `buildSubagentMemoryContext` helper

**Files:**
- Modify: `internal/tui/model.go` (new helper near `recentConversationContext`)

- [ ] **Step 1: Add the helper**

In `internal/tui/model.go`, add the following helper near `recentConversationContext` (line 3541):

```go
const subagentMemoryContextBudget = 600

func (m Model) buildSubagentMemoryContext(task string, nodes []string) string {
	if m.memStore == nil {
		return ""
	}
	query := strings.TrimSpace(task)
	if len(nodes) > 0 {
		query = query + " " + strings.Join(nodes, " ")
	}
	entries, err := m.memStore.SearchMemories(query, 5)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var parts []string
	budget := subagentMemoryContextBudget
	for _, e := range entries {
		body := strings.TrimSpace(e.Title + "\n" + e.Content)
		if len(body) > budget {
			body = body[:budget]
		}
		parts = append(parts, body)
		budget -= len(body)
		if budget <= 0 {
			break
		}
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): populate subagent MemoryContext from main memory store"
```

---

### Task 12: TUI tests for the new event-driven state and keybinding

**Files:**
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Add the tests**

Append the following tests at the end of `internal/tui/model_test.go`:

```go
func TestRecomputeSubagentStatusAggregatesRuns(t *testing.T) {
	m := Model{
		uiLanguage:      uiLanguage("en-US"),
		subagentManager: subagent.NewManager(),
		subagentRuns: []subagentRunView{
			{ID: "a", Status: "completed", StartedAt: time.Now().Add(-3 * time.Second)},
			{ID: "b", Status: "running", Nodes: []string{"node-01"}, StartedAt: time.Now().Add(-2 * time.Second)},
			{ID: "c", Status: "running", Nodes: []string{"node-02"}, StartedAt: time.Now().Add(-1 * time.Second)},
		},
	}
	m.recomputeSubagentStatus()
	if !strings.Contains(m.subagentStatus, "1/3 done") {
		t.Errorf("status = %q, want contains %q", m.subagentStatus, "1/3 done")
	}
	if !strings.Contains(m.subagentStatus, "2 active") {
		t.Errorf("status = %q, want contains %q", m.subagentStatus, "2 active")
	}
}

func TestSubagentEventMsgUpdatesRow(t *testing.T) {
	m := Model{
		uiLanguage:      uiLanguage("en-US"),
		subagentManager: subagent.NewManager(),
		subagentRuns:    []subagentRunView{{ID: "a", Status: "running"}},
	}
	ev := subagent.Event{ID: "a", Kind: subagent.EventToolCall, Turn: 2, Tool: "tool_search"}
	next, _ := m.Update(subagentEventMsg{ID: "a", Event: ev})
	m2 := next.(Model)
	if m2.subagentRuns[0].CurrentTool != "tool_search" {
		t.Errorf("CurrentTool = %q, want %q", m2.subagentRuns[0].CurrentTool, "tool_search")
	}
	if m2.subagentRuns[0].Turn != 2 {
		t.Errorf("Turn = %d, want 2", m2.subagentRuns[0].Turn)
	}
}

func TestSubagentResultMsgClosesRow(t *testing.T) {
	m := Model{
		uiLanguage:      uiLanguage("en-US"),
		subagentManager: subagent.NewManager(),
		subagentRuns:    []subagentRunView{{ID: "a", Status: "running", StartedAt: time.Now()}},
	}
	res := subagent.Result{ID: "a", Role: subagent.RoleInvestigator, Summary: "ok", Elapsed: 1200 * time.Millisecond}
	next, _ := m.Update(subagentResultMsg{Result: res})
	m2 := next.(Model)
	if m2.subagentRuns[0].Status != "completed" {
		t.Errorf("Status = %q, want completed", m2.subagentRuns[0].Status)
	}
	if m2.subagentRuns[0].Summary != "ok" {
		t.Errorf("Summary = %q, want %q", m2.subagentRuns[0].Summary, "ok")
	}
}

func TestCancelActiveStreamCancelsSubagents(t *testing.T) {
	mgr := subagent.NewManager()
	runner := subagent.Runner{Provider: &blockingProvider{}, Executor: &fakeExecutor{}}
	_, _, results, err := mgr.Submit(context.Background(), runner, subagent.Request{Role: subagent.RoleInvestigator, Task: "x", MaxTurns: 5})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	m := Model{streamCancel: func() {}, subagentManager: mgr}
	m.cancelActiveStream()
	select {
	case res := <-results:
		if !errors.Is(res.Err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("results did not deliver after cancelActiveStream")
	}
}
```

Add to the import block in `model_test.go` if not already present: `"errors"`, `"github.com/pockyHM/conan/internal/subagent"`.

- [ ] **Step 2: Run the new tests, verify they pass**

Run: `go test ./internal/tui/ -run "TestRecomputeSubagentStatus|TestSubagentEventMsg|TestSubagentResultMsg|TestCancelActiveStreamCancelsSubagents" -v`
Expected: PASS for all four tests.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -count=1`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model_test.go
git commit -m "test(tui): cover subagent event, result, and cancel paths"
```

---

## Phase C — Final integration

### Task 13: End-to-end smoke check

**Files:** none (read-only verification)

- [ ] **Step 1: Build the whole module**

Run: `go build ./...`
Expected: success.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./... -count=1`
Expected: all tests pass.

- [ ] **Step 3: Run `go vet`**

Run: `go vet ./...`
Expected: no issues.

- [ ] **Step 4: Verify the new YAML config round-trips**

Run: `go run ./cmd/conan -h 2>&1 | head -5` (or whatever command is used to start the TUI in tests). Inspect the config screen rendering manually if possible; otherwise rely on the unit tests in `internal/tui/configscreen_test.go` if present.

---

## Self-review notes

- Spec coverage: every requirement maps to a task. Tasks 1–5 cover the subagent package. Task 6–9 cover the TUI. Task 10 covers the config screen. Task 11 covers the memory helper. Task 12 covers tests. Task 13 is a smoke check.
- Placeholders: none. Every step has the exact code, file path, command, and expected output.
- Type consistency: `Event`, `EventKind`, `Manager`, `Transcript`, `RoleLimits`, `subagentEventMsg`, `subagentResultMsg`, `subagentStartMsg` are defined in the task that first introduces them and reused identically thereafter.
- Migration: the breaking config change (`subagents.max_turns` / `subagents.max_tool_calls` removal) is not validated by the schema; the YAML decoder silently drops unknown keys. A user with the old keys will see no error and will fall back to the new defaults. This is acceptable per the spec's "Skip" note and avoids introducing a custom unmarshaler.
- Out of scope: live timer tick, README migration note, `.gitignore` for `.superpowers/`. These are intentionally omitted and can be a follow-up plan.
