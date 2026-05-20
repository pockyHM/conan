package llm

import (
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestNewProviderByModelName(t *testing.T) {
	configs := []configschema.ModelConfig{
		{Name: "claude", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-ant"},
		{Name: "gpt", Type: "openai", Model: "gpt-4.1", APIKey: "sk-oai"},
		{Name: "local", Type: "openai", Model: "qwen3:32b", Endpoint: "http://localhost:11434/v1"},
	}

	p, name, err := NewProvider(configs, "claude")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if name != "claude" {
		t.Fatalf("name = %q", name)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Fatalf("expected AnthropicProvider, got %T", p)
	}

	p, name, err = NewProvider(configs, "gpt")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Fatalf("expected OpenAIProvider, got %T", p)
	}

	p, name, err = NewProvider(configs, "local")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Fatalf("expected OpenAIProvider for local, got %T", p)
	}
}

func TestNewProviderReturnsErrorForUnknownModel(t *testing.T) {
	_, _, err := NewProvider([]configschema.ModelConfig{}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestNewProviderDefaultModel(t *testing.T) {
	configs := []configschema.ModelConfig{
		{Name: "default-model", Type: "anthropic", Model: "claude-sonnet-4-6", APIKey: "key"},
	}
	p, name, err := NewProvider(configs, "")
	if err != nil {
		t.Fatalf("NewProvider with empty name: %v", err)
	}
	if name != "default-model" {
		t.Fatalf("name = %q", name)
	}
	if p == nil {
		t.Fatal("provider should not be nil")
	}
}
