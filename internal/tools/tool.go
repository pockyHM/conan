package tools

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error)
}

type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	disabled map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		disabled: make(map[string]bool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled[name] {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Tool
	for name, t := range r.tools {
		if !r.disabled[name] {
			result = append(result, t)
		}
	}
	return result
}

func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[name] = true
}

func (r *Registry) DisableAll(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		r.disabled[n] = true
	}
}
