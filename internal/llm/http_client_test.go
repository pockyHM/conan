package llm

import (
	"net/http"
	"testing"
)

func TestDefaultLLMHTTPClientHasHeaderTimeouts(t *testing.T) {
	client := defaultHTTPClient()
	if client == nil {
		t.Fatal("client is nil")
	}
	if client.Timeout != 0 {
		t.Fatalf("client.Timeout = %v, want 0 so streaming responses are not cut off", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatal("ResponseHeaderTimeout should protect model request setup")
	}
	if transport.TLSHandshakeTimeout <= 0 {
		t.Fatal("TLSHandshakeTimeout should protect model request setup")
	}
}

func TestProvidersUseDefaultTimeoutClient(t *testing.T) {
	openai := NewOpenAIProvider(OpenAIConfig{Model: "gpt-4.1"})
	if openai.client == nil || openai.client == http.DefaultClient {
		t.Fatalf("openai client = %#v, want dedicated timeout client", openai.client)
	}
	anthropic := NewAnthropicProvider(AnthropicConfig{Model: "claude-sonnet-4-6"})
	if anthropic.client == nil || anthropic.client == http.DefaultClient {
		t.Fatalf("anthropic client = %#v, want dedicated timeout client", anthropic.client)
	}
}
