package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPkgList(t *testing.T) {
	tool := &pkgListTool{}
	input, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
