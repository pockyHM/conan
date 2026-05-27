package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/pkg/configschema"
)

type modelSelector struct {
	models  []configschema.ModelConfig
	cursor  int
	current string
	lang    uiLanguage
}

func newModelSelector(models []configschema.ModelConfig, current string, lang uiLanguage) modelSelector {
	cursor := 0
	for i, model := range models {
		if model.Name == current {
			cursor = i
			break
		}
	}
	return modelSelector{
		models:  append([]configschema.ModelConfig(nil), models...),
		cursor:  cursor,
		current: current,
		lang:    lang,
	}
}

func (s modelSelector) Update(msg tea.Msg) (modelSelector, tea.Cmd) {
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
		if s.cursor < len(s.models)-1 {
			s.cursor++
		}
	}
	return s, nil
}

func (s modelSelector) Selected() (configschema.ModelConfig, bool) {
	if len(s.models) == 0 || s.cursor < 0 || s.cursor >= len(s.models) {
		return configschema.ModelConfig{}, false
	}
	return s.models[s.cursor], true
}

func (s modelSelector) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	var lines []string
	lines = append(lines, titleStyle.Render(s.lang.tr("Select Model", "选择模型")))
	lines = append(lines, "")
	for i, model := range s.models {
		cursor := "  "
		nameStyle := lipgloss.NewStyle()
		if i == s.cursor {
			cursor = "▸ "
			nameStyle = selectedStyle
		}
		current := ""
		if model.Name == s.current {
			current = " " + mutedStyle.Render(s.lang.tr("(current)", "(当前)"))
		}
		detail := strings.TrimSpace(fmt.Sprintf("%s  %s", model.Type, model.Model))
		if detail != "" {
			detail = " " + mutedStyle.Render(detail)
		}
		lines = append(lines, fmt.Sprintf("%s%s%s%s", cursor, nameStyle.Render(model.Name), detail, current))
	}
	if len(s.models) == 0 {
		lines = append(lines, mutedStyle.Render(s.lang.tr("No configured models", "没有已配置模型")))
	}
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render(s.lang.tr("↑↓ Move  Enter Confirm  Esc Cancel", "↑↓ 移动  Enter 确认  Esc 取消")))

	return panelStyle(0).Render(strings.Join(lines, "\n"))
}
