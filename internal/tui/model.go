package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
)

type NodeInfo struct {
	Name   string
	Host   string
	Online bool
}

type ModelConfig struct {
	Cluster  string
	Model    string
	Provider llm.Provider
	Conv     *conversation.Conversation
	Clients  map[string]*mcp.Client
	Tools    []llm.ToolDef
	Nodes    []NodeInfo
}

type chatMsg struct {
	role        string
	content     string
	toolName    string
	toolInput   string
	toolOutput  string
	nodeResults []nodeToolResult
}

type tuiMode int

const (
	modeChat       tuiMode = iota
	modeNodeSelect
)

type pingResultMsg struct {
	node   string
	online bool
}

type Model struct {
	cluster   string
	model     string
	provider  llm.Provider
	conv      *conversation.Conversation
	clients   map[string]*mcp.Client
	tools     []llm.ToolDef

	nodes         []NodeInfo
	selectedNodes map[string]bool

	mode         tuiMode
	nodeSelector nodeSelector
	prevSelected map[string]bool

	input     string
	messages  []chatMsg
	status    string
	streaming bool
	streamBuf string
	streamCh  <-chan llm.ChatEvent

	width  int
	height int
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
		provider:      cfg.Provider,
		conv:          cfg.Conv,
		clients:       cfg.Clients,
		tools:         cfg.Tools,
		nodes:         cfg.Nodes,
		selectedNodes: selectedNodes,
		status:        "Ready",
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

type streamReadyMsg struct {
	ch  <-chan llm.ChatEvent
	err error
}

type streamEventMsg struct {
	Event llm.ChatEvent
}

type streamDoneMsg struct{}

type nodeToolResult struct {
	Node    string
	Output  string
	Success bool
}

type multiToolResultMsg struct {
	Call    llm.ToolCall
	Results []nodeToolResult
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case streamReadyMsg:
		if msg.err != nil {
			m.streaming = false
			m.status = "Error: " + msg.err.Error()
			return m, nil
		}
		m.streamCh = msg.ch
		return m, m.waitForEvent()

	case streamEventMsg:
		switch e := msg.Event.(type) {
		case llm.TextDeltaEvent:
			m.streamBuf += e.Delta
		case llm.ToolCallEvent:
			m.messages = append(m.messages, chatMsg{
				role:      "tool",
				toolName:  e.Name,
				toolInput: string(e.Arguments),
			})
			if m.conv != nil {
				m.conv.AddToolCall(e.ID, e.Name, string(e.Arguments))
			}
			return m, m.dispatchTool(llm.ToolCall{ID: e.ID, Name: e.Name, Arguments: e.Arguments})
		case llm.StopEvent:
			if m.conv != nil {
				m.conv.AddAssistant(m.streamBuf)
			}
			if m.streamBuf != "" {
				m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf})
			}
			m.streamBuf = ""
			if e.Reason == llm.StopToolUse {
				m.status = "Running tool..."
				return m, m.waitForEvent()
			}
			m.streaming = false
			m.status = "Ready"
			return m, nil
		case llm.ErrorEvent:
			m.streaming = false
			m.status = "Stream error: " + e.Err.Error()
			return m, nil
		}
		return m, m.waitForEvent()

	case streamDoneMsg:
		if m.streamBuf != "" {
			if m.conv != nil {
				m.conv.AddAssistant(m.streamBuf)
			}
			m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf})
			m.streamBuf = ""
		}
		m.streaming = false
		m.status = "Stream ended"
		return m, nil

	case multiToolResultMsg:
		var outputParts []string
		for _, r := range msg.Results {
			if r.Success {
				outputParts = append(outputParts, fmt.Sprintf("[%s] %s", r.Node, r.Output))
			} else {
				outputParts = append(outputParts, fmt.Sprintf("[%s] ERROR: %s", r.Node, r.Output))
			}
		}
		aggregatedOutput := strings.Join(outputParts, "\n")

		found := false
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == "tool" && m.messages[i].toolOutput == "" {
				m.messages[i].toolOutput = aggregatedOutput
				m.messages[i].nodeResults = msg.Results
				found = true
				break
			}
		}
		if !found {
			m.messages = append(m.messages, chatMsg{
				role:        "tool",
				toolName:    msg.Call.Name,
				toolInput:   string(msg.Call.Arguments),
				toolOutput:  aggregatedOutput,
				nodeResults: msg.Results,
			})
		}
		if m.conv != nil {
			m.conv.AddToolResult(msg.Call.ID, aggregatedOutput)
		}
		return m, m.startStream()

	case pingResultMsg:
		for i := range m.nodes {
			if m.nodes[i].Name == msg.node {
				m.nodes[i].Online = msg.online
				break
			}
		}
		if m.mode == modeNodeSelect {
			m.nodeSelector = m.nodeSelector.SetNodes(m.nodes)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeNodeSelect {
		return m.handleNodeSelectKey(key)
	}
	if m.streaming {
		if key.Type == tea.KeyCtrlC {
			m.streaming = false
			m.streamCh = nil
			m.status = "Interrupted"
			return m, nil
		}
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyCtrlL:
		m.messages = nil
		m.status = "Conversation cleared"
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyRunes:
		m.input += string(key.Runes)
		return m, nil
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
	header := lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Conan | Cluster: %s | Model: %s | Nodes: %d/%d", m.cluster, m.model, len(m.selectedNodes), len(m.nodes)),
	)

	if m.mode == modeNodeSelect {
		return fmt.Sprintf("%s\n\n%s\n\n%s", header, m.nodeSelector.View(), m.status)
	}

	var bodyParts []string
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			bodyParts = append(bodyParts, "You: "+msg.content)
		case "assistant":
			bodyParts = append(bodyParts, "Conan: "+msg.content)
		case "tool":
			if len(msg.nodeResults) > 1 {
				header := fmt.Sprintf("-> %s on %d node(s)", msg.toolName, len(msg.nodeResults))
				if msg.toolOutput != "" {
					var lines []string
					for i, r := range msg.nodeResults {
						prefix := "├──"
						if i == len(msg.nodeResults)-1 {
							prefix = "└──"
						}
						icon := "✓"
						if !r.Success {
							icon = "✗"
						}
						output := r.Output
						if idx := strings.Index(output, "\n"); idx != -1 {
							output = output[:idx]
						}
						if len(output) > 60 {
							output = output[:57] + "..."
						}
						lines = append(lines, fmt.Sprintf("%s %s %s  %s", prefix, r.Node, icon, output))
					}
					header += "\n" + strings.Join(lines, "\n")
				} else {
					header += " (running...)"
				}
				bodyParts = append(bodyParts, header)
			} else {
				header := fmt.Sprintf("-> %s", msg.toolName)
				if len(msg.nodeResults) == 1 {
					header = fmt.Sprintf("-> %s on %s", msg.toolName, msg.nodeResults[0].Node)
				}
				if msg.toolOutput != "" {
					header += "\n" + msg.toolOutput
				} else {
					header += " (running...)"
				}
				bodyParts = append(bodyParts, header)
			}
		}
	}

	if m.streaming && m.streamBuf != "" {
		bodyParts = append(bodyParts, "Conan: "+m.streamBuf+"...")
	}

	body := strings.Join(bodyParts, "\n\n")
	if body == "" {
		body = "No messages yet. Type a message or /help."
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n> %s", header, body, m.status, m.input)
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
	return m, m.startStream()
}

