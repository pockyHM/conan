package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/llm"
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

func TestTracePageBoundsTimelineRowsToHeight(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})
	model.mode = modeTrace
	model.height = 14
	for i := 0; i < 20; i++ {
		summary := fmt.Sprintf("trace-node-%02d", i)
		model = model.appendTraceNode(newTraceNode(traceUser, traceDone, "user", summary, summary))
	}
	model.traceCursor = 10

	view := model.renderTracePage()
	for _, want := range []string{"Trace", "20 nodes", "Enter detail", "trace-node-10"} {
		if !strings.Contains(view, want) {
			t.Fatalf("bounded trace view missing %q:\n%s", want, view)
		}
	}
	rendered := 0
	for i := 0; i < 20; i++ {
		if strings.Contains(view, fmt.Sprintf("trace-node-%02d", i)) {
			rendered++
		}
	}
	if rendered > 3 {
		t.Fatalf("rendered %d trace node summaries, want at most 3:\n%s", rendered, view)
	}
}

func TestTraceRecordsUserMessageWithoutProvider(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "production", Model: "claude", ConfigHome: t.TempDir()})

	next, _ := model.submitMessage("check nginx status", nil)
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("traceNodes len = %d, want 1", len(model.traceNodes))
	}
	node := model.traceNodes[0]
	if node.Kind != traceUser || node.Status != traceDone {
		t.Fatalf("user trace node = (%s, %s), want (%s, %s)", node.Kind, node.Status, traceUser, traceDone)
	}
	if !strings.Contains(node.Summary, "check nginx status") || !strings.Contains(node.Detail, "check nginx status") {
		t.Fatalf("user trace node missing content: %#v", node)
	}
}

func TestTraceUpdatesSingleAssistantNodeFromStreamingDeltas(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, ConfigHome: t.TempDir()})
	model.streaming = true
	model.activeStreamID = 1
	model.streamStartedAt = time.Now()

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "Hello "}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "world"}})
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("traceNodes len = %d, want 1", len(model.traceNodes))
	}
	node := model.traceNodes[0]
	if node.Kind != traceAssistant || node.Status != traceRunning {
		t.Fatalf("assistant trace node = (%s, %s), want (%s, %s)", node.Kind, node.Status, traceAssistant, traceRunning)
	}
	if node.Detail != "Hello world" || node.Summary != "Hello world" {
		t.Fatalf("assistant trace content = summary %q detail %q, want Hello world", node.Summary, node.Detail)
	}

	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.StopEvent{Reason: llm.StopEndTurn}})
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("traceNodes len after stop = %d, want 1", len(model.traceNodes))
	}
	node = model.traceNodes[0]
	if node.Status != traceDone || node.EndedAt.IsZero() {
		t.Fatalf("assistant trace completion = (%s, ended %t), want done with end time", node.Status, !node.EndedAt.IsZero())
	}
}

func TestTraceMarksAssistantNodeFailedOnStreamError(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, ConfigHome: t.TempDir()})
	model.streaming = true
	model.activeStreamID = 1
	model.streamStartedAt = time.Now()

	next, _ := model.Update(streamEventMsg{streamID: 1, Event: llm.TextDeltaEvent{Delta: "partial"}})
	model = next.(Model)
	next, _ = model.Update(streamEventMsg{streamID: 1, Event: llm.ErrorEvent{Err: errors.New("stream failed")}})
	model = next.(Model)

	if len(model.traceNodes) != 1 {
		t.Fatalf("traceNodes len = %d, want 1", len(model.traceNodes))
	}
	node := model.traceNodes[0]
	if node.Status != traceFailed || node.EndedAt.IsZero() {
		t.Fatalf("assistant trace status = %s ended=%t, want failed with end time", node.Status, !node.EndedAt.IsZero())
	}
	if !strings.Contains(node.Detail, "partial") {
		t.Fatalf("assistant failed trace should preserve partial detail: %#v", node)
	}
	if !strings.Contains(node.Detail, "stream failed") {
		t.Fatalf("assistant failed trace should include error detail: %#v", node)
	}
}
