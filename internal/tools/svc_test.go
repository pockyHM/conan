package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSvcList(t *testing.T) {
	tool := &svcListTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}

func TestSvcStatus(t *testing.T) {
	tool := &svcStatusTool{}
	input, _ := json.Marshal(map[string]interface{}{"name": "nonexistent-service"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent service")
	}
}
