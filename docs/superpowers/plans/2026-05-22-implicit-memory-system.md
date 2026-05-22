# Implicit Memory System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an implicit, layered memory system that recalls and updates user/profile/rule/ops knowledge without showing memory tool mechanics during normal chat.

**Architecture:** Markdown becomes the durable knowledge layer for profile, rules, topology, runbooks, and incidents. SQLite remains the episodic/search layer. TUI requests pre-load relevant memory, LLMs get restricted underscore-named memory tools, and post-turn extraction routes candidates into Markdown or SQLite.

**Tech Stack:** Go, Bubble Tea TUI, existing `internal/memory` SQLite store, Markdown files under `~/.conan/memory/memory`, existing OpenAI/Anthropic-compatible tool definitions.

---

## File Structure

- Create `internal/memory/markdown.go`: path-safe Markdown memory store, file reads, section patching, and note creation.
- Create `internal/memory/markdown_test.go`: path validation, read, patch, and note creation tests.
- Create `internal/memory/classifier.go`: deterministic routing helpers for memory categories and destinations.
- Create `internal/memory/classifier_test.go`: category routing and explicit-memory destination tests.
- Modify `internal/memory/tools.go`: replace exposed tool names with `memory_search`, `memory_read`, `memory_patch`, `memory_write_note`, and `memory_promote`; keep old slash names as aliases in `HandleTool`.
- Modify `internal/memory/tools_test.go`: test underscore tool definitions and old-name aliases.
- Modify `internal/tui/model.go`: hide memory tool messages, inject progressive memory context, and run post-turn extraction.
- Modify `internal/tui/model_test.go`: test hidden memory tool rendering, prompt injection, explicit remember routing, and post-turn extraction.
- Modify `internal/tui/render.go` only if hidden memory messages need render support.
- Modify `cmd/conan/main.go`: ensure full Markdown memory directory tree is created.

---

### Task 1: Markdown Memory Store

**Files:**
- Create: `internal/memory/markdown.go`
- Test: `internal/memory/markdown_test.go`

- [ ] **Step 1: Write failing tests for safe Markdown reads and patching**

Add `internal/memory/markdown_test.go`:

```go
package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownStoreRejectsPathTraversal(t *testing.T) {
	store := NewMarkdownStore(t.TempDir())

	if _, err := store.Read("../secret.md"); err == nil {
		t.Fatal("expected path traversal read to fail")
	}
	if err := store.PatchSection("../secret.md", "Rules", "content"); err == nil {
		t.Fatal("expected path traversal patch to fail")
	}
}

func TestMarkdownStoreReadAllowedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("core memory"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMarkdownStore(root)

	got, err := store.Read("MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "core memory" {
		t.Fatalf("Read() = %q, want core memory", got)
	}
}

func TestMarkdownStorePatchSectionAddsAndReplacesSection(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	if err := store.PatchSection("rules/ops.md", "Restart Policy", "- restart only after health check"); err != nil {
		t.Fatal(err)
	}
	first, err := store.Read("rules/ops.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "## Restart Policy\n\n- restart only after health check") {
		t.Fatalf("missing section after first patch:\n%s", first)
	}

	if err := store.PatchSection("rules/ops.md", "Restart Policy", "- require approval for production"); err != nil {
		t.Fatal(err)
	}
	second, err := store.Read("rules/ops.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, "restart only after health check") {
		t.Fatalf("old section content was not replaced:\n%s", second)
	}
	if !strings.Contains(second, "## Restart Policy\n\n- require approval for production") {
		t.Fatalf("new section content missing:\n%s", second)
	}
}

func TestMarkdownStoreWriteNoteCreatesSluggedFile(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	path, err := store.WriteNote("incidents", "API OOM", "summary", "details", []string{"api", "oom"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "incidents/") || !strings.HasSuffix(path, "-api-oom.md") {
		t.Fatalf("path = %q, want incidents date slug", path)
	}
	content, err := store.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# API OOM", "summary", "details", "tags: api, oom"} {
		if !strings.Contains(content, want) {
			t.Fatalf("note missing %q:\n%s", want, content)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/memory -run 'TestMarkdownStore' -count=1
```

Expected: FAIL because `NewMarkdownStore`, `Read`, `PatchSection`, and `WriteNote` do not exist.

- [ ] **Step 3: Implement Markdown store**

Create `internal/memory/markdown.go`:

