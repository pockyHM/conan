package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSysCPU(t *testing.T) {
	tool := &sysCPUTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "load_avg") {
		t.Errorf("expected load_avg in output, got: %s", result.Content[0].Text)
	}
}

func TestSysMem(t *testing.T) {
	tool := &sysMemTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}

func TestSysDisk(t *testing.T) {
	tool := &sysDiskTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("should not be error: %s", result.Content[0].Text)
	}
}
