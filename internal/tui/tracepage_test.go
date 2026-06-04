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
