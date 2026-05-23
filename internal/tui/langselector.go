package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type languageOption struct {
	Language uiLanguage
	Label    string
}

type langSelector struct {
	options []languageOption
	cursor  int
	current uiLanguage
	lang    uiLanguage
}

func newLangSelector(current uiLanguage) langSelector {
	options := []languageOption{
		{Language: uiLanguageEnglish, Label: "English"},
		{Language: uiLanguageChinese, Label: "中文"},
	}
	cursor := 0
	for i, option := range options {
		if option.Language == current {
			cursor = i
			break
		}
	}
	return langSelector{
		options: options,
		cursor:  cursor,
		current: current,
		lang:    current,
	}
}

func (s langSelector) Update(msg tea.Msg) (langSelector, tea.Cmd) {
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
		if s.cursor < len(s.options)-1 {
			s.cursor++
		}
	}
	return s, nil
}

func (s langSelector) Selected() uiLanguage {
	if len(s.options) == 0 || s.cursor < 0 || s.cursor >= len(s.options) {
		return s.current
	}
	return s.options[s.cursor].Language
}

func (s langSelector) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	var lines []string
	lines = append(lines, titleStyle.Render(s.lang.tr("Select UI Language", "选择界面语言")))
	lines = append(lines, "")
	for i, option := range s.options {
		cursor := "  "
		nameStyle := lipgloss.NewStyle()
		if i == s.cursor {
			cursor = "▸ "
			nameStyle = selectedStyle
		}
		current := ""
		if option.Language == s.current {
			current = " " + mutedStyle.Render(s.lang.tr("(current)", "(当前)"))
		}
		lines = append(lines, fmt.Sprintf("%s%s%s", cursor, nameStyle.Render(option.Label), current))
	}
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render(s.lang.tr("↑↓ Move  Enter Confirm  Esc Cancel", "↑↓ 移动  Enter 确认  Esc 取消")))

	return panelStyle(0).Render(strings.Join(lines, "\n"))
}
