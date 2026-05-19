package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ModelConfig struct {
	Cluster string
	Model   string
}

type Model struct {
	cluster  string
	model    string
	input    string
	messages []string
	status   string
}

func NewModel(cfg ModelConfig) Model {
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	return Model{cluster: cfg.Cluster, model: cfg.Model, status: "Ready"}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyCtrlL:
		m.messages = nil
		m.status = "Conversation cleared"
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyRunes:
		m.input += string(key.Runes)
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Conan | Cluster: %s | Model: %s", m.cluster, m.model))
	body := strings.Join(m.messages, "\n")
	if body == "" {
		body = "No messages yet. Type a message or /help."
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s\n> %s", header, body, m.status, m.input)
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	if input == "" {
		return m, nil
	}
	if cmd, ok := ParseSlashCommand(input); ok {
		m = m.applyCommand(cmd)
		if cmd.Kind == CommandExit {
			return m, tea.Quit
		}
		return m, nil
	}
	m.messages = append(m.messages, "You: "+input)
	m.status = "Message queued; LLM integration is not implemented yet"
	return m, nil
}

func (m Model) applyCommand(cmd SlashCommand) Model {
	switch cmd.Kind {
	case CommandHelp:
		m.messages = append(m.messages, "Conan: /help /clear /exit /cluster [name] /model [name] /nodes")
		m.status = "Help shown"
	case CommandClear:
		m.messages = nil
		m.status = "Conversation cleared"
	case CommandExit:
		m.status = "Exit requested"
	case CommandCluster:
		if cmd.Arg != "" {
			m.cluster = cmd.Arg
			m.status = "Cluster switched to " + cmd.Arg
		} else {
			m.status = "Current cluster: " + m.cluster
		}
	case CommandModel:
		if cmd.Arg != "" {
			m.model = cmd.Arg
			m.status = "Model switched to " + cmd.Arg
		} else {
			m.status = "Current model: " + m.model
		}
	case CommandNodes:
		m.status = "Interactive node selection is not implemented yet"
	default:
		m.status = "Unknown command: " + cmd.Arg
	}
	return m
}
