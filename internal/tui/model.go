package tui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/fileguard"
	"github.com/pockyHM/conan/internal/fileref"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/localtools"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/internal/skills"
	"github.com/pockyHM/conan/internal/subagent"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/models"
)

type NodeInfo struct {
	Name             string
	Host             string
	Online           bool
	CommandWhitelist []string
}

type ModelConfig struct {
	Cluster            string
	Model              string
	UILanguage         string
	InitialSessionID   string
	Version            string
	Provider           llm.Provider
	VisionProvider     llm.VisionProvider
	VisionError        string
	Vision             configschema.VisionConfig
	Conv               *conversation.Conversation
	Clients            map[string]*mcp.Client
	Tools              []llm.ToolDef
	Nodes              []NodeInfo
	Reviewer           *security.Reviewer
	AuditLogger        *security.AuditLogger
	ConfigHome         string
	LocalWorkspaceRoot string
	NodeAddRunner      nodeAddRunner
	Skills             []skills.Skill
	SkillsConfig       configschema.SkillsConfig
	SkillWarnings      []string

	MemoryStore     *memory.Store
	MemoryExtractor MemoryExtractor
	Subagents       configschema.SubagentConfig
}

type MemoryExtractionInput struct {
	Cluster   string
	Model     string
	User      string
	Assistant string
}

type MemoryExtractor interface {
	ExtractMemory(context.Context, MemoryExtractionInput) ([]memory.MemoryCandidate, error)
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
	hidden       bool
}

type tuiMode int

const (
	modeChat tuiMode = iota
	modeNodeSelect
	modeConfirm
	modeSession
	modeNodePrompt
	modeLangSelect
)

type pingResultMsg struct {
	node   string
	online bool
}

type nodePromptState struct {
	streamID uint64
	call     llm.ToolCall
	field    string
	label    string
	secret   bool
}

const toolOutputPreviewLines = 4
const streamEventTimeout = 60 * time.Second
const markdownPromptMemoryLimit = 3700
const sqlitePromptMemoryContentLimit = 900
const maxResumedVisibleMessages = 20

type Model struct {
	cluster          string
	clusterExplicit  bool
	model            string
	uiLanguage       uiLanguage
	initialSessionID string
	cliVersion       string
	provider         llm.Provider
	visionProvider   llm.VisionProvider
	visionError      string
	vision           configschema.VisionConfig
	conv             *conversation.Conversation
	clients          map[string]*mcp.Client
	tools            []llm.ToolDef
	toolCache        *toolCache

	nodes            []NodeInfo
	selectedNodes    map[string]bool
	nodeToolsEnabled bool

	mode         tuiMode
	nodeSelector nodeSelector
	langSelector langSelector
	prevSelected map[string]bool

	reviewer           *security.Reviewer
	auditLog           *security.AuditLogger
	configHome         string
	localWorkspaceRoot string
	nodeAddRunner      nodeAddRunner
	skills             []skills.Skill
	skillsConfig       configschema.SkillsConfig
	skillWarnings      []string
	pendingToolCall    *llm.ToolCall
	pendingRisk        *security.RiskAssessment
	confirmChoice      int // 0=Allow, 1=Deny
	nodePrompt         nodePromptState

	memStore        *memory.Store
	memoryExtractor MemoryExtractor
	subagents       configschema.SubagentConfig
	subagentResults []subagent.Result
	sessionList     sessionList
	ac              autocomplete

	input              string
	pendingImages      []imageAttachment
	attachedImages     []imageAttachment
	inputHistory       []string
	inputHistoryIndex  int
	inputHistoryDraft  string
	messages           []chatMsg
	status             string
	versionWarning     string
	streaming          bool
	streamBuf          string
	streamReasoningBuf string
	streamThinking     *bool
	streamCh           <-chan llm.ChatEvent
	streamCtx          context.Context
	streamCancel       context.CancelFunc
	streamID           uint64
	activeStreamID     uint64
	streamStartedAt    time.Time
	streamToolExpected int
	streamToolDone     int
	streamEnded        bool
	streamEventSeq     int
	thinkingFrame      int

	width  int
	height int

	vp              viewport.Model
	viewportReady   bool
	lastBodyContent string
	lastBodyChat    bool
}

func NewModel(cfg ModelConfig) Model {
	clusterExplicit := cfg.Cluster != ""
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	language := normalizeUILanguage(cfg.UILanguage)
	selectedNodes := make(map[string]bool)
	for _, node := range cfg.Nodes {
		selectedNodes[node.Name] = true
	}
	return Model{
		cluster:            cfg.Cluster,
		clusterExplicit:    clusterExplicit,
		model:              cfg.Model,
		uiLanguage:         language,
		initialSessionID:   cfg.InitialSessionID,
		cliVersion:         cfg.Version,
		provider:           cfg.Provider,
		visionProvider:     cfg.VisionProvider,
		visionError:        cfg.VisionError,
		vision:             normalizeVisionConfig(cfg.Vision),
		conv:               cfg.Conv,
		clients:            cfg.Clients,
		tools:              cfg.Tools,
		nodes:              cfg.Nodes,
		selectedNodes:      selectedNodes,
		status:             language.tr("Ready", "就绪"),
		reviewer:           cfg.Reviewer,
		auditLog:           cfg.AuditLogger,
		configHome:         cfg.ConfigHome,
		localWorkspaceRoot: cfg.LocalWorkspaceRoot,
		nodeAddRunner:      cfg.NodeAddRunner,
		skills:             cfg.Skills,
		skillsConfig:       normalizeSkillsConfig(cfg.SkillsConfig),
		skillWarnings:      append([]string(nil), cfg.SkillWarnings...),
		memStore:           cfg.MemoryStore,
		memoryExtractor:    cfg.MemoryExtractor,
		subagents:          normalizeSubagentConfig(cfg.Subagents),
		toolCache:          newToolCache(),
		ac:                 newAutocompleteWithLanguage(language),
		inputHistoryIndex:  -1,
	}
}

func normalizeSkillsConfig(cfg configschema.SkillsConfig) configschema.SkillsConfig {
	enabled := cfg.Enabled
	if cfg.IndexTokenBudget == 0 {
		cfg.IndexTokenBudget = 800
	}
	if cfg.MaxSkillChars == 0 {
		cfg.MaxSkillChars = 6000
	}
	if cfg.MaxVisibleSkills == 0 {
		cfg.MaxVisibleSkills = 50
	}
	cfg.Enabled = enabled
	return cfg
}

func (m Model) skillsAvailable() bool {
	return m.skillsConfig.Enabled && len(m.skills) > 0
}

func (m Model) findSkill(name string) (skills.Skill, bool) {
	if !m.skillsAvailable() {
		return skills.Skill{}, false
	}
	for _, skill := range m.skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return skills.Skill{}, false
}

func (m Model) visibleSkillsSummary() string {
	if !m.skillsConfig.Enabled {
		return m.uiLanguage.tr("Skills are disabled", "技能已禁用")
	}
	if len(m.skills) == 0 {
		return m.uiLanguage.tr("No skills available for this cluster", "当前集群没有可用技能")
	}
	lines := make([]string, 0, len(m.skills)+1)
	lines = append(lines, m.uiLanguage.tr("Available skills:", "可用技能:"))
	for _, skill := range m.skills {
		scope := string(skill.Scope)
		if skill.Scope == skills.ScopeCluster {
			scope = "cluster:" + skill.Cluster
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", skill.Name, scope, skill.Description))
	}
	return strings.Join(lines, "\n")
}

func splitSkillInvocationArg(arg string) (name string, rest string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ""
	}
	fields := strings.Fields(arg)
	name = fields[0]
	if len(fields) == 1 {
		return name, ""
	}
	_, rest, _ = strings.Cut(arg, name)
	return name, strings.TrimSpace(rest)
}

