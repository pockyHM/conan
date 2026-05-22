package llm

import (
	"fmt"

	"github.com/pockyHM/conan/pkg/configschema"
)

func NewProvider(models []configschema.ModelConfig, name string) (Provider, string, error) {
	var cfg *configschema.ModelConfig
	for i := range models {
		if models[i].Name == name {
			cfg = &models[i]
			break
		}
	}
	if cfg == nil && len(models) > 0 {
		cfg = &models[0]
	}
	if cfg == nil {
		return nil, "", fmt.Errorf("no model configured")
	}
	switch cfg.Type {
	case "anthropic":
		return NewAnthropicProvider(AnthropicConfig{
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
			BaseURL: cfg.Endpoint,
		}), cfg.Name, nil
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:   cfg.APIKey,
			Model:    cfg.Model,
			BaseURL:  cfg.Endpoint,
			Thinking: cfg.Thinking,
		}), cfg.Name, nil
	default:
		return nil, "", fmt.Errorf("unknown model type: %s", cfg.Type)
	}
}
