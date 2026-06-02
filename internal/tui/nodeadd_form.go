package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/internal/nodeadd"
)

type nodeAddFormField int

const (
	nodeAddFormFieldName nodeAddFormField = iota
	nodeAddFormFieldHost
	nodeAddFormFieldPort
	nodeAddFormFieldUser
	nodeAddFormFieldPassword
)

type nodeAddFormValues struct {
	Name      string
	Host      string
	Names     []string
	Hosts     []string
	AgentPort int
	User      string
	Password  string
}

type nodeAddForm struct {
	lang   uiLanguage
	cursor nodeAddFormField
	values map[nodeAddFormField]string
	edited map[nodeAddFormField]bool
	err    string
}

func newNodeAddForm(lang uiLanguage) nodeAddForm {
	return nodeAddForm{
		lang: lang,
		values: map[nodeAddFormField]string{
			nodeAddFormFieldPort: "9280",
		},
		edited: make(map[nodeAddFormField]bool),
	}
}

func (f nodeAddForm) withValue(field nodeAddFormField, value string) nodeAddForm {
	f.ensureMaps()
	f.values[field] = value
	f.edited[field] = true
	return f
}

func (f nodeAddForm) withError(err string) nodeAddForm {
	f.err = err
	return f
}

func (f nodeAddForm) Update(key tea.KeyMsg) (nodeAddForm, bool) {
	f.err = ""
	switch key.Type {
	case tea.KeyTab:
		return f.next(), false
	case tea.KeyEnter:
		if f.cursor == nodeAddFormFieldPassword {
			return f, true
		}
		return f.next(), false
	case tea.KeyBackspace:
		f.ensureMaps()
		f.edited[f.cursor] = true
		if f.cursor == nodeAddFormFieldPort && f.values[f.cursor] == "9280" {
			f.values[f.cursor] = ""
			return f, false
		}
		current := []rune(f.values[f.cursor])
		if len(current) > 0 {
			f.values[f.cursor] = string(current[:len(current)-1])
		}
	case tea.KeySpace:
		f.ensureMaps()
		f.edited[f.cursor] = true
		f.values[f.cursor] += " "
	case tea.KeyRunes:
		f.ensureMaps()
		if f.cursor == nodeAddFormFieldPort && !f.edited[f.cursor] {
			f.values[f.cursor] = ""
		}
		f.edited[f.cursor] = true
		f.values[f.cursor] += string(key.Runes)
	}
	return f, false
}

func (f *nodeAddForm) ensureMaps() {
	if f.values == nil {
		f.values = make(map[nodeAddFormField]string)
	}
	if f.edited == nil {
		f.edited = make(map[nodeAddFormField]bool)
	}
}

func (f nodeAddForm) next() nodeAddForm {
	if f.cursor < nodeAddFormFieldPassword {
		f.cursor++
	}
	return f
}

func (f nodeAddForm) Values() (nodeAddFormValues, error) {
	name := strings.TrimSpace(f.values[nodeAddFormFieldName])
	host := strings.TrimSpace(f.values[nodeAddFormFieldHost])
	portText := strings.TrimSpace(f.values[nodeAddFormFieldPort])
	user := strings.TrimSpace(f.values[nodeAddFormFieldUser])
	password := f.values[nodeAddFormFieldPassword]
	names := nodeadd.SplitCommaList(name)
	hosts := nodeadd.SplitCommaList(host)

	if len(names) == 0 && len(hosts) <= 1 {
		return nodeAddFormValues{}, fmt.Errorf("name is required")
	}
	if len(hosts) == 0 {
		return nodeAddFormValues{}, fmt.Errorf("host is required")
	}
	if len(names) > 0 && len(names) != len(hosts) {
		return nodeAddFormValues{}, fmt.Errorf("name must be empty or contain %d comma-separated values", len(hosts))
	}
	if portText == "" {
		portText = "9280"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nodeAddFormValues{}, fmt.Errorf("agent port must be a number")
	}
	if port <= 0 || port > 65535 {
		return nodeAddFormValues{}, fmt.Errorf("agent port must be between 1 and 65535")
	}
	if user == "" {
		return nodeAddFormValues{}, fmt.Errorf("username is required")
	}
	if strings.TrimSpace(password) == "" {
		return nodeAddFormValues{}, fmt.Errorf("password is required")
	}
	return nodeAddFormValues{Name: strings.Join(names, ","), Host: strings.Join(hosts, ","), Names: names, Hosts: hosts, AgentPort: port, User: user, Password: password}, nil
}

func (f nodeAddForm) View() string {
	rows := []struct {
		field  nodeAddFormField
		label  string
		secret bool
	}{
		{nodeAddFormFieldName, f.lang.tr("Name", "名称"), false},
		{nodeAddFormFieldHost, f.lang.tr("Host/IP", "Host/IP"), false},
		{nodeAddFormFieldPort, f.lang.tr("Agent port", "Agent 端口"), false},
		{nodeAddFormFieldUser, f.lang.tr("SSH username", "SSH 用户名"), false},
		{nodeAddFormFieldPassword, f.lang.tr("SSH password", "SSH 密码"), true},
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(f.lang.tr("Add New Node", "添加新节点")))
	b.WriteString("\n")
	for _, row := range rows {
		cursor := " "
		if f.cursor == row.field {
			cursor = ">"
		}
		value := f.values[row.field]
		if row.secret && value != "" {
			value = strings.Repeat("*", len([]rune(value)))
		}
		if value == "" {
			value = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(f.lang.tr("(empty)", "(空)"))
		}
		b.WriteString(fmt.Sprintf(" %s %-14s %s\n", cursor, row.label, value))
	}
	if f.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(f.err))
		b.WriteString("\n")
	}
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", 55))
	b.WriteString(sep)
	b.WriteString("\n")
	b.WriteString(f.lang.tr(" Tab/Enter Next  Enter Submit  Esc Cancel", " Tab/Enter 下一项  Enter 提交  Esc 取消"))
	return b.String()
}
