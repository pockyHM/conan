package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/security"
)

type NodeInfo struct {
	Name             string
	Host             string
	Online           bool
	CommandWhitelist []string
}

type ModelConfig struct {
	Cluster     string
	Model       string
	Version     string
	Provider    llm.Provider
	Conv        *conversation.Conversation
	Clients     map[string]*mcp.Client
	Tools       []llm.ToolDef
	Nodes       []NodeInfo
	Reviewer    *security.Reviewer
	AuditLogger *security.AuditLogger
	ConfigHome  string
	NodeAddRunner nodeAddRunner

	MemoryStore *memory.Store
}

type chatMsg struct {
	role         string
	content      string
	elapsed      time.Duration
	toolCallID   string
	toolName     string
	toolInput    string
	toolOutput   string
	nodeResults  []nodeToolResult
	toolExpanded bool
}

type tuiMode int

const (
	modeChat tuiMode = iota
	modeNodeSelect
	modeConfirm
	modeSession
)

type pingResultMsg struct {
	node   string
	online bool
}

const toolOutputPreviewLines = 4

type Model struct {
	cluster    string
	model      string
	cliVersion string
	provider   llm.Provider
	conv       *conversation.Conversation
	clients    map[string]*mcp.Client
	tools      []llm.ToolDef
	toolCache  *toolCache

	nodes            []NodeInfo
	selectedNodes    map[string]bool
	nodeToolsEnabled bool

	mode         tuiMode
	nodeSelector nodeSelector
	prevSelected map[string]bool

	reviewer        *security.Reviewer
	auditLog        *security.AuditLogger
	configHome      string
	nodeAddRunner   nodeAddRunner
	pendingToolCall *llm.ToolCall
	pendingRisk     *security.RiskAssessment
	confirmChoice   int // 0=Allow, 1=Deny

	memStore    *memory.Store
	sessionList sessionList
	ac          autocomplete

	input              string
	messages           []chatMsg
	status             string
	versionWarning     string
	streaming          bool
	streamBuf          string
	streamCh           <-chan llm.ChatEvent
	streamCtx          context.Context
	streamCancel       context.CancelFunc
	streamID           uint64
	activeStreamID     uint64
	streamStartedAt    time.Time
	streamToolExpected int
	streamToolDone     int
	streamEnded        bool

	width  int
	height int

	vp              viewport.Model
	viewportReady   bool
	lastBodyContent string
}

func NewModel(cfg ModelConfig) Model {
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	selectedNodes := make(map[string]bool)
	for _, node := range cfg.Nodes {
		selectedNodes[node.Name] = true
	}
	return Model{
		cluster:       cfg.Cluster,
		model:         cfg.Model,
		cliVersion:    cfg.Version,
		provider:      cfg.Provider,
		conv:          cfg.Conv,
		clients:       cfg.Clients,
		tools:         cfg.Tools,
		nodes:         cfg.Nodes,
		selectedNodes: selectedNodes,
		status:        "Ready",
		reviewer:      cfg.Reviewer,
		auditLog:      cfg.AuditLogger,
		configHome:    cfg.ConfigHome,
		nodeAddRunner: cfg.NodeAddRunner,
		memStore:      cfg.MemoryStore,
		toolCache:     newToolCache(),
	}
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if len(m.clients) > 0 {
		cmds = append(cmds, fetchNodeTools(m.clients))
	}
	if m.cliVersion != "dev" {
		cmds = append(cmds, m.checkAgentVersions())
	}
	return tea.Batch(cmds...)
}

type streamReadyMsg struct {
	streamID uint64
	ch       <-chan llm.ChatEvent
	err      error
}

type streamEventMsg struct {
	streamID uint64
	Event    llm.ChatEvent
}

type streamDoneMsg struct {
	streamID uint64
}

type nodeToolResult struct {
	Node           string
	Output         string
	Success        bool
	ConnectionLost bool
}

type multiToolResultMsg struct {
	streamID uint64
	Call     llm.ToolCall
	Results  []nodeToolResult
}

type riskAssessmentMsg struct {
	streamID   uint64
	call       llm.ToolCall
	assessment security.RiskAssessment
	err        error
}

type sessionLoadMsg struct {
	record *memory.ConversationRecord
	err    error
}