```go
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type MarkdownStore struct {
	root string
}

func NewMarkdownStore(root string) *MarkdownStore {
	return &MarkdownStore{root: root}
}

func (s *MarkdownStore) Read(rel string) (string, error) {
	path, err := s.safePath(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *MarkdownStore) PatchSection(rel string, heading string, content string) error {
	if strings.TrimSpace(heading) == "" {
		return fmt.Errorf("heading is required")
	}
	path, err := s.safePath(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	existingBytes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(existingBytes)
	section := "## " + strings.TrimSpace(heading) + "\n\n" + strings.TrimSpace(content) + "\n"
	updated := replaceMarkdownSection(existing, strings.TrimSpace(heading), section)
	return os.WriteFile(path, []byte(updated), 0644)
}

func (s *MarkdownStore) WriteNote(category string, title string, summary string, content string, tags []string) (string, error) {
	if !allowedMemoryCategory(category) {
		return "", fmt.Errorf("unsupported memory note category: %s", category)
	}
	slug := slugify(title)
	if slug == "" {
		return "", fmt.Errorf("title is required")
	}
	rel := filepath.ToSlash(filepath.Join(category, time.Now().Format("2006-01-02")+"-"+slug+".md"))
	path, err := s.safePath(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	body := "# " + strings.TrimSpace(title) + "\n\n" +
		"summary: " + strings.TrimSpace(summary) + "\n" +
		"tags: " + strings.Join(tags, ", ") + "\n\n" +
		strings.TrimSpace(content) + "\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return "", err
	}
	return rel, nil
}

func (s *MarkdownStore) safePath(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("memory path outside root: %s", rel)
	}
	if !strings.HasSuffix(clean, ".md") {
		return "", fmt.Errorf("memory path must be markdown: %s", rel)
	}
	path := filepath.Join(s.root, clean)
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("memory path outside root: %s", rel)
	}
	return path, nil
}

func replaceMarkdownSection(existing string, heading string, replacement string) string {
	lines := strings.Split(existing, "\n")
	start := -1
	end := len(lines)
	target := "## " + heading
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i
			break
		}
	}
	if start == -1 {
		prefix := strings.TrimRight(existing, "\n")
		if prefix == "" {
			return replacement
		}
		return prefix + "\n\n" + replacement
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	before := strings.Join(lines[:start], "\n")
	after := strings.Join(lines[end:], "\n")
	parts := []string{}
	if strings.TrimSpace(before) != "" {
		parts = append(parts, strings.TrimRight(before, "\n"))
	}
	parts = append(parts, strings.TrimRight(replacement, "\n"))
	if strings.TrimSpace(after) != "" {
		parts = append(parts, strings.TrimLeft(after, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func allowedMemoryCategory(category string) bool {
	switch category {
	case "profile", "rules", "clusters", "runbooks", "incidents":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/memory -run 'TestMarkdownStore' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/markdown.go internal/memory/markdown_test.go
git commit -m "feat: add markdown memory store"
```

---

### Task 2: Memory Classification and Routing

**Files:**
- Create: `internal/memory/classifier.go`
- Test: `internal/memory/classifier_test.go`

- [ ] **Step 1: Write failing routing tests**

Add `internal/memory/classifier_test.go`:

```go
package memory

import "testing"

func TestMemoryDestinationForCategories(t *testing.T) {
	tests := []struct {
		category string
		cluster  string
		wantKind string
		wantPath string
	}{
		{"profile", "prod", "markdown", "profile.md"},
		{"rule", "prod", "markdown", "rules/ops.md"},
		{"topology", "prod", "markdown", "clusters/prod.md"},
		{"runbook", "prod", "markdown-note", "runbooks"},
		{"incident", "prod", "markdown-note", "incidents"},
		{"event", "prod", "sqlite", ""},
		{"discard", "prod", "discard", ""},
	}
	for _, tt := range tests {
		got := DestinationFor(MemoryCandidate{Category: tt.category}, tt.cluster)
		if got.Kind != tt.wantKind || got.Path != tt.wantPath {
			t.Fatalf("DestinationFor(%q) = %#v, want kind=%q path=%q", tt.category, got, tt.wantKind, tt.wantPath)
		}
	}
}

func TestExplicitRememberClassifiesAsProfileForName(t *testing.T) {
	got, ok := CandidateFromExplicitRemember("记住我叫小王", "prod")
	if !ok {
		t.Fatal("expected candidate")
	}
	if got.Category != "profile" {
		t.Fatalf("category = %q, want profile", got.Category)
	}
	if got.Content != "我叫小王" {
		t.Fatalf("content = %q, want 我叫小王", got.Content)
	}
}

func TestExplicitRememberClassifiesAsRuleForNorms(t *testing.T) {
	got, ok := CandidateFromExplicitRemember("以后记得代码必须 gofmt", "prod")
	if !ok {
		t.Fatal("expected candidate")
	}
	if got.Category != "rule" {
		t.Fatalf("category = %q, want rule", got.Category)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/memory -run 'TestMemoryDestination|TestExplicitRemember' -count=1
```

