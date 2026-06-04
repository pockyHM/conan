package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tracePageBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)

	tracePageTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				Padding(0, 1)

	tracePageMutedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	tracePageHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true)

	traceRailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

func (m Model) renderTracePage() string {
	if m.traceDetailVisible {
		return m.renderTraceDetailPage()
	}
	return m.renderTraceTimelinePage()
}

func (m Model) renderTraceTimelinePage() string {
	title := tracePageTitleStyle.Render(fmt.Sprintf("%s · %d %s",
		m.uiLanguage.tr("Trace", "链路追踪"),
		len(m.traceNodes),
		m.uiLanguage.tr("nodes", "节点"),
	))
	if len(m.traceNodes) == 0 {
		empty := tracePageMutedStyle.Render(m.uiLanguage.tr(
			"No trace nodes yet.\n\nSend a message or run a tool to populate the current-session trace.",
			"还没有链路节点。\n\n发送消息或运行工具后，当前会话链路会显示在这里。",
		))
		help := tracePageHelpStyle.Render(m.uiLanguage.tr("Esc close", "Esc 关闭"))

		return tracePageBoxStyle.Render(strings.Join([]string{title, "", empty, "", help}, "\n"))
	}

	start, end := m.visibleTraceTimelineRange()
	lines := []string{title, ""}
	for i := start; i < end; i++ {
		node := m.traceNodes[i]
		lines = append(lines, m.renderTraceTimelineNode(i, node, i == len(m.traceNodes)-1))
	}
	lines = append(lines, "", tracePageHelpStyle.Render(m.uiLanguage.tr(
		"↑↓/jk select · Enter detail · Esc close",
		"↑↓/jk 选择 · Enter 详情 · Esc 关闭",
	)))
	return tracePageBoxStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) visibleTraceTimelineRange() (int, int) {
	total := len(m.traceNodes)
	if total == 0 {
		return 0, 0
	}
	if m.height <= 0 {
		return 0, total
	}
	const (
		traceTimelineReservedLines = 6
		traceTimelineLinesPerNode  = 3
	)
	visible := max((m.height-traceTimelineReservedLines)/traceTimelineLinesPerNode, 1)
	if visible >= total {
		return 0, total
	}
	cursor := m.traceCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > total {
		end = total
		start = max(end-visible, 0)
	}
	return start, end
}

func (m Model) renderTraceTimelineNode(index int, node traceNode, last bool) string {
	marker := traceMarker(node)
	rail := traceRailStyle.Render("│")
	arrow := traceRailStyle.Render("↓")
	if last {
		rail = " "
		arrow = " "
	}
	cursor := "  "
	rowStyle := traceRowStyle(node)
	if index == m.traceCursor {
		cursor = subagentPageCursorStyle.Render("▶ ")
		rowStyle = subagentPageSelectedStyle
	}
	summaryWidth := 80
	if m.width > 0 {
		summaryWidth = max(m.width-34, 40)
	}
	line := fmt.Sprintf("%02d %-11s %-8s %s",
		index+1,
		traceKindLabel(node.Kind, m.uiLanguage),
		traceStatusLabel(node.Status, m.uiLanguage),
		truncateWithEllipsis(node.Summary, summaryWidth),
	)
	return strings.Join([]string{
		cursor + traceMarkerStyle(node).Render(marker),
		"  " + rail,
		"  " + arrow + " " + rowStyle.Render(line),
	}, "\n")
}

