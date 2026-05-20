package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNodeSelectorNavigation(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
		{Name: "node-03", Host: "10.0.1.3", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{"node-01": true})

	if s.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.cursor != 0 {
		t.Fatalf("cursor at top = %d, want 0", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 2 {
		t.Fatalf("cursor at bottom = %d, want 2", s.cursor)
	}
}

func TestNodeSelectorToggle(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{})

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !s.checked["node-01"] {
		t.Fatal("node-01 should be checked after space")
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if s.checked["node-01"] {
		t.Fatal("node-01 should be unchecked after second space")
	}
}

func TestNodeSelectorOfflineUnselectable(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	s := newNodeSelector(nodes, map[string]bool{})

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if s.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", s.cursor)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if s.checked["node-02"] {
		t.Fatal("offline node should not be selectable")
	}
}

func TestNodeSelectorView(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: false},
	}
	s := newNodeSelector(nodes, map[string]bool{})
	view := s.View()

	for _, want := range []string{"node-01", "10.0.1.1", "node-02", "10.0.1.2", "Online", "Offline", "Select Target Nodes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestNodeSelectorEmptyNodes(t *testing.T) {
	s := newNodeSelector(nil, nil)
	view := s.View()
	if !strings.Contains(view, "No nodes configured") {
		t.Fatalf("empty selector should show message:\n%s", view)
	}
}

func TestNodeSelectorSelected(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "node-01", Host: "10.0.1.1", Online: true},
		{Name: "node-02", Host: "10.0.1.2", Online: true},
	}
	s := newNodeSelector(nodes, map[string]bool{"node-01": true, "node-02": true})

	selected := s.Selected()
	if len(selected) != 2 {
		t.Fatalf("len(selected) = %d, want 2", len(selected))
	}
	if !selected["node-01"] || !selected["node-02"] {
		t.Fatal("both nodes should be selected")
	}
}