Expected: FAIL because `DestinationFor`, `MemoryCandidate`, and `CandidateFromExplicitRemember` do not exist.

- [ ] **Step 3: Implement classifier**

Create `internal/memory/classifier.go`:

```go
package memory

import (
	"strings"

	"github.com/pockyHM/conan/pkg/models"
)

type MemoryCandidate struct {
	ID       string
	Category string
	Title    string
	Content  string
	Tags     []string
}

type MemoryDestination struct {
	Kind string
	Path string
}

func DestinationFor(candidate MemoryCandidate, cluster string) MemoryDestination {
	switch candidate.Category {
	case "profile":
		return MemoryDestination{Kind: "markdown", Path: "profile.md"}
	case "rule":
		return MemoryDestination{Kind: "markdown", Path: "rules/ops.md"}
	case "topology":
		if strings.TrimSpace(cluster) == "" {
			cluster = "default"
		}
		return MemoryDestination{Kind: "markdown", Path: "clusters/" + slugify(cluster) + ".md"}
	case "runbook":
		return MemoryDestination{Kind: "markdown-note", Path: "runbooks"}
	case "incident":
		return MemoryDestination{Kind: "markdown-note", Path: "incidents"}
	case "event":
		return MemoryDestination{Kind: "sqlite"}
	default:
		return MemoryDestination{Kind: "discard"}
	}
}

func CandidateFromExplicitRemember(input string, cluster string) (MemoryCandidate, bool) {
	content, ok := ExtractExplicitRememberContent(input)
	if !ok {
		return MemoryCandidate{}, false
	}
	category := "event"
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(content, "我叫") || strings.Contains(lower, "my name is"):
		category = "profile"
	case strings.Contains(content, "规范") || strings.Contains(content, "必须") || strings.Contains(content, "以后") || strings.Contains(lower, "always"):
		category = "rule"
	case strings.Contains(content, "集群") || strings.Contains(content, "节点") || strings.Contains(content, "服务"):
		category = "topology"
	}
	return MemoryCandidate{
		ID:       models.NewID(),
		Category: category,
		Title:    MemoryTitle(content),
		Content:  content,
		Tags:     []string{"user", "explicit"},
	}, true
}

func ExtractExplicitRememberContent(input string) (string, bool) {
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	prefixes := []string{"请记住", "帮我记住", "记住", "记一下", "以后记得", "请记录", "记录一下", "remember that", "remember", "note that", "note:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			content := strings.TrimSpace(text[len(prefix):])
			content = strings.TrimLeft(content, " ：:，,。.")
			return content, content != ""
		}
	}
	return "", false
}

func MemoryTitle(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	if line == "" {
		return "User memory"
	}
	if len([]rune(line)) <= 48 {
		return line
	}
	runes := []rune(line)
	return string(runes[:45]) + "..."
}
```

- [ ] **Step 4: Run classifier tests**

Run:

```bash
go test ./internal/memory -run 'TestMemoryDestination|TestExplicitRemember' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/classifier.go internal/memory/classifier_test.go
git commit -m "feat: classify memory destinations"
```

---

### Task 3: Implicit Memory Tools

**Files:**
- Modify: `internal/memory/tools.go`
- Modify: `internal/memory/tools_test.go`

- [ ] **Step 1: Write failing tests for underscore tools and aliases**

Append tests to `internal/memory/tools_test.go`:

