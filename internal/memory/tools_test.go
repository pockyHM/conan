package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsMemoryTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"memory_save", true},
		{"memory_update", true},
		{"memory_delete", true},
		{"memory_search", true},
		{"shell_run", false},
		{"memory", false},
	}
	for _, tt := range tests {
		if got := IsMemoryTool(tt.name); got != tt.want {
			t.Errorf("IsMemoryTool(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

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
	for _, legacy := range []string{"memory_save", "memory_update", "memory_delete"} {
		for _, name := range names {
			if name == legacy {
				t.Fatalf("ToolDefs should not expose legacy name %s", legacy)
			}
		}
	}
}

func TestMemoryToolAliases(t *testing.T) {
	for _, name := range []string{"memory_save", "memory_search"} {
		if !IsMemoryTool(name) {
			t.Fatalf("IsMemoryTool(%q) = false, want true", name)
		}
	}
}

func TestMemoryWriteNoteSchemaRequiresSummaryAndTags(t *testing.T) {
	var writeNote map[string]interface{}
	for _, def := range ToolDefs() {
		if def["name"] == "memory_write_note" {
			writeNote = def
			break
		}
	}
	if writeNote == nil {
		t.Fatal("ToolDefs missing memory_write_note")
	}
	schema := writeNote["inputSchema"].(map[string]interface{})
	required := schema["required"].([]string)
	for _, want := range []string{"category", "title", "summary", "content", "tags"} {
		found := false
		for _, got := range required {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("memory_write_note required missing %s; required=%v", want, required)
		}
	}
	properties := schema["properties"].(map[string]interface{})
	category := properties["category"].(map[string]interface{})
	description := category["description"].(string)
	if strings.Contains(description, "profile") {
		t.Fatalf("memory_write_note category description should not advertise profile notes: %s", description)
	}
}

func TestHandleMemoryReadAndPatch(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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

func TestHandleMemoryPatchRejectsSecretLikeContent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	args := json.RawMessage(`{"path":"profile.md","section":"Credentials","content":"authorization: bearer abc"}`)
	result := HandleTool(store, "conv1", "memory_patch", args)
	if result.Success {
		t.Fatalf("expected secret-like patch to fail: %s", result.Output)
	}
}

func TestHandleMemoryWriteNote(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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

func TestHandleMemoryWriteNoteRejectsSecretLikeContent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	args := json.RawMessage(`{"category":"incidents","title":"Credential Leak","summary":"credential leak","content":"password=abc","tags":"security"}`)
	result := HandleTool(store, "conv1", "memory_write_note", args)
	if result.Success {
		t.Fatalf("expected secret-like note to fail: %s", result.Output)
	}
}

func TestHandleMemorySaveRejectsSecretLikeContent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	args, _ := json.Marshal(map[string]string{
		"category": "event",
		"title":    "Credential Leak",
		"content":  "api key abc",
		"tags":     "security",
	})
	result := HandleTool(store, "conv1", "memory_save", args)
	if result.Success {
		t.Fatalf("expected secret-like save to fail: %s", result.Output)
	}
}

func TestMemorySearchIncludesMarkdownResults(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	writeArgs := json.RawMessage(`{"category":"incidents","title":"API OOM","summary":"api oom summary","content":"Root cause was cache pressure.","tags":"api,oom"}`)
	write := HandleTool(store, "conv1", "memory_write_note", writeArgs)
	if !write.Success {
		t.Fatalf("write note failed: %s", write.Output)
	}

	searchArgs := json.RawMessage(`{"query":"cache pressure","limit":5}`)
	result := HandleTool(store, "conv1", "memory_search", searchArgs)
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "incidents/") || !strings.Contains(result.Output, "API OOM") {
		t.Fatalf("search output missing markdown result: %s", result.Output)
	}
}

func TestMemoryReadCapsLargeMarkdownOutput(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(memoryRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte(strings.Repeat("x", memoryToolReadLimitBytes+1024)), 0600); err != nil {
		t.Fatal(err)
	}

	read := HandleTool(store, "conv1", "memory_read", json.RawMessage(`{"path":"MEMORY.md"}`))
	if !read.Success {
		t.Fatalf("read failed: %s", read.Output)
	}
	if !strings.Contains(read.Output, "[truncated]") {
		t.Fatalf("large memory read should be truncated")
	}
	if len(read.Output) > memoryToolReadLimitBytes+len("\n[truncated]") {
		t.Fatalf("large memory read output len = %d, want bounded", len(read.Output))
	}
}

