package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/pkg/configschema"
)

type configItemType int

const (
	configString configItemType = iota
	configInt
	configBool
	configEnum
	configList
)

type configEditMode int

const (
	configBrowse configEditMode = iota
	configEditText
	configEditEnum
)

type configItem struct {
	Group   string
	Key     string
	Type    configItemType
	Value   string
	Options []string
}

type configScreen struct {
	global       *configschema.GlobalConfig
	items        []configItem
	selected     int
	editMode     configEditMode
	editValue    string
	enumSelected int
}

func newConfigScreen(global *configschema.GlobalConfig) configScreen {
	if global == nil {
		global = &configschema.GlobalConfig{}
	}
	s := configScreen{global: global}
	s.rebuildItems()
	return s
}

func (s *configScreen) rebuildItems() {
	g := s.global
	s.items = []configItem{
		{Group: "General", Key: "default_model", Type: configString, Value: g.DefaultModel},
		{Group: "General", Key: "default_cluster", Type: configString, Value: g.DefaultCluster},
		{Group: "General", Key: "ui_language", Type: configEnum, Value: g.UILanguage, Options: []string{"en-US", "zh-CN"}},
		{Group: "Logging", Key: "logging.level", Type: configEnum, Value: g.Logging.Level, Options: []string{"debug", "info", "warn", "error"}},
		{Group: "Logging", Key: "logging.file", Type: configString, Value: g.Logging.File},
		{Group: "Logging", Key: "logging.audit", Type: configBool, Value: formatBool(g.Logging.Audit)},
		{Group: "Security", Key: "security.risk_assessment_model", Type: configString, Value: g.Security.RiskAssessmentModel},
		{Group: "Security", Key: "security.local_file_whitelist", Type: configList, Value: strings.Join(g.Security.LocalFileWhitelist, ", ")},
		{Group: "Memory", Key: "memory.rules_token_budget", Type: configInt, Value: strconv.Itoa(g.Memory.RulesTokenBudget)},
		{Group: "Memory", Key: "memory.knowledge_token_budget", Type: configInt, Value: strconv.Itoa(g.Memory.KnowledgeTokenBudget)},
		{Group: "Subagents", Key: "subagents.enabled", Type: configBool, Value: formatBool(g.Subagents.Enabled)},
		{Group: "Subagents", Key: "subagents.max_parallel", Type: configInt, Value: strconv.Itoa(g.Subagents.MaxParallel)},
		{Group: "Subagents", Key: "subagents.default_model", Type: configString, Value: g.Subagents.DefaultModel},
		{Group: "Subagents", Key: "subagents.timeout_seconds", Type: configInt, Value: strconv.Itoa(g.Subagents.TimeoutSeconds)},
		{Group: "Subagents", Key: "subagents.debug", Type: configBool, Value: formatBool(g.Subagents.Debug)},
		{Group: "Vision", Key: "vision.model", Type: configString, Value: g.Vision.Model},
		{Group: "Vision", Key: "vision.max_images", Type: configInt, Value: strconv.Itoa(g.Vision.MaxImages)},
		{Group: "Vision", Key: "vision.max_summary_chars_per_image", Type: configInt, Value: strconv.Itoa(g.Vision.MaxSummaryCharsPerImage)},
	}
	if s.selected >= len(s.items) {
		s.selected = max(len(s.items)-1, 0)
	}
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func (s configScreen) SelectedKey() string {
	if len(s.items) == 0 || s.selected < 0 || s.selected >= len(s.items) {
		return ""
	}
	return s.items[s.selected].Key
}

func (s configScreen) SelectedItem() configItem {
	if len(s.items) == 0 || s.selected < 0 || s.selected >= len(s.items) {
		return configItem{}
	}
	return s.items[s.selected]
}

func (s configScreen) View(width int, lang uiLanguage) string {
	var lines []string
	title := lipgloss.NewStyle().Bold(true).Render("Global Config")
	lines = append(lines, title)
	lines = append(lines, lang.tr("Enter edit/toggle, ↑↓ move, r reload, Esc back", "Enter 编辑/切换，↑↓ 移动，r 重新加载，Esc 返回"))
	if s.editMode == configEditText {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s = %s", s.SelectedKey(), s.editValue))
		lines = append(lines, lang.tr("Enter save, Esc cancel editing", "Enter 保存，Esc 取消编辑"))
		return strings.Join(lines, "\n")
	}
	if s.editMode == configEditEnum {
		item := s.SelectedItem()
		lines = append(lines, "")
		lines = append(lines, item.Key)
		for i, opt := range item.Options {
			cursor := "  "
			if i == s.enumSelected {
				cursor = "> "
			}
			lines = append(lines, cursor+opt)
		}
		lines = append(lines, lang.tr("Enter save, Esc cancel selection", "Enter 保存，Esc 取消选择"))
		return strings.Join(lines, "\n")
	}

	lastGroup := ""
	for i, item := range s.items {
		if item.Group != lastGroup {
			lines = append(lines, "")
			lines = append(lines, lipgloss.NewStyle().Bold(true).Render(item.Group))
			lastGroup = item.Group
		}
		cursor := "  "
		if i == s.selected {
			cursor = "> "
		}
		value := item.Value
		if value == "" {
			value = "(empty)"
		}
		line := fmt.Sprintf("%s%-42s %s", cursor, item.Key, value)
		if width > 0 {
			line = truncateStr(line, max(width-1, 1))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *configScreen) Move(delta int) {
	if len(s.items) == 0 {
		return
	}
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.items) {
		s.selected = len(s.items) - 1
	}
}

func (s *configScreen) StartEdit() {
	item := s.SelectedItem()
	switch item.Type {
	case configEnum:
		s.editMode = configEditEnum
		s.enumSelected = 0
		for i, opt := range item.Options {
			if opt == item.Value {
				s.enumSelected = i
				break
			}
		}
	case configString, configInt, configList:
		s.editMode = configEditText
		s.editValue = item.Value
	case configBool:
		s.editMode = configBrowse
	}
}

func (s *configScreen) CancelEdit() {
	s.editMode = configBrowse
	s.editValue = ""
	s.enumSelected = 0
}

func (s *configScreen) MoveEnum(delta int) {
	item := s.SelectedItem()
	if len(item.Options) == 0 {
		return
	}
	s.enumSelected += delta
	if s.enumSelected < 0 {
		s.enumSelected = 0
	}
	if s.enumSelected >= len(item.Options) {
		s.enumSelected = len(item.Options) - 1
	}
}

func (s configScreen) EditedValue() string {
	if s.editMode == configEditEnum {
		item := s.SelectedItem()
		if s.enumSelected >= 0 && s.enumSelected < len(item.Options) {
			return item.Options[s.enumSelected]
		}
		return item.Value
	}
	return s.editValue
}

func (s *configScreen) SetValue(key string, value string) error {
	if s.global == nil {
		return fmt.Errorf("global config is nil")
	}
	g := s.global
	switch key {
	case "default_model":
		g.DefaultModel = strings.TrimSpace(value)
	case "default_cluster":
		g.DefaultCluster = strings.TrimSpace(value)
	case "ui_language":
		lang, ok := parseUILanguage(value)
		if !ok {
			return fmt.Errorf("invalid ui_language: %s", value)
		}
		g.UILanguage = lang.configValue()
	case "logging.level":
		value = strings.TrimSpace(value)
		if !oneOf(value, []string{"debug", "info", "warn", "error"}) {
			return fmt.Errorf("invalid logging.level: %s", value)
		}
		g.Logging.Level = value
	case "logging.file":
		g.Logging.File = strings.TrimSpace(value)
	case "logging.audit":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid logging.audit: %s", value)
		}
		g.Logging.Audit = parsed
	case "security.risk_assessment_model":
		g.Security.RiskAssessmentModel = strings.TrimSpace(value)
	case "security.local_file_whitelist":
		g.Security.LocalFileWhitelist = splitConfigList(value)
	case "memory.rules_token_budget":
		parsed, err := parsePositiveInt("memory.rules_token_budget", value)
		if err != nil {
			return err
		}
		g.Memory.RulesTokenBudget = parsed
	case "memory.knowledge_token_budget":
		parsed, err := parsePositiveInt("memory.knowledge_token_budget", value)
		if err != nil {
			return err
		}
		g.Memory.KnowledgeTokenBudget = parsed
	case "subagents.enabled":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid subagents.enabled: %s", value)
		}
		g.Subagents.Enabled = parsed
	case "subagents.max_parallel":
		parsed, err := parsePositiveInt("subagents.max_parallel", value)
		if err != nil {
			return err
		}
		g.Subagents.MaxParallel = parsed
	case "subagents.default_model":
		g.Subagents.DefaultModel = strings.TrimSpace(value)
	case "subagents.timeout_seconds":
		parsed, err := parsePositiveInt("subagents.timeout_seconds", value)
		if err != nil {
			return err
		}
		g.Subagents.TimeoutSeconds = parsed
	case "subagents.debug":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid subagents.debug: %s", value)
		}
		g.Subagents.Debug = parsed
	case "vision.model":
		g.Vision.Model = strings.TrimSpace(value)
	case "vision.max_images":
		parsed, err := parsePositiveInt("vision.max_images", value)
		if err != nil {
			return err
		}
		g.Vision.MaxImages = parsed
	case "vision.max_summary_chars_per_image":
		parsed, err := parsePositiveInt("vision.max_summary_chars_per_image", value)
		if err != nil {
			return err
		}
		g.Vision.MaxSummaryCharsPerImage = parsed
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	s.rebuildItems()
	return nil
}

func oneOf(value string, options []string) bool {
	for _, opt := range options {
		if value == opt {
			return true
		}
	}
	return false
}

func parsePositiveInt(key string, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: %s", key, value)
	}
	return parsed, nil
}

func splitConfigList(value string) []string {
	fields := strings.Split(value, ",")
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}
