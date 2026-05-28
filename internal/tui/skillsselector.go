package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/internal/skills"
)

type skillManageEntry struct {
	Name        string
	Description string
	Scope       string
	Cluster     string
}

type skillsManager struct {
	entries []skillManageEntry
	cursor  int
	lang    uiLanguage
}

func newSkillsManager(entries []skillManageEntry, lang uiLanguage) skillsManager {
	return skillsManager{entries: append([]skillManageEntry(nil), entries...), lang: lang}
}

func (s skillsManager) WithEntries(entries []skillManageEntry) skillsManager {
	s.entries = append([]skillManageEntry(nil), entries...)
	if s.cursor >= len(s.entries) && s.cursor > 0 {
		s.cursor = len(s.entries) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
	return s
}

func (s skillsManager) Update(msg tea.Msg) (skillsManager, tea.Cmd) {
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
		if s.cursor < len(s.entries)-1 {
			s.cursor++
		}
	}
	return s, nil
}

func (s skillsManager) Selected() (skillManageEntry, bool) {
	if len(s.entries) == 0 || s.cursor < 0 || s.cursor >= len(s.entries) {
		return skillManageEntry{}, false
	}
	return s.entries[s.cursor], true
}

func (s skillsManager) RemoveSelected() skillsManager {
	if len(s.entries) == 0 || s.cursor < 0 || s.cursor >= len(s.entries) {
		return s
	}
	s.entries = append(append([]skillManageEntry(nil), s.entries[:s.cursor]...), s.entries[s.cursor+1:]...)
	if s.cursor >= len(s.entries) && s.cursor > 0 {
		s.cursor--
	}
	return s
}

func (s skillsManager) View(width int) string {
	contentWidth := width - 6
	if contentWidth < 0 {
		contentWidth = 0
	}
	var lines []string
	lines = append(lines, inputPromptStyle.Render(s.lang.tr("Skills", "技能")))
	lines = append(lines, "")
	if len(s.entries) == 0 {
		lines = append(lines, statusStyle.Render(s.lang.tr("No skills installed.", "没有已安装技能。")))
	} else {
		for i, entry := range s.entries {
			cursor := "  "
			nameStyle := lipgloss.NewStyle()
			if i == s.cursor {
				cursor = "▸ "
				nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
			}
			scope := entry.Scope
			if entry.Scope == skills.ScopeCluster {
				scope = "cluster:" + entry.Cluster
			}
			line := fmt.Sprintf("%s%s  %s  %s", cursor, nameStyle.Render(entry.Name), statusStyle.Render(scope), entry.Description)
			lines = append(lines, truncateDisplay(line, contentWidth))
		}
	}
	lines = append(lines, "")
	lines = append(lines, statusStyle.Render(s.lang.tr("↑↓ Move  d Uninstall  Esc Close", "↑↓ 移动  d 卸载  Esc 关闭")))
	return panelStyle(0).Render(strings.Join(lines, "\n"))
}

type skillInstallSelector struct {
	source  string
	skills  []skills.Skill
	cursor  int
	checked map[string]bool
	lang    uiLanguage
}

func newSkillInstallSelector(source string, discovered []skills.Skill, lang uiLanguage) skillInstallSelector {
	checked := make(map[string]bool, len(discovered))
	for _, skill := range discovered {
		checked[skill.Name] = true
	}
	return skillInstallSelector{
		source:  source,
		skills:  append([]skills.Skill(nil), discovered...),
		checked: checked,
		lang:    lang,
	}
}

func (s skillInstallSelector) Update(msg tea.Msg) (skillInstallSelector, tea.Cmd) {
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
		if s.cursor < len(s.skills)-1 {
			s.cursor++
		}
	case tea.KeySpace:
		if len(s.skills) > 0 && s.cursor >= 0 && s.cursor < len(s.skills) {
			name := s.skills[s.cursor].Name
			if s.checked[name] {
				delete(s.checked, name)
			} else {
				s.checked[name] = true
			}
		}
	}
	return s, nil
}

func (s skillInstallSelector) SelectedNames() []string {
	var names []string
	for _, skill := range s.skills {
		if s.checked[skill.Name] {
			names = append(names, skill.Name)
		}
	}
	return names
}

func (s skillInstallSelector) View(width int) string {
	contentWidth := width - 6
	if contentWidth < 0 {
		contentWidth = 0
	}
	var lines []string
	lines = append(lines, inputPromptStyle.Render(s.lang.tr("Select Skills to Install", "选择要安装的技能")))
	if strings.TrimSpace(s.source) != "" {
		lines = append(lines, statusStyle.Render(s.source))
	}
	lines = append(lines, "")
	if len(s.skills) == 0 {
		lines = append(lines, statusStyle.Render(s.lang.tr("No skills found.", "没有找到技能。")))
	} else {
		for i, skill := range s.skills {
			cursor := "  "
			nameStyle := lipgloss.NewStyle()
			if i == s.cursor {
				cursor = "▸ "
				nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
			}
			mark := "[ ]"
			if s.checked[skill.Name] {
				mark = "[x]"
			}
			line := fmt.Sprintf("%s%s %s  %s", cursor, mark, nameStyle.Render(skill.Name), skill.Description)
			lines = append(lines, truncateDisplay(line, contentWidth))
		}
	}
	lines = append(lines, "")
	lines = append(lines, statusStyle.Render(s.lang.tr("↑↓ Move  Space Select  Enter Install  Esc Cancel", "↑↓ 移动  Space 选择  Enter 安装  Esc 取消")))
	return panelStyle(0).Render(strings.Join(lines, "\n"))
}