type versionCheckMsg struct {
	mismatches []mcp.Mismatch
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := max(msg.Height-5, 3)
		if !m.viewportReady {
			m.vp = viewport.New(msg.Width, vpHeight)
			m.viewportReady = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = vpHeight
		}
		m.updateViewportContent()
		return m, nil

	case versionCheckMsg:
		if len(msg.mismatches) > 0 {
			m.versionWarning = versionWarningStatus(msg.mismatches)
		}
		return m, nil

	case tea.MouseMsg:
		if m.viewportReady && m.mode == modeChat {
			var cmd tea.Cmd
			m.updateViewportContent()
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		return m, nil

	case toolCacheBatchMsg:
		for _, u := range msg.updates {
			m.toolCache.Set(u.node, u.tools)
		}
		return m, nil

	case riskAssessmentMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		if msg.err != nil {
			output := "Risk assessment error: " + msg.err.Error()
			m.fillToolPlaceholder(msg.call, output, []nodeToolResult{{Node: "-", Output: msg.err.Error(), Success: false}})
			if m.conv != nil {
				m.conv.AddToolResult(msg.call.ID, output)
			}
			m.markStreamToolDone(msg.streamID)
			return m.resumeAfterStreamTools(msg.streamID)
		}
		switch msg.assessment.Level {
		case security.RiskAllow:
			m.logAuditDecision(msg.call, msg.assessment, "dispatched")
			return m, m.dispatchTool(msg.streamID, msg.call)
		case security.RiskDeny:
			m.logAuditDecision(msg.call, msg.assessment, "blocked")
			denial := "BLOCKED: " + msg.assessment.Reason
			m.fillToolPlaceholder(msg.call, denial, []nodeToolResult{{Node: "-", Output: denial, Success: false}})
			if m.conv != nil {
				m.conv.AddToolResult(msg.call.ID, denial)
			}
			m.markStreamToolDone(msg.streamID)
			return m.resumeAfterStreamTools(msg.streamID)
		case security.RiskConfirm:
			m.logAuditDecision(msg.call, msg.assessment, "pending confirmation")
			m.mode = modeConfirm
			m.pendingToolCall = &msg.call
			m.pendingRisk = &msg.assessment
			m.input = ""
			m.status = "Use ↑↓ to choose, Enter to confirm"
			return m, nil
		}

	case streamReadyMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		if msg.err != nil {
			m.finishStream(false)
			m.status = "Error: " + msg.err.Error()
			return m, nil
		}
		m.streamCh = msg.ch
		return m, m.waitForEvent(msg.streamID)

	case streamEventMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		switch e := msg.Event.(type) {
		case llm.TextDeltaEvent:
			m.streamBuf += e.Delta
		case llm.ToolCallEvent:
			if m.streamBuf != "" {
				if m.conv != nil {
					m.conv.AddAssistant(m.streamBuf)
				}
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf, elapsed: m.streamElapsed()})
				m.streamBuf = ""
			}
			m.streamToolExpected++
			sanitizedArgs := sanitizeToolArguments(e.Name, e.Arguments)
			m.messages = append(m.messages, chatMsg{
				role:       "tool",
				toolCallID: e.ID,
				toolName:   e.Name,
				toolInput:  string(sanitizedArgs),
			})
			if m.conv != nil {
				m.conv.AddToolCall(e.ID, e.Name, string(sanitizedArgs))
			}
			call := llm.ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments}
			var toolCmd tea.Cmd
			if memory.IsMemoryTool(e.Name) {
				toolCmd = m.handleMemoryTool(msg.streamID, call)
			} else {
				toolCmd = m.assessToolRisk(msg.streamID, call)
			}
			return m, tea.Batch(toolCmd, m.waitForEvent(msg.streamID))
		case llm.StopEvent:
			if m.streamBuf != "" {
				if m.conv != nil {
					m.conv.AddAssistant(m.streamBuf)
				}
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf, elapsed: m.streamElapsed()})
			}
			m.streamBuf = ""
			m.streamEnded = true
			if e.Reason == llm.StopToolUse {
				m.status = "Running tool..."
				return m.resumeAfterStreamTools(msg.streamID)
			}
			m.finishStream(false)
			m.status = "Ready"
			return m, nil
		case llm.ErrorEvent:
			if m.streamBuf != "" {
				if m.conv != nil {
					m.conv.AddAssistant(m.streamBuf)
				}
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf, elapsed: m.streamElapsed()})
				m.streamBuf = ""
				m.status = "Stream error; partial content preserved: " + e.Err.Error()
			} else {
				m.status = "Stream error: " + e.Err.Error()
			}
			m.finishStream(false)
			return m, nil
		}
		return m, m.waitForEvent(msg.streamID)

	case streamDoneMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		if m.streamBuf != "" {
			if m.conv != nil {
				m.conv.AddAssistant(m.streamBuf)
			}
			m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf, elapsed: m.streamElapsed()})
			m.streamBuf = ""
		}
		m.streamEnded = true
		return m.resumeAfterStreamTools(msg.streamID)

	case multiToolResultMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		var outputParts []string
		for _, r := range msg.Results {
			if r.ConnectionLost {
				m.markNodeOnline(r.Node, false)
			}
			if r.Success {
				outputParts = append(outputParts, fmt.Sprintf("[%s] %s", r.Node, r.Output))
			} else {
				outputParts = append(outputParts, fmt.Sprintf("[%s] ERROR: %s", r.Node, r.Output))
			}
		}
		aggregatedOutput := strings.Join(outputParts, "\n")

		m.fillToolPlaceholder(msg.Call, aggregatedOutput, msg.Results)
		if m.conv != nil {
			m.conv.AddToolResult(msg.Call.ID, aggregatedOutput)
		}
		m.logAuditExecution(msg.Call, msg.Results)
		m.markStreamToolDone(msg.streamID)
		return m.resumeAfterStreamTools(msg.streamID)

	case pingResultMsg:
		m.markNodeOnline(msg.node, msg.online)
		return m, nil

	case sessionLoadMsg:
		if msg.err != nil {
			m.status = "Error loading session: " + msg.err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf("Resumed session %s (%s)", msg.record.ID, msg.record.Cluster)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeSession {
		return m.handleSessionSelectKey(key)
	}
	if m.mode == modeConfirm {
		return m.handleConfirmKey(key)
	}
	if m.mode == modeNodeSelect {
		return m.handleNodeSelectKey(key)
	}
	if m.streaming {
		if key.Type == tea.KeyCtrlO {
			m.toggleLastToolOutputExpanded()
			return m, nil
		}
		if key.Type == tea.KeyCtrlC {
			m.finishStream(true)
			m.status = "Interrupted"
			return m, nil
		}
		if m.scrollViewportForKey(key) {
			return m, nil
		}
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		m.saveCurrentConversation()
		return m, tea.Quit
	case tea.KeyCtrlO:
		m.toggleLastToolOutputExpanded()
		return m, nil
	case tea.KeyCtrlL:
		m.messages = nil
		m.lastBodyContent = ""
		m.status = "Conversation cleared"
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
			m.ac = m.ac.update(m.input)
		}
		return m, nil
	case tea.KeyEnter:
		m.ac.visible = false
		return m.submit()
	case tea.KeyTab:
		if m.ac.visible {
			comp := m.ac.completion()
			if comp != "" {
				m.input = comp
				m.ac.visible = false
			}
		}
		return m, nil
	case tea.KeyUp:
		if m.ac.visible {
			m.ac = m.ac.moveUp()
		} else {
			m.scrollViewportForKey(key)
		}
		return m, nil
	case tea.KeyDown:
		if m.ac.visible {
			m.ac = m.ac.moveDown()
		} else {
			m.scrollViewportForKey(key)
		}
		return m, nil
	case tea.KeyPgUp:
		m.scrollViewportForKey(key)
		return m, nil
	case tea.KeyPgDown:
		m.scrollViewportForKey(key)
		return m, nil
	case tea.KeyHome:
		m.scrollViewportForKey(key)
		return m, nil
	case tea.KeyEnd:
		m.scrollViewportForKey(key)
		return m, nil
	case tea.KeyRunes:
		m.input += string(key.Runes)
		m.ac = m.ac.update(m.input)
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.ac = m.ac.update(m.input)
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) scrollViewportForKey(key tea.KeyMsg) bool {
	if !m.viewportReady {
		return false
	}
	m.updateViewportContent()
	switch key.Type {
	case tea.KeyUp:
		m.vp.ScrollUp(1)
	case tea.KeyDown:
		m.vp.ScrollDown(1)
	case tea.KeyPgUp:
		m.vp.HalfPageUp()
	case tea.KeyPgDown:
		m.vp.HalfPageDown()
	case tea.KeyHome:
		m.vp.GotoTop()
	case tea.KeyEnd:
		m.vp.GotoBottom()
	default:
		return false
	}
	return true
}

