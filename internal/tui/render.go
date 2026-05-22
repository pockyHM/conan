package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	userPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	userMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("236"))

	thinkingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("209")).
			Bold(true)

	reasoningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

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

var thinkingFrames = []string{"◐", "◓", "◑", "◒"}

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

func renderUserMsg(content string, width int) string {
	style := userMessageStyle
	if width > 0 {
		style = style.Width(width)
	}
	return style.Render(userPromptStyle.Render("❯ ") + content)
}

func renderAssistantMsg(content string) string {
	return renderMarkdown(content)
}

func renderThinkingMsg(frame int, elapsed time.Duration) string {
	if len(thinkingFrames) == 0 {
		return thinkingStyle.Render("◦ Thinking... " + renderThinkingMeta(elapsed))
	}
	icon := thinkingFrames[frame%len(thinkingFrames)]
	return thinkingStyle.Render(icon + " Thinking... " + renderThinkingMeta(elapsed))
}

func renderThinkingMeta(elapsed time.Duration) string {
	label := formatElapsed(elapsed)
	if label == "" {
		return "Esc to interrupt"
	}
	return label + "  Esc to interrupt"
}

func renderStreamingMsg(content string) string {
	return renderMarkdown(content) + "▌"
}

func renderReasoningMsg(content string) string {
	line := lastNonEmptyLine(content)
	if line == "" {
		line = strings.TrimSpace(content)
	}
	return reasoningStyle.Render("◦ Thinking: " + line)
}

func lastNonEmptyLine(content string) string {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func renderElapsedFooter(elapsed time.Duration) string {
	label := formatElapsed(elapsed)
	if label == "" {
		return ""
	}
	return statusStyle.Render("✱ Took " + label)
}

func renderInputBox(input string, width int) string {
	style := inputBoxStyle
	if width > 0 {
		style = style.Width(max(width-2, 1))
	}
	return style.Render(inputPromptStyle.Render("❯ ") + input + "█")
}

func renderStartupOverview(cluster, model string, nodes []NodeInfo, selected map[string]bool) string {
	const maxNodeRows = 5
	wordmark := strings.Join([]string{
		" ██████╗ ██████╗ ███╗   ██╗ █████╗ ███╗   ██╗",
		"██╔════╝██╔═══██╗████╗  ██║██╔══██╗████╗  ██║",
		"██║     ██║   ██║██╔██╗ ██║███████║██╔██╗ ██║",
		"██║     ██║   ██║██║╚██╗██║██╔══██║██║╚██╗██║",
		"╚██████╗╚██████╔╝██║ ╚████║██║  ██║██║ ╚████║",
		" ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═╝╚═╝  ╚═══╝",
	}, "\n")

	selectedCount := 0
	for _, ok := range selected {
		if ok {
			selectedCount++
		}
	}
	onlineCount := 0
	for _, node := range nodes {
		if node.Online {
			onlineCount++
		}
	}

	var b strings.Builder
	b.WriteString(inputPromptStyle.Render(wordmark))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Cluster   %s\n", cluster))
	b.WriteString(fmt.Sprintf("Model     %s\n", model))
	b.WriteString(fmt.Sprintf("Nodes     %d/%d selected, %d online\n", selectedCount, len(nodes), onlineCount))

	nodeLimit := min(len(nodes), maxNodeRows)
	for i := 0; i < nodeLimit; i++ {
		node := nodes[i]
		icon := toolSuccess.Render("●")
		status := "Online"
		if !node.Online {
			icon = statusStyle.Render("○")
			status = "Offline"
		}
		selection := "unselected"
		if selected[node.Name] {
			selection = "selected"
		}
		b.WriteString(fmt.Sprintf("%s %s  %s  %-7s  %s\n", icon, node.Name, node.Host, status, selection))
	}
	if remaining := len(nodes) - nodeLimit; remaining > 0 {
		b.WriteString(statusStyle.Render(fmt.Sprintf("... %d more node(s)", remaining)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(statusStyle.Render("Type a message or /help"))
	return strings.TrimRight(b.String(), "\n")
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
