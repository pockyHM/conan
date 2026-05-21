package tui

import (
	"fmt"
	"strings"
	"time"

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

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

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

func renderMessageDivider(elapsed time.Duration) string {
	label := formatElapsed(elapsed)
	if label == "" {
		return statusStyle.Render(strings.Repeat("─", 32))
	}
	return statusStyle.Render(strings.Repeat("─", 12) + " " + label + " " + strings.Repeat("─", 12))
}

func renderInputBox(input string) string {
	return inputBoxStyle.Render(inputPromptStyle.Render("❯ ") + input)
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed <= 0 {
		return ""
	}
	if elapsed < time.Second {
		return fmt.Sprintf("%dms", elapsed.Milliseconds())
	}
	if elapsed < 10*time.Second {
		return fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	return fmt.Sprintf("%.0fs", elapsed.Seconds())
}

func renderToolHeader(name string, nodeCount int) string {
	if nodeCount > 1 {
		return toolStyle.Render(fmt.Sprintf("⏚ %s on %d node(s)", name, nodeCount))
	}
	return toolStyle.Render(fmt.Sprintf("⏚ %s", name))
}

func renderToolNode(node string, success bool, output string, expanded bool) string {
	icon := toolSuccess.Render("✓")
	if !success {
		icon = toolFailure.Render("✗")
	}
	output = renderToolOutput(output, expanded)
	return fmt.Sprintf("  %s %s  %s", icon, toolStyle.Render(node), output)
}

func renderToolOutput(output string, expanded bool) string {
	lines := strings.Split(formatToolOutputForDisplay(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if expanded || len(lines) <= toolOutputPreviewLines {
		return toolStyle.Render(strings.Join(lines, "\n"))
	}
	preview := strings.Join(lines[:toolOutputPreviewLines], "\n")
	remaining := len(lines) - toolOutputPreviewLines
	return toolStyle.Render(preview) + "\n" + statusStyle.Render(fmt.Sprintf("… %d more line(s), press Ctrl+O to expand", remaining))
}

func formatToolOutputForDisplay(output string) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var display []string
	start := 0
	if status, ok := strings.CutPrefix(strings.TrimSpace(lines[0]), "exit_code:"); ok {
		display = append(display, "status:"+status)
		start = 1
	}
	for _, line := range lines[start:] {
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "stdout:", "stderr:":
			continue
		default:
			display = append(display, line)
		}
	}
	return strings.Join(trimEmptyEdges(display), "\n")
}

func trimEmptyEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

func renderHeader(cluster, model string, selectedNodes, totalNodes int) string {
	return headerKeyStyle.Render("Conan") + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(cluster) + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(model) + headerSepStyle.Render(" │ ") +
		fmt.Sprintf("%d/%d nodes", selectedNodes, totalNodes)
}