func (m Model) handleConfirmKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		if m.confirmChoice > 0 {
			m.confirmChoice--
		}
		return m, nil
	case tea.KeyDown:
		if m.confirmChoice < len(m.confirmOptions())-1 {
			m.confirmChoice++
		}
		return m, nil
	case tea.KeyEnter:
		call := *m.pendingToolCall
		assessment := m.pendingRisk
		addToAllowlist := m.canAddPendingCommandToAllowlist()
		m.pendingToolCall = nil
		m.pendingRisk = nil
		m.mode = modeChat
		choice := m.confirmChoice
		m.confirmChoice = 0

		if choice == 0 {
			m.status = "Approved — executing..."
			m.logAuditDecision(call, derefRiskAssessment(assessment), "approved")
			return m, m.dispatchTool(m.activeStreamID, call)
		}
		if choice == 1 && addToAllowlist {
			if err := m.addPendingCommandToAllowlist(call); err != nil {
				m.pendingToolCall = &call
				m.pendingRisk = assessment
				m.mode = modeConfirm
				m.confirmChoice = 1
				m.status = "Allowlist update failed: " + err.Error()
				return m, nil
			}
			m.status = "Approved and added to allowlist — executing..."
			m.logAuditDecision(call, derefRiskAssessment(assessment), "approved and allowlisted")
			return m, m.dispatchTool(m.activeStreamID, call)
		}
		m.logAuditDecision(call, derefRiskAssessment(assessment), "cancelled")
		m.fillToolPlaceholder(call, "Cancelled by user", []nodeToolResult{{Node: "-", Output: "Cancelled by user", Success: false}})
		if m.conv != nil {
			m.conv.AddToolResult(call.ID, "Cancelled by user")
		}
		m.status = "Ready"
		m.markStreamToolDone(m.activeStreamID)
		return m.resumeAfterStreamTools(m.activeStreamID)
	case tea.KeyEsc:
		call := *m.pendingToolCall
		assessment := m.pendingRisk
		m.pendingToolCall = nil
		m.pendingRisk = nil
		m.mode = modeChat
		m.confirmChoice = 0
		m.logAuditDecision(call, derefRiskAssessment(assessment), "cancelled")
		m.fillToolPlaceholder(call, "Cancelled by user", []nodeToolResult{{Node: "-", Output: "Cancelled by user", Success: false}})
		if m.conv != nil {
			m.conv.AddToolResult(call.ID, "Cancelled by user")
		}
		m.status = "Ready"
		m.markStreamToolDone(m.activeStreamID)
		return m.resumeAfterStreamTools(m.activeStreamID)
	default:
		return m, nil
	}
}

func (m Model) handleNodeSelectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		m.selectedNodes = m.nodeSelector.Selected()
		m.mode = modeChat
		m.status = fmt.Sprintf("Selected %d node(s)", len(m.selectedNodes))
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.selectedNodes = m.prevSelected
		m.mode = modeChat
		m.status = "Node selection cancelled"
		return m, nil
	default:
		var cmd tea.Cmd
		m.nodeSelector, cmd = m.nodeSelector.Update(key)
		return m, cmd
	}
}

func (m Model) View() string {
	header := renderHeader(m.cluster, m.model, len(m.selectedNodes), len(m.nodes))
	statusView := m.renderStatus()

	if m.mode == modeNodeSelect {
		return header + "\n\n" + m.nodeSelector.View() + "\n\n" + statusView
	}
	var body string

	if m.viewportReady {
		m.updateViewportContent()
		body = m.vp.View()
	} else {
		body = m.renderBody()
	}

	acView := m.ac.View()
	footer := statusView + "\n" + renderInputBox(m.input)
	if m.mode == modeConfirm {
		footer = m.renderConfirmFooter()
	}
	if acView != "" {
		footer = acView + "\n" + footer
	}

	return header + "\n\n" + body + "\n\n" + footer
}