func formatExplicitSkillMessage(skill skills.Skill, args string, maxChars int) string {
	raw, err := json.Marshal(map[string]string{
		"name":   skill.Name,
		"reason": "explicit slash invocation",
	})
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"name":%q,"reason":"explicit slash invocation"}`, skill.Name))
	}
	body := skills.NewToolHandler([]skills.Skill{skill}, maxChars).Handle(raw)
	args = strings.TrimSpace(args)
	if args == "" {
		args = "(none)"
	}
	return fmt.Sprintf("Use Conan skill %q for this request.\n\nUser arguments:\n%s\n\nSkill instructions:\n%s", skill.Name, args, body)
}

func (m Model) submitSkillMessage(visibleInput string, skill skills.Skill, args string) (Model, tea.Cmd) {
	next, c := m.submitProcessedMessage(visibleInput, args, formatExplicitSkillMessage(skill, args, m.skillsConfig.MaxSkillChars), nil)
	return next.(Model), c
}

func normalizeSubagentConfig(cfg configschema.SubagentConfig) configschema.SubagentConfig {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 3
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 120
	}
	return cfg
}

func normalizeVisionConfig(cfg configschema.VisionConfig) configschema.VisionConfig {
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = 10
	}
	if cfg.MaxSummaryCharsPerImage <= 0 {
		cfg.MaxSummaryCharsPerImage = 1200
	}
	return cfg
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if strings.TrimSpace(m.initialSessionID) != "" {
		cmds = append(cmds, m.loadSession(strings.TrimSpace(m.initialSessionID)))
	}
	if len(m.clients) > 0 {
		cmds = append(cmds, m.pingNodes())
		cmds = append(cmds, fetchNodeTools(m.clients))
	}
	if m.cliVersion != "dev" {
		cmds = append(cmds, m.checkAgentVersions())
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
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

type thinkingTickMsg struct {
	streamID uint64
}

type streamTimeoutMsg struct {
	streamID uint64
	eventSeq int
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

type compactResultMsg struct {
	oldMessages []models.Message
	messages    []models.Message
	oldCount    int
	keptCount   int
	err         error
}

type versionCheckMsg struct {
	mismatches []mcp.Mismatch
}

type subagentCommandResultMsg struct {
	result subagent.Result
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

	case subagentCommandResultMsg:
		m.subagentResults = append(m.subagentResults, msg.result)
		content := renderSubagentCommandResult(msg.result)
		m.messages = append(m.messages, chatMsg{role: "assistant", content: content, elapsed: msg.result.Elapsed})
		if msg.result.Err != nil {
			m.status = m.uiLanguage.tr("Subagent failed: ", "Subagent 失败: ") + msg.result.Err.Error()
		} else {
			m.status = m.uiLanguage.tr("Subagent completed", "Subagent 已完成")
		}
		m.updateViewportContent()
		return m, nil

	case clipboardPasteMsg:
		if msg.err != nil {
			m.status = m.uiLanguage.tr("Clipboard paste failed: ", "剪贴板粘贴失败: ") + msg.err.Error()
			return m, nil
		}
		if msg.image.Path != "" {
			m.pendingImages = append(m.pendingImages, msg.image)
			m.status = fmt.Sprintf(m.uiLanguage.tr("Attached image #%d (%dx%d)", "已附加图片 #%d (%dx%d)"), msg.image.ID, msg.image.Width, msg.image.Height)
			return m, nil
		}
		if msg.text != "" {
			m.input += msg.text
			m.resetInputHistoryNavigation()
			m.ac = m.updateAutocomplete()
			return m, nil
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

	case nodeAddToolsFetchedMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		for _, u := range msg.updates {
			m.toolCache.Set(u.node, u.tools)
		}
		m.markStreamToolDone(msg.streamID)
		return m.resumeAfterStreamTools(msg.streamID)

	case nodeAddPromptMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		m.mode = modeNodePrompt
		m.nodePrompt = nodePromptState{
			streamID: msg.streamID,
			call:     msg.call,
			field:    msg.field,
			label:    msg.label,
			secret:   msg.secret,
		}
		m.input = ""
		m.ac = newAutocompleteWithLanguage(m.uiLanguage)
		m.status = msg.label + " " + m.uiLanguage.tr("required", "必填")
		m.updateViewportContent()
		return m, nil

	case nodeAddReadyMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		m.mode = modeChat
		m.nodePrompt = nodePromptState{}
		m.status = m.uiLanguage.tr("Running tool...", "正在运行工具...")
		return m, m.dispatchNodeAdd(msg.streamID, msg.call)

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
			return m.completeToolAndResume(msg.streamID, msg.call)
		}
		switch msg.assessment.Level {
		case security.RiskAllow:
			if requiresExplicitConfirmation(msg.call.Name) {
				msg.assessment.Level = security.RiskConfirm
				if msg.assessment.Reason == "" || msg.assessment.Reason == "allowed" {
					msg.assessment.Reason = msg.call.Name + " requires confirmation"
				}
				m.logAuditDecision(msg.call, msg.assessment, "pending confirmation")
				m.mode = modeConfirm
				m.pendingToolCall = &msg.call
				m.pendingRisk = &msg.assessment
				m.input = ""
				m.status = m.uiLanguage.tr("Use ↑↓ to choose, Enter to confirm", "使用 ↑↓ 选择，Enter 确认")
				return m, nil
			}
			m.logAuditDecision(msg.call, msg.assessment, "dispatched")
			return m, m.dispatchTool(msg.streamID, msg.call)
		case security.RiskDeny:
			m.logAuditDecision(msg.call, msg.assessment, "blocked")
			denial := "BLOCKED: " + msg.assessment.Reason
			m.fillToolPlaceholder(msg.call, denial, []nodeToolResult{{Node: "-", Output: denial, Success: false}})
			if m.conv != nil {
				m.conv.AddToolResult(msg.call.ID, denial)
			}
			return m.completeToolAndResume(msg.streamID, msg.call)
		case security.RiskConfirm:
			m.logAuditDecision(msg.call, msg.assessment, "pending confirmation")
			m.mode = modeConfirm
			m.pendingToolCall = &msg.call
			m.pendingRisk = &msg.assessment
			m.input = ""
			m.status = m.uiLanguage.tr("Use ↑↓ to choose, Enter to confirm", "使用 ↑↓ 选择，Enter 确认")
			return m, nil
		}

	case streamReadyMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		if msg.err != nil {
			m.finishStream(false)
			m.status = m.uiLanguage.tr("Error: ", "错误: ") + msg.err.Error()
			return m, nil
		}
		if msg.ch == nil {
			m.finishStream(false)
			m.status = m.uiLanguage.tr("Stream error: nil event stream", "流错误: 事件流为空")
			return m, nil
		}
		m.streamCh = msg.ch
		return m, tea.Batch(m.waitForEventAndTimeout(msg.streamID)...)

	case streamEventMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		m.debugLogStreamEvent(msg.Event)
		m.streamEventSeq++
		switch e := msg.Event.(type) {
		case llm.TextDeltaEvent:
			m.streamBuf += e.Delta
		case llm.ReasoningDeltaEvent:
			m.streamReasoningBuf += e.Delta
		case llm.ToolCallEvent:
			if m.streamBuf != "" {
				if m.conv != nil {
					m.conv.AddAssistant(m.streamBuf)
				}
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf, elapsed: m.streamElapsed()})
				m.streamBuf = ""
			}
			m.streamReasoningBuf = ""
			m.streamToolExpected++
			hidden := isHiddenInternalTool(e.Name)
			sanitizedArgs := sanitizeToolArguments(e.Name, e.Arguments)
			m.messages = append(m.messages, chatMsg{
				role:       "tool",
				toolCallID: e.ID,
				toolName:   e.Name,
				toolInput:  string(sanitizedArgs),
				hidden:     hidden,
			})
			if m.conv != nil {
				m.conv.AddToolCall(e.ID, e.Name, string(sanitizedArgs))
			}
			if hidden {
				m.status = hiddenToolStatus(e.Name)
			}
			call := llm.ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments}
			var toolCmd tea.Cmd
			if memory.IsMemoryTool(e.Name) {
				toolCmd = m.handleMemoryTool(msg.streamID, call)
			} else if e.Name == metaToolSubagentsRun {
				toolCmd = m.dispatchSubagentsRun(msg.streamID, call)
			} else {
				toolCmd = m.assessToolRisk(msg.streamID, call)
			}
			m.updateViewportContent()
			return m, tea.Batch(append([]tea.Cmd{toolCmd}, m.waitForEventAndTimeout(msg.streamID)...)...)
		case llm.StopEvent:
			hadOutput := m.appendAssistantStreamContent()
			if !hadOutput && m.streamToolExpected == 0 {
				m.finishEmptyResponse(e.Reason)
				m.updateViewportContent()
				return m, nil
			}
			m.streamEnded = true
			if e.Reason == llm.StopToolUse {
				if m.mode == modeNodePrompt {
					m.status = m.nodePrompt.label + " " + m.uiLanguage.tr("required", "必填")
				} else if m.hasPendingVisibleTool() {
					m.status = m.uiLanguage.tr("Running tool...", "正在运行工具...")
				} else if m.hasPendingHiddenTool() {
					m.status = m.uiLanguage.tr("Inspecting...", "正在检查...")
				}
				m.updateViewportContent()
				return m.resumeAfterStreamTools(msg.streamID)
			}
			m.finishStream(false)
			if hadOutput {
				m.runMemoryExtraction(m.latestUserMessage(), m.latestAssistantMessage())
			}
			m.status = m.uiLanguage.tr("Ready", "就绪")
			m.updateViewportContent()
			return m, nil
		case llm.ErrorEvent:
			if m.appendAssistantStreamContent() {
				m.status = m.uiLanguage.tr("Stream error; partial content preserved: ", "流错误，已保留部分内容: ") + e.Err.Error()
			} else {
				m.status = m.uiLanguage.tr("Stream error: ", "流错误: ") + e.Err.Error()
			}
			m.finishStream(false)
			m.updateViewportContent()
			return m, nil
		}
		m.updateViewportContent()
		return m, tea.Batch(m.waitForEventAndTimeout(msg.streamID)...)

	case streamDoneMsg:
		if !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		slog.Debug("llm stream done", "stream_id", msg.streamID, "buffer_len", len(m.streamBuf), "tool_calls", m.streamToolExpected)
		hadOutput := m.appendAssistantStreamContent()
		if !hadOutput && m.streamToolExpected == 0 {
			m.finishEmptyResponse("stream closed")
			return m, nil
		}
		m.streamEnded = true
		if hadOutput && m.streamToolExpected == 0 {
			m.runMemoryExtraction(m.latestUserMessage(), m.latestAssistantMessage())
		}
		return m.resumeAfterStreamTools(msg.streamID)

	case streamTimeoutMsg:
		if !m.isActiveStream(msg.streamID) || !m.streaming || msg.eventSeq != m.streamEventSeq {
			return m, nil
		}
		m.finishStream(true)
		m.status = fmt.Sprintf("Stream timeout: no model event for %.0fs", streamEventTimeout.Seconds())
		return m, nil

	case thinkingTickMsg:
		if !m.isActiveStream(msg.streamID) || !m.streaming || m.streamBuf != "" || m.streamReasoningBuf != "" {
			return m, nil
		}
		m.thinkingFrame++
		m.updateViewportContent()
		return m, m.scheduleThinkingTick(msg.streamID)

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
		return m.completeToolAndResume(msg.streamID, msg.Call)

	case skillManagementResultMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.skills = msg.skills
		m.skillWarnings = msg.warnings
		m.status = msg.status
		m.updateViewportContent()
		return m, nil

	case nodeAddResultMsg:
		if msg.streamID != 0 && !m.isActiveStream(msg.streamID) {
			return m, nil
		}
		results := []nodeToolResult{{Node: "local", Output: msg.Output, Success: true}}
		m = m.applyNodeAddResult(msg.Cluster, msg.Result, msg.TLS)
		m.fillToolPlaceholder(msg.Call, msg.Output, results)
		if m.conv != nil {
			m.conv.AddToolResult(msg.Call.ID, msg.Output)
		}
		m.logAuditExecution(msg.Call, results)
		m.status = m.uiLanguage.tr("Node added and deployed", "节点已添加并部署")
		m.updateViewportContent()
		m.clearNodeToolExposure()
		if len(m.clients) > 0 {
			return m, fetchNodeToolsBeforeNodeAddResume(msg.streamID, m.clients)
		}
		return m.completeToolAndResume(msg.streamID, msg.Call)

	case pingResultMsg:
		m.markNodeOnline(msg.node, msg.online)
		return m, nil

	case sessionLoadMsg:
		if msg.err != nil {
			m.status = m.uiLanguage.tr("Error loading session: ", "加载会话失败: ") + msg.err.Error()
			return m, nil
		}
		m.applyLoadedSession(msg.record)
		m.status = fmt.Sprintf(m.uiLanguage.tr("Resumed session %s (%s)", "已恢复会话 %s (%s)"), msg.record.ID, msg.record.Cluster)
		return m, nil

	case compactResultMsg:
		if msg.err != nil {
			m.status = m.uiLanguage.tr("Compact failed: ", "压缩失败: ") + msg.err.Error()
			return m, nil
		}
		if m.conv != nil {
			if _, err := m.archiveCompaction(msg.oldMessages); err != nil {
				m.status = m.uiLanguage.tr("Compact archive failed: ", "压缩归档失败: ") + err.Error()
				return m, nil
			}
			m.conv.ReplaceMessages(msg.messages)
		}
		m.messages = chatMessagesFromModels(msg.messages)
		m.messages = append(m.messages, chatMsg{
			role:    "assistant",
			content: fmt.Sprintf(m.uiLanguage.tr("Compacted %d message(s) into a structured summary and kept %d recent message(s).", "已将 %d 条消息压缩为结构化摘要，并保留 %d 条最近消息。"), msg.oldCount, msg.keptCount),
		})
		m.status = fmt.Sprintf(m.uiLanguage.tr("Compacted %d message(s)", "已压缩 %d 条消息"), msg.oldCount)
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeNodePrompt {
		return m.handleNodePromptKey(key)
	}
	if m.mode == modeLangSelect {
		return m.handleLangSelectKey(key)
	}
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
		if key.Type == tea.KeyCtrlC || key.Type == tea.KeyEsc {
			m.finishStream(true)
			m.status = m.uiLanguage.tr("Interrupted", "已中断")
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
	case tea.KeyCtrlV:
		return m, clipboardImageOrTextCmd(m.attachmentDir(), len(m.attachedImages)+len(m.pendingImages)+1)
	case tea.KeyCtrlO:
		m.toggleLastToolOutputExpanded()
		return m, nil
	case tea.KeyCtrlL:
		m.messages = nil
		m.lastBodyContent = ""
		m.status = m.uiLanguage.tr("Conversation cleared", "对话已清空")
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
			m.resetInputHistoryNavigation()
			m.ac = m.updateAutocomplete()
		}
		return m, nil
	case tea.KeyEnter:
		if m.ac.visible {
			comp := m.ac.completion()
			if comp != "" && strings.TrimSpace(m.input) != strings.TrimSpace(comp) {
				m.input = comp
				m.ac.visible = false
				return m, nil
			}
		}
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
			m.navigateInputHistory(-1)
		}
		return m, nil
	case tea.KeyDown:
		if m.ac.visible {
			m.ac = m.ac.moveDown()
		} else {
			m.navigateInputHistory(1)
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
		added := string(key.Runes)
		if key.Paste {
			images, ok, err := imageAttachmentsFromPastedText(added, m.attachmentDir(), len(m.attachedImages)+len(m.pendingImages)+1)
			if err != nil {
				m.status = m.uiLanguage.tr("Image paste failed: ", "图片粘贴失败: ") + err.Error()
				return m, nil
			}
			if ok {
				m.pendingImages = append(m.pendingImages, images...)
				m.status = fmt.Sprintf(m.uiLanguage.tr("Attached %d image(s)", "已附加 %d 张图片"), len(images))
				return m, nil
			}
		}
		m.input += added
		m.resetInputHistoryNavigation()
		m.ac = m.updateAutocomplete()
		if key.Paste && strings.ContainsAny(added, "\r\n") {
			lines := multilineInputLineCount(added)
			label := m.uiLanguage.tr("lines", "行")
			if lines == 1 {
				label = m.uiLanguage.tr("line", "行")
			}
			m.status = fmt.Sprintf(m.uiLanguage.tr("Pasted %d %s into input", "已粘贴 %d %s 到输入框"), lines, label)
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.resetInputHistoryNavigation()
		m.ac = m.updateAutocomplete()
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

func (m *Model) navigateInputHistory(direction int) {
	if len(m.inputHistory) == 0 {
		return
	}
	if m.inputHistoryIndex == -1 {
		if direction > 0 {
			return
		}
		m.inputHistoryDraft = m.input
		m.inputHistoryIndex = len(m.inputHistory) - 1
		m.input = m.inputHistory[m.inputHistoryIndex]
		m.ac = m.updateAutocomplete()
		return
	}

	m.inputHistoryIndex += direction
	if m.inputHistoryIndex < 0 {
		m.inputHistoryIndex = 0
	}
	if m.inputHistoryIndex >= len(m.inputHistory) {
		m.inputHistoryIndex = -1
		m.input = m.inputHistoryDraft
		m.inputHistoryDraft = ""
		m.ac = m.updateAutocomplete()
		return
	}
	m.input = m.inputHistory[m.inputHistoryIndex]
	m.ac = m.updateAutocomplete()
}

func (m *Model) resetInputHistoryNavigation() {
	m.inputHistoryIndex = -1
	m.inputHistoryDraft = ""
}

func (m Model) updateAutocomplete() autocomplete {
	root := m.localWorkspaceRoot
	if root == "" {
		root = "."
	}
	return m.ac.updateWithRoot(m.input, root)
}

func (m Model) handleNodePromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return m.cancelNodePrompt()
	case tea.KeyEnter:
		return m.submitNodePrompt()
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyRunes:
		m.input += string(key.Runes)
		return m, nil
	case tea.KeySpace:
		m.input += " "
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) submitNodePrompt() (tea.Model, tea.Cmd) {
	state := m.nodePrompt
	value := m.input
	m.input = ""
	m.ac = newAutocompleteWithLanguage(m.uiLanguage)
	m.resetInputHistoryNavigation()
	if strings.TrimSpace(value) == "" {
		m.status = state.label + " " + m.uiLanguage.tr("required", "必填")
		return m, nil
	}
	if !state.secret {
		value = strings.TrimSpace(value)
	}
	call := state.call
	updatedArgs, err := setNodeAddArg(call.Arguments, state.field, value)
	if err != nil {
		m.mode = modeChat
		m.nodePrompt = nodePromptState{}
		return m, func() tea.Msg {
			return nodeAddLocalResult(state.streamID, call, "invalid node_add arguments: "+err.Error(), false)
		}
	}
	call.Arguments = updatedArgs
	m.mode = modeChat
	m.nodePrompt = nodePromptState{}
	m.status = m.uiLanguage.tr("Running tool...", "正在运行工具...")
	return m, m.prepareNodeAddOrPrompt(state.streamID, call)
}

func (m Model) cancelNodePrompt() (tea.Model, tea.Cmd) {
	state := m.nodePrompt
	m.mode = modeChat
	m.nodePrompt = nodePromptState{}
	m.input = ""
	m.ac = newAutocompleteWithLanguage(m.uiLanguage)
	output := m.uiLanguage.tr("Cancelled by user", "用户已取消")
	m.fillToolPlaceholder(state.call, output, []nodeToolResult{{Node: "-", Output: output, Success: false}})
	if m.conv != nil {
		m.conv.AddToolResult(state.call.ID, "Cancelled by user")
	}
	m.status = m.uiLanguage.tr("Ready", "就绪")
	if state.call.Name == metaToolNodeAdd {
		m.clearNodeToolExposure()
	}
	m.updateViewportContent()
	if state.streamID == 0 || !m.streaming {
		return m, nil
	}
	return m.completeToolAndResume(state.streamID, state.call)
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
		addToAllowlist := m.canAddPendingToolToAllowlist()
		m.pendingToolCall = nil
		m.pendingRisk = nil
		m.mode = modeChat
		choice := m.confirmChoice
		m.confirmChoice = 0

		if choice == 0 {
			m.status = m.uiLanguage.tr("Approved, executing...", "已批准，正在执行...")
			m.logAuditDecision(call, derefRiskAssessment(assessment), "approved")
			return m, m.dispatchTool(m.activeStreamID, call)
		}
		if choice == 1 && addToAllowlist {
			if err := m.addPendingToolToAllowlist(call); err != nil {
				m.pendingToolCall = &call
				m.pendingRisk = assessment
				m.mode = modeConfirm
				m.confirmChoice = 1
				m.status = m.uiLanguage.tr("Allowlist update failed: ", "白名单更新失败: ") + err.Error()
				return m, nil
			}
			m.status = m.uiLanguage.tr("Approved and added to allowlist, executing...", "已批准并加入白名单，正在执行...")
			m.logAuditDecision(call, derefRiskAssessment(assessment), "approved and allowlisted")
			return m, m.dispatchTool(m.activeStreamID, call)
		}
		m.logAuditDecision(call, derefRiskAssessment(assessment), "cancelled")
		cancelled := m.uiLanguage.tr("Cancelled by user", "用户已取消")
		m.fillToolPlaceholder(call, cancelled, []nodeToolResult{{Node: "-", Output: cancelled, Success: false}})
		if m.conv != nil {
			m.conv.AddToolResult(call.ID, "Cancelled by user")
		}
		m.status = m.uiLanguage.tr("Ready", "就绪")
		return m.completeToolAndResume(m.activeStreamID, call)
	case tea.KeyEsc:
		call := *m.pendingToolCall
		assessment := m.pendingRisk
		m.pendingToolCall = nil
		m.pendingRisk = nil
		m.mode = modeChat
		m.confirmChoice = 0
		m.logAuditDecision(call, derefRiskAssessment(assessment), "cancelled")
		cancelled := m.uiLanguage.tr("Cancelled by user", "用户已取消")
		m.fillToolPlaceholder(call, cancelled, []nodeToolResult{{Node: "-", Output: cancelled, Success: false}})
		if m.conv != nil {
			m.conv.AddToolResult(call.ID, "Cancelled by user")
		}
		m.status = m.uiLanguage.tr("Ready", "就绪")
		return m.completeToolAndResume(m.activeStreamID, call)
	default:
		return m, nil
	}
}

func (m *Model) addPendingToolToAllowlist(call llm.ToolCall) error {
	if isShellCommandTool(call.Name) {
		return m.addPendingCommandToAllowlist(call)
	}
	if isLocalFileMutationTool(call.Name) {
		return m.addPendingLocalFileToAllowlist(call)
	}
	return fmt.Errorf("tool cannot be added to allowlist: %s", call.Name)
}

func (m Model) handleNodeSelectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		m.selectedNodes = m.nodeSelector.Selected()
		m.mode = modeChat
		m.status = fmt.Sprintf(m.uiLanguage.tr("Selected %d node(s)", "已选择 %d 个节点"), len(m.selectedNodes))
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.selectedNodes = m.prevSelected
		m.mode = modeChat
		m.status = m.uiLanguage.tr("Node selection cancelled", "已取消节点选择")
		return m, nil
	default:
		var cmd tea.Cmd
		m.nodeSelector, cmd = m.nodeSelector.Update(key)
		return m, cmd
	}
}

func (m Model) handleLangSelectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		selected := m.langSelector.Selected()
		m.mode = modeChat
		m = m.applyUILanguage(selected)
		if err := m.saveUILanguage(selected); err != nil {
			m.status = m.uiLanguage.tr("Language changed, but config save failed: ", "语言已切换，但配置保存失败: ") + err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf(m.uiLanguage.tr("UI language changed to %s", "界面语言已切换为 %s"), selected.displayName())
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeChat
		m.status = m.uiLanguage.tr("Language selection cancelled", "已取消语言选择")
		return m, nil
	default:
		var cmd tea.Cmd
		m.langSelector, cmd = m.langSelector.Update(key)
		return m, cmd
	}
}

func (m Model) applyUILanguage(lang uiLanguage) Model {
	m.uiLanguage = lang
	m.ac = newAutocompleteWithLanguage(lang)
	m.langSelector = newLangSelector(lang)
	m.lastBodyContent = ""
	return m
}

func (m Model) saveUILanguage(lang uiLanguage) error {
	if m.configHome == "" {
		return nil
	}
	loader := cfgloader.NewLoader(m.configHome)
	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	global.UILanguage = lang.configValue()
	return loader.SaveGlobal(global)
}

func (m Model) View() string {
	header := renderHeader(m.cluster, m.model, len(m.selectedNodes), len(m.nodes), m.uiLanguage)
	statusView := m.renderStatus()

	if m.mode == modeNodeSelect {
		return header + "\n\n" + m.nodeSelector.View() + "\n\n" + statusView
	}
	if m.mode == modeLangSelect {
		return header + "\n\n" + m.langSelector.View() + "\n\n" + statusView
	}
	if m.mode == modeSession {
		return header + "\n\n" + m.sessionList.View(m.width) + "\n\n" + statusView
	}
	var body string

	if m.viewportReady {
		m.updateViewportContent()
		body = m.vp.View()
	} else {
		body = m.renderBody()
	}

	acView := m.ac.View(m.width)
	footer := statusView + "\n" + renderInputBox(m.inputWithImageChips(), m.width, m.uiLanguage)
	if m.mode == modeConfirm {
		footer = m.renderConfirmFooter()
	} else if m.mode == modeNodePrompt {
		footer = m.renderNodePromptFooter()
	}
	if acView != "" && m.mode == modeChat {
		footer = footer + "\n" + acView
	}

	return header + "\n\n" + body + "\n\n" + footer
}

func (m Model) inputWithImageChips() string {
	chips := imageChipText(m.pendingImages)
	if chips == "" {
		return m.input
	}
	if strings.TrimSpace(m.input) == "" {
		return chips
	}
	return m.input + " " + chips
}

func (m Model) attachmentDir() string {
	home := m.configHome
	if home == "" {
		home = cfgloader.DefaultHome()
	}
	convID := "session"
	if m.conv != nil && m.conv.ID() != "" {
		convID = m.conv.ID()
	}
	return filepath.Join(home, "attachments", convID)
}

func (m Model) renderNodePromptFooter() string {
	state := m.nodePrompt
	input := m.input
	if state.secret {
		input = strings.Repeat("*", len([]rune(input)))
	}
	lines := []string{
		statusStyle.Render(m.status),
		inputPromptStyle.Render(state.label),
		renderInputBox(input, m.width, m.uiLanguage),
		statusStyle.Render(m.uiLanguage.tr("Enter to continue, Esc to cancel", "Enter 继续，Esc 取消")),
	}
	return strings.Join(lines, "\n")
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
		inputPromptStyle.Render(m.uiLanguage.tr("Security Review", "安全确认")) + "  " + reason,
		toolStyle.Render(m.uiLanguage.tr("Command: ", "命令: ")) + command,
		strings.Join(opts, "\n"),
		statusStyle.Render(m.uiLanguage.tr("Use ↑↓ to choose, Enter to confirm, Esc to cancel", "使用 ↑↓ 选择，Enter 确认，Esc 取消")),
	}
	if m.pendingRisk != nil && m.pendingRisk.Suggestion != "" {
		lines = append(lines[:1], append([]string{statusStyle.Render(m.uiLanguage.tr("Suggestion: ", "建议: ")) + m.pendingRisk.Suggestion}, lines[1:]...)...)
	}
	return strings.Join(lines, "\n")
}

func (m Model) confirmOptions() []string {
	if m.canAddPendingToolToAllowlist() {
		return []string{"Allow", "Allow and add to allowlist", "Deny"}
	}
	return []string{"Allow", "Deny"}
}

func (m Model) canAddPendingToolToAllowlist() bool {
	return m.canAddPendingCommandToAllowlist() || m.canAddPendingLocalFileToAllowlist()
}

func (m Model) canAddPendingCommandToAllowlist() bool {
	return m.configHome != "" && m.pendingToolCall != nil && isShellCommandTool(m.pendingToolCall.Name) && m.pendingToolCommand() != ""
}

func (m Model) canAddPendingLocalFileToAllowlist() bool {
	return m.configHome != "" && m.pendingToolCall != nil && isLocalFileMutationTool(m.pendingToolCall.Name) && pendingLocalFilePath(*m.pendingToolCall) != ""
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

func requiresExplicitConfirmation(toolName string) bool {
	return toolName == metaToolNodeAdd
}

func isFileTransferTool(toolName string) bool {
	return toolName == metaToolFilePut || toolName == metaToolFileGet
}

func isHiddenInternalTool(toolName string) bool {
	if memory.IsMemoryTool(toolName) {
		return true
	}
	switch toolName {
	case metaToolToolSearch, metaToolCallTool, metaToolSubagentsRun:
		return true
	default:
		return false
	}
}

func hiddenToolStatus(toolName string) string {
	switch toolName {
	case metaToolSubagentsRun:
		return "Reviewing..."
	case metaToolToolSearch, metaToolCallTool:
		return "Inspecting..."
	default:
		if memory.IsMemoryTool(toolName) {
			return "Reading memory..."
		}
		return "Thinking..."
	}
}

func isLocalFileMutationTool(toolName string) bool {
	return localtools.IsLocalTool(toolName) && !localtools.IsReadOnly(toolName)
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

func (m *Model) addPendingLocalFileToAllowlist(call llm.ToolCall) error {
	path := pendingLocalFilePath(call)
	if path == "" {
		return fmt.Errorf("local file path is required")
	}
	writer := cfgloader.NewNodeWriter(m.configHome)
	if err := writer.AddLocalFileWhitelist(path); err != nil {
		return err
	}
	if m.reviewer != nil {
		m.reviewer.AddLocalFileWhitelist(path)
	}
	return nil
}

func pendingLocalFilePath(call llm.ToolCall) string {
	path := localtools.PathFromCall(call.Name, call.Arguments)
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return ""
	}
	return path
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
	initialContent := m.lastBodyContent == ""
	wasChat := m.lastBodyChat
	isChat := len(m.messages) > 0 || m.streaming
	m.lastBodyContent = content
	m.lastBodyChat = isChat
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(content)
	if (atBottom && !initialContent) || (!wasChat && isChat) {
		m.vp.GotoBottom()
	}
}

func (m Model) renderBody() string {
	var bodyParts []string
	for _, msg := range m.messages {
		if msg.hidden {
			continue
		}
		switch msg.role {
		case "user":
			bodyParts = append(bodyParts, renderUserMsg(msg.content, m.width))
		case "assistant":
			bodyParts = append(bodyParts, renderMessageWithElapsed(renderAssistantMsg(msg.content), msg.elapsed, m.uiLanguage))
		case "tool":
			bodyParts = append(bodyParts, renderMessageWithElapsed(m.renderToolMsg(msg), msg.elapsed, m.uiLanguage))
		}
	}
	if m.streaming {
		if m.streamBuf != "" {
			bodyParts = append(bodyParts, renderStreamingMsg(m.streamBuf))
		} else if m.streamReasoningBuf != "" {
			bodyParts = append(bodyParts, renderReasoningMsg(m.streamReasoningBuf, m.uiLanguage))
		} else {
			bodyParts = append(bodyParts, renderThinkingMsg(m.thinkingFrame, m.streamElapsed(), m.uiLanguage))
		}
	}
	body := strings.Join(bodyParts, "\n\n")
	if body == "" {
		body = renderStartupOverview(m.cluster, m.model, m.nodes, m.selectedNodes, m.uiLanguage)
	}
	return body
}

func renderMessageWithElapsed(content string, elapsed time.Duration, lang uiLanguage) string {
	footer := renderElapsedFooter(elapsed, lang)
	if footer == "" {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n\n" + footer
}

func (m Model) streamElapsed() time.Duration {
	if m.streamStartedAt.IsZero() {
		return 0
	}
	return time.Since(m.streamStartedAt).Round(100 * time.Millisecond)
}

func (m Model) renderStatus() string {
	if m.streaming && m.status == m.uiLanguage.tr("Thinking...", "思考中...") {
		return statusStyle.Render(m.versionWarning)
	}
	if m.versionWarning == "" {
		return statusStyle.Render(m.status)
	}
	return statusStyle.Render(m.status) + "\n" + statusStyle.Render(m.versionWarning)
}

func (m Model) renderToolMsg(msg chatMsg) string {
	if len(msg.nodeResults) > 1 {
		h := renderToolHeader(msg.toolName, len(msg.nodeResults), m.uiLanguage)
		if msg.toolOutput != "" {
			var lines []string
			for i, r := range msg.nodeResults {
				prefix := "├──"
				if i == len(msg.nodeResults)-1 {
					prefix = "└──"
				}
				lines = append(lines, prefix+" "+renderToolNode(r.Node, r.Success, r.Output, msg.toolExpanded, m.uiLanguage))
			}
			h += "\n" + strings.Join(lines, "\n")
		} else {
			h += " " + m.uiLanguage.tr("(running...)", "(运行中...)")
			if preview := runningToolPreview(msg, m.uiLanguage); preview != "" {
				h += "\n" + preview
			}
		}
		return h
	}
	h := renderToolHeader(msg.toolName, 0, m.uiLanguage)
	if len(msg.nodeResults) == 1 {
		h = toolStyle.Render(fmt.Sprintf(m.uiLanguage.tr("⏚ %s on %s", "⏚ %s 在 %s 上"), msg.toolName, msg.nodeResults[0].Node))
	}
	if msg.toolOutput != "" {
		output := msg.toolOutput
		if len(msg.nodeResults) == 1 {
			output = msg.nodeResults[0].Output
		}
		h += "\n" + renderToolOutput(output, msg.toolExpanded, m.uiLanguage)
	} else {
		h += " " + m.uiLanguage.tr("(running...)", "(运行中...)")
		if preview := runningToolPreview(msg, m.uiLanguage); preview != "" {
			h += "\n" + preview
		}
	}
	return h
}

func runningToolPreview(msg chatMsg, lang uiLanguage) string {
	if !isShellCommandTool(msg.toolName) || strings.TrimSpace(msg.toolInput) == "" {
		return ""
	}
	call := llm.ToolCall{Name: msg.toolName, Arguments: json.RawMessage(msg.toolInput)}
	command := strings.TrimSpace(toolCommand(call))
	if command == "" {
		return ""
	}
	return toolStyle.Render(lang.tr("Command: ", "命令: ")) + command
}

func (m *Model) toggleLastToolOutputExpanded() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.hidden || msg.role != "tool" || msg.toolOutput == "" {
			continue
		}
		msg.toolExpanded = !msg.toolExpanded
		m.lastBodyContent = ""
		if msg.toolExpanded {
			m.status = m.uiLanguage.tr("Last tool output expanded", "已展开最后一个工具输出")
		} else {
			m.status = m.uiLanguage.tr("Last tool output collapsed", "已折叠最后一个工具输出")
		}
		m.updateViewportContent()
		return
	}
	m.status = m.uiLanguage.tr("No tool output to expand", "没有可展开的工具输出")
}

func (m Model) hasPendingVisibleTool() bool {
	for _, msg := range m.messages {
		if msg.hidden || msg.role != "tool" {
			continue
		}
		if msg.toolOutput == "" {
			return true
		}
	}
	return false
}

func (m Model) hasPendingHiddenTool() bool {
	for _, msg := range m.messages {
		if !msg.hidden || msg.role != "tool" {
			continue
		}
		if msg.toolOutput == "" {
			return true
		}
	}
	return false
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	if m.mode == modeNodePrompt {
		return m.submitNodePrompt()
	}
	input := strings.TrimSpace(m.input)
	m.input = ""
	hasImages := len(m.pendingImages) > 0
	if input == "" && hasImages {
		input = m.uiLanguage.tr("Analyze the attached image(s).", "分析附加的图片。")
	}
	if input == "" {
		m.resetInputHistoryNavigation()
		return m, nil
	}
	m.inputHistory = append(m.inputHistory, input)
	m.resetInputHistoryNavigation()
	if !hasImages {
		if cmd, ok := ParseSlashCommand(input); ok {
			if cmd.Kind == CommandThinking {
				if strings.TrimSpace(cmd.Arg) == "" {
					m.status = m.uiLanguage.tr("Usage: /thinking <message>", "用法: /thinking <消息>")
					return m, nil
				}
				thinking := true
				return m.submitMessage(cmd.Arg, &thinking)
			}
			var c tea.Cmd
			m, c = m.applyCommand(cmd)
			if cmd.Kind == CommandExit {
				m.saveCurrentConversation()
				return m, tea.Quit
			}
			return m, c
		}
	}
	return m.submitMessage(input, nil)
}

func (m Model) submitMessage(input string, thinking *bool) (tea.Model, tea.Cmd) {
	return m.submitProcessedMessage(input, input, input, thinking)
}

func (m Model) submitProcessedMessage(visibleInput string, referenceInput string, llmInput string, thinking *bool) (tea.Model, tea.Cmd) {
	submitImages := append([]imageAttachment(nil), m.pendingImages...)
	if refs := fileref.Parse(referenceInput); len(refs) > 0 {
		root := m.localWorkspaceRoot
		if root == "" {
			root = "."
		}
		nextID := len(m.attachedImages) + len(submitImages) + 1
		imageRefs, textRefs, err := imageAttachmentsFromFileRefs(root, refs, m.attachmentDir(), nextID)
		if err != nil {
			m.status = m.uiLanguage.tr("Image reference error: ", "图片引用错误: ") + err.Error()
			return m, nil
		}
		submitImages = append(submitImages, imageRefs...)
		loaded, err := fileref.Load(root, textRefs, fileref.Limits{})
		if err != nil {
			m.status = m.uiLanguage.tr("File reference error: ", "文件引用错误: ") + err.Error()
			return m, nil
		}
		llmInput = fileref.AppendContext(llmInput, loaded)
	}
	if len(submitImages) > 0 {
		totalImages := len(m.attachedImages) + len(submitImages)
		if m.vision.MaxImages > 0 && totalImages > m.vision.MaxImages {
			m.status = fmt.Sprintf(m.uiLanguage.tr("Too many images: %d attached, max %d", "图片过多: 已附加 %d 张，最多 %d 张"), totalImages, m.vision.MaxImages)
			return m, nil
		}
		m.pendingImages = nil
		m.attachedImages = append(m.attachedImages, submitImages...)
		visibleInput = strings.TrimSpace(visibleInput + " " + imageChipText(submitImages))
		llmInput = appendImageToolContext(llmInput, submitImages)
		return m.startSubmittedMessage(visibleInput, llmInput, thinking)
	}
	return m.startSubmittedMessage(visibleInput, llmInput, thinking)
}

func (m Model) startSubmittedMessage(visibleInput string, llmInput string, thinking *bool) (tea.Model, tea.Cmd) {
	if m.provider == nil {
		m.messages = append(m.messages, chatMsg{role: "user", content: visibleInput})
		if m.conv != nil {
			m.conv.AddUser(llmInput)
		}
		m.maybeAutoSaveUserMemory(visibleInput)
		m.pendingImages = nil
		m.status = m.uiLanguage.tr("No LLM provider configured", "未配置 LLM provider")
		return m, nil
	}
	m.messages = append(m.messages, chatMsg{role: "user", content: visibleInput})
	if m.conv != nil {
		m.conv.AddUser(llmInput)
	}
	m.maybeAutoSaveUserMemory(visibleInput)
	m.pendingImages = nil
	m.streaming = true
	m.streamBuf = ""
	m.streamReasoningBuf = ""
	m.streamThinking = thinking
	m.status = m.uiLanguage.tr("Thinking...", "思考中...")
	return m.startStream()
}

func (m Model) applyCommand(cmd SlashCommand) (Model, tea.Cmd) {
	switch cmd.Kind {
	case CommandHelp:
		m.messages = append(m.messages, chatMsg{
			role:    "assistant",
			content: m.uiLanguage.tr("Conan: /help /clear /compact [focus] /exit /cluster [name] /skills /skill <name> [arguments] /lang /model [name] /node [off] /nodes /memory /resume /thinking <message> /agent <role> <task> /subagents [on|off|limit]", "Conan: /help /clear /compact [重点] /exit /cluster [名称] /skills /skill <名称> [参数] /lang /model [名称] /node [off] /nodes /memory /resume /thinking <消息> /agent <角色> <任务> /subagents [on|off|limit]"),
		})
		m.status = m.uiLanguage.tr("Help shown", "已显示帮助")
	case CommandClear:
		m.messages = nil
		if m.conv != nil {
			m.conv.Clear()
		}
		m.status = m.uiLanguage.tr("Conversation cleared", "对话已清空")
	case CommandExit:
		m.status = m.uiLanguage.tr("Exit requested", "已请求退出")
	case CommandCluster:
		if cmd.Arg != "" {
			m.cluster = cmd.Arg
			m.status = m.uiLanguage.tr("Cluster switched to ", "已切换集群: ") + cmd.Arg
		} else {
			m.status = m.uiLanguage.tr("Current cluster: ", "当前集群: ") + m.cluster
		}
	case CommandSkills:
		return m.applySkillsCommand(cmd.Arg)
	case CommandSkill:
		name, rest := splitSkillInvocationArg(cmd.Arg)
		if name == "" {
			m.status = m.uiLanguage.tr("Usage: /skill <name> [arguments]", "用法: /skill <名称> [参数]")
		} else {
			skill, found := m.findSkill(name)
			if !found {
				m.status = m.uiLanguage.tr("Unknown skill: ", "未知技能: ") + name
				return m, nil
			}
			visible := strings.TrimSpace("/skill " + name + " " + rest)
			return m.submitSkillMessage(visible, skill, rest)
		}
	case CommandLang:
		if strings.TrimSpace(cmd.Arg) != "" {
			lang, ok := parseUILanguage(cmd.Arg)
			if !ok {
				m.status = m.uiLanguage.tr("Usage: /lang [en|zh]", "用法: /lang [en|zh]")
				return m, nil
			}
			m = m.applyUILanguage(lang)
			if err := m.saveUILanguage(lang); err != nil {
				m.status = m.uiLanguage.tr("Language changed, but config save failed: ", "语言已切换，但配置保存失败: ") + err.Error()
				return m, nil
			}
			m.status = fmt.Sprintf(m.uiLanguage.tr("UI language changed to %s", "界面语言已切换为 %s"), lang.displayName())
			return m, nil
		}
		m.mode = modeLangSelect
		m.langSelector = newLangSelector(m.uiLanguage)
		m.status = m.uiLanguage.tr("Select UI language", "选择界面语言")
		return m, nil
	case CommandModel:
		if cmd.Arg != "" {
			m.model = cmd.Arg
			m.status = m.uiLanguage.tr("Model switched to ", "已切换模型: ") + cmd.Arg
		} else {
			m.status = m.uiLanguage.tr("Current model: ", "当前模型: ") + m.model
		}
	case CommandNode:
		switch strings.TrimSpace(cmd.Arg) {
		case "":
			m.nodeToolsEnabled = true
			m.status = m.uiLanguage.tr("Node management enabled for next model response", "下一次模型回复将启用节点管理")
		case "off":
			m.nodeToolsEnabled = false
			m.status = m.uiLanguage.tr("Node management disabled", "节点管理已禁用")
		default:
			m.status = m.uiLanguage.tr("Usage: /node [off]", "用法: /node [off]")
		}
	case CommandNodes:
		if len(m.nodes) == 0 {
			m.status = m.uiLanguage.tr("No nodes configured", "未配置节点")
			return m, nil
		}
		m.mode = modeNodeSelect
		m.prevSelected = m.selectedNodes
		m.nodeSelector = newNodeSelector(m.nodes, m.selectedNodes, m.uiLanguage)
		m.status = m.uiLanguage.tr("Checking node status...", "正在检查节点状态...")
		return m, m.pingNodes()
	case CommandMemory:
		if m.memStore == nil {
			m.status = m.uiLanguage.tr("Memory not available", "记忆不可用")
			return m, nil
		}
		results, err := m.memStore.ListMemories("", 10)
		if err != nil {
			m.status = m.uiLanguage.tr("Error: ", "错误: ") + err.Error()
			return m, nil
		}
		if len(results) == 0 {
			m.status = m.uiLanguage.tr("No memories stored yet", "还没有保存记忆")
			return m, nil
		}
		var lines []string
		for _, r := range results {
			lines = append(lines, fmt.Sprintf("[%s] %s: %s", r.ID, r.Title, truncateStr(r.Content, 60)))
		}
		m.messages = append(m.messages, chatMsg{role: "assistant", content: m.uiLanguage.tr("Memory:\n", "记忆:\n") + strings.Join(lines, "\n")})
		m.status = fmt.Sprintf(m.uiLanguage.tr("%d memories", "%d 条记忆"), len(results))
	case CommandResume:
		if m.memStore == nil {
			m.status = m.uiLanguage.tr("Memory not available", "记忆不可用")
			return m, nil
		}
		if cmd.Arg != "" {
			return m, m.loadSession(cmd.Arg)
		}
		sessions, err := m.memStore.ListConversations(20)
		if err != nil {
			m.status = m.uiLanguage.tr("Error: ", "错误: ") + err.Error()
			return m, nil
		}
		if len(sessions) == 0 {
			m.status = m.uiLanguage.tr("No previous sessions", "没有历史会话")
			return m, nil
		}
		var infos []SessionInfo
		for _, s := range sessions {
			preview := m.resumeSessionLastUserMessage(s.ID)
			if preview == "" {
				preview = m.uiLanguage.tr("(no user message)", "(无用户消息)")
			}
			infos = append(infos, SessionInfo{
				ID:        s.ID,
				Cluster:   s.Cluster,
				CreatedAt: s.CreatedAt,
				Summary:   preview,
			})
		}
		m.mode = modeSession
		m.sessionList = newSessionList(infos, m.uiLanguage)
		m.status = m.uiLanguage.tr("Select a session to resume", "选择要恢复的会话")
		return m, nil
	case CommandCompact:
		return m.compactConversation(cmd.Arg)
	case CommandAgent:
		return m.startManualSubagent(cmd.Arg)
	case CommandSubagents:
		return m.applySubagentsCommand(cmd.Arg), nil
	default:
		name, rest := splitSkillInvocationArg(cmd.Arg)
		if skill, found := m.findSkill(name); found {
			visible := strings.TrimSpace("/" + name + " " + rest)
			return m.submitSkillMessage(visible, skill, rest)
		}
		m.status = m.uiLanguage.tr("Unknown command: /", "未知命令: /") + cmd.Arg
	}
	return m, nil
}

func (m Model) resumeSessionLastUserMessage(id string) string {
	if m.memStore == nil {
		return ""
	}
	rec, err := m.memStore.LoadConversation(id)
	if err != nil || rec == nil {
		return ""
	}
	var messages []models.Message
	if strings.TrimSpace(rec.Messages) != "" {
		if err := json.Unmarshal([]byte(rec.Messages), &messages); err != nil {
			return ""
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != conversation.RoleUser {
			continue
		}
		content := strings.Join(strings.Fields(messages[i].Content), " ")
		if content != "" {
			return truncateStr(content, 120)
		}
	}
	return ""
}

func (m Model) startManualSubagent(arg string) (Model, tea.Cmd) {
	if m.provider == nil {
		m.status = m.uiLanguage.tr("No LLM provider configured", "未配置 LLM provider")
		return m, nil
	}
	role, task := parseSubagentCommand(arg)
	if strings.TrimSpace(task) == "" {
		m.status = m.uiLanguage.tr("Usage: /agent <investigator|reviewer|summarizer> <task>", "用法: /agent <investigator|reviewer|summarizer> <任务>")
		return m, nil
	}
	req := m.newSubagentRequest(role, task, m.selectedNodeNames())
	m.status = m.uiLanguage.tr("Subagent running...", "Subagent 运行中...")
	return m, func() tea.Msg {
		result := m.runSubagent(context.Background(), req)
		return subagentCommandResultMsg{result: result}
	}
}

func (m Model) compactConversation(focus string) (Model, tea.Cmd) {
	if m.streaming {
		m.status = m.uiLanguage.tr("Cannot compact while streaming", "流式回复中无法压缩")
		return m, nil
	}
	if m.provider == nil {
		m.status = m.uiLanguage.tr("No LLM provider configured", "未配置 LLM provider")
		return m, nil
	}
	if m.conv == nil || len(m.conv.Messages()) == 0 {
		m.status = m.uiLanguage.tr("Nothing to compact", "没有可压缩的上下文")
		return m, nil
	}
	m.status = m.uiLanguage.tr("Compacting conversation...", "正在压缩会话...")
	oldMessages := m.conv.Messages()
	convID := m.conv.ID()
	provider := m.provider
	return m, func() tea.Msg {
		resp, err := provider.Chat(context.Background(), &llm.ChatRequest{
			SystemPrompt: compactSystemPrompt(focus),
			Messages:     oldMessages,
			MaxTokens:    2200,
		})
		if err != nil {
			return compactResultMsg{err: err}
		}
		summary := strings.TrimSpace(resp.Message.Content)
		if summary == "" {
			return compactResultMsg{err: fmt.Errorf("empty compact summary")}
		}
		messages := buildCompactedMessages(convID, summary, oldMessages)
		return compactResultMsg{
			oldMessages: oldMessages,
			messages:    messages,
			oldCount:    len(oldMessages),
			keptCount:   len(messages) - 1,
		}
	}
}

func parseSubagentCommand(arg string) (subagent.Role, string) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return subagent.RoleInvestigator, ""
	}
	switch subagent.Role(fields[0]) {
	case subagent.RoleInvestigator, subagent.RoleReviewer, subagent.RoleSummarizer:
		return subagent.Role(fields[0]), strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(arg), fields[0]))
	default:
		return subagent.RoleInvestigator, strings.TrimSpace(arg)
	}
}

func (m Model) applySubagentsCommand(arg string) Model {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		state := "off"
		if m.subagents.Enabled {
			state = "on"
		}
		var lines []string
		lines = append(lines, fmt.Sprintf(m.uiLanguage.tr("Subagents: %s, limit %d, timeout %ds", "Subagents: %s，限制 %d，超时 %ds"), state, m.subagents.MaxParallel, m.subagents.TimeoutSeconds))
		if len(m.subagentResults) == 0 {
			lines = append(lines, m.uiLanguage.tr("No subagent runs yet.", "还没有 subagent 运行记录。"))
		} else {
			for _, result := range m.subagentResults {
				lines = append(lines, renderSubagentResultLine(result))
			}
		}
		m.messages = append(m.messages, chatMsg{role: "assistant", content: strings.Join(lines, "\n")})
		m.status = m.uiLanguage.tr("Subagents shown", "已显示 Subagents")
		return m
	}
	switch fields[0] {
	case "on":
		m.subagents.Enabled = true
		m.status = m.uiLanguage.tr("Subagents enabled", "Subagents 已启用")
	case "off":
		m.subagents.Enabled = false
		m.status = m.uiLanguage.tr("Subagents disabled", "Subagents 已禁用")
	case "limit":
		if len(fields) < 2 {
			m.status = m.uiLanguage.tr("Usage: /subagents limit <n>", "用法: /subagents limit <n>")
			return m
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n <= 0 {
			m.status = m.uiLanguage.tr("Subagent limit must be a positive integer", "Subagent 限制必须是正整数")
			return m
		}
		if n > 8 {
			n = 8
		}
		m.subagents.MaxParallel = n
		m.status = fmt.Sprintf(m.uiLanguage.tr("Subagent limit set to %d", "Subagent 限制已设置为 %d"), n)
	default:
		m.status = m.uiLanguage.tr("Usage: /subagents [on|off|limit <n>]", "用法: /subagents [on|off|limit <n>]")
	}
	return m
}

func (m Model) newSubagentRequest(role subagent.Role, task string, nodes []string) subagent.Request {
	return subagent.Request{
		ID:      models.NewID(),
		Role:    role,
		Task:    task,
		Cluster: m.cluster,
		Nodes:   nodes,
		Model:   m.model,
		Context: recentConversationContext(m.conv, 3000),
		Timeout: time.Duration(m.subagents.TimeoutSeconds) * time.Second,
	}
}

func recentConversationContext(conv *conversation.Conversation, maxChars int) []models.Message {
	if conv == nil {
		return nil
	}
	return conv.Context(maxChars)
}

func (m Model) runSubagent(ctx context.Context, req subagent.Request) subagent.Result {
	runner := subagent.Runner{
		Provider:     m.provider,
		Tools:        m.availableToolDefsForSubagent(),
		Executor:     subagentToolExecutor{model: m, nodes: req.Nodes},
		MaxTurns:     4,
		MaxToolCalls: 8,
	}
	return runner.Run(ctx, req)
}

func (m Model) availableToolDefsForSubagent() []llm.ToolDef {
	all := m.availableToolDefs()
	result := make([]llm.ToolDef, 0, len(all))
	for _, tool := range all {
		switch tool.Name {
		case metaToolToolSearch, metaToolCallTool, "memory_search", "memory_read":
			result = append(result, tool)
		}
	}
	return result
}

func renderSubagentCommandResult(result subagent.Result) string {
	return "Subagent result\n" + renderSubagentResultLine(result)
}

func renderSubagentResultLine(result subagent.Result) string {
	status := "ok"
	if result.Err != nil {
		status = "error: " + result.Err.Error()
	}
	nodes := "local"
	if len(result.Nodes) > 0 {
		nodes = strings.Join(result.Nodes, ",")
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "(no summary)"
	}
	return fmt.Sprintf("- %s [%s] %s: %s", result.Role, nodes, status, summary)
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
	m.streamEventSeq = 0
	m.thinkingFrame = 0
	m.streamReasoningBuf = ""
	ctx, cancel := context.WithCancel(context.Background())
	m.streamCtx = ctx
	m.streamCancel = cancel
	m.streamStartedAt = time.Now()
	m.updateViewportContent()
	provider := m.provider
	streamID := m.activeStreamID

	allTools := m.availableToolDefs()
	req := &llm.ChatRequest{
		SystemPrompt: m.buildSystemPromptWithMemory(),
		Messages:     m.conv.Messages(),
		Tools:        allTools,
		Thinking:     m.streamThinking,
	}
	m.debugLogLLMRequest(req)
	streamCmd := func() tea.Msg {
		ch, err := provider.ChatStream(ctx, req)
		return streamReadyMsg{streamID: streamID, ch: ch, err: err}
	}
	return m, tea.Batch(streamCmd, m.scheduleThinkingTick(streamID))
}

func (m Model) availableToolDefs() []llm.ToolDef {
	allTools := make([]llm.ToolDef, 0, len(metaToolDefs)+len(nodeManagementToolDefs)+5)
	for _, tool := range metaToolDefs {
		if tool.Name == metaToolSubagentsRun && !m.subagents.Enabled {
			continue
		}
		allTools = append(allTools, tool)
	}
	if len(m.attachedImages) > 0 {
		allTools = append(allTools, imageToolDefs...)
	}
	if m.skillsConfig.Enabled && len(m.skills) > 0 {
		allTools = append(allTools, skills.ToolDefs()...)
	}
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
	allTools = append(allTools, localtools.ToolDefs()...)
	return allTools
}

func (m Model) scheduleThinkingTick(streamID uint64) tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return thinkingTickMsg{streamID: streamID}
	})
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

func (m Model) waitForEventAndTimeout(streamID uint64) []tea.Cmd {
	waitCmd := m.waitForEvent(streamID)
	if waitCmd == nil {
		return nil
	}
	return []tea.Cmd{waitCmd, m.scheduleStreamTimeout(streamID, m.streamEventSeq)}
}

func (m Model) scheduleStreamTimeout(streamID uint64, eventSeq int) tea.Cmd {
	return tea.Tick(streamEventTimeout, func(time.Time) tea.Msg {
		return streamTimeoutMsg{streamID: streamID, eventSeq: eventSeq}
	})
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
	m.streamReasoningBuf = ""
	m.streamThinking = nil
	m.streamCh = nil
	m.streamCtx = nil
	m.streamCancel = nil
	m.activeStreamID = 0
	m.streamStartedAt = time.Time{}
	m.streamToolExpected = 0
	m.streamToolDone = 0
	m.streamEnded = false
	m.streamEventSeq = 0
}

func (m *Model) clearNodeToolExposure() {
	m.nodeToolsEnabled = false
}

func (m *Model) appendAssistantStreamContent() bool {
	content := m.streamBuf
	if content == "" {
		m.streamReasoningBuf = ""
		return false
	}
	if m.conv != nil {
		m.conv.AddAssistant(content)
	}
	m.messages = append(m.messages, chatMsg{role: "assistant", content: content, elapsed: m.streamElapsed()})
	m.streamBuf = ""
	m.streamReasoningBuf = ""
	return true
}

func (m *Model) finishEmptyResponse(reason string) {
	elapsed := m.streamElapsed()
	slog.Debug("llm empty response", "stream_id", m.activeStreamID, "reason", reason, "elapsed_ms", elapsed.Milliseconds())
	m.finishStream(false)
	if reason != "" {
		m.status = m.uiLanguage.tr("Stream error: empty response (", "流错误: 空回复 (") + reason + ")"
	} else {
		m.status = m.uiLanguage.tr("Stream error: empty response", "流错误: 空回复")
	}
	m.messages = append(m.messages, chatMsg{
		role:    "assistant",
		content: m.uiLanguage.tr("Model returned an empty response. Please try again.", "模型返回了空回复，请重试。"),
		elapsed: elapsed,
	})
}

func (m Model) debugLogLLMRequest(req *llm.ChatRequest) {
	if req == nil {
		return
	}
	slog.Debug("llm request",
		"cluster", m.cluster,
		"model", m.model,
		"system_prompt", req.SystemPrompt,
		"messages", debugMessages(req.Messages),
		"tools", debugTools(req.Tools),
		"thinking", debugThinking(req.Thinking),
	)
}

func (m Model) debugLogStreamEvent(event llm.ChatEvent) {
	switch e := event.(type) {
	case llm.TextDeltaEvent:
		slog.Debug("llm stream text_delta", "stream_id", m.activeStreamID, "delta", e.Delta, "delta_len", len(e.Delta))
	case llm.ReasoningDeltaEvent:
		slog.Debug("llm stream reasoning_delta", "stream_id", m.activeStreamID, "delta", e.Delta, "delta_len", len(e.Delta))
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

func debugThinking(thinking *bool) string {
	if thinking == nil {
		return "default"
	}
	if *thinking {
		return "enabled"
	}
	return "disabled"
}

func debugMessages(messages []models.Message) []map[string]string {
	result := make([]map[string]string, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]string{
			"role":         msg.Role,
			"content":      msg.Content,
			"tool_call_id": msg.ToolCallID,
			"tool_name":    msg.ToolName,
			"tool_input":   msg.ToolInput,
		})
	}
	return result
}

func debugTools(tools []llm.ToolDef) []map[string]string {
	result := make([]map[string]string, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]string{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": string(tool.InputSchema),
		})
	}
	return result
}

func (m *Model) markStreamToolDone(streamID uint64) {
	if streamID != 0 && !m.isActiveStream(streamID) {
		return
	}
	m.streamToolDone++
}

func (m Model) completeToolAndResume(streamID uint64, call llm.ToolCall) (tea.Model, tea.Cmd) {
	if call.Name == metaToolNodeAdd {
		m.clearNodeToolExposure()
	}
	m.markStreamToolDone(streamID)
	return m.resumeAfterStreamTools(streamID)
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
		m.status = m.uiLanguage.tr("Stream ended", "流已结束")
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
	if call.Name == metaToolImageAnalyze {
		return m.dispatchTool(streamID, call)
	}
	if localtools.IsLocalTool(call.Name) && localtools.IsReadOnly(call.Name) {
		return m.dispatchTool(streamID, call)
	}
	reviewer := m.reviewer
	if reviewer == nil {
		if isFileTransferTool(call.Name) {
			return func() tea.Msg {
				return riskAssessmentMsg{
					streamID: streamID,
					call:     call,
					assessment: security.RiskAssessment{
						Level:  security.RiskConfirm,
						Reason: "managed file transfer requires confirmation",
					},
				}
			}
		}
		if isLocalFileMutationTool(call.Name) {
			return func() tea.Msg {
				return riskAssessmentMsg{
					streamID: streamID,
					call:     call,
					assessment: security.RiskAssessment{
						Level:  security.RiskConfirm,
						Reason: "local file mutation requires confirmation",
					},
				}
			}
		}
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
	if call.Name == metaToolExec || call.Name == metaToolCallTool || call.Name == metaToolToolSearch || isFileTransferTool(call.Name) {
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
	case metaToolSubagentsRun:
		return m.dispatchSubagentsRun(streamID, call)
	case metaToolImageAnalyze:
		return m.dispatchImageAnalyze(streamID, call)
	case skills.ToolName:
		return m.dispatchSkillRead(streamID, call)
	case metaToolNodeAdd:
		return m.prepareNodeAddOrPrompt(streamID, call)
	case metaToolFilePut:
		return m.dispatchFilePut(streamID, call)
	case metaToolFileGet:
		return m.dispatchFileGet(streamID, call)
	default:
		if localtools.IsLocalTool(call.Name) {
			return m.dispatchLocalTool(streamID, call)
		}
		return m.dispatchMemoryOrDirectTool(streamID, call)
	}
}

func (m Model) dispatchSkillRead(streamID uint64, call llm.ToolCall) tea.Cmd {
	visible := append([]skills.Skill(nil), m.skills...)
	maxChars := m.skillsConfig.MaxSkillChars
	enabled := m.skillsConfig.Enabled && len(visible) > 0
	return func() tea.Msg {
		output := "skill_read error: skill_read not available"
		if enabled {
			output = skills.NewToolHandler(visible, maxChars).Handle(call.Arguments)
		}
		return multiToolResultMsg{
			streamID: streamID,
			Call:     call,
			Results: []nodeToolResult{{
				Node:    "local",
				Output:  output,
				Success: !strings.HasPrefix(output, "skill_read error:"),
			}},
		}
	}
}

func (m Model) dispatchLocalTool(streamID uint64, call llm.ToolCall) tea.Cmd {
	root := m.localWorkspaceRoot
	if root == "" {
		root = "."
	}
	return func() tea.Msg {
		result := localtools.Handle(localtools.RootedFS{Root: root}, call.Name, call.Arguments)
		return multiToolResultMsg{
			streamID: streamID,
			Call:     call,
			Results:  []nodeToolResult{{Node: "local", Output: result.Output, Success: result.Success}},
		}
	}
}

type metaCallArgs struct {
	Node       string          `json:"node"`
	Command    string          `json:"command"`
	Tool       string          `json:"tool"`
	Query      string          `json:"query"`
	LocalPath  string          `json:"local_path"`
	RemotePath string          `json:"remote_path"`
	Arguments  json.RawMessage `json:"arguments"`
}

func parseMetaArgs(raw json.RawMessage) (metaCallArgs, error) {
	var args metaCallArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, err
	}
	return args, nil
}

type imageAnalyzeArgs struct {
	ImageID  int    `json:"image_id"`
	ImageIDs []int  `json:"image_ids"`
	Question string `json:"question"`
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

func (m Model) dispatchImageAnalyze(streamID uint64, call llm.ToolCall) tea.Cmd {
	provider := m.visionProvider
	visionErr := strings.TrimSpace(m.visionError)
	images := append([]imageAttachment(nil), m.attachedImages...)
	maxChars := m.vision.MaxSummaryCharsPerImage
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return func() tea.Msg {
		if provider == nil {
			if visionErr == "" {
				visionErr = "no vision model configured or available"
			}
			return singleToolError(streamID, call, "image analysis unavailable: "+visionErr)
		}
		var args imageAnalyzeArgs
		if len(call.Arguments) > 0 {
			if err := json.Unmarshal(call.Arguments, &args); err != nil {
				return singleToolError(streamID, call, "invalid arguments: "+err.Error())
			}
		}
		selected, err := selectImageAttachments(images, args)
		if err != nil {
			return singleToolError(streamID, call, err.Error())
		}
		inputs := make([]llm.ImageInput, 0, len(selected))
		for _, image := range selected {
			input, err := imageInputFromAttachment(image)
			if err != nil {
				return singleToolError(streamID, call, fmt.Sprintf("read image #%d failed: %v", image.ID, err))
			}
			inputs = append(inputs, input)
		}
		ctx, cancel := context.WithTimeout(parentCtx, 2*time.Minute)
		defer cancel()
		resp, err := provider.DescribeImages(ctx, &llm.VisionRequest{
			Prompt:    imageAnalyzePrompt(args.Question, selected, maxChars),
			Images:    inputs,
			MaxTokens: defaultVisionMaxTokens,
		})
		if err != nil {
			return singleToolError(streamID, call, "image analysis failed: "+err.Error())
		}
		return multiToolResultMsg{
			streamID: streamID,
			Call:     call,
			Results:  []nodeToolResult{{Node: "local", Output: strings.TrimSpace(resp.Summary), Success: true}},
		}
	}
}

func (m Model) dispatchFilePut(streamID uint64, call llm.ToolCall) tea.Cmd {
	clients := m.clients
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	root := m.localWorkspaceRoot
	return func() tea.Msg {
		args, err := parseMetaArgs(call.Arguments)
		if err != nil {
			return singleToolError(streamID, call, "invalid arguments: "+err.Error())
		}
		node, client, err := resolveFileTransferClient(args.Node, clients)
		if err != nil {
			return singleToolError(streamID, call, err.Error())
		}
		localPath, err := resolveWorkspaceFilePath(root, args.LocalPath)
		if err != nil {
			return singleToolError(streamID, call, "invalid local_path: "+err.Error())
		}
		if err := fileguard.ValidateTextFile(localPath); err != nil {
			return singleToolError(streamID, call, "local file is not an allowed text file: "+err.Error())
		}
		remotePath := strings.TrimSpace(args.RemotePath)
		if remotePath == "" {
			return singleToolError(streamID, call, "remote_path is required")
		}
		if err := fileguard.ValidateTextPath(remotePath); err != nil {
			return singleToolError(streamID, call, "remote_path is not an allowed text path: "+err.Error())
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return singleToolError(streamID, call, "local file stat failed: "+err.Error())
		}
		if info.IsDir() {
			return singleToolError(streamID, call, "local_path is a directory")
		}
		file, err := os.Open(localPath)
		if err != nil {
			return singleToolError(streamID, call, "local file open failed: "+err.Error())
		}
		defer file.Close()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
		defer cancel()
		bytesWritten, err := client.UploadFile(ctx, remotePath, file)
		if err != nil {
			return singleNodeToolResult(streamID, call, node, "file upload failed: "+err.Error(), false)
		}
		output := fmt.Sprintf("uploaded %s to %s:%s (%d bytes)", args.LocalPath, node, remotePath, bytesWritten)
		return singleNodeToolResult(streamID, call, node, output, true)
	}
}

func (m Model) dispatchFileGet(streamID uint64, call llm.ToolCall) tea.Cmd {
	clients := m.clients
	parentCtx := m.streamCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	root := m.localWorkspaceRoot
	return func() tea.Msg {
		args, err := parseMetaArgs(call.Arguments)
		if err != nil {
			return singleToolError(streamID, call, "invalid arguments: "+err.Error())
		}
		node, client, err := resolveFileTransferClient(args.Node, clients)
		if err != nil {
			return singleToolError(streamID, call, err.Error())
		}
		remotePath := strings.TrimSpace(args.RemotePath)
		if remotePath == "" {
			return singleToolError(streamID, call, "remote_path is required")
		}
		localPath, err := resolveWorkspaceFilePath(root, args.LocalPath)
		if err != nil {
			return singleToolError(streamID, call, "invalid local_path: "+err.Error())
		}
		if err := fileguard.ValidateTextPath(localPath); err != nil {
			return singleToolError(streamID, call, "local_path is not an allowed text path: "+err.Error())
		}
		if _, err := os.Stat(localPath); err == nil {
			if err := fileguard.ValidateTextFile(localPath); err != nil {
				return singleToolError(streamID, call, "existing local file is not an allowed text file: "+err.Error())
			}
		} else if !os.IsNotExist(err) {
			return singleToolError(streamID, call, "local file stat failed: "+err.Error())
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return singleToolError(streamID, call, "create local directory failed: "+err.Error())
		}
		tmp, err := os.CreateTemp(filepath.Dir(localPath), ".conan-download-*")
		if err != nil {
			return singleToolError(streamID, call, "temporary file create failed: "+err.Error())
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
		defer cancel()
		bytesWritten, err := client.DownloadFile(ctx, remotePath, tmp)
		closeErr := tmp.Close()
		if err != nil {
			return singleNodeToolResult(streamID, call, node, "file download failed: "+err.Error(), false)
		}
		if closeErr != nil {
			return singleToolError(streamID, call, "temporary file close failed: "+closeErr.Error())
		}
		if err := fileguard.ValidateTextFile(tmpPath); err != nil {
			return singleToolError(streamID, call, "downloaded file is not allowed text: "+err.Error())
		}
		if err := os.Rename(tmpPath, localPath); err != nil {
			return singleToolError(streamID, call, "move downloaded file failed: "+err.Error())
		}
		output := fmt.Sprintf("downloaded %s:%s to %s (%d bytes)", node, remotePath, args.LocalPath, bytesWritten)
		return singleNodeToolResult(streamID, call, node, output, true)
	}
}

func resolveFileTransferClient(node string, clients map[string]*mcp.Client) (string, *mcp.Client, error) {
	node = strings.TrimSpace(node)
	if node == "" {
		return "", nil, fmt.Errorf("node is required for file transfer")
	}
	client, ok := clients[node]
	if !ok || client == nil {
		return "", nil, fmt.Errorf("node not found or no client configured: %s", node)
	}
	return node, client, nil
}

func resolveWorkspaceFilePath(root, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative to the local workspace")
	}
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside local workspace")
	}
	if err := rejectWorkspaceSymlinkPath(rootAbs, targetAbs); err != nil {
		return "", err
	}
	return targetAbs, nil
}

func rejectWorkspaceSymlinkPath(rootAbs, targetAbs string) error {
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains symlink: %s", part)
		}
	}
	return nil
}

func singleToolError(streamID uint64, call llm.ToolCall, output string) multiToolResultMsg {
	return singleNodeToolResult(streamID, call, "-", output, false)
}

func singleNodeToolResult(streamID uint64, call llm.ToolCall, node, output string, success bool) multiToolResultMsg {
	return multiToolResultMsg{
		streamID: streamID,
		Call:     call,
		Results:  []nodeToolResult{{Node: node, Output: output, Success: success}},
	}
}

func (m Model) dispatchSubagentsRun(streamID uint64, call llm.ToolCall) tea.Cmd {
	if !m.subagents.Enabled {
		return func() tea.Msg {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "subagents are disabled. Use /subagents on to enable delegation.", Success: false}}}
		}
	}
	return func() tea.Msg {
		tasks, err := subagent.ParseTasks(call.Arguments)
		if err != nil {
			return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "invalid subagent tasks: " + err.Error(), Success: false}}}
		}
		requests := make([]subagent.Request, 0, len(tasks))
		for _, task := range tasks {
			nodes := m.restrictSubagentNodes(task.Nodes)
			if len(task.Nodes) > 0 && len(nodes) == 0 {
				return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: "subagent task has no allowed target nodes", Success: false}}}
			}
			requests = append(requests, m.newSubagentRequest(task.Role, task.Task, nodes))
		}
		ctx := m.streamCtx
		if ctx == nil {
			ctx = context.Background()
		}
		results := m.runSubagentBatch(ctx, requests)
		output := subagent.FormatResults(results)
		return multiToolResultMsg{streamID: streamID, Call: call, Results: []nodeToolResult{{Node: "local", Output: output, Success: subagentResultsSuccessful(results)}}}
	}
}

func (m Model) runSubagentBatch(ctx context.Context, requests []subagent.Request) []subagent.Result {
	maxParallel := m.subagents.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 1
	}
	results := make([]subagent.Result, len(requests))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := range requests {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = m.runSubagent(ctx, requests[i])
		}()
	}
	wg.Wait()
	return results
}

func subagentResultsSuccessful(results []subagent.Result) bool {
	for _, result := range results {
		if result.Err != nil {
			return false
		}
	}
	return true
}

func (m Model) restrictSubagentNodes(requested []string) []string {
	allowed := make(map[string]bool)
	for _, node := range m.selectedNodeNames() {
		allowed[node] = true
	}
	if len(requested) == 0 {
		return m.selectedNodeNames()
	}
	var nodes []string
	for _, node := range requested {
		if allowed[node] {
			nodes = append(nodes, node)
		}
	}
	sort.Strings(nodes)
	return nodes
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

type subagentToolExecutor struct {
	model Model
	nodes []string
}

func (e subagentToolExecutor) ExecuteSubagentTool(ctx context.Context, call llm.ToolCall) (string, bool) {
	switch call.Name {
	case metaToolToolSearch:
		return e.executeToolSearch(call)
	case metaToolCallTool:
		return e.executeCallTool(ctx, call)
	case "memory_search", "memory_read":
		if e.model.memStore == nil {
			return "memory not available", false
		}
		result := memory.HandleTool(e.model.memStore, "", call.Name, call.Arguments)
		return result.Output, result.Success
	default:
		return "blocked: subagents may only use read-only search, call_tool, memory_search, and memory_read", false
	}
}

func (e subagentToolExecutor) executeToolSearch(call llm.ToolCall) (string, bool) {
	args, err := parseMetaArgs(call.Arguments)
	if err != nil {
		return "invalid arguments: " + err.Error(), false
	}
	if args.Query == "" {
		return "query is required", false
	}
	nodes := e.targetNodes(args.Node)
	if len(nodes) == 0 {
		return "no allowed target nodes", false
	}
	results := e.model.toolCache.Search(args.Query, nodes)
	if len(results) == 0 {
		return "No tools found matching query: " + args.Query, true
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	return string(b), true
}

func (e subagentToolExecutor) executeCallTool(ctx context.Context, call llm.ToolCall) (string, bool) {
	args, err := parseMetaArgs(call.Arguments)
	if err != nil {
		return "invalid arguments: " + err.Error(), false
	}
	if args.Tool == "" {
		return "tool name is required", false
	}
	if !isReadOnlyNodeTool(args.Tool) {
		return "blocked: subagents may only call specialized read-only tools", false
	}
	nodes := e.targetNodes(args.Node)
	if len(nodes) == 0 {
		return "no allowed target nodes", false
	}
	toolArgs := args.Arguments
	if toolArgs == nil {
		toolArgs = json.RawMessage("{}")
	}
	result := e.model.fanOutCallTool(0, call, nodes, e.model.clients, args.Tool, func() json.RawMessage { return toolArgs }, ctx)
	return formatNodeToolResults(result.Results), allNodeToolResultsSuccessful(result.Results)
}

func (e subagentToolExecutor) targetNodes(specified string) []string {
	allowed := e.nodes
	if len(allowed) == 0 {
		allowed = e.model.selectedNodeNames()
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, node := range allowed {
		allowedSet[node] = true
	}
	if specified != "" {
		if allowedSet[specified] {
			return []string{specified}
		}
		return nil
	}
	nodes := append([]string(nil), allowed...)
	sort.Strings(nodes)
	return nodes
}

func isReadOnlyNodeTool(name string) bool {
	switch name {
	case "fs/read", "fs/list", "fs/stat",
		"sys/cpu", "sys/mem", "sys/disk", "sys/net", "sys/processes",
		"svc/list", "svc/status",
		"log/read", "log/journalctl",
		"net/ping", "net/traceroute", "net/portcheck",
		"web/search", "web/fetch",
		"k8s/pods", "k8s/logs", "k8s/events", "k8s/describe",
		"pkg/list", "pkg/search",
		"cron/list", "cron/show",
		"docker/ps", "docker/images", "docker/logs":
		return true
	default:
		return false
	}
}

func formatNodeToolResults(results []nodeToolResult) string {
	var lines []string
	for _, result := range results {
		prefix := result.Node
		if !result.Success {
			prefix += " ERROR"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", prefix, result.Output))
	}
	return strings.Join(lines, "\n")
}

func allNodeToolResultsSuccessful(results []nodeToolResult) bool {
	for _, result := range results {
		if !result.Success {
			return false
		}
	}
	return true
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
	parts = append(parts, "You are Conan, a high-discipline infrastructure operations agent in a TUI. Answer in the user's language; be concise, evidence-based, and careful.")
	parts = append(parts, fmt.Sprintf("Cluster: %s", m.cluster))
	if len(nodes) > 0 {
		parts = append(parts, fmt.Sprintf("Available nodes: %s. Use the node parameter in tools to target a specific node. If omitted, the tool runs on all nodes.", strings.Join(nodes, ", ")))
	}
	parts = append(parts, strings.Join([]string{
		"Core operating rules:",
		"- Preserve user intent. If an ambiguous next action could change resources, ask one short clarifying question.",
		"- Do not expose hidden tool plumbing. Report final findings, evidence, and user-visible actions.",
		"- Never claim success without tool output or observable evidence.",
		"- Avoid raw shell when Conan/MCP capabilities exist. Do not use ad hoc scp, rsync, curl, or wget for file transfer; use file_put or file_get directly.",
	}, "\n"))
	parts = append(parts, strings.Join([]string{
		"Tool routing contract:",
		"- For file upload/download/transfer/copy between local workspace and a node, use file_put or file_get directly. Do not call tool_search first for file transfer.",
		"- For node state/action, diagnostics, inspection, logs, services, containers, Kubernetes, packages, cron, or remote filesystem access, call tool_search first unless the user explicitly asked for shell or gave an exact command.",
		"- For attached images, call image_analyze before claiming visual details.",
		"- After tool_search, use call_tool with a discovered specialized tool when it fits; follow its schema exactly.",
		"- Use exec only as fallback when no suitable specialized tool exists, specialized output is insufficient, the user asked for shell, or shell risk review is intentional.",
		"- For resource-changing operations, first use read-only tools when useful, then execute through a reviewed path. Do not bypass confirmations.",
		"- Use local/fs/read, list, and stat for local inspection; use local/fs/write, patch, and delete for local changes with confirmation unless allowlisted.",
		"- If no specialized tool is relevant, say so before using or suggesting shell fallback.",
	}, "\n"))
	if m.subagents.Enabled {
		parts = append(parts, strings.Join([]string{
			"Subagent policy:",
			"- Use subagents_run for independent investigation, cross-node comparison, review, or summarization when delegation reduces latency or improves reliability.",
			"- Do not delegate destructive actions or resource-changing operations.",
			"- Give each subagent a bounded task, selected node scope, and expected output.",
			"- Use subagent results as evidence, then answer the user yourself.",
		}, "\n"))
	}

	if m.skillsConfig.Enabled && len(m.skills) > 0 {
		if index := skills.BuildSkillIndex(m.skills, m.skillsConfig.IndexTokenBudget); strings.TrimSpace(index) != "" {
			parts = append(parts, "\n[Skills]\n"+index)
		}
	}

	if m.memStore != nil {
		parts = append(parts, strings.Join([]string{
			"Memory policy:",
			"- Use memory_search when durable preferences, rules, topology, incidents, or runbooks may help.",
			"- Use memory_patch or memory_write_note when the user explicitly asks you to remember, save, or record durable facts, or for important durable operational facts.",
			"- Save durable operational facts: topology, incidents, troubleshooting findings, preferences, and runbook experience. Do not save casual chat, secrets, credentials, or tokens.",
			"- Use memory_read before patching existing Markdown memory.",
		}, "\n"))
		memoryRoot := filepath.Join(m.memStore.Dir(), "memory")
		var injectedMarkdown []string
		rc, err := memory.LoadRules(memoryRoot)
		if err == nil && !rc.Empty() {
			rules := rc.Format()
			injectedMarkdown = append(injectedMarkdown, rules)
			parts = append(parts, "\n[Behavioral Rules]\n"+limitPromptSnippet(rules, markdownPromptMemoryLimit))
		}
		clusterMemory, err := memory.NewMarkdownStore(memoryRoot).Read(filepath.ToSlash(filepath.Join("clusters", sanitizeMemoryFileName(m.cluster)+".md")))
		if err == nil && strings.TrimSpace(clusterMemory) != "" {
			injectedMarkdown = append(injectedMarkdown, clusterMemory)
			parts = append(parts, "\n[Cluster Memory]\n"+limitPromptSnippet(clusterMemory, markdownPromptMemoryLimit))
		}
		results, err := m.memStore.ListMemories("", 5)
		if err == nil && len(results) > 0 {
			var memLines []string
			markdownText := strings.Join(injectedMarkdown, "\n")
			for _, r := range results {
				if memoryEntryDuplicatedInMarkdown(r, markdownText) {
					continue
				}
				memLines = append(memLines, fmt.Sprintf("- [%s] %s: %s", r.Category, r.Title, limitPromptSnippet(r.Content, sqlitePromptMemoryContentLimit)))
			}
			if len(memLines) > 0 {
				parts = append(parts, "\n[Memory Context]\n"+strings.Join(memLines, "\n"))
			}
		}
	}

	return strings.Join(parts, "\n")
}

func limitPromptSnippet(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}

func memoryEntryDuplicatedInMarkdown(entry memory.MemoryEntry, markdownText string) bool {
	content := strings.TrimSpace(entry.Content)
	if content == "" || markdownText == "" {
		return false
	}
	return strings.Contains(markdownText, content)
}

func sanitizeMemoryFileName(cluster string) string {
	normalized := strings.ToLower(strings.TrimSpace(cluster))
	var b strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name != "" {
		return name
	}
	sum := sha1.Sum([]byte(normalized))
	return "cluster-" + hex.EncodeToString(sum[:])[:12]
}

func (m Model) maybeAutoSaveUserMemory(input string) {
	if m.memStore == nil {
		return
	}
	candidate, ok := memory.CandidateFromExplicitRemember(input, m.cluster)
	if !ok {
		return
	}
	m.saveMemoryCandidate(candidate, input, false)
}

func (m Model) runMemoryExtraction(userText, assistantText string) {
	if m.memStore == nil || m.memoryExtractor == nil {
		return
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if assistantText == "" {
		return
	}
	candidates, err := m.memoryExtractor.ExtractMemory(context.Background(), MemoryExtractionInput{
		Cluster:   m.cluster,
		Model:     m.model,
		User:      userText,
		Assistant: assistantText,
	})
	if err != nil {
		slog.Debug("memory extraction failed", "error", err)
		return
	}
	evidenceText := strings.Join([]string{userText, assistantText}, "\n")
	for _, candidate := range candidates {
		m.saveMemoryCandidate(candidate, evidenceText, true)
	}
}

func (m Model) saveMemoryCandidate(candidate memory.MemoryCandidate, evidenceText string, requireEvidence bool) {
	if m.memStore == nil {
		return
	}
	if err := memory.ValidateMemoryCandidate(candidate, evidenceText, requireEvidence); err != nil {
		slog.Debug("auto-save memory skipped invalid candidate", "error", err)
		return
	}
	if candidate.ID == "" {
		candidate.ID = models.NewID()
	}
	convID := ""
	if m.conv != nil {
		convID = m.conv.ID()
	}
	dest := memory.DestinationFor(candidate, m.cluster)
	markdown := memory.NewMarkdownStore(filepath.Join(m.memStore.Dir(), "memory"))

	var err error
	switch dest.Kind {
	case "markdown":
		err = markdown.PatchSection(dest.Path, candidate.Title, candidate.Content)
	case "markdown-note":
		_, err = markdown.WriteNote(dest.Path, candidate.Title, candidate.Content, candidate.Content, candidate.Tags)
	case "sqlite":
		tags, marshalErr := json.Marshal(candidate.Tags)
		if marshalErr != nil {
			slog.Debug("auto-save memory failed", "error", marshalErr)
			return
		}
		err = m.memStore.SaveMemory(memory.MemoryEntry{
			ID:         candidate.ID,
			Category:   candidate.Category,
			Title:      candidate.Title,
			Content:    candidate.Content,
			Tags:       string(tags),
			SourceConv: convID,
		})
	case "discard":
		return
	default:
		slog.Debug("auto-save memory skipped unknown destination", "destination", dest.Kind, "category", candidate.Category)
		return
	}
	if err != nil {
		slog.Debug("auto-save memory failed", "error", err, "destination", dest.Kind, "path", dest.Path)
	}
}

func (m Model) latestUserMessage() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "user" && strings.TrimSpace(m.messages[i].content) != "" {
			return m.messages[i].content
		}
	}
	return ""
}

func (m Model) latestAssistantMessage() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "assistant" && strings.TrimSpace(m.messages[i].content) != "" {
			return m.messages[i].content
		}
	}
	return ""
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
		hidden:      isHiddenInternalTool(call.Name),
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
		m.sessionList, _ = m.sessionList.Update(key)
		selected := m.sessionList.Selected()
		m.mode = modeChat
		if selected != nil {
			m.status = fmt.Sprintf(m.uiLanguage.tr("Loading session %s...", "正在加载会话 %s..."), selected.ID)
			return m, m.loadSession(selected.ID)
		}
		m.status = m.uiLanguage.tr("No session selected", "未选择会话")
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeChat
		m.status = m.uiLanguage.tr("Resume cancelled", "已取消恢复")
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
		if store == nil {
			return sessionLoadMsg{err: fmt.Errorf("memory store not available")}
		}
		rec, err := store.LoadConversation(id)
		if err != nil {
			return sessionLoadMsg{err: err}
		}
		return sessionLoadMsg{record: rec}
	}
}

func (m *Model) applyLoadedSession(rec *memory.ConversationRecord) {
	if rec == nil {
		return
	}
	var messages []models.Message
	if strings.TrimSpace(rec.Messages) != "" {
		_ = json.Unmarshal([]byte(rec.Messages), &messages)
	}
	var nodes []string
	if strings.TrimSpace(rec.Nodes) != "" {
		_ = json.Unmarshal([]byte(rec.Nodes), &nodes)
	}
	m.conv = conversation.Restore(rec.ID, rec.Cluster, nodes, rec.Model, messages)
	m.cluster = rec.Cluster
	m.model = rec.Model
	m.selectedNodes = make(map[string]bool)
	for _, node := range nodes {
		m.selectedNodes[node] = true
	}
	visibleMessages := messages
	if len(visibleMessages) > maxResumedVisibleMessages {
		visibleMessages = visibleMessages[len(visibleMessages)-maxResumedVisibleMessages:]
	}
	m.messages = chatMessagesFromModels(visibleMessages)
	m.lastBodyContent = ""
}

func chatMessagesFromModels(messages []models.Message) []chatMsg {
	result := make([]chatMsg, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case conversation.RoleUser:
			result = append(result, chatMsg{role: "user", content: msg.Content})
		case conversation.RoleAssistant:
			result = append(result, chatMsg{role: "assistant", content: msg.Content, toolCallID: msg.ToolCallID, toolName: msg.ToolName, toolInput: msg.ToolInput})
		case conversation.RoleTool:
			result = append(result, chatMsg{role: "tool", content: msg.Content, toolCallID: msg.ToolCallID, toolName: msg.ToolName, toolInput: msg.ToolInput, toolOutput: msg.ToolOutput})
		}
	}
	return result
}

func (m Model) archiveCompaction(messages []models.Message) (string, error) {
	if m.memStore == nil || m.conv == nil || len(messages) == 0 {
		return "", nil
	}
	dir := filepath.Join(m.memStore.Dir(), "archives", m.conv.ID())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "compact-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	data, err := json.MarshalIndent(compactArchive{
		ConversationID: m.conv.ID(),
		Cluster:        m.cluster,
		Model:          m.model,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		Messages:       messages,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func (m Model) SaveCurrentConversation() (string, error) {
	return m.saveCurrentConversation()
}

func (m Model) saveCurrentConversation() (string, error) {
	if m.memStore == nil || m.conv == nil {
		return "", fmt.Errorf("memory store or conversation not available")
	}
	msgs := m.conv.Messages()
	msgJSON, _ := json.Marshal(msgs)
	nodes := make([]string, 0, len(m.selectedNodes))
	for n := range m.selectedNodes {
		nodes = append(nodes, n)
	}
	nodesJSON, _ := json.Marshal(nodes)
	if err := m.memStore.SaveConversation(memory.ConversationRecord{
		ID:       m.conv.ID(),
		Cluster:  m.cluster,
		Nodes:    string(nodesJSON),
		Model:    m.model,
		Summary:  conversationSummaryForMessages(msgs),
		Messages: string(msgJSON),
	}); err != nil {
		return "", err
	}
	return m.conv.ID(), nil
}

func conversationSummaryForMessages(messages []models.Message) string {
	for _, msg := range messages {
		if msg.Role != conversation.RoleUser {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if !strings.HasPrefix(content, "Previous conversation compacted.") {
			continue
		}
		if idx := strings.Index(content, "\n\nSummary:\n"); idx >= 0 {
			summary := strings.TrimSpace(content[idx+len("\n\nSummary:\n"):])
			if summary != "" {
				firstLine, _, ok := strings.Cut(summary, "\n")
				if ok {
					return truncateStr(strings.TrimSpace(firstLine), 120)
				}
				return truncateStr(summary, 120)
			}
		}
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if txt := strings.TrimSpace(last.Content); txt != "" {
			return truncateStr(txt, 120)
		}
	}
	return ""
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
