package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type dummyTool struct{}

func (d *dummyTool) Name() string        { return "test/dummy" }
func (d *dummyTool) Description() string { return "A dummy tool" }
func (d *dummyTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
}
func (d *dummyTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	return nil, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &dummyTool{}
	r.Register(tool)

	got, ok := r.Get("test/dummy")
	if !ok {
		t.Fatal("tool not found in registry")
	}
	if got.Name() != "test/dummy" {
		t.Errorf("name = %q, want test/dummy", got.Name())
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{})

	if len(r.List()) != 1 {
		t.Errorf("list length = %d, want 1", len(r.List()))
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent tool")
	}
}

func TestRegistryDisabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{})
	r.Disable("test/dummy")
	_, ok := r.Get("test/dummy")
	if ok {
		t.Error("disabled tool should not be found")
	}
	list := r.List()
	if len(list) != 0 {
		t.Errorf("list length = %d, want 0 after disable", len(list))
	}
}
