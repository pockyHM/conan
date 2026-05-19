package configschema

import (
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-12345")
	result := ExpandEnv("${TEST_API_KEY}")
	if result != "sk-12345" {
		t.Errorf("got %q, want sk-12345", result)
	}
}

func TestExpandEnvEmpty(t *testing.T) {
	result := ExpandEnv("plain-text-no-env")
	if result != "plain-text-no-env" {
		t.Errorf("got %q, want plain-text-no-env", result)
	}
}

func TestAgentConfigDefaults(t *testing.T) {
	cfg := DefaultAgentConfig()
	if cfg.Listen != "0.0.0.0:9200" {
		t.Errorf("listen = %q, want 0.0.0.0:9200", cfg.Listen)
	}
	if cfg.RateLimit != 10 {
		t.Errorf("rate_limit = %d, want 10", cfg.RateLimit)
	}
}
