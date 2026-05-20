package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SessionInfo struct {
	ID        string
	Cluster   string
	CreatedAt string
	Summary   string
}

type sessionList struct {
	sessions []SessionInfo
	cursor   int
	selected *SessionInfo
}

func newSessionList(sessions []SessionInfo) sessionList {
	return sessionList{sessions: sessions}
}

func (s sessionList) Selected() *SessionInfo {
	return s.selected
}

func (s sessionList) Update(msg tea.Msg) (sessionList, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch key.Type {
	case tea.KeyUp:
		if s.cursor > 0 {
			s.cursor--
		}
	case tea.KeyDown:
		if s.cursor < len(s.sessions)-1 {
			s.cursor++
		}
	case tea.KeyEnter:
		if len(s.sessions) > 0 {
			sess := s.sessions[s.cursor]
			s.selected = &sess
		}
	}
	return s, nil
}

func (s sessionList) View() string {
	if len(s.sessions) == 0 {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Render("No previous sessions found.")
	}

	var lines []string
	for i, sess := range s.sessions {
		cursor := "  "
		if i == s.cursor {
			cursor = "▸ "
		}
		firstLine := fmt.Sprintf("%s%s  %-20s  %s", cursor, sess.ID, sess.CreatedAt, sess.Cluster)
		summary := sess.Summary
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		secondLine := fmt.Sprintf("%s%s", strings.Repeat(" ", len(cursor)), summary)
		lines = append(lines, firstLine+"\n"+secondLine)
	}

	panel := strings.Join(lines, "\n\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(fmt.Sprintf("Historical Sessions\n\n%s\n\n↑↓ Move  Enter Resume  Esc Cancel", panel))
}
