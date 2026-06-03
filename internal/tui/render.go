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

	footerModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

	footerClusterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("220"))

	footerMetaSepStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
)

var mdRenderer *glamour.TermRenderer

var thinkingFrames = []string{"◐", "◓", "◑", "◒"}
var startupFrames = []string{"▌", "▐", "▔", "▁"}

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

func renderThinkingMsg(frame int, elapsed time.Duration, lang uiLanguage) string {
	if len(thinkingFrames) == 0 {
		return thinkingStyle.Render("◦ " + lang.tr("Thinking...", "思考中...") + " " + renderThinkingMeta(elapsed, lang))
	}
	icon := thinkingFrames[frame%len(thinkingFrames)]
	return thinkingStyle.Render(icon + " " + lang.tr("Thinking...", "思考中...") + " " + renderThinkingMeta(elapsed, lang))
}

func renderThinkingMeta(elapsed time.Duration, lang uiLanguage) string {
	label := formatElapsed(elapsed)
	if label == "" {
		return lang.tr("Esc to interrupt", "Esc 中断")
	}
	return label + "  " + lang.tr("Esc to interrupt", "Esc 中断")
}

func renderCompactProgress(frame int, startedAt time.Time, lang uiLanguage) string {
	icon := "◦"
	if len(thinkingFrames) > 0 {
		icon = thinkingFrames[frame%len(thinkingFrames)]
	}
	percent := 12 + frame*6
	if percent > 92 {
		percent = 92
	}
	stage := lang.tr("Summarizing context", "正在总结上下文")
	switch {
	case frame < 2:
		stage = lang.tr("Preparing messages", "正在准备消息")
	case frame >= 8:
		stage = lang.tr("Writing compacted state", "正在写入压缩状态")
	}
	bar := renderProgressBar(percent, 18)
	elapsed := formatElapsed(time.Since(startedAt).Round(100 * time.Millisecond))
	if elapsed == "" {
		return fmt.Sprintf("%s %s %s %d%% · %s", icon, lang.tr("Compacting context", "正在压缩上下文"), bar, percent, stage)
	}
	return fmt.Sprintf("%s %s %s %d%% · %s · %s", icon, lang.tr("Compacting context", "正在压缩上下文"), bar, percent, stage, elapsed)
}

