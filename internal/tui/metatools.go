package tui

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	toolmeta "github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

const (
	metaToolExec         = "exec"
	metaToolToolSearch   = "tool_search"
	metaToolCallTool     = "call_tool"
	metaToolSubagentsRun = "subagents_run"
	metaToolNodeAdd      = "node_add"
	metaToolFilePut      = "file_put"
	metaToolFileGet      = "file_get"
	metaToolImageAnalyze = "image_analyze"
	metaToolAskChoice    = "ask_choice"
)

var metaToolDefs = []llm.ToolDef{
	{
		Name:        metaToolExec,
		Description: "Last-resort shell execution on selected nodes. Do not use this as the first choice for diagnostics, inspection, service checks, logs, containers, Kubernetes, packages, or filesystem tasks. For file transfer, use file_put or file_get directly instead of shell commands such as scp or rsync. First call tool_search to discover a safer specialized Conan/MCP tool unless the user explicitly asks for a shell command or provides an exact command to run. Use exec only when no specialized tool fits, specialized output is insufficient, or the operation must intentionally go through shell risk review.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node": {"type": "string", "description": "Target node name. Omit to execute on all selected nodes."},
				"command": {"type": "string", "description": "Shell command to execute"}
			},
			"required": ["command"]
		}`),
	},
	{
		Name:        metaToolToolSearch,
		Description: "Primary discovery step for node capabilities except first-class file transfer. Search specialized Conan/MCP tools by capability, noun, verb, and synonyms; examples: 'service status logs journalctl', 'docker container logs', 'kubernetes pod events'. Use this before exec for operational requests unless the user explicitly asked for shell. For file upload/download, use file_put or file_get directly. Returns matching tool names, descriptions, available nodes, and parameter schemas; then use call_tool for the selected tool.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Capability search query using concrete verbs, nouns, and synonyms from the user's request"},
				"node": {"type": "string", "description": "Search tools on a specific node. Omit to search across all selected nodes."}
			},
			"required": ["query"]
		}`),
	},
	{
		Name:        metaToolCallTool,
		Description: "Call a specialized Conan/MCP tool discovered with tool_search. Prefer this over exec when the discovered tool can answer the request or perform the requested managed operation. Use the exact input schema returned by tool_search. Read-only tools may be called directly; mutating tools are subject to Conan risk review and user confirmation.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node": {"type": "string", "description": "Target node name"},
				"tool": {"type": "string", "description": "Name of the tool to call"},
				"arguments": {"type": "object", "description": "Arguments to pass to the tool"}
			},
			"required": ["node", "tool"]
		}`),
	},
	{
		Name:        metaToolSubagentsRun,
		Description: "Delegate bounded read-only investigation, review, or summarization tasks to local subagents. Use for independent multi-node investigation, review, or summarization. Do not use for destructive actions.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tasks": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"role": {"type": "string", "enum": ["investigator", "reviewer", "summarizer"]},
							"task": {"type": "string"},
							"nodes": {"type": "array", "items": {"type": "string"}}
						},
						"required": ["role", "task"]
					}
				}
			},
			"required": ["tasks"]
		}`),
	},
	{
		Name:        metaToolFilePut,
		Description: "Upload a local workspace text file to a remote node through Conan's managed file transfer API. Binary and image files are refused. Use this directly for file upload, send, copy, put, transfer-to-node, or local-to-remote requests. Do not use tool_search, scp, rsync, curl, or shell for this. Requires user confirmation because it writes a remote file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node": {"type": "string", "description": "Target node name. Required."},
				"local_path": {"type": "string", "description": "Local workspace file path to upload. Must be relative to the workspace."},
				"remote_path": {"type": "string", "description": "Absolute or agent-visible destination path on the remote node."}
			},
			"required": ["node", "local_path", "remote_path"]
		}`),
	},
	{
		Name:        metaToolFileGet,
		Description: "Download a remote node text file into the local workspace through Conan's managed file transfer API. Binary and image files are refused. Use this directly for file download, fetch, get, copy-from-node, transfer-from-node, or remote-to-local requests. Do not use tool_search, scp, rsync, curl, or shell for this. Requires user confirmation because it writes a local file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"node": {"type": "string", "description": "Source node name. Required."},
				"remote_path": {"type": "string", "description": "Remote file path to download."},
				"local_path": {"type": "string", "description": "Local workspace destination path. Must be relative to the workspace."}
			},
			"required": ["node", "remote_path", "local_path"]
		}`),
	},
	{
		Name:        metaToolAskChoice,
		Description: "Ask the user to choose one option in the TUI before continuing. Use this when the next step depends on a user decision, such as choosing a plan, approving a non-tool workflow choice, selecting a mode, or clarifying an ambiguous preference. Do not use for security approval of tool execution; Conan handles that separately.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"question": {"type": "string", "description": "The concise question to show to the user."},
				"options": {
					"type": "array",
					"minItems": 2,
					"maxItems": 10,
					"items": {
						"type": "object",
						"properties": {
							"label": {"type": "string", "description": "Short user-visible option label."},
							"value": {"type": "string", "description": "Stable machine-readable value returned to the model."},
							"description": {"type": "string", "description": "Optional short explanation of the option."}
						},
						"required": ["label", "value"]
					}
				},
				"default_value": {"type": "string", "description": "Optional option value to preselect."},
				"allow_cancel": {"type": "boolean", "description": "Whether Esc should return a cancellation result."}
			},
			"required": ["question", "options"]
		}`),
	},
}

var imageToolDefs = []llm.ToolDef{
	{
		Name:        metaToolImageAnalyze,
		Description: "Analyze attached images by ID using Conan's configured vision model. Use this when the user's request depends on visual content from attached images, screenshots, diagrams, charts, or UI captures. The main agent cannot see image pixels directly; call this tool before making claims about an image.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"image_id": {"type": "integer", "description": "Single image ID to analyze, such as 1 for [Image #1]. Omit to analyze all attached images."},
				"image_ids": {"type": "array", "items": {"type": "integer"}, "description": "Multiple image IDs to analyze. Omit to analyze all attached images."},
				"question": {"type": "string", "description": "What to inspect or extract from the image(s). Be specific and concise."}
			}
		}`),
	},
}

