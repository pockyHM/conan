package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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
	lang     uiLanguage
}

func newSessionList(sessions []SessionInfo, lang ...uiLanguage) sessionList {
	language := uiLanguageEnglish
	if len(lang) > 0 {
		language = lang[0]
	}
	return sessionList{sessions: sessions, lang: language}
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

func (s sessionList) View(width ...int) string {
	outerWidth := 0
	if len(width) > 0 {
		outerWidth = width[0]
	}
	contentWidth := outerWidth - 6
	if contentWidth < 0 {
		contentWidth = 0
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2)
	if contentWidth > 0 {
		style = style.Width(contentWidth)
	}

	if len(s.sessions) == 0 {
		return style.Render(truncateDisplay(s.lang.tr("No previous sessions found.", "没有历史会话。"), contentWidth))
	}

	var lines []string
	for i, sess := range s.sessions {
		cursor := "  "
		if i == s.cursor {
			cursor = "▸ "
		}
		line := fmt.Sprintf("%s%-18s  %-16s  %-12s  %s", cursor, sess.ID, sess.CreatedAt, sess.Cluster, sess.Summary)
		lines = append(lines, truncateDisplay(line, contentWidth))
	}

	panel := strings.Join(lines, "\n")
	title := truncateDisplay(s.lang.tr("Historical Sessions", "历史会话"), contentWidth)
	help := truncateDisplay(s.lang.tr("↑↓ Move  Enter Resume  Esc Cancel", "↑↓ 移动  Enter 恢复  Esc 取消"), contentWidth)
	return style.Render(fmt.Sprintf("%s\n\n%s\n\n%s", title, panel, help))
}

func truncateDisplay(s string, width int) string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "...")
}