func TestMemorySearchAndReadRunbookMarkdown(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "runbooks"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memoryRoot, "runbooks", "2026-05-23-nginx-502.md")
	content := "# Nginx 502 快速诊断\n\nsummary: nginx runbook\n\n## 步骤\n\n1. [read] svc_status\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	search := HandleTool(store, "conv1", "memory_search", json.RawMessage(`{"query":"nginx runbook","limit":5}`))
	if !search.Success {
		t.Fatalf("memory_search failed: %s", search.Output)
	}
	if !strings.Contains(search.Output, "runbooks/2026-05-23-nginx-502.md") {
		t.Fatalf("memory_search missing runbook: %s", search.Output)
	}

	read := HandleTool(store, "conv1", "memory_read", json.RawMessage(`{"path":"runbooks/2026-05-23-nginx-502.md"}`))
	if !read.Success {
		t.Fatalf("memory_read failed: %s", read.Output)
	}
	if !strings.Contains(read.Output, "Nginx 502") {
		t.Fatalf("memory_read missing runbook content: %s", read.Output)
	}
}

func TestMemorySearchSkipsSymlinkedMarkdownOutsideRoot(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# Leaked\n\noutside secret needle"), 0600); err != nil {
		t.Fatal(err)
	}
	memoryRoot := filepath.Join(store.Dir(), "memory")
	if err := os.MkdirAll(memoryRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(memoryRoot, "leak.md")); err != nil {
		t.Fatal(err)
	}

	result := HandleTool(store, "conv1", "memory_search", json.RawMessage(`{"query":"needle","limit":5}`))
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	if strings.Contains(result.Output, "outside secret") || strings.Contains(result.Output, "leak.md") {
		t.Fatalf("search returned symlinked outside content/path: %s", result.Output)
	}
}

func TestMemorySearchEnforcesCombinedLimit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, title := range []string{"SQLite one", "SQLite two", "SQLite three"} {
		args, _ := json.Marshal(map[string]string{
			"category": "experience",
			"title":    title,
			"content":  "combined-limit-query sqlite",
		})
		result := HandleTool(store, "conv1", "memory_save", args)
		if !result.Success {
			t.Fatalf("save failed: %s", result.Output)
		}
	}
	for _, title := range []string{"Markdown one", "Markdown two", "Markdown three"} {
		args, _ := json.Marshal(map[string]string{
			"category": "incidents",
			"title":    title,
			"summary":  "combined-limit-query markdown",
			"content":  "combined-limit-query markdown",
			"tags":     "limit",
		})
		result := HandleTool(store, "conv1", "memory_write_note", args)
		if !result.Success {
			t.Fatalf("write note failed: %s", result.Output)
		}
	}

	result := HandleTool(store, "conv1", "memory_search", json.RawMessage(`{"query":"combined-limit-query","limit":2}`))
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) > 2 {
		t.Fatalf("search returned %d lines, want at most 2:\n%s", len(lines), result.Output)
	}
}

func TestMemorySearchRejectsEmptyQuery(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result := HandleTool(store, "conv1", "memory_search", json.RawMessage(`{"query":"   ","limit":5}`))
	if result.Success {
		t.Fatalf("empty memory_search query should fail: %s", result.Output)
	}
}

func TestMemorySearchCapsExcessiveLimit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for i := 0; i < maxMemorySearchLimit+5; i++ {
		args, _ := json.Marshal(map[string]string{
			"category": "event",
			"title":    fmt.Sprintf("Limit cap %02d", i),
			"content":  "bounded-search-query",
		})
		result := HandleTool(store, "conv1", "memory_save", args)
		if !result.Success {
			t.Fatalf("save failed: %s", result.Output)
		}
	}

	result := HandleTool(store, "conv1", "memory_search", json.RawMessage(`{"query":"bounded-search-query","limit":999}`))
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != maxMemorySearchLimit {
		t.Fatalf("search returned %d lines, want cap %d:\n%s", len(lines), maxMemorySearchLimit, result.Output)
	}
}

func TestHandleToolFailsWhenStoreUnavailable(t *testing.T) {
	result := HandleTool(nil, "", "memory_search", json.RawMessage(`{"query":"nginx"}`))
	if result.Success || !strings.Contains(result.Output, "not available") {
		t.Fatalf("nil memory store result = %#v, want unavailable failure", result)
	}
}

