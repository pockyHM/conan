package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestK8sPods(t *testing.T) {
	tool := &k8sPodsTool{}
	input, _ := json.Marshal(map[string]interface{}{"namespace": "default"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
