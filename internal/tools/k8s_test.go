package tools

import (
	"encoding/json"
	"os"
	"testing"
)

func TestK8sPods(t *testing.T) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		t.Skip("not running inside a Kubernetes cluster")
	}
	tool := &k8sPodsTool{}
	input, _ := json.Marshal(map[string]any{"namespace": "default"})
	result, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	_ = result
}
