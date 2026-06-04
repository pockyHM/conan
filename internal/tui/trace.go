package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/subagent"
	"github.com/pockyHM/conan/pkg/models"
)

type traceKind string

const (
	traceUser       traceKind = "user"
	traceAssistant  traceKind = "assistant"
	traceToolCall   traceKind = "tool_call"
	traceToolResult traceKind = "tool_result"
	traceSubagent   traceKind = "subagent"
)

type traceStatus string

const (
	tracePending traceStatus = "pending"
	traceRunning traceStatus = "running"
	traceDone    traceStatus = "done"
	traceFailed  traceStatus = "failed"
	traceBlocked traceStatus = "blocked"
)

type traceNode struct {
	ID        string
	ParentID  string
	Kind      traceKind
	Status    traceStatus
	Title     string
	Summary   string
	Detail    string
	StartedAt time.Time
	EndedAt   time.Time

	ToolCallID string
	ToolName   string
	SubagentID string
}

func newTraceNode(kind traceKind, status traceStatus, title, summary, detail string) traceNode {
	now := time.Now()
	return traceNode{
		ID:        models.NewID(),
		Kind:      kind,
		Status:    status,
		Title:     title,
		Summary:   strings.TrimSpace(summary),
		Detail:    strings.TrimSpace(detail),
		StartedAt: now,
	}
}

func (m Model) appendTraceNode(node traceNode) Model {
	if node.ID == "" {
		node.ID = models.NewID()
	}
	if node.StartedAt.IsZero() {
		node.StartedAt = time.Now()
	}
	m.traceNodes = append(m.traceNodes, node)
	if m.traceCursor < 0 || m.traceCursor >= len(m.traceNodes) {
		m.traceCursor = len(m.traceNodes) - 1
	}
	return m
}

func (m Model) updateTraceNode(id string, fn func(*traceNode)) Model {
	if id == "" || fn == nil {
		return m
	}
	for i := range m.traceNodes {
		if m.traceNodes[i].ID == id {
			fn(&m.traceNodes[i])
			return m
		}
	}
	return m
}

func (m Model) findTraceByToolCallID(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].ToolCallID == id {
			return i
		}
	}
	return -1
}

func (m Model) findTraceBySubagentID(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].SubagentID == id {
			return i
		}
	}
	return -1
}

func (m Model) clearTraceState() Model {
	m.traceNodes = nil
	m.traceCursor = 0
	m.traceDetailVisible = false
	m.activeTraceAssistantID = ""
	return m
}

func (m Model) recordUserTrace(content string) Model {
	content = strings.TrimSpace(content)
	return m.appendTraceNode(newTraceNode(traceUser, traceDone, "user", firstTraceLine(content), content))
}

func (m Model) ensureActiveTraceAssistant() Model {
	if m.activeTraceAssistantID != "" {
		for _, node := range m.traceNodes {
			if node.ID == m.activeTraceAssistantID {
				return m
			}
		}
		m.activeTraceAssistantID = ""
	}
	node := newTraceNode(traceAssistant, traceRunning, "assistant", "receiving...", "")
	m = m.appendTraceNode(node)
	m.activeTraceAssistantID = node.ID
	return m
}

func (m Model) updateActiveTraceAssistant(content string) Model {
	content = strings.TrimSpace(content)
	m = m.ensureActiveTraceAssistant()
	return m.updateTraceNode(m.activeTraceAssistantID, func(node *traceNode) {
		node.Status = traceRunning
		node.Detail = content
		if summary := firstTraceLine(content); summary != "" {
			node.Summary = summary
		}
	})
}

