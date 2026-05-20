package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	toolSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	toolFailure = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	headerKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)

	headerValStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	headerSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

var mdRenderer *glamour.TermRenderer

func init() {
	var err error
	mdRenderer, err = glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		mdRenderer, _ = glamour.NewTermRenderer(glamour.WithWordWrap(80))
	}
}

func renderMarkdown(text string) string {
	if mdRenderer == nil {
		return text
	}
	rendered, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSpace(rendered)
}

func renderUserMsg(content string) string {
	return userStyle.Render("❯ ") + content
}

func renderAssistantMsg(content string) string {
	return renderMarkdown(content)
}

func renderStreamingMsg(content string) string {
	return renderMarkdown(content) + "▌"
}

func renderToolHeader(name string, nodeCount int) string {
	if nodeCount > 1 {
		return toolStyle.Render(fmt.Sprintf("⏚ %s on %d node(s)", name, nodeCount))
	}
	return toolStyle.Render(fmt.Sprintf("⏚ %s", name))
}

func renderToolNode(node string, success bool, output string) string {
	icon := toolSuccess.Render("✓")
	if !success {
		icon = toolFailure.Render("✗")
	}
	if idx := strings.Index(output, "\n"); idx != -1 {
		output = output[:idx]
	}
	if len(output) > 60 {
		output = output[:57] + "..."
	}
	return fmt.Sprintf("  %s %s  %s", icon, toolStyle.Render(node), output)
}

func renderHeader(cluster, model string, selectedNodes, totalNodes int) string {
	return headerKeyStyle.Render("Conan") + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(cluster) + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(model) + headerSepStyle.Render(" │ ") +
		fmt.Sprintf("%d/%d nodes", selectedNodes, totalNodes)
}
