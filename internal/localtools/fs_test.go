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

	result := Handle(RootedFS{Root: root}, "local_fs_read", json.RawMessage(`{"path":"notes.txt"}`))
	if !result.Success {
		t.Fatalf("read failed: %s", result.Output)
	}
	if result.Output != "hello local files" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestLocalReadCapsOutputLength(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("x", 80)), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result := Handle(RootedFS{Root: root}, "local_fs_read", json.RawMessage(`{"path":"large.txt","max_bytes":10}`))
	if !result.Success {
		t.Fatalf("read failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[truncated: output exceeds 10 bytes]") {
		t.Fatalf("output was not truncated: %q", result.Output)
	}
}

func TestLocalWritePatchAndDeleteRequirePathInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")

	write := Handle(RootedFS{Root: root}, "local_fs_write", mustJSON(t, map[string]any{
		"path":    "dir/file.txt",
		"content": "hello world",
	}))
	if !write.Success {
		t.Fatalf("write failed: %s", write.Output)
	}

	patch := Handle(RootedFS{Root: root}, "local_fs_patch", mustJSON(t, map[string]any{
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

	blocked := Handle(RootedFS{Root: root}, "local_fs_write", mustJSON(t, map[string]any{
		"path":    outside,
		"content": "blocked",
	}))
	if blocked.Success || !strings.Contains(blocked.Output, "outside workspace") {
		t.Fatalf("outside write result = %+v", blocked)
	}

	del := Handle(RootedFS{Root: root}, "local_fs_delete", json.RawMessage(`{"path":"dir/file.txt"}`))
	if !del.Success {
		t.Fatalf("delete failed: %s", del.Output)
	}
	if _, err := os.Stat(filepath.Join(root, "dir", "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, stat err=%v", err)
	}
}

func TestLocalPatchLineRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dir", "file.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	patch := Handle(RootedFS{Root: root}, "local_fs_patch", mustJSON(t, map[string]any{
		"path":       "dir/file.txt",
		"start_line": 2,
		"end_line":   3,
		"content":    "TWO\nTHREE",
	}))
	if !patch.Success {
		t.Fatalf("patch failed: %s", patch.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "one\nTWO\nTHREE\nfour\n" {
		t.Fatalf("patched content = %q", data)
	}
}

func TestLocalListAndStat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	list := Handle(RootedFS{Root: root}, "local_fs_list", json.RawMessage(`{"path":"."}`))
	if !list.Success || !strings.Contains(list.Output, "a.txt") {
		t.Fatalf("list = %+v", list)
	}

	stat := Handle(RootedFS{Root: root}, "local_fs_stat", json.RawMessage(`{"path":"a.txt"}`))
	if !stat.Success || !strings.Contains(stat.Output, "size: 1") {
		t.Fatalf("stat = %+v", stat)
	}
}

func TestLocalFilesystemRejectsBinaryAndImageFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "image.png"), []byte("text with image extension"), 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.txt"), []byte{'a', 0, 'b'}, 0644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	readImage := Handle(RootedFS{Root: root}, "local_fs_read", json.RawMessage(`{"path":"image.png"}`))
	if readImage.Success || !strings.Contains(readImage.Output, "binary/image") {
		t.Fatalf("read image result = %+v", readImage)
	}

	readBinary := Handle(RootedFS{Root: root}, "local_fs_read", json.RawMessage(`{"path":"binary.txt"}`))
	if readBinary.Success || !strings.Contains(readBinary.Output, "binary content") {
		t.Fatalf("read binary result = %+v", readBinary)
	}

	writeImage := Handle(RootedFS{Root: root}, "local_fs_write", mustJSON(t, map[string]any{
		"path":    "new.png",
		"content": "text",
	}))
	if writeImage.Success || !strings.Contains(writeImage.Output, "binary/image") {
		t.Fatalf("write image result = %+v", writeImage)
	}

	writeBinary := Handle(RootedFS{Root: root}, "local_fs_write", mustJSON(t, map[string]any{
		"path":    "new.txt",
		"content": "a\x00b",
	}))
	if writeBinary.Success || !strings.Contains(writeBinary.Output, "binary content") {
		t.Fatalf("write binary result = %+v", writeBinary)
	}

	patchBinary := Handle(RootedFS{Root: root}, "local_fs_patch", mustJSON(t, map[string]any{
		"path":     "binary.txt",
		"old_text": "a",
		"new_text": "c",
	}))
	if patchBinary.Success || !strings.Contains(patchBinary.Output, "binary content") {
		t.Fatalf("patch binary result = %+v", patchBinary)
	}

	deleteImage := Handle(RootedFS{Root: root}, "local_fs_delete", json.RawMessage(`{"path":"image.png"}`))
	if deleteImage.Success || !strings.Contains(deleteImage.Output, "binary/image") {
		t.Fatalf("delete image result = %+v", deleteImage)
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
