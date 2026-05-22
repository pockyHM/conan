package localtools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalReadAllowsFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("hello local files"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result := Handle(RootedFS{Root: root}, "local/fs/read", json.RawMessage(`{"path":"notes.txt"}`))
	if !result.Success {
		t.Fatalf("read failed: %s", result.Output)
	}
	if result.Output != "hello local files" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestLocalWritePatchAndDeleteRequirePathInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")

	write := Handle(RootedFS{Root: root}, "local/fs/write", mustJSON(t, map[string]any{
		"path":    "dir/file.txt",
		"content": "hello world",
	}))
	if !write.Success {
		t.Fatalf("write failed: %s", write.Output)
	}

	patch := Handle(RootedFS{Root: root}, "local/fs/patch", mustJSON(t, map[string]any{
		"path":     "dir/file.txt",
		"old_text": "world",
		"new_text": "conan",
	}))
	if !patch.Success {
		t.Fatalf("patch failed: %s", patch.Output)
	}
	data, err := os.ReadFile(filepath.Join(root, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "hello conan" {
		t.Fatalf("patched content = %q", data)
	}

	blocked := Handle(RootedFS{Root: root}, "local/fs/write", mustJSON(t, map[string]any{
		"path":    outside,
		"content": "blocked",
	}))
	if blocked.Success || !strings.Contains(blocked.Output, "outside workspace") {
		t.Fatalf("outside write result = %+v", blocked)
	}

	del := Handle(RootedFS{Root: root}, "local/fs/delete", json.RawMessage(`{"path":"dir/file.txt"}`))
	if !del.Success {
		t.Fatalf("delete failed: %s", del.Output)
	}
	if _, err := os.Stat(filepath.Join(root, "dir", "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, stat err=%v", err)
	}
}

func TestLocalListAndStat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	list := Handle(RootedFS{Root: root}, "local/fs/list", json.RawMessage(`{"path":"."}`))
	if !list.Success || !strings.Contains(list.Output, "a.txt") {
		t.Fatalf("list = %+v", list)
	}

	stat := Handle(RootedFS{Root: root}, "local/fs/stat", json.RawMessage(`{"path":"a.txt"}`))
	if !stat.Success || !strings.Contains(stat.Output, "size: 1") {
		t.Fatalf("stat = %+v", stat)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}