func (m Model) renderConfirmFooter() string {
	reason := ""
	if m.pendingRisk != nil {
		reason = m.pendingRisk.Reason
	}
	command := m.pendingToolCommand()
	options := m.confirmOptions()
	var opts []string
	for i, opt := range options {
		if i == m.confirmChoice {
			opts = append(opts, lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true).Render("\u25b6 "+opt))
		} else {
			opts = append(opts, lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("  "+opt))
		}
	}

	lines := []string{
		inputPromptStyle.Render("Security Review") + "  " + reason,
		toolStyle.Render("Command: ") + command,
		strings.Join(opts, "\n"),
		statusStyle.Render("Use ↑↓ to choose, Enter to confirm, Esc to cancel"),
	}
	if m.pendingRisk != nil && m.pendingRisk.Suggestion != "" {
		lines = append(lines[:1], append([]string{statusStyle.Render("Suggestion: ") + m.pendingRisk.Suggestion}, lines[1:]...)...)
	}
	return strings.Join(lines, "\n")
}

func (m Model) confirmOptions() []string {
	if m.canAddPendingCommandToAllowlist() {
		return []string{"Allow", "Allow and add to allowlist", "Deny"}
	}
	return []string{"Allow", "Deny"}
}

func (m Model) canAddPendingCommandToAllowlist() bool {
	return m.configHome != "" && m.pendingToolCall != nil && isShellCommandTool(m.pendingToolCall.Name) && m.pendingToolCommand() != ""
}

func (m Model) pendingToolCommand() string {
	if m.pendingToolCall == nil {
		return ""
	}
	return toolCommand(*m.pendingToolCall)
}

func isShellCommandTool(toolName string) bool {
	return toolName == "shell/run" || toolName == metaToolExec
}

func (m *Model) addPendingCommandToAllowlist(call llm.ToolCall) error {
	command := strings.TrimSpace(toolCommand(call))
	if command == "" {
		return fmt.Errorf("command is required")
	}
	targets := m.targetNodesForCall(call)
	if len(targets) == 0 {
		return fmt.Errorf("no target nodes resolved")
	}
	writer := cfgloader.NewNodeWriter(m.configHome)
	for _, node := range targets {
		if err := writer.AddCommandWhitelist(m.cluster, node, command); err != nil {
			return err
		}
		for i := range m.nodes {
			if m.nodes[i].Name == node && !stringSliceContains(m.nodes[i].CommandWhitelist, command) {
				m.nodes[i].CommandWhitelist = append(m.nodes[i].CommandWhitelist, command)
			}
		}
		if m.reviewer != nil {
			m.reviewer.AddNodeWhitelist(node, command)
		}
	}
	return nil
}

func stringSliceContains(values []string, value string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == value {
			return true
		}
	}
	return false
}

func toolCommand(call llm.ToolCall) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err == nil && args.Command != "" {
		return args.Command
	}
	return strings.TrimSpace(string(call.Arguments))
}

func (m *Model) updateViewportContent() {
	if !m.viewportReady {
		return
	}
	content := m.renderBody()
	if content == m.lastBodyContent {
		return
	}
	m.lastBodyContent = content
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(content)
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m Model) renderBody() string {
	var bodyParts []string
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			bodyParts = append(bodyParts, renderUserMsg(msg.content))
		case "assistant":
			bodyParts = append(bodyParts, renderMessageDivider(msg.elapsed))
			bodyParts = append(bodyParts, renderAssistantMsg(msg.content))
		case "tool":
			bodyParts = append(bodyParts, renderMessageDivider(msg.elapsed))
			bodyParts = append(bodyParts, m.renderToolMsg(msg))
		}
	}
	if m.streaming && m.streamBuf != "" {
		bodyParts = append(bodyParts, renderStreamingMsg(m.streamBuf))
	}
	body := strings.Join(bodyParts, "\n\n")
	if body == "" {
		body = statusStyle.Render("No messages yet. Type a message or /help.")
	}
	return body
}

func (m Model) streamElapsed() time.Duration {
	if m.streamStartedAt.IsZero() {
		return 0
	}
	return time.Since(m.streamStartedAt).Round(100 * time.Millisecond)
}

func (m Model) renderStatus() string {
	if m.versionWarning == "" {
		return statusStyle.Render(m.status)
	}
	return statusStyle.Render(m.status) + "\n" + statusStyle.Render(m.versionWarning)
}

func (m Model) renderToolMsg(msg chatMsg) string {
	if len(msg.nodeResults) > 1 {
		h := renderToolHeader(msg.toolName, len(msg.nodeResults))
		if msg.toolOutput != "" {
			var lines []string
			for i, r := range msg.nodeResults {
				prefix := "├──"
				if i == len(msg.nodeResults)-1 {
					prefix = "└──"
				}
				lines = append(lines, prefix+" "+renderToolNode(r.Node, r.Success, r.Output, msg.toolExpanded))
			}
			h += "\n" + strings.Join(lines, "\n")
		} else {
			h += " (running...)"
		}
		return h
	}
	h := renderToolHeader(msg.toolName, 0)
	if len(msg.nodeResults) == 1 {
		h = toolStyle.Render(fmt.Sprintf("⏚ %s on %s", msg.toolName, msg.nodeResults[0].Node))
	}
	if msg.toolOutput != "" {
		output := msg.toolOutput
		if len(msg.nodeResults) == 1 {
			output = msg.nodeResults[0].Output
		}
		h += "\n" + renderToolOutput(output, msg.toolExpanded)
	} else {
		h += " (running...)"
	}
	return h
}

