package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tracePageBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)

	tracePageTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				Padding(0, 1)

	tracePageMutedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	tracePageHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true)
)

func (m Model) renderTracePage() string {
	title := tracePageTitleStyle.Render(m.uiLanguage.tr("Trace", "链路"))
	empty := tracePageMutedStyle.Render(m.uiLanguage.tr("No trace nodes yet", "还没有链路节点"))
	help := tracePageHelpStyle.Render(m.uiLanguage.tr("Esc close", "Esc 关闭"))

	return tracePageBoxStyle.Render(strings.Join([]string{title, "", empty, "", help}, "\n"))
}

func (m Model) handleTraceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
		m.mode = modeChat
		m.traceDetailVisible = false
		m.status = m.uiLanguage.tr("Trace closed", "已关闭链路")
		return m, nil
	}
	return m, nil
}
