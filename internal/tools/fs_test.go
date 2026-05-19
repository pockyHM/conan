package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	tool := &fsReadTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": path})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	if result.Content[0].Text != "hello world" {
		t.Errorf("text = %q, want hello world", result.Content[0].Text)
	}
}

func TestFsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	tool := &fsWriteTool{}
	input, _ := json.Marshal(map[string]interface{}{
		"path":    path,
		"content": "written content",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "written content" {
		t.Errorf("file content = %q, want written content", data)
	}
}

func TestFsEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("hello world\nline two"), 0644)

	tool := &fsEditTool{}
	input, _ := json.Marshal(map[string]interface{}{
		"path":     path,
		"old_text": "hello world",
		"new_text": "goodbye world",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content[0].Text)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye world\nline two" {
		t.Errorf("file content = %q", data)
	}
}

func TestFsList(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0644)

	tool := &fsListTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": dir})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
}

func TestFsStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat.txt")
	os.WriteFile(path, []byte("content"), 0644)

	tool := &fsStatTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": path})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
}
