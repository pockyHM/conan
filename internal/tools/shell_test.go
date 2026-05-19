package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestShellRun(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "echo hello", Timeout: 5}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Error("should not be error")
	}
	if len(result.Content) == 0 {
		t.Fatal("no content")
	}
	if result.Content[0].Text == "" {
		t.Error("expected output")
	}
}

func TestShellRunTimeout(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "sleep 10", Timeout: 1}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("should be error (timeout)")
	}
}

func TestShellRunNonZeroExit(t *testing.T) {
	tool := &ShellTool{}
	input := shellInput{Command: "false", Timeout: 5}
	data, _ := json.Marshal(input)

	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Content[0].Text == "" {
		t.Error("expected output with exit code info")
	}
}