func (m Model) renderTraceDetailPage() string {
	if len(m.traceNodes) == 0 {
		return m.renderTraceTimelinePage()
	}
	if m.traceCursor < 0 {
		m.traceCursor = 0
	}
	if m.traceCursor >= len(m.traceNodes) {
		m.traceCursor = len(m.traceNodes) - 1
	}
	node := m.traceNodes[m.traceCursor]
	title := tracePageTitleStyle.Render(m.uiLanguage.tr("Trace detail", "链路详情"))
	lines := []string{
		title,
		"",
		subagentPageLabelStyle.Render("ID: ") + node.ID,
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Type: ", "类型: ")) + traceKindLabel(node.Kind, m.uiLanguage),
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Status: ", "状态: ")) + traceStatusLabel(node.Status, m.uiLanguage),
		subagentPageLabelStyle.Render(m.uiLanguage.tr("Summary: ", "摘要: ")) + node.Summary,
	}
	lines = append(lines, m.renderTraceDetailMetadata(node)...)
	lines = append(lines, "", subagentPageSectionStyle.Render(m.uiLanguage.tr("Detail", "详情")))
	detail := strings.TrimSpace(node.Detail)
	if detail == "" {
		detail = m.uiLanguage.tr("(no detail)", "（无详情）")
	}
	for _, line := range strings.Split(truncateWithEllipsis(detail, 2400), "\n") {
		lines = append(lines, subagentAssistantStyle.Render("  "+line))
	}
	lines = append(lines, "", tracePageHelpStyle.Render(m.uiLanguage.tr("Esc back", "Esc 返回")))
	return tracePageBoxStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) renderTraceDetailMetadata(node traceNode) []string {
	var lines []string
	if node.ParentID != "" {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Parent ID: ", "父节点 ID: "))+node.ParentID)
	}
	if node.ToolCallID != "" {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Tool call ID: ", "工具调用 ID: "))+node.ToolCallID)
	}
	if node.ToolName != "" {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Tool: ", "工具: "))+node.ToolName)
	}
	if node.SubagentID != "" {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Subagent ID: ", "子智能体 ID: "))+node.SubagentID)
	}
	if !node.StartedAt.IsZero() {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Started: ", "开始: "))+node.StartedAt.Format("15:04:05"))
	}
	if !node.EndedAt.IsZero() {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Ended: ", "结束: "))+node.EndedAt.Format("15:04:05"))
	}
	if elapsed, ok := traceNodeElapsed(node); ok {
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Elapsed: ", "耗时: "))+elapsed)
	}
	return lines
}

func traceNodeElapsed(node traceNode) (string, bool) {
	if node.StartedAt.IsZero() {
		return "", false
	}
	end := node.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	if end.Before(node.StartedAt) {
		return "", false
	}
	elapsed := end.Sub(node.StartedAt).Round(100 * time.Millisecond)
	return elapsed.String(), true
}

func traceMarker(node traceNode) string {
	switch node.Kind {
	case traceUser:
		return "●"
	case traceAssistant:
		return "◆"
	case traceToolCall:
		return "▶"
	case traceToolResult:
		if node.Status == traceFailed {
			return "✗"
		}
		return "✓"
	case traceSubagent:
		return "◇"
	default:
		return "■"
	}
}

func traceMarkerStyle(node traceNode) lipgloss.Style {
	color := "244"
	switch node.Kind {
	case traceUser:
		color = "39"
	case traceAssistant:
		color = "141"
	case traceToolCall:
		color = "220"
	case traceToolResult:
		if node.Status == traceFailed {
			color = "196"
		} else {
			color = "82"
		}
	case traceSubagent:
		color = "14"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
}

func traceRowStyle(node traceNode) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(traceMarkerStyle(node).GetForeground())
}

func (m Model) handleTraceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.traceDetailVisible {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			m.traceDetailVisible = false
			return m, nil
		}
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeChat
		m.status = m.uiLanguage.tr("Trace closed", "已关闭链路")
	case tea.KeyUp:
		if m.traceCursor > 0 {
			m.traceCursor--
		}
	case tea.KeyDown:
		if m.traceCursor < len(m.traceNodes)-1 {
			m.traceCursor++
		}
	case tea.KeyEnter:
		if len(m.traceNodes) > 0 {
			m.traceDetailVisible = true
		}
	case tea.KeyRunes:
		if len(key.Runes) == 1 {
			switch key.Runes[0] {
			case 'k':
				if m.traceCursor > 0 {
					m.traceCursor--
				}
			case 'j':
				if m.traceCursor < len(m.traceNodes)-1 {
					m.traceCursor++
				}
			}
		}
	}
	return m, nil
}