func (m Model) finishActiveTraceAssistant(status traceStatus, fallbackDetail string) Model {
	if m.activeTraceAssistantID == "" {
		return m
	}
	id := m.activeTraceAssistantID
	m.activeTraceAssistantID = ""
	return m.updateTraceNode(id, func(node *traceNode) {
		node.Status = status
		node.EndedAt = time.Now()
		detail := strings.TrimSpace(node.Detail)
		fallback := strings.TrimSpace(fallbackDetail)
		if status != traceDone && fallback != "" {
			if detail == "" {
				detail = fallback
			} else if !strings.Contains(detail, fallback) {
				detail += "\n\n" + fallback
			}
		} else if detail == "" {
			detail = fallback
		}
		node.Detail = detail
		if strings.TrimSpace(node.Summary) == "" || node.Summary == "receiving..." {
			node.Summary = firstTraceLine(node.Detail)
		}
		if strings.TrimSpace(node.Summary) == "" {
			node.Summary = string(status)
		}
	})
}

func (m Model) recordToolCallTrace(call llm.ToolCall, detail string) Model {
	if call.ID == "" {
		call.ID = models.NewID()
	}
	if idx := m.findTraceToolCallNode(call.ID); idx >= 0 {
		m.traceNodes[idx].Status = traceRunning
		m.traceNodes[idx].ToolName = call.Name
		m.traceNodes[idx].Summary = traceToolSummary(call.Name, detail)
		m.traceNodes[idx].Detail = strings.TrimSpace(detail)
		return m
	}
	node := newTraceNode(traceToolCall, traceRunning, call.Name, traceToolSummary(call.Name, detail), detail)
	node.ToolCallID = call.ID
	node.ToolName = call.Name
	return m.appendTraceNode(node)
}

func (m Model) recordToolResultTrace(call llm.ToolCall, results []nodeToolResult, output string) Model {
	status := traceDone
	if !allNodeToolResultsSuccessful(results) {
		status = traceFailed
	}
	if idx := m.findTraceToolCallNode(call.ID); idx >= 0 {
		m.traceNodes[idx].Status = status
		m.traceNodes[idx].EndedAt = time.Now()
	}
	node := newTraceNode(traceToolResult, status, "tool result", traceToolResultSummary(results), output)
	node.ToolCallID = call.ID
	node.ToolName = call.Name
	return m.appendTraceNode(node)
}

func (m Model) findTraceToolCallNode(id string) int {
	if id == "" {
		return -1
	}
	for i := len(m.traceNodes) - 1; i >= 0; i-- {
		if m.traceNodes[i].Kind == traceToolCall && m.traceNodes[i].ToolCallID == id {
			return i
		}
	}
	return -1
}

func (m Model) recordSubagentTrace(req subagent.Request) Model {
	if req.ID == "" {
		req.ID = models.NewID()
	}
	title := string(normalizeSubagentRoleForStatus(req.Role))
	summary := strings.TrimSpace(fmt.Sprintf("%s · %s", title, firstTraceLine(req.Task)))
	node := newTraceNode(traceSubagent, traceRunning, title, summary, subagentPromptForDisplay(req))
	node.SubagentID = req.ID
	return m.appendTraceNode(node)
}

func (m Model) updateSubagentTraceResult(result subagent.Result) Model {
	status := traceDone
	if result.Err != nil {
		status = traceFailed
	}
	return m.updateSubagentTraceByID(result.ID, status, renderSubagentTraceDetail(result), result.Summary)
}

func (m Model) updateSubagentTraceByID(id string, status traceStatus, detail string, summary string) Model {
	if id == "" {
		return m
	}
	for i := range m.traceNodes {
		if m.traceNodes[i].Kind != traceSubagent || m.traceNodes[i].SubagentID != id {
			continue
		}
		m.traceNodes[i].Status = status
		m.traceNodes[i].EndedAt = time.Now()
		if strings.TrimSpace(summary) != "" {
			m.traceNodes[i].Summary = firstTraceLine(summary)
		}
		if strings.TrimSpace(m.traceNodes[i].Summary) == "" {
			m.traceNodes[i].Summary = traceStatusLabel(status, m.uiLanguage)
		}
		if strings.TrimSpace(detail) != "" {
			m.traceNodes[i].Detail = strings.TrimSpace(detail)
		}
		return m
	}
	return m
}

