package tui

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

const (
	metaToolExec       = "exec"
	metaToolToolSearch = "tool_search"
	metaToolCallTool   = "call_tool"
)

var metaToolDefs = []llm.ToolDef{
	{
		Name:        metaToolExec,
		Description: "Execute a shell command on a specific node. Use this for ad-hoc commands, diagnostics, or quick operations.",
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
		Description: "Search for available tools on nodes. Returns matching tool names, descriptions, and parameter schemas. Use this before calling call_tool.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Search query to match against tool names and descriptions"},
				"node": {"type": "string", "description": "Search tools on a specific node. Omit to search across all selected nodes."}
			},
			"required": ["query"]
		}`),
	},
	{
		Name:        metaToolCallTool,
		Description: "Call a discovered tool on a specific node. Use tool_search first to find available tools and their parameters.",
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

	q := strings.ToLower(query)
	var results []toolSearchResult
	seen := make(map[string]bool)

	for _, node := range nodes {
		tools, ok := c.tools[node]
		if !ok {
			continue
		}
		for _, t := range tools {
			name := strings.ToLower(t.Name)
			desc := strings.ToLower(t.Description)
			if strings.Contains(name, q) || strings.Contains(desc, q) {
				if !seen[t.Name] {
					seen[t.Name] = true
					results = append(results, toolSearchResult{
						Name:        t.Name,
						Description: t.Description,
						Schema:      t.InputSchema,
						Nodes:       []string{node},
					})
				} else {
					for i := range results {
						if results[i].Name == t.Name {
							results[i].Nodes = append(results[i].Nodes, node)
							break
						}
					}
				}
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

type toolSearchResult struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema,omitempty"`
	Nodes       []string        `json:"available_on"`
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