```go
func TestToolDefsExposeImplicitUnderscoreNames(t *testing.T) {
	defs := ToolDefs()
	var names []string
	for _, def := range defs {
		names = append(names, def["name"].(string))
	}
	for _, want := range []string{"memory_search", "memory_read", "memory_patch", "memory_write_note", "memory_promote"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ToolDefs missing %s; names=%v", want, names)
		}
	}
	for _, legacy := range []string{"memory/save", "memory/search", "memory/update", "memory/delete"} {
		for _, name := range names {
			if name == legacy {
				t.Fatalf("ToolDefs should not expose legacy name %s", legacy)
			}
		}
	}
}

func TestMemoryToolAliases(t *testing.T) {
	for _, name := range []string{"memory_save", "memory/save", "memory_search", "memory/search"} {
		if !IsMemoryTool(name) {
			t.Fatalf("IsMemoryTool(%q) = false, want true", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/memory -run 'TestToolDefsExposeImplicitUnderscoreNames|TestMemoryToolAliases' -count=1
```

Expected: FAIL because current exposed names are slash-style only.

- [ ] **Step 3: Update tool definitions and aliases**

Modify `internal/memory/tools.go`:

```go
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
			"description": "Create a structured Markdown memory note for incidents, runbooks, topology, or profile details.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{"type": "string", "description": "profile, rules, clusters, runbooks, or incidents"},
					"title":    map[string]interface{}{"type": "string", "description": "Note title"},
					"summary":  map[string]interface{}{"type": "string", "description": "Short summary"},
					"content":  map[string]interface{}{"type": "string", "description": "Full note content"},
					"tags":     map[string]interface{}{"type": "string", "description": "Comma-separated tags"},
				},
				"required": []string{"category", "title", "content"},
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
```

Then update `HandleTool` to switch on `normalizeMemoryToolName(name)`. Keep existing SQLite handlers for `memory_save`, `memory_update`, `memory_delete`, and `memory_search`. Add Markdown handlers in Task 4.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/memory -run 'TestToolDefsExposeImplicitUnderscoreNames|TestMemoryToolAliases|TestHandleMemory' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/tools.go internal/memory/tools_test.go
git commit -m "feat: expose implicit memory tools"
```

---

### Task 4: Markdown Tool Handlers

**Files:**
- Modify: `internal/memory/tools.go`
- Modify: `internal/memory/tools_test.go`

- [ ] **Step 1: Write failing handler tests**

Append tests to `internal/memory/tools_test.go`:

```go
func TestHandleMemoryReadAndPatch(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	patchArgs := json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User prefers concise Chinese responses."}`)
	patch := HandleTool(store, "conv1", "memory_patch", patchArgs)
	if !patch.Success {
		t.Fatalf("patch failed: %s", patch.Output)
	}

	read := HandleTool(store, "conv1", "memory_read", json.RawMessage(`{"path":"profile.md"}`))
	if !read.Success {
		t.Fatalf("read failed: %s", read.Output)
	}
	if !strings.Contains(read.Output, "User prefers concise Chinese responses.") {
		t.Fatalf("read output missing patched content: %s", read.Output)
	}
}