func (m *Model) toggleLastToolOutputExpanded() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.role != "tool" || msg.toolOutput == "" {
			continue
		}
		msg.toolExpanded = !msg.toolExpanded
		m.lastBodyContent = ""
		if msg.toolExpanded {
			m.status = "Last tool output expanded"
		} else {
			m.status = "Last tool output collapsed"
		}
		m.updateViewportContent()
		return
	}
	m.status = "No tool output to expand"
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	if input == "" {
		return m, nil
	}
	if cmd, ok := ParseSlashCommand(input); ok {
		var c tea.Cmd
		m, c = m.applyCommand(cmd)
		if cmd.Kind == CommandExit {
			m.saveCurrentConversation()
			return m, tea.Quit
		}
		return m, c
	}
	if m.provider == nil {
		m.messages = append(m.messages, chatMsg{role: "user", content: input})
		m.status = "No LLM provider configured"
		return m, nil
	}
	m.messages = append(m.messages, chatMsg{role: "user", content: input})
	if m.conv != nil {
		m.conv.AddUser(input)
	}
	m.streaming = true
	m.streamBuf = ""
	m.status = "Thinking..."
	return m.startStream()
}

func (m Model) applyCommand(cmd SlashCommand) (Model, tea.Cmd) {
	switch cmd.Kind {
	case CommandHelp:
		m.messages = append(m.messages, chatMsg{
			role:    "assistant",
			content: "Conan: /help /clear /exit /cluster [name] /model [name] /node [off] /nodes /memory /resume /config",
		})
		m.status = "Help shown"
	case CommandClear:
		m.messages = nil
		if m.conv != nil {
			m.conv.Clear()
		}
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
	case CommandNode:
		switch strings.TrimSpace(cmd.Arg) {
		case "":
			m.nodeToolsEnabled = true
			m.status = "Node management enabled for next model response"
		case "off":
			m.nodeToolsEnabled = false
			m.status = "Node management disabled"
		default:
			m.status = "Usage: /node [off]"
		}
	case CommandNodes:
		if len(m.nodes) == 0 {
			m.status = "No nodes configured"
			return m, nil
		}
		m.mode = modeNodeSelect
		m.prevSelected = m.selectedNodes
		m.nodeSelector = newNodeSelector(m.nodes, m.selectedNodes)
		m.status = "Checking node status..."
		return m, m.pingNodes()
	case CommandMemory:
		if m.memStore == nil {
			m.status = "Memory not available"
			return m, nil
		}
		results, err := m.memStore.ListMemories("", 10)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		if len(results) == 0 {
			m.status = "No memories stored yet"
			return m, nil
		}
		var lines []string
		for _, r := range results {
			lines = append(lines, fmt.Sprintf("[%s] %s: %s", r.ID, r.Title, truncateStr(r.Content, 60)))
		}
		m.messages = append(m.messages, chatMsg{role: "assistant", content: "Memory:\n" + strings.Join(lines, "\n")})
		m.status = fmt.Sprintf("%d memories", len(results))
	case CommandResume:
		if m.memStore == nil {
			m.status = "Memory not available"
			return m, nil
		}
		if cmd.Arg != "" {
			return m, m.loadSession(cmd.Arg)
		}
		sessions, err := m.memStore.ListConversations(20)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		if len(sessions) == 0 {
			m.status = "No previous sessions"
			return m, nil
		}
		var infos []SessionInfo
		for _, s := range sessions {
			summary := s.Summary
			if summary == "" {
				summary = "(no summary)"
			}
			infos = append(infos, SessionInfo{
				ID:        s.ID,
				Cluster:   s.Cluster,
				CreatedAt: s.CreatedAt,
				Summary:   summary,
			})
		}
		m.mode = modeSession
		m.sessionList = newSessionList(infos)
		m.status = "Select a session to resume"
		return m, nil
	default:
		m.status = "Unknown command: /" + cmd.Arg
	}
	return m, nil
}

func (m Model) startStream() (Model, tea.Cmd) {
	if m.provider == nil {
		return m, nil
	}
	m.cancelActiveStream()
	m.streamID++
	m.activeStreamID = m.streamID
	m.streaming = true
	m.streamBuf = ""
	m.streamCh = nil
	m.streamToolExpected = 0
	m.streamToolDone = 0
	m.streamEnded = false
	ctx, cancel := context.WithCancel(context.Background())
	m.streamCtx = ctx
	m.streamCancel = cancel
	m.streamStartedAt = time.Now()
	provider := m.provider
	streamID := m.activeStreamID

	allTools := m.availableToolDefs()

	req := &llm.ChatRequest{
		SystemPrompt: m.buildSystemPromptWithMemory(),
		Messages:     m.conv.Messages(),
		Tools:        allTools,
	}
	return m, func() tea.Msg {
		ch, err := provider.ChatStream(ctx, req)
		return streamReadyMsg{streamID: streamID, ch: ch, err: err}
	}
}

func (m Model) availableToolDefs() []llm.ToolDef {
	allTools := make([]llm.ToolDef, 0, len(metaToolDefs)+len(nodeManagementToolDefs)+5)
	allTools = append(allTools, metaToolDefs...)
	if m.nodeToolsEnabled {
		allTools = append(allTools, nodeManagementToolDefs...)
	}
	if m.memStore != nil {
		for _, td := range memory.ToolDefs() {
			b, err := json.Marshal(td)
			if err != nil {
				continue
			}
			var def llm.ToolDef
			if err := json.Unmarshal(b, &def); err != nil {
				continue
			}
			allTools = append(allTools, def)
		}
	}
	return allTools
}

func (m Model) waitForEvent(streamID uint64) tea.Cmd {
	ch := m.streamCh
	ctx := m.streamCtx
	if ch == nil || ctx == nil || !m.isActiveStream(streamID) {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return streamDoneMsg{streamID: streamID}
		case event, ok := <-ch:
			if !ok {
				return streamDoneMsg{streamID: streamID}
			}
			return streamEventMsg{streamID: streamID, Event: event}
		}
	}
}

