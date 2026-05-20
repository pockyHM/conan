package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type commandInfo struct {
	Name        string
	Description string
	ArgHint     string
}

var commandRegistry = []commandInfo{
	{Name: "help", Description: "Show help information"},
	{Name: "clear", Description: "Clear conversation"},
	{Name: "exit", Description: "Exit Conan"},
	{Name: "cluster", Description: "Switch/display cluster", ArgHint: "[name]"},
	{Name: "model", Description: "Switch/display model", ArgHint: "[name]"},
	{Name: "nodes", Description: "Open node selector"},
	{Name: "memory", Description: "View memory summary"},
	{Name: "resume", Description: "Resume session", ArgHint: "[id]"},
}

type autocomplete struct {
	visible  bool
	selected int
	prefix   string
}

func newAutocomplete() autocomplete {
	return autocomplete{}
}

func (a autocomplete) update(input string) autocomplete {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		a.visible = false
		return a
	}
	a.visible = true
	a.prefix = strings.TrimPrefix(input, "/")
	if a.selected >= len(a.filtered()) {
		a.selected = 0
	}
	return a
}

func (a autocomplete) filtered() []commandInfo {
	if a.prefix == "" {
		return commandRegistry
	}
	var result []commandInfo
	for _, cmd := range commandRegistry {
		if strings.HasPrefix(cmd.Name, a.prefix) {
			result = append(result, cmd)
		}
	}
	return result
}

func (a autocomplete) moveUp() autocomplete {
	if a.selected > 0 {
		a.selected--
	}
	return a
}

func (a autocomplete) moveDown() autocomplete {
	filtered := a.filtered()
	if a.selected < len(filtered)-1 {
		a.selected++
	}
	return a
}

func (a autocomplete) completion() string {
	filtered := a.filtered()
	if a.selected < len(filtered) {
		return "/" + filtered[a.selected].Name + " "
	}
	return ""
}

func (a autocomplete) View() string {
	if !a.visible {
		return ""
	}
	filtered := a.filtered()
	if len(filtered) == 0 {
		return ""
	}

	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	var lines []string
	for i, cmd := range filtered {
		cursor := "  "
		nameStyle := normalStyle
		if i == a.selected {
			cursor = "▸ "
			nameStyle = selectedStyle
		}
		hint := ""
		if cmd.ArgHint != "" {
			hint = " " + cmd.ArgHint
		}
		line := fmt.Sprintf("%s%s  %s", cursor, nameStyle.Render("/"+cmd.Name+hint), descStyle.Render(cmd.Description))
		lines = append(lines, line)
	}

	panel := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(panel)
}
