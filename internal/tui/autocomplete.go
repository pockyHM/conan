package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

type commandInfo struct {
	Name        string
	Description string
	ArgHint     string
	Skill       bool
	Category    commandCategory
}

type commandCategory int

const (
	commandCategoryBuiltin commandCategory = iota
	commandCategorySystem
	commandCategorySkill
)

var commandRegistry = []commandInfo{
	{Name: "help", Description: "Show help information", Category: commandCategoryBuiltin},
	{Name: "clear", Description: "Clear conversation", Category: commandCategoryBuiltin},
	{Name: "compact", Description: "Compact conversation context", ArgHint: "[focus]", Category: commandCategoryBuiltin},
	{Name: "exit", Description: "Exit Conan", Category: commandCategoryBuiltin},
	{Name: "cluster", Description: "Switch/display cluster", ArgHint: "[name]", Category: commandCategoryBuiltin},
	{Name: "config", Description: "Open global configuration", Category: commandCategoryBuiltin},
	{Name: "skills", Description: "Manage skills or invoke a skill", ArgHint: "[install|remove|update|<skill>]", Category: commandCategoryBuiltin},
	{Name: "skill", Description: "Use a skill", ArgHint: "<name> [arguments]", Category: commandCategoryBuiltin},
	{Name: "lang", Description: "Change UI language", Category: commandCategoryBuiltin},
	{Name: "model", Description: "Switch/display model", ArgHint: "[name]", Category: commandCategoryBuiltin},
	{Name: "resume", Description: "Resume session", ArgHint: "[id]", Category: commandCategoryBuiltin},
	{Name: "thinking", Description: "Send one message with thinking enabled", ArgHint: "<message>", Category: commandCategoryBuiltin},
	{Name: "node", Description: "Enable node management tools", ArgHint: "[off]", Category: commandCategorySystem},
	{Name: "nodes", Description: "Open node selector", Category: commandCategorySystem},
	{Name: "memory", Description: "View memory summary", Category: commandCategorySystem},
	{Name: "agent", Description: "Run a local subagent", ArgHint: "<role> <task>", Category: commandCategorySystem},
	{Name: "subagents", Description: "Manage local subagents", ArgHint: "[on|off|limit]", Category: commandCategorySystem},
	{Name: "incident", Description: "Manage incident notes", ArgHint: "<start|status|note|export|close>", Category: commandCategorySystem},
	{Name: "runbook", Description: "Draft, preview, or run runbooks", ArgHint: "<draft|preview|run>", Category: commandCategorySystem},
}

type autocomplete struct {
	visible     bool
	selected    int
	prefix      string
	mode        autocompleteMode
	lang        uiLanguage
	input       string
	commands    []commandInfo
	tokenStart  int
	fileMatches []fileCompletion
}

type autocompleteMode int

const (
	autocompleteCommands autocompleteMode = iota
	autocompleteFiles
)

type fileCompletion struct {
	Path  string
	IsDir bool
}

func newAutocomplete() autocomplete {
	return newAutocompleteWithLanguage(uiLanguageEnglish)
}

func newAutocompleteWithLanguage(lang uiLanguage) autocomplete {
	return autocomplete{lang: lang, commands: commandRegistry}
}

func (a autocomplete) withCommands(commands []commandInfo) autocomplete {
	if len(commands) == 0 {
		a.commands = commandRegistry
		return a
	}
	a.commands = append([]commandInfo(nil), commands...)
	if a.selected >= len(a.filtered()) {
		a.selected = 0
	}
	return a
}

func (a autocomplete) update(input string) autocomplete {
	return a.updateWithRoot(input, "")
}

func (a autocomplete) updateWithRoot(input string, root string) autocomplete {
	a.input = input
	a.fileMatches = nil
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		if updated, ok := a.updateFileRefs(input, root); ok {
			return updated
		}
		return a.hide()
	}
	a.visible = true
	a.mode = autocompleteCommands
	a.prefix = strings.TrimPrefix(input, "/")
	if a.selected >= len(a.filtered()) {
		a.selected = 0
	}
	return a
}