func (m Model) isActiveStream(streamID uint64) bool {
	return streamID != 0 && m.activeStreamID == streamID
}

func (m *Model) finishStream(cancel bool) {
	if cancel {
		m.cancelActiveStream()
	}
	m.clearNodeToolExposure()
	m.streaming = false
	m.streamBuf = ""
	m.streamCh = nil
	m.streamCtx = nil
	m.streamCancel = nil
	m.activeStreamID = 0
	m.streamStartedAt = time.Time{}
	m.streamToolExpected = 0
	m.streamToolDone = 0
	m.streamEnded = false
}

func (m *Model) clearNodeToolExposure() {
	m.nodeToolsEnabled = false
}

func (m Model) debugLogStreamEvent(event llm.ChatEvent) {
	switch e := event.(type) {
	case llm.TextDeltaEvent:
		slog.Debug("llm stream text_delta", "stream_id", m.activeStreamID, "delta", e.Delta, "delta_len", len(e.Delta))
	case llm.ToolCallEvent:
		sanitizedArgs := sanitizeToolArguments(e.Name, e.Arguments)
		slog.Debug("llm stream tool_call", "stream_id", m.activeStreamID, "id", e.ID, "name", e.Name, "arguments", string(sanitizedArgs))
	case llm.StopEvent:
		slog.Debug("llm stream stop", "stream_id", m.activeStreamID, "reason", e.Reason, "buffer_len", len(m.streamBuf), "tool_calls", m.streamToolExpected)
	case llm.ErrorEvent:
		errText := ""
		if e.Err != nil {
			errText = e.Err.Error()
		}
		slog.Debug("llm stream error", "stream_id", m.activeStreamID, "error", errText, "buffer_len", len(m.streamBuf))
	default:
		slog.Debug("llm stream event", "stream_id", m.activeStreamID, "type", fmt.Sprintf("%T", event))
	}
}

func (m *Model) markStreamToolDone(streamID uint64) {
	if streamID != 0 && !m.isActiveStream(streamID) {
		return
	}
	m.streamToolDone++
}

func (m Model) resumeAfterStreamTools(streamID uint64) (tea.Model, tea.Cmd) {
	if streamID == 0 {
		return m.startStream()
	}
	if !m.isActiveStream(streamID) {
		return m, nil
	}
	if !m.streamEnded || m.streamToolDone < m.streamToolExpected {
		return m, nil
	}
	if m.streamToolExpected == 0 {
		m.finishStream(false)
		m.status = "Stream ended"
		return m, nil
	}
	return m.startStream()
}

func (m *Model) cancelActiveStream() {
	if m.streamCancel != nil {
		m.streamCancel()
	}
}

func (m Model) assessToolRisk(streamID uint64, call llm.ToolCall) tea.Cmd {
	reviewer := m.reviewer
	if reviewer == nil {
		return m.dispatchTool(streamID, call)
	}
	ctx := m.streamCtx
	if ctx == nil {
		ctx = context.Background()
	}
	targetNodes := m.targetNodesForCall(call)
	return func() tea.Msg {
		reviewInput := string(sanitizeToolArguments(call.Name, call.Arguments))
		assessment, err := reviewer.Review(ctx, call.Name, reviewInput, targetNodes)
		return riskAssessmentMsg{streamID: streamID, call: call, assessment: assessment, err: err}
	}
}

func (m Model) targetNodesForCall(call llm.ToolCall) []string {
	if call.Name == metaToolExec || call.Name == metaToolCallTool || call.Name == metaToolToolSearch {
		args, err := parseMetaArgs(call.Arguments)
		if err == nil && args.Node != "" {
			if m.hasConfiguredNode(args.Node) {
				return []string{args.Node}
			}
			return nil
		}
	}
	return m.selectedNodeNames()
}

func (m Model) hasConfiguredNode(name string) bool {
	if _, ok := m.clients[name]; ok {
		return true
	}
	for _, node := range m.nodes {
		if node.Name == name {
			return true
		}
	}
	return false
}

func (m Model) dispatchTool(streamID uint64, call llm.ToolCall) tea.Cmd {
	switch call.Name {
	case metaToolExec:
		return m.dispatchExec(streamID, call)
	case metaToolToolSearch:
		return m.dispatchToolSearch(streamID, call)
	case metaToolCallTool:
		return m.dispatchCallTool(streamID, call)
	case metaToolNodeAdd:
		return m.dispatchNodeAdd(streamID, call)
	default:
		return m.dispatchMemoryOrDirectTool(streamID, call)
	}
}

type metaCallArgs struct {
	Node      string          `json:"node"`
	Command   string          `json:"command"`
	Tool      string          `json:"tool"`
	Query     string          `json:"query"`
	Arguments json.RawMessage `json:"arguments"`
}

func parseMetaArgs(raw json.RawMessage) (metaCallArgs, error) {
	var args metaCallArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, err
	}
	return args, nil
}

func (m Model) resolveTargetNodes(specified string) []string {
	if specified != "" {
		if _, exists := m.clients[specified]; !exists {
			return nil
		}
		return []string{specified}
	}
	nodes := make([]string, 0, len(m.selectedNodes))
	for name := range m.selectedNodes {
		nodes = append(nodes, name)
	}
	sort.Strings(nodes)
	return nodes
}

func (m Model) dispatchExec(streamID uint64, call llm.ToolCall) tea.Cmd {
	clients := m.clients
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return func() tea.Msg {
		args, err := parseMetaArgs(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "invalid arguments: " + err.Error(), Success: false}}}
		}
		if args.Command == "" {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "command is required", Success: false}}}
		}
		targets := m.resolveTargetNodes(args.Node)
		if len(targets) == 0 {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "No nodes selected or node not found. Use /nodes to select target nodes.", Success: false}}}
		}
		return m.fanOutCallTool(streamID, call, targets, clients, "shell/run", func() json.RawMessage {
			b, _ := json.Marshal(map[string]string{"command": args.Command})
			return b
		}, parentCtx)
	}
}

