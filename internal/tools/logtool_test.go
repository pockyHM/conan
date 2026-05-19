package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLogRead(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(logPath, []byte(content), 0644)

	tool := &logReadTool{}
	input, _ := json.Marshal(map[string]interface{}{"path": logPath, "tail": 2})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}