func (a autocomplete) hide() autocomplete {
	a.visible = false
	a.prefix = ""
	a.fileMatches = nil
	a.tokenStart = 0
	return a
}

func (a autocomplete) updateFileRefs(input string, root string) (autocomplete, bool) {
	if root == "" {
		root = "."
	}
	start, prefix, ok := activeAtToken(input)
	if !ok {
		return a, false
	}
	matches := fileCompletions(root, prefix)
	if len(matches) == 0 {
		return a.hide(), true
	}
	a.visible = true
	a.mode = autocompleteFiles
	a.prefix = prefix
	a.tokenStart = start
	a.fileMatches = matches
	if a.selected >= len(matches) {
		a.selected = 0
	}
	return a, true
}

func activeAtToken(input string) (int, string, bool) {
	runes := []rune(input)
	for i := len(runes) - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) {
			break
		}
		if runes[i] != '@' {
			continue
		}
		if i > 0 && runes[i-1] == '@' {
			return 0, "", false
		}
		if i > 0 && !unicode.IsSpace(runes[i-1]) {
			return 0, "", false
		}
		prefix := string(runes[i+1:])
		if strings.Contains(prefix, "\"") {
			return 0, "", false
		}
		return len(string(runes[:i])), prefix, true
	}
	return 0, "", false
}

func fileCompletions(root string, prefix string) []fileCompletion {
	cleanPrefix := filepath.Clean(prefix)
	if prefix == "" {
		cleanPrefix = "."
	}
	if filepath.IsAbs(cleanPrefix) || cleanPrefix == ".." || strings.HasPrefix(cleanPrefix, ".."+string(filepath.Separator)) {
		return nil
	}
	dirPart := filepath.Dir(cleanPrefix)
	basePart := filepath.Base(cleanPrefix)
	if strings.HasSuffix(prefix, "/") || prefix == "" {
		dirPart = cleanPrefix
		basePart = ""
	}
	if dirPart == "." {
		dirPart = ""
	}
	dir := filepath.Join(root, dirPart)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var matches []fileCompletion
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(name, basePart) {
			continue
		}
		path := filepath.ToSlash(filepath.Join(dirPart, name))
		if entry.IsDir() {
			path += "/"
		}
		matches = append(matches, fileCompletion{Path: path, IsDir: entry.IsDir()})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].IsDir != matches[j].IsDir {
			return matches[i].IsDir
		}
		return matches[i].Path < matches[j].Path
	})
	return matches
}

func (a autocomplete) filtered() []commandInfo {
	if a.mode == autocompleteFiles {
		return nil
	}
	commands := a.commands
	if len(commands) == 0 {
		commands = commandRegistry
	}
	if a.prefix == "" {
		return commands
	}
	var result []commandInfo
	for _, cmd := range commands {
		if strings.HasPrefix(cmd.Name, a.prefix) {
			result = append(result, cmd)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].normalizedCategory()
		right := result[j].normalizedCategory()
		if left != right {
			return left < right
		}
		if result[i].Name == a.prefix {
			return true
		}
		if result[j].Name == a.prefix {
			return false
		}
		return false
	})
	return result
}

func (c commandInfo) normalizedCategory() commandCategory {
	if c.Skill || c.Category == commandCategorySkill {
		return commandCategorySkill
	}
	if c.Category == commandCategorySystem {
		return commandCategorySystem
	}
	return commandCategoryBuiltin
}

func (a autocomplete) moveUp() autocomplete {
	if a.selected > 0 {
		a.selected--
	}
	return a
}

func (a autocomplete) moveDown() autocomplete {
	count := len(a.filtered())
	if a.mode == autocompleteFiles {
		count = len(a.fileMatches)
	}
	if a.selected < count-1 {
		a.selected++
	}
	return a
}

func (a autocomplete) completion() string {
	if a.mode == autocompleteFiles {
		if a.selected < len(a.fileMatches) {
			return a.input[:a.tokenStart] + "@" + a.fileMatches[a.selected].Path + completionSuffix(a.fileMatches[a.selected])
		}
		return ""
	}
	filtered := a.filtered()
	if a.selected < len(filtered) {
		return "/" + filtered[a.selected].Name + " "
	}
	return ""
}

