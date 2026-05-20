package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsMemoryTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"memory/save", true},
		{"memory/update", true},
		{"memory/delete", true},
		{"memory/search", true},
		{"shell/run", false},
		{"memory", false},
	}
	for _, tt := range tests {
		if got := IsMemoryTool(tt.name); got != tt.want {
			t.Errorf("IsMemoryTool(%q) = %v, want %v", tt.name, got, tt.want)
		}
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
	result := HandleTool(store, "conv1", "memory/save", args)
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
	HandleTool(store, "conv1", "memory/save", saveArgs)

	searchArgs, _ := json.Marshal(map[string]interface{}{
		"query": "nginx",
		"limit": 5,
	})
	result := HandleTool(store, "conv1", "memory/search", searchArgs)
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
	result := HandleTool(store, "conv1", "memory/save", saveArgs)
	idStart := strings.Index(result.Output, "Saved memory ") + len("Saved memory ")
	idEnd := strings.Index(result.Output[idStart:], ":")
	id := result.Output[idStart : idStart+idEnd]

	updateArgs, _ := json.Marshal(map[string]string{
		"id":      id,
		"title":   "Updated",
		"content": "Updated content",
	})
	result = HandleTool(store, "conv1", "memory/update", updateArgs)
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
	result := HandleTool(store, "conv1", "memory/save", saveArgs)
	idStart := strings.Index(result.Output, "Saved memory ") + len("Saved memory ")
	idEnd := strings.Index(result.Output[idStart:], ":")
	id := result.Output[idStart : idStart+idEnd]

	delArgs, _ := json.Marshal(map[string]string{"id": id})
	result = HandleTool(store, "conv1", "memory/delete", delArgs)
	if !result.Success {
		t.Fatalf("delete failed: %s", result.Output)
	}
}

func TestHandleUnknownTool(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	result := HandleTool(store, "", "memory/unknown", json.RawMessage(`{}`))
	if result.Success {
		t.Fatal("expected failure for unknown tool")
	}
}

func TestHandleInvalidArgs(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	result := HandleTool(store, "", "memory/save", json.RawMessage(`invalid`))
	if result.Success {
		t.Fatal("expected failure for invalid JSON")
	}
}

func TestSearchNoResults(t *testing.T) {
	store, _ := Open(t.TempDir())
	defer store.Close()

	searchArgs, _ := json.Marshal(map[string]interface{}{"query": "nonexistent"})
	result := HandleTool(store, "conv1", "memory/search", searchArgs)
	if !result.Success {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "No memories found") {
		t.Fatalf("output = %q", result.Output)
	}
}