func TestHandleMemoryWriteNote(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	args := json.RawMessage(`{"category":"incidents","title":"API OOM","summary":"api oom summary","content":"Root cause was cache pressure.","tags":"api,oom"}`)
	result := HandleTool(store, "conv1", "memory_write_note", args)
	if !result.Success {
		t.Fatalf("write note failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "incidents/") {
		t.Fatalf("output missing note path: %s", result.Output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/memory -run 'TestHandleMemoryReadAndPatch|TestHandleMemoryWriteNote' -count=1
```

Expected: FAIL because markdown handlers are not implemented.

- [ ] **Step 3: Implement handlers**

Add argument structs and handlers in `internal/memory/tools.go`:

```go
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
	err := NewMarkdownStore(filepath.Join(store.Dir(), "memory")).PatchSection(a.Path, a.Section, a.Content)
	if err != nil {
		return ToolResult{Output: "patch failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: "Updated memory markdown: " + a.Path + "#" + a.Section, Success: true}
}

func handleMemoryWriteNote(store *Store, args json.RawMessage) ToolResult {
	var a writeNoteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return ToolResult{Output: "invalid args: " + err.Error(), Success: false}
	}
	path, err := NewMarkdownStore(filepath.Join(store.Dir(), "memory")).WriteNote(a.Category, a.Title, a.Summary, a.Content, splitTags(a.Tags))
	if err != nil {
		return ToolResult{Output: "write note failed: " + err.Error(), Success: false}
	}
	return ToolResult{Output: "Created memory note: " + path, Success: true}
}
```

Update imports to include `path/filepath`. Update `HandleTool`:

```go
switch normalizeMemoryToolName(name) {
case "memory_read":
	return handleMemoryRead(store, args)
case "memory_patch":
	return handleMemoryPatch(store, args)
case "memory_write_note":
	return handleMemoryWriteNote(store, args)
}
```

- [ ] **Step 4: Run memory tests**

Run:

```bash
go test ./internal/memory -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/tools.go internal/memory/tools_test.go
git commit -m "feat: handle markdown memory tools"
```

---

### Task 5: Progressive Prompt Injection

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing prompt tests**

Add tests to `internal/tui/model_test.go`:

```go
func TestSystemPromptInjectsCoreRulesAndClusterMemory(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(root, "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "clusters"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("User prefers Chinese."), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules", "ops.md"), []byte("Never restart prod without checks."), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clusters", "production.md"), []byte("api runs on node-01."), 0644); err != nil {
		t.Fatal(err)
	}
	model := NewModel(ModelConfig{Cluster: "production", Model: "m", MemoryStore: store})

	prompt := model.buildSystemPromptWithMemory()

	for _, want := range []string{"User prefers Chinese.", "Never restart prod without checks.", "api runs on node-01."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/tui -run TestSystemPromptInjectsCoreRulesAndClusterMemory -count=1
```

Expected: FAIL because cluster Markdown memory is not injected and rules loading is shallow.

- [ ] **Step 3: Implement progressive injection**

In `internal/tui/model.go`, update `buildSystemPromptWithMemory()` inside `if m.memStore != nil`:

```go
memoryRoot := filepath.Join(m.memStore.Dir(), "memory")
rc, err := memory.LoadRules(memoryRoot)
if err == nil && !rc.Empty() {
	parts = append(parts, "\n[Core Memory]\n"+rc.Format())
}
clusterPath := filepath.Join(memoryRoot, "clusters", sanitizeMemoryFileName(m.cluster)+".md")
if data, err := os.ReadFile(clusterPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
	parts = append(parts, "\n[Cluster Memory]\n"+strings.TrimSpace(string(data)))
}
```

Add helper in `internal/tui/model.go`:

```go
func sanitizeMemoryFileName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
```

Add `os` import if needed.

- [ ] **Step 4: Run prompt tests**

Run:

```bash
go test ./internal/tui -run 'TestSystemPromptInjectsCoreRulesAndClusterMemory|TestSystemPromptExplainsMemoryPolicy' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: inject layered markdown memory"
```

---

### Task 6: Hide Memory Tools in Normal TUI

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing hidden-render test**

Add test:

```go
func TestMemoryToolCallIsHiddenFromNormalChat(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", MemoryStore: store})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, _ := model.Update(streamEventMsg{
		streamID: 1,
		Event: llm.ToolCallEvent{
			ID:        "mem1",
			Name:      "memory_patch",
			Arguments: json.RawMessage(`{"path":"profile.md","section":"Identity","content":"User name is Alice."}`),
		},
	})
	model = next.(Model)

	view := model.View()
	if strings.Contains(view, "memory_patch") || strings.Contains(view, "User name is Alice") {
		t.Fatalf("memory tool leaked into normal chat:\n%s", view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/tui -run TestMemoryToolCallIsHiddenFromNormalChat -count=1
```

Expected: FAIL because memory tool messages currently render like normal tool calls.

- [ ] **Step 3: Add hidden flag for memory tool messages**

Modify `chatMsg` in `internal/tui/model.go`:

```go
hidden bool
```

In the `llm.ToolCallEvent` branch, set hidden for memory tools:

```go
hidden := memory.IsMemoryTool(e.Name)
m.messages = append(m.messages, chatMsg{
	role:       "tool",
	toolCallID: e.ID,
	toolName:   e.Name,
	toolInput:  string(e.Arguments),
	hidden:     hidden,
})
```

In `renderBody()`, skip hidden messages:

```go
if msg.hidden {
	continue
}
```

- [ ] **Step 4: Run TUI hidden tool tests**

Run:

```bash
go test ./internal/tui -run 'TestMemoryToolCallIsHiddenFromNormalChat|TestToolCallReturnsCommandThatContinuesStreamWaiting' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: hide implicit memory tools in chat"
```

---

### Task 7: Explicit Remember Routes to Markdown

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing explicit remember Markdown test**

Replace or add explicit remember test:

```go
func TestExplicitRememberNameWritesProfileMarkdown(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "m",
		Conv:        conversation.New("production", nil, "m"),
		MemoryStore: store,
	})

	next, _ := model.submitMessage("记住我叫小王", nil)
	model = next.(Model)

	data, err := os.ReadFile(filepath.Join(store.Dir(), "memory", "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "我叫小王") {
		t.Fatalf("profile memory missing explicit name:\n%s", string(data))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/tui -run TestExplicitRememberNameWritesProfileMarkdown -count=1
```

Expected: FAIL because explicit remember currently writes SQLite only.

- [ ] **Step 3: Route explicit remember through classifier**

Replace `maybeAutoSaveUserMemory()` in `internal/tui/model.go`:

```go
func (m Model) maybeAutoSaveUserMemory(input string) {
	if m.memStore == nil {
		return
	}
	candidate, ok := memory.CandidateFromExplicitRemember(input, m.cluster)
	if !ok {
		return
	}
	dest := memory.DestinationFor(candidate, m.cluster)
	if dest.Kind == "discard" {
		return
	}
	if dest.Kind == "markdown" {
		md := memory.NewMarkdownStore(filepath.Join(m.memStore.Dir(), "memory"))
		if err := md.PatchSection(dest.Path, candidate.Title, candidate.Content); err != nil {
			slog.Debug("auto-save markdown memory failed", "error", err)
		}
		return
	}
	if dest.Kind == "markdown-note" {
		md := memory.NewMarkdownStore(filepath.Join(m.memStore.Dir(), "memory"))
		if _, err := md.WriteNote(dest.Path, candidate.Title, candidate.Content, candidate.Content, candidate.Tags); err != nil {
			slog.Debug("auto-save markdown note failed", "error", err)
		}
		return
	}
	convID := ""
	if m.conv != nil {
		convID = m.conv.ID()
	}
	tags, _ := json.Marshal(candidate.Tags)
	err := m.memStore.SaveMemory(memory.MemoryEntry{
		ID:         candidate.ID,
		Category:   candidate.Category,
		Title:      candidate.Title,
		Content:    candidate.Content,
		Tags:       string(tags),
		SourceConv: convID,
	})
	if err != nil {
		slog.Debug("auto-save sqlite memory failed", "error", err)
	}
}
```

Remove local `extractExplicitMemoryContent` and `memoryTitle` helpers after replacing their usage.

- [ ] **Step 4: Run explicit remember tests**

Run:

```bash
go test ./internal/tui -run 'TestExplicitRememberNameWritesProfileMarkdown|TestExplicitRememberMessageAutoSavesMemory' -count=1
```

Expected: PASS after updating the older SQLite-specific test to assert either profile Markdown or SQLite based on new category routing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: route explicit memory to markdown"
```

---

### Task 8: Post-Turn Memory Extraction Hook

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing post-turn extraction test**

Add a deterministic extractor interface to tests first:

```go
type stubMemoryExtractor struct {
	candidates []memory.MemoryCandidate
}

func (s stubMemoryExtractor) ExtractMemory(context.Context, MemoryExtractionInput) ([]memory.MemoryCandidate, error) {
	return s.candidates, nil
}

func TestPostTurnExtractionWritesIncidentNote(t *testing.T) {
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model := NewModel(ModelConfig{
		Cluster:     "production",
		Model:       "m",
		MemoryStore: store,
	})
	model.memoryExtractor = stubMemoryExtractor{candidates: []memory.MemoryCandidate{{
		ID:       "m1",
		Category: "incident",
		Title:    "API OOM",
		Content:  "Root cause was cache pressure.",
		Tags:     []string{"api", "oom"},
	}}}
	model.messages = append(model.messages, chatMsg{role: "user", content: "api oom"})
	model.streamBuf = "Root cause was cache pressure."

	model.runMemoryExtraction("api oom", "Root cause was cache pressure.")

	entries, err := os.ReadDir(filepath.Join(store.Dir(), "memory", "incidents"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("incident notes = %d, want 1", len(entries))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/tui -run TestPostTurnExtractionWritesIncidentNote -count=1
```

Expected: FAIL because extractor hook and types do not exist.

- [ ] **Step 3: Add extractor types and hook**

In `internal/tui/model.go`, add:

```go
type MemoryExtractionInput struct {
	Cluster   string
	Model     string
	User      string
	Assistant string
}

type MemoryExtractor interface {
	ExtractMemory(context.Context, MemoryExtractionInput) ([]memory.MemoryCandidate, error)
}
```

Add field to `ModelConfig`:

```go
MemoryExtractor MemoryExtractor
```

Add field to `Model`:

```go
memoryExtractor MemoryExtractor
```

Set it in `NewModel`.

Add:

```go
func (m Model) runMemoryExtraction(userText string, assistantText string) {
	if m.memStore == nil || m.memoryExtractor == nil || strings.TrimSpace(assistantText) == "" {
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
	for _, candidate := range candidates {
		m.saveMemoryCandidate(candidate)
	}
}

func (m Model) saveMemoryCandidate(candidate memory.MemoryCandidate) {
	dest := memory.DestinationFor(candidate, m.cluster)
	if dest.Kind == "discard" {
		return
	}
	md := memory.NewMarkdownStore(filepath.Join(m.memStore.Dir(), "memory"))
	switch dest.Kind {
	case "markdown":
		if err := md.PatchSection(dest.Path, candidate.Title, candidate.Content); err != nil {
			slog.Debug("memory markdown patch failed", "error", err)
		}
	case "markdown-note":
		if _, err := md.WriteNote(dest.Path, candidate.Title, candidate.Content, candidate.Content, candidate.Tags); err != nil {
			slog.Debug("memory note write failed", "error", err)
		}
	case "sqlite":
		tags, _ := json.Marshal(candidate.Tags)
		if err := m.memStore.SaveMemory(memory.MemoryEntry{
			ID:       candidate.ID,
			Category: candidate.Category,
			Title:    candidate.Title,
			Content:  candidate.Content,
			Tags:     string(tags),
		}); err != nil {
			slog.Debug("memory sqlite save failed", "error", err)
		}
	}
}
```

Call `runMemoryExtraction` after assistant content is appended on normal stop. If no reliable latest user helper exists, add `latestUserMessage()`:

```go
func (m Model) latestUserMessage() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "user" {
			return m.messages[i].content
		}
	}
	return ""
}
```

- [ ] **Step 4: Run post-turn extraction test**

Run:

```bash
go test ./internal/tui -run TestPostTurnExtractionWritesIncidentNote -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: add post-turn memory extraction hook"
```

---

### Task 9: Directory Setup and Full Verification

**Files:**
- Modify: `internal/memory/rules.go`
- Modify: `internal/memory/rules_test.go`
- Modify: `cmd/conan/main.go`

- [ ] **Step 1: Write failing directory setup test**

Add to `internal/memory/rules_test.go`:

```go
func TestEnsureMemoryDirCreatesStructuredMarkdownDirs(t *testing.T) {
	root := t.TempDir()
	if err := EnsureMemoryDir(root); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"rules", "clusters", "runbooks", "incidents"} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", rel)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/memory -run TestEnsureMemoryDirCreatesStructuredMarkdownDirs -count=1
```

Expected: FAIL because only `rules` is created today.

- [ ] **Step 3: Update directory creation**

Modify `EnsureMemoryDir` in `internal/memory/rules.go`:

```go
func EnsureMemoryDir(dir string) error {
	for _, rel := range []string{"", "rules", "clusters", "runbooks", "incidents"} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			return err
		}
	}
	return nil
}
```

Confirm `cmd/conan/main.go` still calls:

```go
memory.EnsureMemoryDir(filepath.Join(loader.Home(), "memory", "memory"))
```

- [ ] **Step 4: Run full verification**

Run:

```bash
go test ./internal/memory -count=1
go test ./internal/tui -count=1
go test ./... -count=1
```

Expected: all commands PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/rules.go internal/memory/rules_test.go cmd/conan/main.go
git commit -m "feat: create structured memory directories"
```

---

## Execution Notes

- Use TDD for every task. Run the specified failing test before implementation.
- Do not revert unrelated dirty files in the working tree.
- Prefer hidden memory behavior in normal TUI; debug logs may expose memory details for diagnosis.
- Existing `memory/save` slash aliases must continue working internally until a migration removes them.
- If a model provider rejects slash-style tool names, verify only underscore names are exposed in `ChatRequest.Tools`.

## Self-Review

- Spec coverage: Tasks cover Markdown layers, SQLite compatibility, implicit tools, progressive prompt injection, hidden UI behavior, explicit remember routing, post-turn extraction, safety through path validation, and directory migration.
- Placeholder scan: no placeholder implementation steps are intentionally left open.
- Type consistency: `MemoryCandidate`, `MemoryDestination`, `MarkdownStore`, and `MemoryExtractor` are introduced before later tasks use them.
