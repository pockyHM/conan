package memory

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
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
			"name":        "memory_search",
			"description": "Implicitly search Conan memory across Markdown and SQLite. Use when prior user preferences, rules, topology, incidents, or runbooks may help.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query"},
					"limit": map[string]interface{}{"type": "integer", "description": "Max results (default 5)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "memory_read",
			"description": "Read an allowed Markdown memory file or section under the Conan memory directory.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Relative Markdown memory path"},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "memory_patch",
			"description": "Patch a named section in an allowed Markdown memory file. Use for durable preferences, rules, topology, and profile facts.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "Relative Markdown memory path"},
					"section": map[string]interface{}{"type": "string", "description": "Markdown section heading"},
					"content": map[string]interface{}{"type": "string", "description": "Replacement section content"},
				},
				"required": []string{"path", "section", "content"},
			},
		},
		{
			"name":        "memory_write_note",
			"description": "Create a structured Markdown memory note for incidents, runbooks, or topology details.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{"type": "string", "description": "rules, clusters, runbooks, or incidents"},
					"title":    map[string]interface{}{"type": "string", "description": "Note title"},
					"summary":  map[string]interface{}{"type": "string", "description": "Short summary"},
					"content":  map[string]interface{}{"type": "string", "description": "Full note content"},
					"tags":     map[string]interface{}{"type": "string", "description": "Comma-separated tags"},
				},
				"required": []string{"category", "title", "summary", "content", "tags"},
			},
		},
		{
			"name":        "memory_promote",
			"description": "Promote a SQLite memory entry into Markdown memory.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":       map[string]interface{}{"type": "string", "description": "SQLite memory ID"},
					"category": map[string]interface{}{"type": "string", "description": "Destination category"},
				},
				"required": []string{"id", "category"},
			},
		},
	}
}

func IsMemoryTool(name string) bool {
	switch normalizeMemoryToolName(name) {
	case "memory_save", "memory_update", "memory_delete", "memory_search", "memory_read", "memory_patch", "memory_write_note", "memory_promote":
		return true
	default:
		return false
	}
}

func normalizeMemoryToolName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

