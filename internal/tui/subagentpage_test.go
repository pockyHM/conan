package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/subagent"
	"github.com/pockyHM/conan/pkg/configschema"
)

func TestSubagentListPageOpensOnCtrlA(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude", Nodes: []string{"node-01"}, Status: "receiving", Prompt: "Role: investigator\nTask: ping"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)

	if model.mode != modeSubagentList {
		t.Fatalf("mode = %v, want modeSubagentList", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Subagents", "abc123", "investigator", "node-01", "ping"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list view missing %q:\n%s", want, view)
		}
	}
}

func TestSubagentListPageCtrlAWithNoRunsShowsStatus(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat (no runs, should stay in chat)", model.mode)
	}
	if !strings.Contains(model.status, "No subagents") && !strings.Contains(model.status, "暂无") {
		t.Fatalf("expected no-subagent status, got %q", model.status)
	}
}

func TestSubagentListPageEnterShowsDetail(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleReviewer, Model: "claude", Nodes: []string{"node-01", "node-02"}, Status: "completed", Prompt: "Role: reviewer\nTask: review logs\nCluster: prod\nNodes: node-01, node-02", Elapsed: 2 * time.Second},
		{ID: "def456", Role: subagent.RoleInvestigator, Model: "claude", Nodes: []string{"node-03"}, Status: "receiving", Prompt: "Role: investigator\nTask: check disk"},
	}
	model.subagentListCursor = 0

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	if model.subagentDetailVisible {
		t.Fatalf("expected list view (detail hidden) after Ctrl+A")
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !model.subagentDetailVisible {
		t.Fatalf("expected detail visible after Enter")
	}
	view := model.View()
	for _, want := range []string{"abc123", "reviewer", "node-01, node-02", "2s", "Conversation", "(no trace events captured)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail view missing %q:\n%s", want, view)
		}
	}
}

func TestSubagentListPageEscFromDetailReturnsToList(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude", Nodes: []string{"node-01"}, Status: "receiving", Prompt: "Role: investigator\nTask: x"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !model.subagentDetailVisible {
		t.Fatalf("expected detail visible after Enter")
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.subagentDetailVisible {
		t.Fatalf("expected detail hidden after Esc from detail")
	}
	if model.mode != modeSubagentList {
		t.Fatalf("mode = %v, want modeSubagentList (back in list)", model.mode)
	}
}

func TestSubagentListPageEscFromListReturnsToChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude", Nodes: []string{"node-01"}, Status: "receiving", Prompt: "Role: investigator\nTask: x"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}