func TestHandleMemoryPromoteAcceptsPluralIncidentCategory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	saveArgs, _ := json.Marshal(map[string]string{
		"category": "experience",
		"title":    "API OOM",
		"content":  "Root cause was cache pressure.",
		"tags":     "api,oom",
	})
	result := HandleTool(store, "conv1", "memory_save", saveArgs)
	if !result.Success {
		t.Fatalf("save failed: %s", result.Output)
	}
	idStart := strings.Index(result.Output, "Saved memory ") + len("Saved memory ")
	idEnd := strings.Index(result.Output[idStart:], ":")
	id := result.Output[idStart : idStart+idEnd]

	promoteArgs, _ := json.Marshal(map[string]string{"id": id, "category": "incidents"})
	result = HandleTool(store, "conv1", "memory_promote", promoteArgs)
	if !result.Success {
		t.Fatalf("promote failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "incidents/") {
		t.Fatalf("promote output missing incidents path: %s", result.Output)
	}
	matches, err := filepath.Glob(filepath.Join(store.Dir(), "memory", "incidents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("incident note count = %d, want 1", len(matches))
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Root cause was cache pressure.") {
		t.Fatalf("incident note missing promoted content:\n%s", string(content))
	}
}

func TestHandleMemoryPromoteRejectsSecretLikeMemory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entry := MemoryEntry{
		ID:       "secret-memory",
		Category: "experience",
		Title:    "Credential",
		Content:  "password=abc123",
		Tags:     `["secret"]`,
	}
	if err := store.SaveMemory(entry); err != nil {
		t.Fatal(err)
	}

	result := HandleTool(store, "conv1", "memory_promote", json.RawMessage(`{"id":"secret-memory","category":"incidents"}`))
	if result.Success {
		t.Fatalf("expected promote to reject secret-like memory: %s", result.Output)
	}
	matches, err := filepath.Glob(filepath.Join(store.Dir(), "memory", "incidents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("secret-like memory should not be promoted, found notes: %v", matches)
	}
}

func TestHandleMemorySave(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	args, _ := json.Marshal(map[string]string{
		"category": "experience",
		"title":    "Nginx leak",
		"content":  "Found cache settings causing OOM",
		"tags":     "nginx, memory, production",
	})
	result := HandleTool(store, "conv1", "memory_save", args)
	if !result.Success {
		t.Fatalf("save failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Saved memory") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestHandleMemorySearch(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	saveArgs, _ := json.Marshal(map[string]string{
		"category": "experience",
		"title":    "Nginx leak",
		"content":  "Found cache settings causing OOM",
		"tags":     "nginx",
	})
	HandleTool(store, "conv1", "memory_save", saveArgs)

	searchArgs, _ := json.Marshal(map[string]interface{}{
		"query": "nginx",
		"limit": 5,
	})
	result := HandleTool(store, "conv1", "memory_search", searchArgs)
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Nginx leak") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestHandleMemoryUpdate(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	saveArgs, _ := json.Marshal(map[string]string{
		"category": "experience",
		"title":    "Original",
		"content":  "Original content",
	})
	result := HandleTool(store, "conv1", "memory_save", saveArgs)
	idStart := strings.Index(result.Output, "Saved memory ") + len("Saved memory ")
	idEnd := strings.Index(result.Output[idStart:], ":")
	id := result.Output[idStart : idStart+idEnd]

	updateArgs, _ := json.Marshal(map[string]string{
		"id":      id,
		"title":   "Updated",
		"content": "Updated content",
	})
	result = HandleTool(store, "conv1", "memory_update", updateArgs)
	if !result.Success {
		t.Fatalf("update failed: %s", result.Output)
	}

	got, _ := store.GetMemory(id)
	if got.Title != "Updated" {
		t.Fatalf("title = %q, want Updated", got.Title)
	}
}

func TestHandleMemoryDelete(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	saveArgs, _ := json.Marshal(map[string]string{
		"category": "event", "title": "Temp", "content": "Temporary",
	})
	result := HandleTool(store, "conv1", "memory_save", saveArgs)
	idStart := strings.Index(result.Output, "Saved memory ") + len("Saved memory ")
	idEnd := strings.Index(result.Output[idStart:], ":")
	id := result.Output[idStart : idStart+idEnd]

	delArgs, _ := json.Marshal(map[string]string{"id": id})
	result = HandleTool(store, "conv1", "memory_delete", delArgs)
	if !result.Success {
		t.Fatalf("delete failed: %s", result.Output)
	}
}

func TestHandleUnknownTool(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	result := HandleTool(store, "", "memory_unknown", json.RawMessage(`{}`))
	if result.Success {
		t.Fatal("expected failure for unknown tool")
	}
}

func TestHandleInvalidArgs(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	result := HandleTool(store, "", "memory_save", json.RawMessage(`invalid`))
	if result.Success {
		t.Fatal("expected failure for invalid JSON")
	}
}

func TestSearchNoResults(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	searchArgs, _ := json.Marshal(map[string]interface{}{"query": "nonexistent"})
	result := HandleTool(store, "conv1", "memory_search", searchArgs)
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "No memories found") {
		t.Fatalf("output = %q", result.Output)
	}
}
