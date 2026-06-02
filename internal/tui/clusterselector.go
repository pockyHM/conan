package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type clusterSelector struct {
	clusters []string
	cursor   int
	current  string
	lang     uiLanguage
}

func newClusterSelector(clusters []string, current string, lang uiLanguage) clusterSelector {
	cursor := 0
	for i, cluster := range clusters {
		if cluster == current {
			cursor = i
			break
		}
	}
	return clusterSelector{
		clusters: append([]string(nil), clusters...),
		cursor:   cursor,
		current:  current,
		lang:     lang,
	}
}

func (s clusterSelector) Update(msg tea.Msg) (clusterSelector, tea.Cmd) {
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
		if s.cursor < len(s.clusters)-1 {
			s.cursor++
		}
	}
	return s, nil
}

func (s clusterSelector) Selected() (string, bool) {
	if len(s.clusters) == 0 || s.cursor < 0 || s.cursor >= len(s.clusters) {
		return "", false
	}
	return s.clusters[s.cursor], true
}

func (s clusterSelector) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	var lines []string
	lines = append(lines, titleStyle.Render(s.lang.tr("Select Cluster", "选择集群")))
	lines = append(lines, "")
	for i, cluster := range s.clusters {
		cursor := "  "
		nameStyle := lipgloss.NewStyle()
		if i == s.cursor {
			cursor = "▸ "
			nameStyle = selectedStyle
		}
		current := ""
		if cluster == s.current {
			current = " " + mutedStyle.Render(s.lang.tr("(current)", "(当前)"))
		}
		lines = append(lines, fmt.Sprintf("%s%s%s", cursor, nameStyle.Render(cluster), current))
	}
	if len(s.clusters) == 0 {
		lines = append(lines, mutedStyle.Render(s.lang.tr("No configured clusters", "没有已配置集群")))
	}
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render(s.lang.tr("↑↓ Move  Enter Confirm  Esc Cancel", "↑↓ 移动  Enter 确认  Esc 取消")))

	return panelStyle(0).Render(strings.Join(lines, "\n"))
}