func (m Model) dispatchToolSearch(streamID uint64, call llm.ToolCall) tea.Cmd {
	return func() tea.Msg {
		args, err := parseMetaArgs(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "invalid arguments: " + err.Error(), Success: false}}}
		}
		if args.Query == "" {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "query is required", Success: false}}}
		}
		searchNodes := m.resolveTargetNodes(args.Node)
		if len(searchNodes) == 0 {
			searchNodes = make([]string, 0, len(m.clients))
			for name := range m.clients {
				searchNodes = append(searchNodes, name)
			}
			sort.Strings(searchNodes)
		}
		results := m.toolCache.Search(args.Query, searchNodes)
		if len(results) == 0 {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "No tools found matching query: " + args.Query, Success: true}}}
		}
		b, _ := json.MarshalIndent(results, "", "  ")
		return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: string(b), Success: true}}}
	}
}

func (m Model) dispatchCallTool(streamID uint64, call llm.ToolCall) tea.Cmd {
	clients := m.clients
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return func() tea.Msg {
		args, err := parseMetaArgs(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "invalid arguments: " + err.Error(), Success: false}}}
		}
		if args.Tool == "" {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "tool name is required", Success: false}}}
		}
		targets := m.resolveTargetNodes(args.Node)
		if len(targets) == 0 {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "-", Output: "No nodes selected or node not found. Use /nodes to select target nodes.", Success: false}}}
		}
		toolArgs := args.Arguments
		if toolArgs == nil {
			toolArgs = json.RawMessage("{}")
		}
		return m.fanOutCallTool(streamID, call, targets, clients, args.Tool, func() json.RawMessage { return toolArgs }, parentCtx)
	}
}

func (m Model) dispatchMemoryOrDirectTool(streamID uint64, call llm.ToolCall) tea.Cmd {
	clients := m.clients
	selected := m.selectedNodes
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return func() tea.Msg {
		if len(selected) == 0 {
			return multiToolResultMsg{
				streamID: streamID,
				Call:     call,
				Results:  []nodeToolResult{{Node: "-", Output: "No nodes selected.", Success: false}},
			}
		}
		return m.fanOutCallTool(streamID, call, m.resolveTargetNodes(""), clients, call.Name, func() json.RawMessage { return call.Arguments }, parentCtx)
	}
}

func (m Model) fanOutCallTool(streamID uint64, call llm.ToolCall, targets []string, clients map[string]*mcp.Client, toolName string, toolArgs func() json.RawMessage, parentCtx context.Context) multiToolResultMsg {
	type result struct {
		node           string
		output         string
		success        bool
		connectionLost bool
	}

	var wg sync.WaitGroup
	ch := make(chan result, len(targets))

	for _, name := range targets {
		client, exists := clients[name]
		if !exists {
			ch <- result{node: name, output: "no client configured for node", success: false}
			continue
		}
		wg.Add(1)
		go func(n string, c *mcp.Client) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
			defer cancel()
			toolResult, err := c.CallTool(ctx, toolName, toolArgs())
			if err != nil {
				classified := mcp.ClassifyError(err)
				ch <- result{node: n, output: err.Error(), success: false, connectionLost: classified.Type == mcp.ErrorConnection}
				return
			}
			var output strings.Builder
			for _, block := range toolResult.Content {
				output.WriteString(block.Text)
			}
			ch <- result{node: n, output: output.String(), success: true}
		}(name, client)
	}

	wg.Wait()
	close(ch)

	var results []nodeToolResult
	for r := range ch {
		results = append(results, nodeToolResult{Node: r.node, Output: r.output, Success: r.success, ConnectionLost: r.connectionLost})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Node < results[j].Node
	})

	return multiToolResultMsg{streamID: streamID, Call: call, Results: results}
}

func (m Model) pingNodes() tea.Cmd {
	clients := m.clients
	var cmds []tea.Cmd
	for name, client := range clients {
		c := client
		n := name
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.Ping(ctx)
			return pingResultMsg{node: n, online: err == nil}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) checkAgentVersions() tea.Cmd {
	clients := m.clients
	cliVersion := m.cliVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		results := mcp.CheckVersions(ctx, clients)
		return versionCheckMsg{mismatches: mcp.CheckVersionMismatches(cliVersion, results)}
	}
}

func versionWarningStatus(mismatches []mcp.Mismatch) string {
	parts := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		if mismatch.IsError {
			parts = append(parts, fmt.Sprintf("%s: unreachable (%s)", mismatch.Node, mismatch.Got))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s (expected %s)", mismatch.Node, mismatch.Got, mismatch.Expected))
	}
	return "Version warning: " + strings.Join(parts, "; ")
}

func (m Model) buildSystemPromptWithMemory() string {
	nodes := make([]string, 0, len(m.selectedNodes))
	for name := range m.selectedNodes {
		nodes = append(nodes, name)
	}
	sort.Strings(nodes)

	var parts []string
	parts = append(parts, "You are Conan, an AI operations assistant. Help the user manage their infrastructure.")
	parts = append(parts, fmt.Sprintf("Cluster: %s", m.cluster))
	if len(nodes) > 0 {
		parts = append(parts, fmt.Sprintf("Available nodes: %s. Use the node parameter in tools to target a specific node. If omitted, the tool runs on all nodes.", strings.Join(nodes, ", ")))
	}

	if m.memStore != nil {
		rc, err := memory.LoadRules(filepath.Join(m.memStore.Dir(), "memory"))
		if err == nil && !rc.Empty() {
			parts = append(parts, "\n[Behavioral Rules]\n"+rc.Format())
		}
		results, err := m.memStore.ListMemories("", 5)
		if err == nil && len(results) > 0 {
			var memLines []string
			for _, r := range results {
				memLines = append(memLines, fmt.Sprintf("- [%s] %s: %s", r.Category, r.Title, r.Content))
			}
			parts = append(parts, "\n[Memory Context]\n"+strings.Join(memLines, "\n"))
		}
	}

	return strings.Join(parts, "\n")
}

