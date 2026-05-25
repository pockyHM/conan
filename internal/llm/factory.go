package llm

import (
	"fmt"

	"github.com/pockyHM/conan/pkg/configschema"
)

func NewProvider(models []configschema.ModelConfig, name string) (Provider, string, error) {
	cfg := selectModelConfig(models, name)
	if cfg == nil {
		return nil, "", fmt.Errorf("no model configured")
	}
	return providerFromConfig(*cfg)
}

func NewVisionProvider(models []configschema.ModelConfig, name string) (VisionProvider, string, error) {
	cfg := selectModelConfig(models, name)
	if cfg == nil {
		return nil, "", fmt.Errorf("no model configured")
	}
	provider, modelName, err := providerFromConfig(*cfg)
	if err != nil {
		return nil, "", err
	}
	visionProvider, ok := provider.(VisionProvider)
	if !ok || !visionProvider.SupportsVision() {
		return nil, "", fmt.Errorf("model %q does not support image input", cfg.Name)
	}
	return visionProvider, modelName, nil
}

func selectModelConfig(models []configschema.ModelConfig, name string) *configschema.ModelConfig {
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
	return cfg
}

func providerFromConfig(cfg configschema.ModelConfig) (Provider, string, error) {
	switch cfg.Type {
	case "anthropic":
		return NewAnthropicProvider(AnthropicConfig{
			APIKey:              cfg.APIKey,
			Model:               cfg.Model,
			BaseURL:             cfg.Endpoint,
			UseEndpointDirectly: cfg.UseEndpointDirectly,
		}), cfg.Name, nil
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:              cfg.APIKey,
			Model:               cfg.Model,
			BaseURL:             cfg.Endpoint,
			UseEndpointDirectly: cfg.UseEndpointDirectly,
			Thinking:            cfg.Thinking,
		}), cfg.Name, nil
	default:
		return nil, "", fmt.Errorf("unknown model type: %s", cfg.Type)
	}
}
