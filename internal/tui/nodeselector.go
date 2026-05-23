package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type nodeSelector struct {
	nodes   []NodeInfo
	cursor  int
	checked map[string]bool
	lang    uiLanguage
}

func newNodeSelector(nodes []NodeInfo, selected map[string]bool, lang ...uiLanguage) nodeSelector {
	language := uiLanguageEnglish
	if len(lang) > 0 {
		language = lang[0]
	}
	checked := make(map[string]bool)
	for name, ok := range selected {
		if ok {
			checked[name] = true
		}
	}
	return nodeSelector{
		nodes:   nodes,
		checked: checked,
		lang:    language,
	}
}

func (s nodeSelector) Update(msg tea.Msg) (nodeSelector, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if s.cursor > 0 {
				s.cursor--
			}
		case tea.KeyDown:
			if s.cursor < len(s.nodes)-1 {
				s.cursor++
			}
		case tea.KeySpace:
			if s.cursor < len(s.nodes) {
				node := s.nodes[s.cursor]
				if node.Online {
					if s.checked[node.Name] {
						delete(s.checked, node.Name)
					} else {
						s.checked[node.Name] = true
					}
				}
			}
		}
	}
	return s, nil
}

func (s nodeSelector) Selected() map[string]bool {
	return s.checked
}

func (s nodeSelector) SetNodes(nodes []NodeInfo) nodeSelector {
	s.nodes = nodes
	return s
}

func (s nodeSelector) View() string {
	if len(s.nodes) == 0 {
		return s.lang.tr("No nodes configured for this cluster.", "当前集群没有配置节点。")
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(s.lang.tr("Select Target Nodes", "选择目标节点")))
	b.WriteString("\n")

	for i, node := range s.nodes {
		cursor := " "
		if i == s.cursor {
			cursor = ">"
		}
		checked := "○"
		if s.checked[node.Name] {
			checked = "●"
		}

		status := "● " + s.lang.tr("Online", "在线")
		style := lipgloss.NewStyle()
		if !node.Online {
			status = "○ " + s.lang.tr("Offline", "离线")
			style = style.Foreground(lipgloss.Color("240"))
		}

		line := fmt.Sprintf(" %s %s  %-20s  %-15s  %s", cursor, checked, node.Name, node.Host, style.Render(status))
		b.WriteString(line)
		b.WriteString("\n")
	}

	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", 55))
	b.WriteString(sep)
	b.WriteString("\n")
	b.WriteString(s.lang.tr(" ↑↓ Move  Space Select  Enter Confirm  Esc Cancel", " ↑↓ 移动  Space 选择  Enter 确认  Esc 取消"))

	return b.String()
}