var nodeManagementToolDefs = []llm.ToolDef{
	{
		Name:        metaToolNodeAdd,
		Description: "Add or update a node, write local cluster configuration, deploys or updates conan-agent over SSH, and verifies the agent health endpoint. This is a high-impact node management operation and requires user confirmation.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"cluster": {"type": "string", "description": "Cluster name. Omit to use the current TUI cluster."},
				"host": {"type": "string", "description": "Hostname or IP address to add."},
				"name": {"type": "string", "description": "Node name override. Defaults to host."},
				"user": {"type": "string", "description": "SSH username. Omit to prompt or use saved credentials."},
				"password": {"type": "string", "description": "SSH password. Omit to prompt or use saved credentials."},
				"ssh_port": {"type": "integer", "description": "SSH port. Defaults to cluster node_defaults.ssh_port, then 22."},
				"agent_port": {"type": "integer", "description": "conan-agent listen port. Defaults to 9280."},
				"agent_bin": {"type": "string", "description": "Local conan-agent binary override for this deployment."},
				"update": {"type": "boolean", "description": "Update an existing node instead of failing on duplicate name."},
				"rotate_token": {"type": "boolean", "description": "Generate a new per-node agent token while updating."}
			},
			"required": ["host"]
		}`),
	},
}

func sanitizeToolArguments(toolName string, raw json.RawMessage) json.RawMessage {
	if toolName != metaToolNodeAdd {
		return raw
	}
	invalidNodeAddArgs := json.RawMessage(`{"error":"invalid node_add arguments"}`)
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return invalidNodeAddArgs
	}
	if _, ok := args["password"]; ok {
		args["password"] = "[REDACTED]"
	}
	sanitized, err := json.Marshal(args)
	if err != nil {
		return invalidNodeAddArgs
	}
	return sanitized
}

type toolCache struct {
	mu    sync.RWMutex
	tools map[string][]mcpproto.ToolDefinition // node name -> tools
}

type toolCacheMsg struct {
	node  string
	tools []mcpproto.ToolDefinition
}

func newToolCache() *toolCache {
	return &toolCache{tools: make(map[string][]mcpproto.ToolDefinition)}
}

func (c *toolCache) Set(node string, tools []mcpproto.ToolDefinition) {
	c.mu.Lock()
	c.tools[node] = tools
	c.mu.Unlock()
}

func (c *toolCache) Get(node string) ([]mcpproto.ToolDefinition, bool) {
	c.mu.RLock()
	t, ok := c.tools[node]
	c.mu.RUnlock()
	return t, ok
}

func (c *toolCache) All() map[string][]mcpproto.ToolDefinition {
	c.mu.RLock()
	cp := make(map[string][]mcpproto.ToolDefinition, len(c.tools))
	for k, v := range c.tools {
		cp[k] = v
	}
	c.mu.RUnlock()
	return cp
}

func (c *toolCache) Search(query string, nodes []string) []toolSearchResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	queryTokens := tokenizeSearchText(query)
	if len(queryTokens) == 0 {
		return nil
	}

	docs := make([]toolSearchDoc, 0)
	for _, node := range nodes {
		tools, ok := c.tools[node]
		if !ok {
			continue
		}
		for _, t := range tools {
			docs = append(docs, newToolSearchDoc(node, t))
		}
	}
	if len(docs) == 0 {
		return nil
	}

	avgDocLen := averageToolSearchDocLength(docs)
	docFreq := toolSearchDocFrequencies(docs, queryTokens)
	type scoredResult struct {
		result toolSearchResult
		score  float64
	}
	resultsByName := make(map[string]*scoredResult)
	for _, doc := range docs {
		score := scoreToolSearchDoc(doc, query, queryTokens, docFreq, len(docs), avgDocLen)
		if score <= 0 {
			continue
		}
		existing, ok := resultsByName[doc.tool.Name]
		if !ok {
			resultsByName[doc.tool.Name] = &scoredResult{
				result: toolSearchResult{
					Name:        doc.tool.Name,
					Description: doc.tool.Description,
					Schema:      doc.tool.InputSchema,
					Nodes:       []string{doc.node},
					Safety:      string(doc.metadata.Safety),
					Scope:       string(doc.metadata.Scope),
					Capability:  append([]string(nil), doc.metadata.Capability...),
				},
				score: score,
			}
			continue
		}
		existing.result.Nodes = append(existing.result.Nodes, doc.node)
		if score > existing.score {
			existing.score = score
			existing.result.Description = doc.tool.Description
			existing.result.Schema = doc.tool.InputSchema
			existing.result.Safety = string(doc.metadata.Safety)
			existing.result.Scope = string(doc.metadata.Scope)
			existing.result.Capability = append([]string(nil), doc.metadata.Capability...)
		}
	}

	var scored []scoredResult
	for _, result := range resultsByName {
		sort.Strings(result.result.Nodes)
		scored = append(scored, *result)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].result.Name < scored[j].result.Name
		}
		return scored[i].score > scored[j].score
	})

	results := make([]toolSearchResult, 0, len(scored))
	for _, result := range scored {
		results = append(results, result.result)
	}
	return results
}

type toolSearchDoc struct {
	node     string
	tool     mcpproto.ToolDefinition
	metadata toolmeta.Metadata
	fields   []weightedSearchField
	length   float64
}

type weightedSearchField struct {
	weight float64
	tokens []string
}

func newToolSearchDoc(node string, tool mcpproto.ToolDefinition) toolSearchDoc {
	metadata, _ := toolmeta.MetadataFor(tool.Name)
	fields := []weightedSearchField{
		{weight: 4.0, tokens: tokenizeSearchText(tool.Name)},
		{weight: 2.0, tokens: tokenizeSearchText(tool.Description)},
		{weight: 0.75, tokens: tokenizeSearchText(string(tool.InputSchema))},
		{weight: 5.0, tokens: tokenizeSearchText(strings.Join(metadata.Capability, " "))},
		{weight: 3.0, tokens: tokenizeSearchText(string(metadata.Safety) + " " + strings.ReplaceAll(string(metadata.Safety), "-", " "))},
		{weight: 1.5, tokens: tokenizeSearchText(string(metadata.Scope))},
		{weight: 2.0, tokens: tokenizeSearchText(strings.Join(metadata.Tags, " "))},
	}
	length := 0.0
	for _, field := range fields {
		length += float64(len(field.tokens)) * field.weight
	}
	return toolSearchDoc{node: node, tool: tool, metadata: metadata, fields: fields, length: length}
}

func averageToolSearchDocLength(docs []toolSearchDoc) float64 {
	total := 0.0
	for _, doc := range docs {
		total += doc.length
	}
	if total == 0 {
		return 1
	}
	return total / float64(len(docs))
}

func toolSearchDocFrequencies(docs []toolSearchDoc, queryTokens []string) map[string]int {
	frequencies := make(map[string]int)
	for _, token := range uniqueTokens(queryTokens) {
		for _, doc := range docs {
			if toolSearchDocContains(doc, token) {
				frequencies[token]++
			}
		}
	}
	return frequencies
}

func toolSearchDocContains(doc toolSearchDoc, token string) bool {
	for _, field := range doc.fields {
		for _, fieldToken := range field.tokens {
			if fieldToken == token {
				return true
			}
		}
	}
	return false
}

func scoreToolSearchDoc(doc toolSearchDoc, query string, queryTokens []string, docFreq map[string]int, docCount int, avgDocLen float64) float64 {
	const (
		k1 = 1.2
		b  = 0.75
	)
	score := 0.0
	for _, token := range uniqueTokens(queryTokens) {
		tf := weightedTermFrequency(doc, token)
		if tf == 0 {
			continue
		}
		df := docFreq[token]
		idf := math.Log(1 + (float64(docCount)-float64(df)+0.5)/(float64(df)+0.5))
		denom := tf + k1*(1-b+b*(doc.length/avgDocLen))
		score += idf * ((tf * (k1 + 1)) / denom)
	}

	q := strings.ToLower(strings.TrimSpace(query))
	name := strings.ToLower(doc.tool.Name)
	desc := strings.ToLower(doc.tool.Description)
	if q != "" && strings.Contains(name, q) {
		score += 5
	}
	if q != "" && strings.Contains(desc, q) {
		score += 2
	}
	return score
}

func weightedTermFrequency(doc toolSearchDoc, token string) float64 {
	tf := 0.0
	for _, field := range doc.fields {
		for _, fieldToken := range field.tokens {
			if fieldToken == token {
				tf += field.weight
			}
		}
	}
	return tf
}

func uniqueTokens(tokens []string) []string {
	seen := make(map[string]bool, len(tokens))
	var unique []string
	for _, token := range tokens {
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		unique = append(unique, token)
	}
	return unique
}

func tokenizeSearchText(text string) []string {
	var tokens []string
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		tokens = append(tokens, b.String())
	}
	return tokens
}

type toolSearchResult struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema,omitempty"`
	Nodes       []string        `json:"available_on"`
	Safety      string          `json:"safety,omitempty"`
	Scope       string          `json:"scope,omitempty"`
	Capability  []string        `json:"capability,omitempty"`
}

func fetchNodeTools(clients map[string]*mcp.Client) tea.Cmd {
	return func() tea.Msg {
		type nodeTools struct {
			node  string
			tools []mcpproto.ToolDefinition
		}
		ch := make(chan nodeTools, len(clients))
		for name, client := range clients {
			n, c := name, client
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tools, err := c.ListTools(ctx)
				if err != nil {
					ch <- nodeTools{node: n, tools: nil}
					return
				}
				ch <- nodeTools{node: n, tools: tools}
			}()
		}

		var msgs []toolCacheMsg
		for range clients {
			nt := <-ch
			if nt.tools != nil {
				msgs = append(msgs, toolCacheMsg{node: nt.node, tools: nt.tools})
			}
		}
		return toolCacheBatchMsg{updates: msgs}
	}
}

type toolCacheBatchMsg struct {
	updates []toolCacheMsg
}
