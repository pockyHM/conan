package memory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pockyHM/conan/pkg/models"
)

type ToolResult struct {
	Output  string
	Success bool
}

func ToolDefs() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "memory/save",
			"description": "Save an operational memory entry (experience, event, troubleshooting finding, topology info)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{"type": "string", "description": "Memory category: event, experience, troubleshooting, topology"},
					"title":    map[string]interface{}{"type": "string", "description": "Short descriptive title"},
					"content":  map[string]interface{}{"type": "string", "description": "Full memory content"},
					"tags":     map[string]interface{}{"type": "string", "description": "Comma-separated tags for retrieval"},
				},
				"required": []string{"category", "title", "content"},
			},
		},
		{
			"name":        "memory/update",
			"description": "Update an existing memory entry by ID",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":       map[string]interface{}{"type": "string", "description": "Memory entry ID"},
					"category": map[string]interface{}{"type": "string", "description": "New category"},
					"title":    map[string]interface{}{"type": "string", "description": "New title"},
					"content":  map[string]interface{}{"type": "string", "description": "New content"},
					"tags":     map[string]interface{}{"type": "string", "description": "New comma-separated tags"},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "memory/delete",
			"description": "Delete a memory entry by ID",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "string", "description": "Memory entry ID to delete"},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "memory/search",
			"description": "Search memories by keyword across title, content, tags, and category",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query keyword"},
					"limit": map[string]interface{}{"type": "integer", "description": "Max results (default 10)"},
				},
				"required": []string{"query"},
			},
		},
	}
}

func IsMemoryTool(name string) bool {
	return name == "memory/save" || name == "memory/update" || name == "memory/delete" || name == "memory/search"
}

func HandleTool(store *Store, convID string, name string, args json.RawMessage) ToolResult {
	switch name {
	case "memory/save":
		return handleMemorySave(store, convID, args)
	case "memory/update":
		return handleMemoryUpdate(store, args)
	case "memory/delete":
		return handleMemoryDelete(store, args)
	case "memory/search":
		return handleMemorySearch(store, args)
	default:
		return ToolResult{Output: "unknown memory tool: " + name, Success: false}
	}
}

type saveArgs struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Tags     string `json:"tags"`
}

func handleMemorySave(store *Store, convID string, args json.RawMessage) ToolResult {
	var a saveArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	id := models.NewID()
	tags := a.Tags
	if tags == "" {
		tags = "[]"
	} else {
		parts := splitTags(tags)
		b, _ := json.Marshal(parts)
		tags = string(b)
	}
	entry := MemoryEntry{
		ID:         id,
		Category:   a.Category,
		Title:      a.Title,
		Content:    a.Content,
		Tags:       tags,
		SourceConv: convID,
	}
	if err := store.SaveMemory(entry); err != nil {
		return ToolResult{Output: "save failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: fmt.Sprintf("Saved memory %s: %s", id, a.Title), Success: true}
}

type updateArgs struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Tags     string `json:"tags"`
}

func handleMemoryUpdate(store *Store, args json.RawMessage) ToolResult {
	var a updateArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	existing, err := store.GetMemory(a.ID)
	if err != nil {
		return ToolResult{Output: "memory not found: " + a.ID, Success: false}
	}
	if a.Category != "" {
		existing.Category = a.Category
	}
	if a.Title != "" {
		existing.Title = a.Title
	}
	if a.Content != "" {
		existing.Content = a.Content
	}
	if a.Tags != "" {
		parts := splitTags(a.Tags)
		b, _ := json.Marshal(parts)
		existing.Tags = string(b)
	}
	if err := store.UpdateMemory(*existing); err != nil {
		return ToolResult{Output: "update failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: fmt.Sprintf("Updated memory %s", a.ID), Success: true}
}

type deleteArgs struct {
	ID string `json:"id"`
}

func handleMemoryDelete(store *Store, args json.RawMessage) ToolResult {
	var a deleteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	if err := store.DeleteMemory(a.ID); err != nil {
		return ToolResult{Output: "delete failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: fmt.Sprintf("Deleted memory %s", a.ID), Success: true}
}

type searchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func handleMemorySearch(store *Store, args json.RawMessage) ToolResult {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	results, err := store.SearchMemories(a.Query, a.Limit)
	if err != nil {
		return ToolResult{Output: "search failed: " + err.Error(), Success: false}
	}
	if len(results) == 0 {
		return ToolResult{Output: "No memories found for: " + a.Query, Success: true}
	}
	var lines []string
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", r.ID, r.Title, truncate(r.Content, 100)))
	}
	return ToolResult{Output: strings.Join(lines, "\n"), Success: true}
}

func splitTags(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
