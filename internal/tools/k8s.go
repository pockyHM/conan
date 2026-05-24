package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// k8s/pods
type k8sPodsTool struct{}

func (k *k8sPodsTool) Name() string        { return "k8s/pods" }
func (k *k8sPodsTool) Description() string { return "List Kubernetes pods" }
func (k *k8sPodsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string","description":"Namespace"},"label_selector":{"type":"string","description":"Label selector"}}}`)
}
func (k *k8sPodsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Namespace     string `json:"namespace"`
		LabelSelector string `json:"label_selector"`
	}
	json.Unmarshal(input, &args)
	cmd := "kubectl get pods"
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	if args.LabelSelector != "" {
		cmd += fmt.Sprintf(" -l '%s'", args.LabelSelector)
	}
	return runCommand(ctx, cmd)
}

// k8s/logs
type k8sLogsTool struct{}

func (k *k8sLogsTool) Name() string        { return "k8s/logs" }
func (k *k8sLogsTool) Description() string { return "Get pod logs" }
func (k *k8sLogsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"},"pod":{"type":"string"},"tail":{"type":"integer"},"follow":{"type":"boolean"}},"required":["pod"]}`)
}
func (k *k8sLogsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Tail      int    `json:"tail"`
		Follow    bool   `json:"follow"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl logs %s", args.Pod)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	if args.Tail > 0 {
		cmd += fmt.Sprintf(" --tail=%d", args.Tail)
	}
	return runCommand(ctx, cmd)
}

// k8s/events
type k8sEventsTool struct{}

func (k *k8sEventsTool) Name() string        { return "k8s/events" }
func (k *k8sEventsTool) Description() string { return "List Kubernetes events" }
func (k *k8sEventsTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"}}}`)
}
func (k *k8sEventsTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Namespace string `json:"namespace"`
	}
	json.Unmarshal(input, &args)
	cmd := "kubectl get events --sort-by=.lastTimestamp"
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(ctx, cmd)
}

// k8s/describe
type k8sDescribeTool struct{}

func (k *k8sDescribeTool) Name() string        { return "k8s/describe" }
func (k *k8sDescribeTool) Description() string { return "Describe a Kubernetes resource" }
func (k *k8sDescribeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string","description":"Resource type (e.g. pod)"},"name":{"type":"string","description":"Resource name"},"namespace":{"type":"string"}},"required":["resource","name"]}`)
}
func (k *k8sDescribeTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Resource  string `json:"resource"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl describe %s %s", args.Resource, args.Name)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(ctx, cmd)
}

// k8s/apply
type k8sApplyTool struct{}

func (k *k8sApplyTool) Name() string        { return "k8s/apply" }
func (k *k8sApplyTool) Description() string { return "Apply Kubernetes manifest" }
func (k *k8sApplyTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"manifest":{"type":"string","description":"YAML manifest content"},"namespace":{"type":"string"}},"required":["manifest"]}`)
}
func (k *k8sApplyTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Manifest  string `json:"manifest"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("echo '%s' | kubectl apply -f -", args.Manifest)
	if args.Namespace != "" {
		cmd = fmt.Sprintf("echo '%s' | kubectl apply -f - -n %s", args.Manifest, args.Namespace)
	}
	return runCommand(ctx, cmd)
}

// k8s/delete
type k8sDeleteTool struct{}

func (k *k8sDeleteTool) Name() string        { return "k8s/delete" }
func (k *k8sDeleteTool) Description() string { return "Delete Kubernetes resource" }
func (k *k8sDeleteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string"},"name":{"type":"string"},"namespace":{"type":"string"}},"required":["resource","name"]}`)
}
func (k *k8sDeleteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Resource  string `json:"resource"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("kubectl delete %s %s", args.Resource, args.Name)
	if args.Namespace != "" {
		cmd += " -n " + args.Namespace
	}
	return runCommand(ctx, cmd)
}

func NewK8sTools() []Tool {
	return []Tool{&k8sPodsTool{}, &k8sLogsTool{}, &k8sEventsTool{}, &k8sDescribeTool{}, &k8sApplyTool{}, &k8sDeleteTool{}}
}
