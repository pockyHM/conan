package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestFsReadCapsOutputLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	os.WriteFile(path, []byte(strings.Repeat("x", 80)), 0644)

	tool := &fsReadTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": path, "max_bytes": 10})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "[truncated: output exceeds 10 bytes]") {
		t.Fatalf("output was not truncated: %q", result.Content[0].Text)
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

func TestFsEditLineRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit-lines.txt")
	os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0644)

	tool := &fsEditTool{}
	input, _ := json.Marshal(map[string]interface{}{
		"path":       path,
		"start_line": 2,
		"end_line":   3,
		"content":    "TWO\nTHREE",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content[0].Text)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "one\nTWO\nTHREE\nfour\n" {
		t.Fatalf("file content = %q", data)
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

func TestFsToolsRejectBinaryAndImageFiles(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "image.jpg")
	if err := os.WriteFile(imagePath, []byte("not really image"), 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	binaryPath := filepath.Join(dir, "binary.txt")
	if err := os.WriteFile(binaryPath, []byte{'a', 0, 'b'}, 0644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	readInput, _ := json.Marshal(map[string]interface{}{"path": imagePath})
	readResult, err := (&fsReadTool{}).Execute(context.Background(), readInput)
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if !readResult.IsError || !strings.Contains(readResult.Content[0].Text, "binary/image") {
		t.Fatalf("read image result = %+v", readResult)
	}

	writeInput, _ := json.Marshal(map[string]interface{}{"path": filepath.Join(dir, "out.png"), "content": "text"})
	writeResult, err := (&fsWriteTool{}).Execute(context.Background(), writeInput)
	if err != nil {
		t.Fatalf("write execute: %v", err)
	}
	if !writeResult.IsError || !strings.Contains(writeResult.Content[0].Text, "binary/image") {
		t.Fatalf("write image result = %+v", writeResult)
	}

	editInput, _ := json.Marshal(map[string]interface{}{"path": binaryPath, "old_text": "a", "new_text": "c"})
	editResult, err := (&fsEditTool{}).Execute(context.Background(), editInput)
	if err != nil {
		t.Fatalf("edit execute: %v", err)
	}
	if !editResult.IsError || !strings.Contains(editResult.Content[0].Text, "binary content") {
		t.Fatalf("edit binary result = %+v", editResult)
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

func TestNewFsToolsDoesNotIncludeTransferTools(t *testing.T) {
	for _, tool := range NewFsTools() {
		switch tool.Name() {
		case "fs_download", "fs_upload":
			t.Fatalf("transfer tool %q should not be exposed as an MCP fs tool", tool.Name())
		}
	}
}