func TestSubagentListPageNavigation(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "first", Role: subagent.RoleInvestigator, Model: "claude", Nodes: []string{"a"}, Status: "receiving", Prompt: "first"},
		{ID: "second", Role: subagent.RoleReviewer, Model: "claude", Nodes: []string{"b"}, Status: "completed", Prompt: "second"},
		{ID: "third", Role: subagent.RoleSummarizer, Model: "claude", Nodes: []string{"c"}, Status: "failed", Prompt: "third"},
	}
	model.subagentListCursor = 2

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	if model.subagentListCursor != 2 {
		t.Fatalf("expected cursor to clamp to last index 2, got %d", model.subagentListCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.subagentListCursor != 2 {
		t.Fatalf("cursor at last, Down should keep it at 2, got %d", model.subagentListCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentListCursor != 1 {
		t.Fatalf("expected cursor 1 after Up, got %d", model.subagentListCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentListCursor != 0 {
		t.Fatalf("expected cursor 0 at first, got %d", model.subagentListCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentListCursor != 0 {
		t.Fatalf("cursor at first, Up should keep it at 0, got %d", model.subagentListCursor)
	}
}

func TestSubagentListPageJKNavigation(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "first", Role: subagent.RoleInvestigator, Model: "claude", Status: "receiving", Prompt: "x"},
		{ID: "second", Role: subagent.RoleInvestigator, Model: "claude", Status: "receiving", Prompt: "y"},
	}
	model.subagentListCursor = 0

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = next.(Model)
	if model.subagentListCursor != 1 {
		t.Fatalf("after j cursor = %d, want 1", model.subagentListCursor)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = next.(Model)
	if model.subagentListCursor != 0 {
		t.Fatalf("after k cursor = %d, want 0", model.subagentListCursor)
	}
}

func TestSubagentListPageStatusBadgesInList(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "r1", Role: subagent.RoleInvestigator, Model: "claude", Status: "receiving", Prompt: "x"},
		{ID: "c1", Role: subagent.RoleReviewer, Model: "claude", Status: "completed", Prompt: "y"},
		{ID: "f1", Role: subagent.RoleSummarizer, Model: "claude", Status: "failed", Prompt: "z"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"receiving", "completed", "failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list view missing status %q:\n%s", want, view)
		}
	}
}

func TestSubagentListPageTruncatesLongPrompt(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	longPrompt := "Role: investigator\nTask: " + strings.Repeat("very long prompt line ", 30)
	model.subagentRuns = []subagentRunView{
		{ID: "long", Role: subagent.RoleInvestigator, Model: "claude", Status: "receiving", Prompt: longPrompt},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "…") {
		t.Fatalf("expected ellipsis in truncated list prompt, got:\n%s", view)
	}
}

func TestSubagentListPageCancelKeyMarksCancelled(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude", Status: "receiving", Prompt: "x"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)

	if model.subagentRuns[0].Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", model.subagentRuns[0].Status)
	}
}

func TestSubagentListPageCancelIgnoredForCompleted(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude", Status: "completed", Prompt: "x"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)

	if model.subagentRuns[0].Status != "completed" {
		t.Fatalf("status = %q, want unchanged completed", model.subagentRuns[0].Status)
	}
}

func sampleThreeTurnRun() subagentRunView {
	return subagentRunView{
		ID: "abc123", Role: subagent.RoleInvestigator, Model: "claude-opus",
		Nodes:   []string{"node-01"},
		Status:  "completed",
		Summary: "Two pods pending on node-01.",
		Elapsed: 4 * time.Second,
		Events: []subagent.Event{
			{Kind: subagent.EventTurnStart, Turn: 1, Elapsed: 0},
			{Kind: subagent.EventAssistantText, Turn: 1, Content: "I'll check disk space.", Elapsed: 300 * time.Millisecond},
			{Kind: subagent.EventToolCall, Turn: 1, Tool: "k8s_pods", Args: `{"ns":"*"}`, Elapsed: 350 * time.Millisecond},
			{Kind: subagent.EventToolResult, Turn: 1, Tool: "k8s_pods", Out: "pod-1 Running\npod-2 Pending", OK: true, Elapsed: 1*time.Second + 200*time.Millisecond},
			{Kind: subagent.EventTurnEnd, Turn: 1, Elapsed: 1*time.Second + 200*time.Millisecond},
			{Kind: subagent.EventTurnStart, Turn: 2, Elapsed: 1*time.Second + 300*time.Millisecond},
			{Kind: subagent.EventToolCall, Turn: 2, Tool: "k8s_logs", Args: `{"pod":"pod-2"}`, Elapsed: 1*time.Second + 400*time.Millisecond},
			{Kind: subagent.EventToolResult, Turn: 2, Tool: "k8s_logs", Out: "warning: image pull backoff", OK: true, Elapsed: 2 * time.Second},
			{Kind: subagent.EventTurnEnd, Turn: 2, Elapsed: 2 * time.Second},
			{Kind: subagent.EventTurnStart, Turn: 3, Elapsed: 2*time.Second + 100*time.Millisecond},
			{Kind: subagent.EventAssistantText, Turn: 3, Content: "Two pods are pending on node-01.", Elapsed: 3 * time.Second},
			{Kind: subagent.EventTurnEnd, Turn: 3, Elapsed: 3 * time.Second},
		},
	}
}

func TestSubagentDetailShowsConversation(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{"Conversation", "Turn 1", "Turn 2", "Turn 3", "k8s_pods", "k8s_logs"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail view missing %q:\n%s", want, view)
		}
	}
}

func TestSubagentDetailLastTurnAlwaysExpanded(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "Two pods are pending on node-01.") {
		t.Fatalf("final reply text missing from detail view:\n%s", view)
	}
	if strings.Contains(view, "I'll check disk space.") {
		t.Fatalf("intermediate turn text leaked into detail view (turn 1 should be collapsed):\n%s", view)
	}
}

func TestSubagentDetailOtherTurnsCollapsedByDefault(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "pod-1 Running") {
		t.Fatalf("turn 1 tool result leaked (turn 1 should be collapsed by default):\n%s", view)
	}
	if strings.Contains(view, "warning: image pull backoff") {
		t.Fatalf("turn 2 tool result leaked (turn 2 should be collapsed by default):\n%s", view)
	}
}

