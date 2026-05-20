package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pockyHM/conan/pkg/models"
)

func TestOpenCreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Join(dir, "conan.db")); err != nil {
		t.Fatalf("database file not created: %v", err)
	}
}

func TestSaveAndGetMemory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entry := MemoryEntry{
		ID:       models.NewID(),
		Category: "experience",
		Title:    "Nginx memory leak",
		Content:  "Found cache settings caused OOM on node-03",
		Tags:     marshalTags([]string{"nginx", "memory", "production"}),
	}
	if err := store.SaveMemory(entry); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetMemory(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != entry.Title {
		t.Fatalf("title = %q, want %q", got.Title, entry.Title)
	}
	if got.CreatedAt == "" {
		t.Fatal("created_at should be set")
	}
}

func TestUpdateMemory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entry := MemoryEntry{ID: models.NewID(), Category: "event", Title: "Old title", Content: "Old content"}
	store.SaveMemory(entry)

	entry.Title = "New title"
	if err := store.UpdateMemory(entry); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetMemory(entry.ID)
	if got.Title != "New title" {
		t.Fatalf("title = %q, want %q", got.Title, "New title")
	}
}

func TestDeleteMemory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entry := MemoryEntry{ID: models.NewID(), Category: "event", Title: "Temp"}
	store.SaveMemory(entry)

	if err := store.DeleteMemory(entry.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetMemory(entry.ID); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteMemoryNotFound(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.DeleteMemory("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestSearchMemories(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SaveMemory(MemoryEntry{ID: "s1", Category: "experience", Title: "Nginx memory leak", Content: "Cache OOM on node-03"})
	store.SaveMemory(MemoryEntry{ID: "s2", Category: "topology", Title: "Network layout", Content: "10.0.1.0/24 subnet"})
	store.SaveMemory(MemoryEntry{ID: "s3", Category: "experience", Title: "K8s pod crash", Content: "OOMKilled in production"})

	results, err := store.SearchMemories("nginx", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ID != "s1" {
		t.Fatalf("got id %q, want s1", results[0].ID)
	}
}

func TestSearchMemoriesByTag(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SaveMemory(MemoryEntry{ID: "t1", Category: "troubleshooting", Title: "DNS issue", Content: "DNS timeout", Tags: `["dns","network"]`})
	store.SaveMemory(MemoryEntry{ID: "t2", Category: "event", Title: "Deploy v2", Content: "Deployed v2.3.1", Tags: `["deploy"]`})

	results, err := store.SearchMemories("dns", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "t1" {
		t.Fatalf("expected t1, got %v", results)
	}
}

func TestListMemoriesByCategory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SaveMemory(MemoryEntry{ID: "c1", Category: "experience", Title: "A"})
	store.SaveMemory(MemoryEntry{ID: "c2", Category: "topology", Title: "B"})
	store.SaveMemory(MemoryEntry{ID: "c3", Category: "experience", Title: "C"})

	results, err := store.ListMemories("experience", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestSaveAndLoadConversation(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rec := ConversationRecord{
		ID:       models.NewID(),
		Cluster:  "production",
		Nodes:    `["node-01","node-02"]`,
		Model:    "claude-sonnet",
		Summary:  "Investigated memory leak on node-03",
		Messages: `[{"role":"user","content":"check node-03"}]`,
	}
	if err := store.SaveConversation(rec); err != nil {
		t.Fatal(err)
	}

	got, err := store.LoadConversation(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != rec.Summary {
		t.Fatalf("summary = %q, want %q", got.Summary, rec.Summary)
	}
	if got.Cluster != "production" {
		t.Fatalf("cluster = %q, want production", got.Cluster)
	}
}

func TestListConversations(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SaveConversation(ConversationRecord{ID: "conv1", Cluster: "production", Summary: "First session"})
	store.SaveConversation(ConversationRecord{ID: "conv2", Cluster: "staging", Summary: "Second session"})

	results, err := store.ListConversations(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d conversations, want 2", len(results))
	}
}
