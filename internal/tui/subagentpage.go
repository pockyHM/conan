package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pockyHM/conan/internal/subagent"
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

	subagentPageSectionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("63")).
				Bold(true)

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

	subagentAssistantStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	subagentToolCallStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("220")).
				Bold(true)

	subagentToolOKStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("82"))

	subagentToolFailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
)

const (
	subagentTurnIndent     = "  "
	subagentTurnBodyIndent = "    "
	subagentToolOutputMax  = 240
)

type subagentTurn struct {
	Number    int
	StartAt   time.Duration
	EndAt     time.Duration
	Text      string
	HasText   bool
	ToolCalls []subagentToolTrace
}

type subagentToolTrace struct {
	Name   string
	Args   string
	Output string
	OK     bool
}

func renderSubagentStatusCell(status string, frame int, lang uiLanguage) string {
	label := subagentStatusLabel(status, lang)
	if isSubagentStatusActive(status) {
		return subagentSpinnerGlyph(frame) + " " + label
	}
	return label
}

func isSubagentStatusActive(status string) bool {
	switch status {
	case "receiving", "tool", "queued":
		return true
	}
	return false
}

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
		subagentStatusBadgeStyle(run.Status).Render(padCell(renderSubagentStatusCell(run.Status, m.subagentAnimFrame, m.uiLanguage), 10)),
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
	status := subagentStatusBadgeStyle(run.Status).Render(renderSubagentStatusCell(run.Status, m.subagentAnimFrame, m.uiLanguage))
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
	if len(run.Nodes) > 0 {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Nodes:", "节点:")), strings.Join(run.Nodes, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Nodes:", "节点:")), subagentPageMutedStyle.Render(m.uiLanguage.tr("(none)", "（无）"))))
	}
	if elapsed != "" {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Elapsed:", "耗时:")), elapsed))
	}
	if run.Err != "" {
		lines = append(lines, fmt.Sprintf("%s %s", subagentPageLabelStyle.Render(m.uiLanguage.tr("Error:", "错误:")), run.Err))
	}

	lines = append(lines, "")
	lines = append(lines, subagentPageSectionStyle.Render(m.uiLanguage.tr("Conversation", "对话")))
	turns := collectSubagentTurns(run.Events)
	if len(turns) == 0 {
		lines = append(lines, subagentPageMutedStyle.Render(subagentTurnIndent+m.uiLanguage.tr("(no trace events captured)", "（未捕获到事件）")))
	} else {
		if m.subagentDetailCursor < 0 || m.subagentDetailCursor >= len(turns) {
			m.subagentDetailCursor = len(turns) - 1
		}
		for i, turn := range turns {
			lines = append(lines, m.renderSubagentDetailTurn(i, turn, turns))
		}
	}

	if run.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, subagentPageSectionStyle.Render(m.uiLanguage.tr("Summary", "总结")))
		for _, line := range strings.Split(strings.TrimRight(run.Summary, "\n"), "\n") {
			lines = append(lines, subagentAssistantStyle.Render(subagentTurnIndent+line))
		}
	}

	lines = append(lines, "")
	lines = append(lines, subagentPageHelpStyle.Render(m.uiLanguage.tr(
		"↑↓/jk turn · Space/Enter expand · c cancel · Esc back",
		"↑↓/jk 切换轮次 · Space/Enter 展开 · c 取消 · Esc 返回",
	)))
	return subagentPageBoxStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) renderSubagentDetailTurn(index int, turn subagentTurn, turns []subagentTurn) string {
	expanded := isLastTurn(turns, index) || m.subagentDetailExpanded[turn.Number]
	marker := "▸ "
	if expanded {
		marker = "▾ "
	}
	cursor := subagentTurnIndent
	rowStyle := subagentPageMutedStyle
	if index == m.subagentDetailCursor {
		cursor = subagentPageCursorStyle.Render("▶ ")
		rowStyle = subagentPageSelectedStyle
	}
	summary := turnSummaryLabel(turn, m.uiLanguage)
	header := cursor + marker + rowStyle.Render(summary)

	if !expanded {
		return header
	}
	var body []string
	body = append(body, header)
	if turn.HasText {
		body = append(body, subagentPageMutedStyle.Render(subagentTurnBodyIndent+m.uiLanguage.tr("assistant:", "智能体:")))
		for _, ln := range strings.Split(strings.TrimRight(turn.Text, "\n"), "\n") {
			body = append(body, subagentAssistantStyle.Render(subagentTurnBodyIndent+ln))
		}
	}
	for _, call := range turn.ToolCalls {
		body = append(body, renderSubagentDetailToolCall(call))
	}
	return strings.Join(body, "\n")
}