func TestSubagentDetailSpaceTogglesExpansion(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.subagentDetailExpanded[1] {
		t.Fatal("turn 1 should start collapsed (subagentDetailExpanded[1] should be false)")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentDetailCursor != 0 {
		t.Fatalf("expected cursor 0 after two Ups, got %d", model.subagentDetailCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if !model.subagentDetailExpanded[1] {
		t.Fatalf("expected turn 1 to be expanded after Space")
	}
	view := model.View()
	if !strings.Contains(view, "I'll check disk space.") {
		t.Fatalf("turn 1 assistant text missing after Space toggle:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if model.subagentDetailExpanded[1] {
		t.Fatalf("expected turn 1 to be collapsed after second Space")
	}
	view = model.View()
	if strings.Contains(view, "I'll check disk space.") {
		t.Fatalf("turn 1 assistant text still present after collapse:\n%s", view)
	}
}

func TestSubagentDetailEnterTogglesExpansion(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if model.subagentDetailCursor != 2 {
		t.Fatalf("expected cursor at last turn (2) on entry, got %d", model.subagentDetailCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentDetailCursor != 0 {
		t.Fatalf("after two Ups cursor = %d, want 0", model.subagentDetailCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	view := model.View()
	if !strings.Contains(view, "I'll check disk space.") {
		t.Fatalf("Enter on turn 1 should toggle expansion - assistant text missing:\n%s", view)
	}
}

func TestSubagentDetailUpDownMovesCursor(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.subagentDetailCursor != 2 {
		t.Fatalf("entry cursor = %d, want 2", model.subagentDetailCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentDetailCursor != 1 {
		t.Fatalf("after Up cursor = %d, want 1", model.subagentDetailCursor)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentDetailCursor != 0 {
		t.Fatalf("after Up cursor = %d, want 0", model.subagentDetailCursor)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = next.(Model)
	if model.subagentDetailCursor != 0 {
		t.Fatalf("at top, Up should keep cursor at 0, got %d", model.subagentDetailCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.subagentDetailCursor != 1 {
		t.Fatalf("after Down cursor = %d, want 1", model.subagentDetailCursor)
	}
}

func TestSubagentDetailEmptyEventsShowsPlaceholder(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{
		{ID: "abc", Role: subagent.RoleInvestigator, Model: "claude", Status: "completed", Summary: "done"},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	view := model.View()
	if !strings.Contains(view, "Conversation") {
		t.Fatalf("expected Conversation section, got:\n%s", view)
	}
	if !strings.Contains(view, "no trace events") {
		t.Fatalf("expected placeholder for empty events, got:\n%s", view)
	}
	if !strings.Contains(view, "Summary") {
		t.Fatalf("expected Summary section, got:\n%s", view)
	}
	if !strings.Contains(view, "done") {
		t.Fatalf("expected summary text, got:\n%s", view)
	}
}

func TestSubagentDetailJKAlsoMovesCursor(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.subagentRuns = []subagentRunView{sampleThreeTurnRun()}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = next.(Model)
	if model.subagentDetailCursor != 1 {
		t.Fatalf("after k cursor = %d, want 1", model.subagentDetailCursor)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = next.(Model)
	if model.subagentDetailCursor != 2 {
		t.Fatalf("after j cursor = %d, want 2", model.subagentDetailCursor)
	}
}

func TestSubagentDetailEventsArriveViaStreamingToolPath(t *testing.T) {
	home := t.TempDir()
	conv := conversation.New("test", nil, "m")
	model := NewModel(ModelConfig{
		Cluster:    "test",
		Model:      "m",
		ConfigHome: home,
		Conv:       conv,
		Subagents:  configschema.SubagentConfig{Enabled: true},
	})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamCh = make(chan llm.ChatEvent)
	model.streamCtx = context.Background()

	args := json.RawMessage(`{"tasks":[{"role":"investigator","task":"check disk"}]}`)
	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "sa1", Name: metaToolSubagentsRun, Arguments: args,
	}})
	model = next.(Model)

	if len(model.subagentRuns) != 1 {
		t.Fatalf("expected 1 subagent run after tool call, got %d", len(model.subagentRuns))
	}
	runID := model.subagentRuns[0].ID
	if runID == "" {
		t.Fatal("subagent run has empty ID")
	}

	events := []subagent.Event{
		{Kind: subagent.EventTurnStart, Turn: 1, Elapsed: 0},
		{Kind: subagent.EventAssistantText, Turn: 1, Content: "checking now", Elapsed: 200 * time.Millisecond},
		{Kind: subagent.EventToolCall, Turn: 1, Tool: "k8s_pods", Args: `{"ns":"*"}`, Elapsed: 300 * time.Millisecond},
		{Kind: subagent.EventToolResult, Turn: 1, Tool: "k8s_pods", Out: "pod-1 OK", OK: true, Elapsed: 1 * time.Second},
		{Kind: subagent.EventTurnEnd, Turn: 1, Elapsed: 1 * time.Second},
		{Kind: subagent.EventTurnStart, Turn: 2, Elapsed: 1*time.Second + 100*time.Millisecond},
		{Kind: subagent.EventAssistantText, Turn: 2, Content: "all good", Elapsed: 2 * time.Second},
		{Kind: subagent.EventTurnEnd, Turn: 2, Elapsed: 2 * time.Second},
		{Kind: subagent.EventDone, Elapsed: 2 * time.Second},
	}
	call := llm.ToolCall{ID: "sa1", Name: metaToolSubagentsRun, Arguments: args}
	next, _ = model.Update(multiToolResultMsg{
		Call:    call,
		Results: []nodeToolResult{{Node: "local", Output: "[investigator:local:ok] all good", Success: true}},
		subagentEvents: map[string][]subagent.Event{
			runID: events,
		},
	})
	model = next.(Model)

	if len(model.subagentRuns[0].Events) == 0 {
		t.Fatal("expected events to be copied onto the run view, got none")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"Conversation", "Turn 1", "Turn 2", "k8s_pods", "all good"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail view missing %q after streaming path:\n%s", want, view)
		}
	}
	if strings.Contains(view, "checking now") {
		t.Fatalf("turn 1 assistant text leaked (should be collapsed):\n%s", view)
	}
}
