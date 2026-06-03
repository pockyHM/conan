package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	subagentPageBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)

	subagentPageTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true).
				Padding(0, 1)

	subagentPageMetaStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	subagentPageLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

	subagentPageMutedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	subagentPageCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

	subagentPageSelectedStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("236")).
					Foreground(lipgloss.Color("252"))

	subagentPageHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true)
)

func subagentStatusBadgeStyle(status string) lipgloss.Style {
	color := "243"
	switch status {
	case "queued", "receiving":
		color = "220"
	case "tool":
		color = "39"
	case "completed":
		color = "82"
	case "failed":
		color = "196"
	case "cancelled":
		color = "244"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

func (m Model) renderSubagentPage() string {
	if len(m.subagentRuns) == 0 {
		return subagentPageBoxStyle.Render(m.uiLanguage.tr(
			"No subagents have run yet.\n\nSubagents are dispatched automatically by the main agent when a task spans multiple nodes.",
			"还没有运行过的子智能体。\n\n当任务跨多个节点时，主智能体会自动派发子智能体。",
		))
	}
	if m.subagentDetailVisible {
		return m.renderSubagentDetailPage()
	}
	return m.renderSubagentListPage()
}

func (m Model) renderSubagentListPage() string {
	title := subagentPageTitleStyle.Render(m.uiLanguage.tr("Subagents", "子智能体"))
	headerCols := m.uiLanguage.tr(
		"  ID                       STATUS     ROLE          MODEL           NODES                  PROMPT",
		"  ID                       状态       角色          模型            节点                   提示词",
	)
	header := subagentPageLabelStyle.Render(headerCols)
	var rows []string
	for i, run := range m.subagentRuns {
		rows = append(rows, m.renderSubagentListRow(i, run))
	}
	help := subagentPageHelpStyle.Render(m.uiLanguage.tr(
		"↑↓/jk select · Enter detail · c cancel · Esc close",
		"↑↓/jk 选择 · Enter 查看详情 · c 取消 · Esc 关闭",
	))
	body := strings.Join([]string{title, header, strings.Join(rows, "\n"), "", help}, "\n")
	return subagentPageBoxStyle.Render(body)
}

func (m Model) renderSubagentListRow(index int, run subagentRunView) string {
	cursor := "  "
	rowStyle := subagentPageMutedStyle
	if index == m.subagentListCursor {
		cursor = subagentPageCursorStyle.Render("▶ ")
		rowStyle = subagentPageSelectedStyle
	}
	cells := []string{
		padCell(run.ID, 24),
		subagentStatusBadgeStyle(run.Status).Render(padCell(subagentStatusLabel(run.Status, m.uiLanguage), 10)),
		padCell(string(normalizeSubagentRoleForStatus(run.Role)), 13),
		padCell(run.Model, 15),
		padCell(strings.Join(run.Nodes, ", "), 20),
		truncateWithEllipsis(oneLineSubagentPrompt(run.Prompt), 60),
	}
	row := cursor + rowStyle.Render(strings.Join(cells, " "))
	return row
}

func (m Model) renderSubagentDetailPage() string {
	idx := m.subagentListCursor
	if idx < 0 || idx >= len(m.subagentRuns) {
		return subagentPageBoxStyle.Render(m.uiLanguage.tr("No subagent selected", "未选择子智能体"))
	}
	run := m.subagentRuns[idx]
	title := subagentPageTitleStyle.Render(
		m.uiLanguage.tr("Subagent detail", "子智能体详情") + " · " + truncateWithEllipsis(run.ID, 16),
	)
	status := subagentStatusBadgeStyle(run.Status).Render(subagentStatusLabel(run.Status, m.uiLanguage))
	elapsed := ""
	if run.Elapsed > 0 {
		elapsed = run.Elapsed.Round(100 * time.Millisecond).String()
	}

	var lines []string
	lines = append(lines, title)
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("ID:", "ID:")), run.ID))
	lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Status:", "状态:")), status))
	lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Role:", "角色:")), string(normalizeSubagentRoleForStatus(run.Role))))
	lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Model:", "模型:")), run.Model))
	if elapsed != "" {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Elapsed:", "耗时:")), elapsed))
	}
	if len(run.Nodes) > 0 {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Nodes:", "节点:")), strings.Join(run.Nodes, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Nodes:", "节点:")), subagentPageMutedStyle.Render(m.uiLanguage.tr("(none)", "（无）"))))
	}
	lines = append(lines, "")
	lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Prompt:", "提示词:")))
	if strings.TrimSpace(run.Prompt) == "" {
		lines = append(lines, subagentPageMutedStyle.Render("  "+m.uiLanguage.tr("(empty)", "（无）")))
	} else {
		for _, line := range strings.Split(strings.TrimRight(run.Prompt, "\n"), "\n") {
			lines = append(lines, subagentPageMutedStyle.Render("  "+line))
		}
	}
	if run.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Summary:", "总结:")))
		for _, line := range strings.Split(strings.TrimRight(run.Summary, "\n"), "\n") {
			lines = append(lines, subagentPageMutedStyle.Render("  "+line))
		}
	}
	if run.Err != "" {
		lines = append(lines, "")
		lines = append(lines, subagentPageLabelStyle.Render(m.uiLanguage.tr("Error:", "错误:")))
		lines = append(lines, subagentPageMutedStyle.Render("  "+run.Err))
	}
	lines = append(lines, "")
	lines = append(lines, subagentPageHelpStyle.Render(m.uiLanguage.tr(
		"↑↓/jk select · c cancel · Esc back",
		"↑↓/jk 选择 · c 取消 · Esc 返回",
	)))
	return subagentPageBoxStyle.Render(strings.Join(lines, "\n"))
}

func padCell(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func truncateWithEllipsis(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func oneLineSubagentPrompt(prompt string) string {
	line := strings.Join(strings.Fields(prompt), " ")
	if len(line) > 100 {
		return line[:100] + "..."
	}
	return line
}
