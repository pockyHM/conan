package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/subagent"
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
	for _, want := range []string{"abc123", "reviewer", "node-01, node-02", "2s", "Prompt:", "review logs"} {
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
