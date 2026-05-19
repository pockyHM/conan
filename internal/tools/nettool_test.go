package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNetPortCheck(t *testing.T) {
	tool := &netPortcheckTool{}
	input, _ := json.Marshal(map[string]interface{}{"host": "127.0.0.1", "port": 22})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