func renderSubagentTraceDetail(result subagent.Result) string {
	lines := []string{renderSubagentResultLine(result)}
	if len(result.Events) > 0 {
		lines = append(lines, "", "Events:")
		for _, event := range result.Events {
			line := strings.TrimSpace(formatSubagentTraceEvent(event))
			if line != "" {
				lines = append(lines, "- "+line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func formatSubagentTraceEvent(event subagent.Event) string {
	switch event.Kind {
	case subagent.EventAssistantText:
		return firstTraceLine(event.Content)
	case subagent.EventToolCall:
		return traceToolSummary(event.Tool, event.Args)
	case subagent.EventToolResult:
		if event.OK {
			return "ok · " + firstTraceLine(event.Out)
		}
		return "failed · " + firstTraceLine(event.Out)
	case subagent.EventDone:
		return "done"
	case subagent.EventTurnStart:
		return fmt.Sprintf("turn %d started", event.Turn)
	case subagent.EventTurnEnd:
		return fmt.Sprintf("turn %d ended", event.Turn)
	default:
		return ""
	}
}

func traceKindLabel(kind traceKind, lang uiLanguage) string {
	switch kind {
	case traceUser:
		return lang.tr("user", "用户")
	case traceAssistant:
		return lang.tr("assistant", "助手")
	case traceToolCall:
		return lang.tr("tool call", "工具调用")
	case traceToolResult:
		return lang.tr("tool result", "工具结果")
	case traceSubagent:
		return lang.tr("subagent", "子智能体")
	default:
		return string(kind)
	}
}

func traceStatusLabel(status traceStatus, lang uiLanguage) string {
	switch status {
	case tracePending:
		return lang.tr("pending", "等待")
	case traceRunning:
		return lang.tr("running", "运行中")
	case traceDone:
		return lang.tr("done", "完成")
	case traceFailed:
		return lang.tr("failed", "失败")
	case traceBlocked:
		return lang.tr("blocked", "已阻止")
	default:
		return string(status)
	}
}

func (m Model) rebuildTraceFromMessages(messages []models.Message) Model {
	m.traceNodes = nil
	m.traceCursor = 0
	m.traceDetailVisible = false
	m.activeTraceAssistantID = ""
	for _, msg := range messages {
		switch msg.Role {
		case conversation.RoleUser:
			m = m.appendTraceNode(newTraceNode(traceUser, traceDone, "user", msg.Content, msg.Content))
		case conversation.RoleAssistant:
			if msg.ToolCallID != "" {
				node := newTraceNode(traceToolCall, traceDone, msg.ToolName, traceToolSummary(msg.ToolName, msg.ToolInput), msg.ToolInput)
				node.ToolCallID = msg.ToolCallID
				node.ToolName = msg.ToolName
				m = m.appendTraceNode(node)
			} else if strings.TrimSpace(msg.Content) != "" {
				m = m.appendTraceNode(newTraceNode(traceAssistant, traceDone, "assistant", firstTraceLine(msg.Content), msg.Content))
			}
		case conversation.RoleTool:
			node := newTraceNode(traceToolResult, traceDone, "tool result", traceToolResultSummary([]nodeToolResult{{Node: "local", Output: msg.Content, Success: true}}), msg.Content)
			node.ToolCallID = msg.ToolCallID
			m = m.appendTraceNode(node)
		}
	}
	return m
}

func firstTraceLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := strings.Split(text, "\n")[0]
	return truncateWithEllipsis(strings.Join(strings.Fields(line), " "), 120)
}

func traceToolSummary(name, args string) string {
	args = strings.Join(strings.Fields(args), " ")
	if args == "" {
		return name
	}
	return truncateWithEllipsis(fmt.Sprintf("%s %s", name, args), 140)
}

func traceToolResultSummary(results []nodeToolResult) string {
	if len(results) == 0 {
		return "0 nodes"
	}
	okCount := 0
	failCount := 0
	for _, r := range results {
		if r.Success {
			okCount++
		} else {
			failCount++
		}
	}
	if len(results) == 1 {
		status := "failed"
		if okCount == 1 {
			status = "ok"
		}
		return fmt.Sprintf("%s · %s", results[0].Node, status)
	}
	return fmt.Sprintf("%d nodes · %d ok · %d failed", len(results), okCount, failCount)
}