func turnSummaryLabel(turn subagentTurn, lang uiLanguage) string {
	dur := turn.EndAt - turn.StartAt
	durStr := ""
	if dur > 0 {
		durStr = " · " + dur.Round(100*time.Millisecond).String()
	}
	switch {
	case len(turn.ToolCalls) == 0 && turn.HasText:
		return fmt.Sprintf("%s %d · %s%s", lang.tr("Turn", "第"), turn.Number, lang.tr("final reply", "最终回复"), durStr)
	case len(turn.ToolCalls) == 0:
		return fmt.Sprintf("%s %d · %s%s", lang.tr("Turn", "第"), turn.Number, lang.tr("no tool call", "无工具调用"), durStr)
	default:
		count := len(turn.ToolCalls)
		names := make([]string, 0, count)
		for _, call := range turn.ToolCalls {
			names = append(names, call.Name)
		}
		label := lang.tr("tool call", "次工具调用")
		if count != 1 {
			label = lang.tr("tool calls", "次工具调用")
		}
		return fmt.Sprintf("%s %d · %d %s · %s%s", lang.tr("Turn", "第"), turn.Number, count, label, strings.Join(names, ", "), durStr)
	}
}

func renderSubagentDetailToolCall(call subagentToolTrace) string {
	header := subagentToolCallStyle.Render(subagentTurnBodyIndent + "⤷ " + call.Name)
	if call.Args != "" {
		header += subagentPageMutedStyle.Render("  " + oneLineSubagentPrompt(call.Args))
	}
	out := strings.TrimRight(call.Output, "\n")
	if len([]rune(out)) > subagentToolOutputMax {
		out = truncateWithEllipsis(out, subagentToolOutputMax)
	}
	outStyle := subagentToolOKStyle
	marker := "→"
	if !call.OK {
		outStyle = subagentToolFailStyle
		marker = "✗"
	}
	lines := strings.Split(out, "\n")
	var body []string
	for i, line := range lines {
		prefix := subagentTurnBodyIndent + "  "
		if i == 0 {
			prefix = subagentTurnBodyIndent + outStyle.Render(marker) + " "
		}
		body = append(body, prefix+outStyle.Render(line))
	}
	return header + "\n" + strings.Join(body, "\n")
}

func collectSubagentTurns(events []subagent.Event) []subagentTurn {
	byNumber := map[int]*subagentTurn{}
	order := []int{}
	getOrCreate := func(n int) *subagentTurn {
		t, ok := byNumber[n]
		if !ok {
			t = &subagentTurn{Number: n}
			byNumber[n] = t
			order = append(order, n)
		}
		return t
	}
	for _, ev := range events {
		switch ev.Kind {
		case subagent.EventTurnStart:
			t := getOrCreate(ev.Turn)
			t.StartAt = ev.Elapsed
		case subagent.EventTurnEnd:
			if t, ok := byNumber[ev.Turn]; ok {
				t.EndAt = ev.Elapsed
			}
		case subagent.EventAssistantText:
			t := getOrCreate(ev.Turn)
			t.Text = ev.Content
			t.HasText = true
		case subagent.EventToolCall:
			t := getOrCreate(ev.Turn)
			t.ToolCalls = append(t.ToolCalls, subagentToolTrace{Name: ev.Tool, Args: ev.Args})
		case subagent.EventToolResult:
			t, ok := byNumber[ev.Turn]
			if !ok {
				continue
			}
			for i := len(t.ToolCalls) - 1; i >= 0; i-- {
				if t.ToolCalls[i].Name == ev.Tool && t.ToolCalls[i].Output == "" {
					t.ToolCalls[i].Output = ev.Out
					t.ToolCalls[i].OK = ev.OK
					break
				}
			}
		}
	}
	turns := make([]subagentTurn, 0, len(order))
	for _, n := range order {
		turns = append(turns, *byNumber[n])
	}
	return turns
}

func isLastTurn(turns []subagentTurn, index int) bool {
	return index == len(turns)-1
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