func renderProgressBar(percent int, width int) string {
	if width <= 0 {
		return ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := percent * width / 100
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func renderStreamingMsg(content string) string {
	return renderMarkdown(content) + "▌"
}

func renderReasoningMsg(content string, elapsed time.Duration, lang uiLanguage) string {
	line := lastNonEmptyLine(content)
	if line == "" {
		line = strings.TrimSpace(content)
	}
	return reasoningStyle.Render("◦ " + lang.tr("Thinking:", "思考:") + " " + line + " " + renderThinkingMeta(elapsed, lang))
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

func renderElapsedFooter(elapsed time.Duration, lang uiLanguage) string {
	label := formatElapsed(elapsed)
	if label == "" {
		return ""
	}
	return statusStyle.Render("✱ " + lang.tr("Took ", "耗时 ") + label)
}

func renderInputBox(input string, width int, lang uiLanguage) string {
	style := inputBoxStyle
	if width > 0 {
		style = style.Width(max(width-2, 1))
	}
	content := inputPromptStyle.Render("❯ ") + compactInputForDisplay(input, lang) + "█"
	return style.Render(content)
}

func renderContextMeter(meter contextMeter, lang uiLanguage, width int) string {
	return renderFooterMeta("", meter, lang, width)
}

func renderModelClusterMeta(model string, cluster string) string {
	model = strings.TrimSpace(model)
	cluster = strings.TrimSpace(cluster)
	switch {
	case model == "" && cluster == "":
		return ""
	case model == "":
		return footerClusterStyle.Render(cluster)
	case cluster == "":
		return footerModelStyle.Render(model)
	default:
		return footerModelStyle.Render(model) + footerMetaSepStyle.Render(" · ") + footerClusterStyle.Render(cluster)
	}
}

func renderFooterMeta(left string, meter contextMeter, lang uiLanguage, width int) string {
	if width <= 0 {
		return ""
	}
	label := lang.tr("Context", "上下文")
	var right string
	if meter.Known && meter.Limit > 0 {
		percent := contextMeterPercent(meter)
		right = fmt.Sprintf("%s %s/%s (%d%%) %s", label, formatContextCount(meter.Used), formatContextCount(meter.Limit), percent, renderProgressBar(percent, 10))
	} else {
		right = fmt.Sprintf("%s %s/unknown context limit", label, formatContextCount(meter.Used))
	}
	left = strings.TrimSpace(left)
	if left == "" {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, truncateDisplay(right, width))
	}
	left = truncateDisplay(left, width)
	right = truncateDisplay(right, width)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		leftWidth := max(width-lipgloss.Width(right)-2, 0)
		left = truncateDisplay(left, leftWidth)
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func compactInputForDisplay(input string, lang uiLanguage) string {
	if !strings.ContainsAny(input, "\r\n") {
		return input
	}
	lines := multilineInputLineCount(input)
	label := lang.tr("lines", "行")
	if lines == 1 {
		label = lang.tr("line", "行")
	}
	return fmt.Sprintf(lang.tr("Pasted %d %s", "已粘贴 %d %s"), lines, label)
}

func multilineInputLineCount(input string) int {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimRight(normalized, "\n")
	if normalized == "" {
		return 1
	}
	return strings.Count(normalized, "\n") + 1
}

func renderStartupOverview(cluster, model string, nodes []NodeInfo, selected map[string]bool, lang uiLanguage, width int, bodyHeight int, frame int) string {
	const maxNodeRows = 5
	markFrame := "▌"
	if len(startupFrames) > 0 {
		markFrame = startupFrames[frame%len(startupFrames)]
	}
	fullWordmark := startupWordmark(frame)
	compact := width > 0 && width < maxDisplayLineWidth(strings.Split(fullWordmark, "\n"))
	if bodyHeight > 0 && bodyHeight < 10 {
		compact = true
	}
	wordmark := fullWordmark
	if compact {
		wordmark = "CONAN " + markFrame
	}

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
	b.WriteString(fmt.Sprintf("%-9s %s\n", lang.tr("Cluster", "集群"), cluster))
	b.WriteString(fmt.Sprintf("%-9s %s\n", lang.tr("Model", "模型"), model))
	if compact && bodyHeight > 0 && bodyHeight <= 5 {
		b.WriteString(statusStyle.Render(lang.tr("Type a message or /help", "输入消息或 /help")))
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(fmt.Sprintf("%-9s %s\n", lang.tr("Nodes", "节点"), fmt.Sprintf(lang.tr("%d/%d selected, %d online", "已选 %d/%d，在线 %d"), selectedCount, len(nodes), onlineCount)))

	nodeLimit := min(len(nodes), maxNodeRows)
	for i := 0; i < nodeLimit; i++ {
		node := nodes[i]
		icon := toolSuccess.Render("●")
		status := lang.tr("Online", "在线")
		if !node.Online {
			icon = statusStyle.Render("○")
			status = lang.tr("Offline", "离线")
		}
		selection := lang.tr("unselected", "未选择")
		if selected[node.Name] {
			selection = lang.tr("selected", "已选择")
		}
		b.WriteString(fmt.Sprintf("%s %s  %s  %-7s  %s\n", icon, node.Name, node.Host, status, selection))
	}
	if remaining := len(nodes) - nodeLimit; remaining > 0 {
		b.WriteString(statusStyle.Render(fmt.Sprintf(lang.tr("... %d more node(s)", "... 还有 %d 个节点"), remaining)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(statusStyle.Render(lang.tr("Type a message or /help", "输入消息或 /help")))
	return strings.TrimRight(b.String(), "\n")
}

func startupWordmark(frame int) string {
	lines := []string{
		" █████  █████  █   █  █████  █   █",
		"█       █   █  ██  █  █   █  ██  █",
		"█       █   █  █ █ █  █████  █ █ █",
		"█       █   █  █  ██  █   █  █  ██",
		" █████  █████  █   █  █   █  █   █",
	}
	return strings.Join(animateStartupWordmark(lines, frame), "\n")
}

func animateStartupWordmark(lines []string, frame int) []string {
	if len(lines) == 0 {
		return nil
	}
	animated := make([]string, len(lines))
	for i, line := range lines {
		runes := []rune(line)
		if frame > 0 {
			for col, r := range runes {
				if r == '█' && (col+i+frame)%17 == 0 {
					runes[col] = '▓'
				}
			}
		}
		animated[i] = string(runes)
	}
	return animated
}

func maxDisplayLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	return width
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

func renderToolHeader(name string, nodeCount int, lang uiLanguage) string {
	if nodeCount > 1 {
		return toolStyle.Render(fmt.Sprintf(lang.tr("⏚ %s on %d node(s)", "⏚ %s 在 %d 个节点上"), name, nodeCount))
	}
	return toolStyle.Render(fmt.Sprintf("⏚ %s", name))
}

func renderToolNode(node string, success bool, output string, expanded bool, lang uiLanguage) string {
	icon := toolSuccess.Render("✓")
	if !success {
		icon = toolFailure.Render("✗")
	}
	output = renderToolOutput(output, expanded, lang)
	return fmt.Sprintf("  %s %s  %s", icon, toolStyle.Render(node), output)
}

func renderToolOutput(output string, expanded bool, lang uiLanguage) string {
	lines := strings.Split(formatToolOutputForDisplay(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if expanded || len(lines) <= toolOutputPreviewLines {
		return toolStyle.Render(strings.Join(lines, "\n"))
	}
	preview := strings.Join(lines[:toolOutputPreviewLines], "\n")
	remaining := len(lines) - toolOutputPreviewLines
	return toolStyle.Render(preview) + "\n" + statusStyle.Render(fmt.Sprintf(lang.tr("... %d more line(s), press Ctrl+O to expand", "... 还有 %d 行，按 Ctrl+O 展开"), remaining))
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

func renderHeader(cluster, model string, selectedNodes, totalNodes int, lang uiLanguage) string {
	return headerKeyStyle.Render("Conan") + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(cluster) + headerSepStyle.Render(" │ ") +
		headerValStyle.Render(model) + headerSepStyle.Render(" │ ") +
		fmt.Sprintf(lang.tr("%d/%d nodes", "%d/%d 节点"), selectedNodes, totalNodes)
}