func (m *Model) markNodeOnline(node string, online bool) {
	for i := range m.nodes {
		if m.nodes[i].Name == node {
			m.nodes[i].Online = online
			break
		}
	}
	if m.mode == modeNodeSelect {
		m.nodeSelector = m.nodeSelector.SetNodes(m.nodes)
	}
}

func (m *Model) fillToolPlaceholder(call llm.ToolCall, output string, results []nodeToolResult) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.role != "tool" || msg.toolOutput != "" {
			continue
		}
		if msg.toolCallID == call.ID {
			msg.toolOutput = output
			msg.nodeResults = results
			msg.elapsed = m.streamElapsed()
			return
		}
	}
	sanitizedArgs := sanitizeToolArguments(call.Name, call.Arguments)
	m.messages = append(m.messages, chatMsg{
		role:        "tool",
		elapsed:     m.streamElapsed(),
		toolCallID:  call.ID,
		toolName:    call.Name,
		toolInput:   string(sanitizedArgs),
		toolOutput:  output,
		nodeResults: results,
	})
}

func (m Model) logAuditDecision(call llm.ToolCall, assessment security.RiskAssessment, outcome string) {
	if m.auditLog == nil {
		return
	}
	sanitizedArgs := sanitizeToolArguments(call.Name, call.Arguments)
	m.auditLog.Log(security.AuditEntry{
		Tool:    call.Name,
		Input:   string(sanitizedArgs),
		Risk:    auditRiskName(assessment.Level),
		Outcome: outcome,
		Reason:  assessment.Reason,
		Nodes:   m.selectedNodeNames(),
	})
}

func (m Model) logAuditExecution(call llm.ToolCall, results []nodeToolResult) {
	if m.auditLog == nil {
		return
	}
	outcome := "success"
	for _, result := range results {
		if !result.Success {
			outcome = "failure"
			break
		}
	}
	sanitizedArgs := sanitizeToolArguments(call.Name, call.Arguments)
	m.auditLog.Log(security.AuditEntry{
		Tool:    call.Name,
		Input:   string(sanitizedArgs),
		Risk:    "EXECUTE",
		Outcome: outcome,
		Nodes:   resultNodeNames(results),
	})
}

func (m Model) selectedNodeNames() []string {
	nodes := make([]string, 0, len(m.selectedNodes))
	for name, selected := range m.selectedNodes {
		if selected {
			nodes = append(nodes, name)
		}
	}
	sort.Strings(nodes)
	return nodes
}

func resultNodeNames(results []nodeToolResult) []string {
	nodes := make([]string, 0, len(results))
	for _, result := range results {
		nodes = append(nodes, result.Node)
	}
	sort.Strings(nodes)
	return nodes
}

func auditRiskName(level security.RiskLevel) string {
	switch level {
	case security.RiskAllow:
		return "ALLOW"
	case security.RiskConfirm:
		return "CONFIRM"
	case security.RiskDeny:
		return "DENY"
	default:
		return "UNKNOWN"
	}
}

func derefRiskAssessment(assessment *security.RiskAssessment) security.RiskAssessment {
	if assessment == nil {
		return security.RiskAssessment{Level: security.RiskConfirm, Reason: "confirmation outcome"}
	}
	return *assessment
}

func (m Model) handleMemoryTool(streamID uint64, call llm.ToolCall) tea.Cmd {
	store := m.memStore
	convID := ""
	if m.conv != nil {
		convID = m.conv.ID()
	}
	return func() tea.Msg {
		result := memory.HandleTool(store, convID, call.Name, call.Arguments)
		return multiToolResultMsg{
			streamID: streamID,
			Call:     call,
			Results: []nodeToolResult{
				{Node: "local", Output: result.Output, Success: result.Success},
			},
		}
	}
}

func (m Model) handleSessionSelectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		selected := m.sessionList.Selected()
		m.mode = modeChat
		if selected != nil {
			m.status = fmt.Sprintf("Loading session %s...", selected.ID)
			return m, m.loadSession(selected.ID)
		}
		m.status = "No session selected"
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeChat
		m.status = "Resume cancelled"
		return m, nil
	default:
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(key)
		return m, cmd
	}
}

func (m Model) loadSession(id string) tea.Cmd {
	store := m.memStore
	return func() tea.Msg {
		rec, err := store.LoadConversation(id)
		if err != nil {
			return sessionLoadMsg{err: err}
		}
		return sessionLoadMsg{record: rec}
	}
}

func (m Model) saveCurrentConversation() {
	if m.memStore == nil || m.conv == nil {
		return
	}
	msgs := m.conv.Messages()
	msgJSON, _ := json.Marshal(msgs)
	nodes := make([]string, 0, len(m.selectedNodes))
	for n := range m.selectedNodes {
		nodes = append(nodes, n)
	}
	nodesJSON, _ := json.Marshal(nodes)
	m.memStore.SaveConversation(memory.ConversationRecord{
		ID:       m.conv.ID(),
		Cluster:  m.cluster,
		Nodes:    string(nodesJSON),
		Model:    m.model,
		Messages: string(msgJSON),
	})
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
