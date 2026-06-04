package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTraceCommandOpensEmptyTracePage(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.applyCommand(SlashCommand{Kind: CommandTrace})
	model = next

	if model.mode != modeTrace {
		t.Fatalf("mode = %v, want modeTrace", model.mode)
	}
	view := model.View()
	for _, want := range []string{"Trace", "No trace nodes yet"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trace view missing %q:\n%s", want, view)
		}
	}
}

func TestTracePageEscReturnsToChat(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}

func TestTracePageRendersArrowTimelineMarkers(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace
	model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", "check nginx", "check nginx"))
	model = model.appendTraceNode(traceNode{ID: "tool-1", Kind: traceToolCall, Status: traceRunning, Title: "tool call", Summary: `shell_run {"command":"uptime"}`, Detail: "shell_run"})
	model = model.appendTraceNode(traceNode{ID: "result-1", Kind: traceToolResult, Status: traceDone, Title: "tool result", Summary: "local · ok", Detail: "ok"})
	model = model.appendTraceNode(traceNode{ID: "subagent-1", Kind: traceSubagent, Status: traceRunning, Title: "subagent", Summary: "investigator[node-01] running", Detail: "turn 1"})

	view := model.View()
	for _, want := range []string{"●", "│", "↓", "▶", "✓", "◇", "check nginx", "shell_run", "local · ok"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trace view missing %q:\n%s", want, view)
		}
	}
}

func TestTracePageNavigationAndDetail(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace
	model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", "first", "first detail"))
	model = model.appendTraceNode(newTraceNode(traceAssistant, traceDone, "assistant", "second", "second detail"))

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.traceCursor != 1 {
		t.Fatalf("traceCursor = %d, want 1", model.traceCursor)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !model.traceDetailVisible {
		t.Fatal("trace detail should be visible after Enter")
	}
	if view := model.View(); !strings.Contains(view, "second detail") {
		t.Fatalf("detail missing selected node detail:\n%s", view)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.traceDetailVisible {
		t.Fatal("Esc from detail should return to timeline")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
}