func HandleTool(store *Store, convID string, name string, args json.RawMessage) ToolResult {
	switch normalizeMemoryToolName(name) {
	case "memory_save":
		return handleMemorySave(store, convID, args)
	case "memory_update":
		return handleMemoryUpdate(store, args)
	case "memory_delete":
		return handleMemoryDelete(store, args)
	case "memory_search":
		return handleMemorySearch(store, args)
	case "memory_read":
		return handleMemoryRead(store, args)
	case "memory_patch":
		return handleMemoryPatch(store, args)
	case "memory_write_note":
		return handleMemoryWriteNote(store, args)
	case "memory_promote":
		return handleMemoryPromote(store, args)
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
	if err := validateMemoryTitle("title", a.Title); err != nil {
		return ToolResult{Output: "save failed: " + err.Error(), Success: false}
	}
	if err := validateMemoryContent("content", a.Content); err != nil {
		return ToolResult{Output: "save failed: " + err.Error(), Success: false}
	}
	if err := rejectSecretLikeMemoryText(a.Title + "\n" + a.Content); err != nil {
		return ToolResult{Output: "save failed: " + err.Error(), Success: false}
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
	if err := validateMemoryTitle("title", existing.Title); err != nil {
		return ToolResult{Output: "update failed: " + err.Error(), Success: false}
	}
	if err := validateMemoryContent("content", existing.Content); err != nil {
		return ToolResult{Output: "update failed: " + err.Error(), Success: false}
	}
	if err := rejectSecretLikeMemoryText(existing.Title + "\n" + existing.Content); err != nil {
		return ToolResult{Output: "update failed: " + err.Error(), Success: false}
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

type readArgs struct {
	Path string `json:"path"`
}

type patchArgs struct {
	Path    string `json:"path"`
	Section string `json:"section"`
	Content string `json:"content"`
}

type writeNoteArgs struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Content  string `json:"content"`
	Tags     string `json:"tags"`
}

type promoteArgs struct {
	ID       string `json:"id"`
	Category string `json:"category"`
}

func handleMemorySearch(store *Store, args json.RawMessage) ToolResult {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 5
	}
	results, err := store.SearchMemories(a.Query, limit)
	if err != nil {
		return ToolResult{Output: "search failed: " + err.Error(), Success: false}
	}
	var lines []string
	for _, r := range results {
		if len(lines) >= limit {
			break
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", r.ID, r.Title, truncate(r.Content, 100)))
	}
	if len(lines) < limit {
		markdownResults := searchMarkdownMemory(filepath.Join(store.Dir(), "memory"), a.Query, limit-len(lines))
		lines = append(lines, markdownResults...)
	}
	if len(lines) == 0 {
		return ToolResult{Output: "No memories found for: " + a.Query, Success: true}
	}
	return ToolResult{Output: strings.Join(lines, "\n"), Success: true}
}

func handleMemoryRead(store *Store, args json.RawMessage) ToolResult {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	out, err := NewMarkdownStore(filepath.Join(store.Dir(), "memory")).Read(a.Path)
	if err != nil {
		return ToolResult{Output: "read failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: out, Success: true}
}

func handleMemoryPatch(store *Store, args json.RawMessage) ToolResult {
	var a patchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	if err := NewMarkdownStore(filepath.Join(store.Dir(), "memory")).PatchSection(a.Path, a.Section, a.Content); err != nil {
		return ToolResult{Output: "patch failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: "Updated memory markdown: " + a.Path + "#" + a.Section, Success: true}
}

func handleMemoryWriteNote(store *Store, args json.RawMessage) ToolResult {
	var a writeNoteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	if strings.TrimSpace(a.Category) == "" || strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Summary) == "" || strings.TrimSpace(a.Content) == "" || strings.TrimSpace(a.Tags) == "" {
		return ToolResult{Output: "write note failed: category, title, summary, content, and tags are required", Success: false}
	}
	path, err := NewMarkdownStore(filepath.Join(store.Dir(), "memory")).WriteNote(a.Category, a.Title, a.Summary, a.Content, splitTags(a.Tags))
	if err != nil {
		return ToolResult{Output: "write note failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: "Created memory note: " + path, Success: true}
}

func handleMemoryPromote(store *Store, args json.RawMessage) ToolResult {
	var a promoteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	entry, err := store.GetMemory(a.ID)
	if err != nil {
		return ToolResult{Output: "memory not found: " + a.ID, Success: false}
	}
	category := strings.TrimSpace(a.Category)
	if category == "" {
		category = entry.Category
	}
	category = normalizePromotionCategory(category)
	candidate := MemoryCandidate{
		ID:       entry.ID,
		Category: category,
		Title:    entry.Title,
		Content:  entry.Content,
		Tags:     memoryEntryTags(entry.Tags),
	}
	if err := ValidateMemoryCandidate(candidate, entry.Content, false); err != nil {
		return ToolResult{Output: "promote failed: " + err.Error(), Success: false}
	}
	dest := DestinationFor(candidate, "")
	markdown := NewMarkdownStore(filepath.Join(store.Dir(), "memory"))
	switch dest.Kind {
	case "markdown":
		if err := markdown.PatchSection(dest.Path, candidate.Title, candidate.Content); err != nil {
			return ToolResult{Output: "promote failed: " + err.Error(), Success: false}
		}
		return ToolResult{Output: "Promoted memory to markdown: " + dest.Path, Success: true}
	case "markdown-note":
		path, err := markdown.WriteNote(dest.Path, candidate.Title, truncate(candidate.Content, 120), candidate.Content, candidate.Tags)
		if err != nil {
			return ToolResult{Output: "promote failed: " + err.Error(), Success: false}
		}
		return ToolResult{Output: "Promoted memory to markdown note: " + path, Success: true}
	case "sqlite":
		return ToolResult{Output: "Memory remains in SQLite: " + entry.ID, Success: true}
	default:
		return ToolResult{Output: "promote skipped: unsupported destination for category " + category, Success: false}
	}
}

func searchMarkdownMemory(root string, query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit <= 0 {
		return nil
	}
	var lines []string
	store := NewMarkdownStore(root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(lines) >= limit {
			return fs.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		data, err := store.Read(rel)
		if err != nil {
			return nil
		}
		if !strings.Contains(strings.ToLower(data), query) {
			return nil
		}
		lines = append(lines, fmt.Sprintf("- [markdown] %s: %s", rel, markdownSearchSummary(data, query)))
		return nil
	})
	if err != nil {
		return nil
	}
	return lines
}

func normalizePromotionCategory(category string) string {
	switch strings.TrimSpace(category) {
	case "rules":
		return "rule"
	case "clusters":
		return "topology"
	case "runbooks":
		return "runbook"
	case "incidents":
		return "incident"
	default:
		return category
	}
}

func markdownSearchSummary(content string, query string) string {
	title := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	if title != "" {
		return title
	}
	lower := strings.ToLower(content)
	idx := strings.Index(lower, query)
	if idx == -1 {
		return truncate(strings.TrimSpace(content), 100)
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 60
	if end > len(content) {
		end = len(content)
	}
	return truncate(strings.TrimSpace(content[start:end]), 100)
}

func memoryEntryTags(tags string) []string {
	parsed := unmarshalTags(tags)
	if len(parsed) > 0 {
		return parsed
	}
	return splitTags(tags)
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