func (a autocomplete) View(width int) string {
	if !a.visible {
		return ""
	}
	var lines []string
	if a.mode == autocompleteFiles {
		if len(a.fileMatches) == 0 {
			return ""
		}
		lines = a.fileLines()
	} else {
		filtered := a.filtered()
		if len(filtered) == 0 {
			return ""
		}
		lines = a.commandLines(filtered, autocompleteContentWidth(width))
	}
	panel := strings.Join(lines, "\n")
	style := strings.TrimSpace(panelStyle(width).Render(panel))
	return style
}

func autocompleteContentWidth(width int) int {
	if width <= 0 {
		return 0
	}
	contentWidth := width - 6
	if contentWidth < 1 {
		return 1
	}
	return contentWidth
}

func (a autocomplete) commandLines(filtered []commandInfo, contentWidth int) []string {
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	lines := []string{a.commandLegendLine(contentWidth)}
	for i, cmd := range filtered {
		cursor := "  "
		nameStyle := commandCategoryStyle(cmd.normalizedCategory(), false)
		if i == a.selected {
			cursor = "▸ "
			nameStyle = commandCategoryStyle(cmd.normalizedCategory(), true)
		}
		hint := ""
		if cmd.ArgHint != "" {
			hint = " " + cmd.ArgHint
		}
		description := a.lang.commandDescription(cmd.Name)
		if description == "" {
			description = cmd.Description
		}
		nameText, descText := autocompleteLineParts("/"+cmd.Name+hint, description, contentWidth)
		line := fmt.Sprintf("%s%s", cursor, nameStyle.Render(nameText))
		if descText != "" {
			line += "  " + descStyle.Render(descText)
		}
		lines = append(lines, line)
	}
	return lines
}

func (a autocomplete) commandLegendLine(contentWidth int) string {
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	legend := descStyle.Render(a.lang.tr("Legend:", "图例:")) + " " +
		commandCategoryStyle(commandCategoryBuiltin, false).Render(a.lang.tr("Built-in", "内置")) + "  " +
		commandCategoryStyle(commandCategorySystem, false).Render(a.lang.tr("System", "系统")) + "  " +
		commandCategoryStyle(commandCategorySkill, false).Render(a.lang.tr("Skill", "技能"))
	return truncateDisplay(legend, contentWidth)
}

func commandCategoryStyle(category commandCategory, selected bool) lipgloss.Style {
	color := lipgloss.Color("252")
	switch category {
	case commandCategoryBuiltin:
		color = lipgloss.Color("14")
	case commandCategorySystem:
		color = lipgloss.Color("220")
	case commandCategorySkill:
		color = lipgloss.Color("82")
	}
	style := lipgloss.NewStyle().Foreground(color)
	if selected {
		style = style.Bold(true)
	}
	return style
}

func autocompleteLineParts(name string, description string, contentWidth int) (string, string) {
	if contentWidth <= 0 {
		return name, description
	}
	available := contentWidth - 2
	if available <= 0 {
		return "", ""
	}
	nameBudget := available
	if description != "" && available > 24 {
		nameBudget = min(42, available)
	}
	name = truncateDisplay(name, nameBudget)
	descBudget := available - lipgloss.Width(name) - 2
	if descBudget <= 0 || description == "" {
		return name, ""
	}
	return name, truncateDisplay(description, descBudget)
}

func (a autocomplete) fileLines() []string {
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	var lines []string
	for i, match := range a.fileMatches {
		cursor := "  "
		nameStyle := normalStyle
		if i == a.selected {
			cursor = "▸ "
			nameStyle = selectedStyle
		}
		kind := a.lang.tr("file", "文件")
		if match.IsDir {
			kind = a.lang.tr("dir", "目录")
		}
		lines = append(lines, fmt.Sprintf("%s%s  %s", cursor, nameStyle.Render("@"+match.Path), descStyle.Render(kind)))
	}
	return lines
}

func panelStyle(width int) lipgloss.Style {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	if width > 0 {
		style = style.Width(max(width-2, 1))
	}
	return style
}

func completionSuffix(match fileCompletion) string {
	if match.IsDir {
		return ""
	}
	return " "
}
