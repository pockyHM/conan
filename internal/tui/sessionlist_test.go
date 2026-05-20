package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionListEmpty(t *testing.T) {
	sl := newSessionList(nil)
	view := sl.View()
	if !strings.Contains(view, "No previous sessions") {
		t.Fatalf("expected empty message:\n%s", view)
	}
}

func TestSessionListRenders(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "a3f2e1", Cluster: "production", CreatedAt: "2026-05-19 14:30", Summary: "Investigated memory leak"},
		{ID: "b7c9d4", Cluster: "staging", CreatedAt: "2026-05-19 10:15", Summary: "Deployed v2.3.1"},
	}
	sl := newSessionList(sessions)
	view := sl.View()
	for _, want := range []string{"a3f2e1", "production", "b7c9d4", "Investigated memory leak"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestSessionListNavigation(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "s1", Cluster: "prod", CreatedAt: "2026-05-19", Summary: "First"},
		{ID: "s2", Cluster: "staging", CreatedAt: "2026-05-18", Summary: "Second"},
	}
	sl := newSessionList(sessions)

	sl, _ = sl.Update(tea.KeyMsg{Type: tea.KeyDown})
	if sl.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", sl.cursor)
	}

	sl, _ = sl.Update(tea.KeyMsg{Type: tea.KeyUp})
	if sl.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", sl.cursor)
	}
}

func TestSessionListSelect(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "s1", Cluster: "prod", CreatedAt: "2026-05-19", Summary: "First"},
		{ID: "s2", Cluster: "staging", CreatedAt: "2026-05-18", Summary: "Second"},
	}
	sl := newSessionList(sessions)

	sl, _ = sl.Update(tea.KeyMsg{Type: tea.KeyDown})
	sl, _ = sl.Update(tea.KeyMsg{Type: tea.KeyEnter})

	selected := sl.Selected()
	if selected == nil {
		t.Fatal("expected selection")
	}
	if selected.ID != "s2" {
		t.Fatalf("selected.ID = %q, want s2", selected.ID)
	}
}