func (m Model) applyCommand(cmd SlashCommand) (Model, tea.Cmd) {
	switch cmd.Kind {
	case CommandHelp:
		m.messages = append(m.messages, chatMsg{
			role:    "assistant",
			content: "Conan: /help /clear /exit /cluster [name] /model [name] /nodes /memory /resume /config",
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
	default:
		m.status = "Unknown command: /" + cmd.Arg
	}
	return m, nil
}

func (m Model) startStream() tea.Cmd {
	if m.provider == nil {
		return nil
	}
	provider := m.provider
	var selected []string
	for name := range m.selectedNodes {
		selected = append(selected, name)
	}
	sort.Strings(selected)
	req := &llm.ChatRequest{
		SystemPrompt: buildSystemPrompt(m.cluster, selected),
		Messages:     m.conv.Messages(),
		Tools:        m.tools,
	}
	return func() tea.Msg {
		ch, err := provider.ChatStream(context.Background(), req)
		return streamReadyMsg{ch: ch, err: err}
	}
}

func (m Model) waitForEvent() tea.Cmd {
	ch := m.streamCh
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return streamEventMsg{Event: event}
	}
}

func (m Model) dispatchTool(call llm.ToolCall) tea.Cmd {
	clients := m.clients
	selected := m.selectedNodes
	return func() tea.Msg {
		if len(selected) == 0 {
			return multiToolResultMsg{
				Call: call,
				Results: []nodeToolResult{
					{Node: "-", Output: "No nodes selected. Use /nodes to select target nodes.", Success: false},
				},
			}
		}

		type result struct {
			node    string
			output  string
			success bool
		}

		var wg sync.WaitGroup
		ch := make(chan result, len(selected))

		for name := range selected {
			client, exists := clients[name]
			if !exists {
				ch <- result{node: name, output: "no client configured for node", success: false}
				continue
			}
			wg.Add(1)
			go func(n string, c *mcp.Client) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				toolResult, err := c.CallTool(ctx, call.Name, call.Arguments)
				if err != nil {
					ch <- result{node: n, output: err.Error(), success: false}
					return
				}
				var output string
				for _, block := range toolResult.Content {
					output += block.Text
				}
				ch <- result{node: n, output: output, success: true}
			}(name, client)
		}

		wg.Wait()
		close(ch)

		var results []nodeToolResult
		for r := range ch {
			results = append(results, nodeToolResult{Node: r.node, Output: r.output, Success: r.success})
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Node < results[j].Node
		})

		return multiToolResultMsg{Call: call, Results: results}
	}
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

func buildSystemPrompt(cluster string, selectedNodes []string) string {
	nodes := strings.Join(selectedNodes, ", ")
	return fmt.Sprintf("You are Conan, an AI operations assistant. Cluster: %s. Target nodes: %s. Help the user manage their infrastructure.", cluster, nodes)
}
