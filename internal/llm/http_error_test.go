package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pockyHM/conan/pkg/models"
)

func TestAnthropicChatReturnsHTTPErrorWithTrimmedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "  rate limited  ", http.StatusTooManyRequests)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(AnthropicConfig{APIKey: "test-key", Model: "claude-sonnet-4-6", BaseURL: server.URL})
	_, err := provider.Chat(context.Background(), &ChatRequest{Messages: []models.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *httpError", err)
	}
	if httpErr.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", httpErr.Status, http.StatusTooManyRequests)
	}
	if httpErr.Body != "rate limited" {
		t.Fatalf("body = %q, want trimmed body", httpErr.Body)
	}
	if err.Error() != "http 429: rate limited" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestAnthropicChatStreamReturnsHTTPErrorWithTrimmedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "  overloaded  ", http.StatusBadGateway)
	}))
	defer server.Close()

	provider := NewAnthropicProvider(AnthropicConfig{APIKey: "test-key", Model: "claude-sonnet-4-6", BaseURL: server.URL})
	_, err := provider.ChatStream(context.Background(), &ChatRequest{Messages: []models.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("ChatStream error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *httpError", err)
	}
	if httpErr.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", httpErr.Status, http.StatusBadGateway)
	}
	if httpErr.Body != "overloaded" {
		t.Fatalf("body = %q, want trimmed body", httpErr.Body)
	}
}

func TestOpenAIChatReturnsHTTPErrorWithTrimmedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "  bad gateway  ", http.StatusBadGateway)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIConfig{APIKey: "test-key", Model: "gpt-4.1", BaseURL: server.URL})
	_, err := provider.Chat(context.Background(), &ChatRequest{Messages: []models.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *httpError", err)
	}
	if httpErr.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", httpErr.Status, http.StatusBadGateway)
	}
	if httpErr.Body != "bad gateway" {
		t.Fatalf("body = %q, want trimmed body", httpErr.Body)
	}
}

func TestOpenAIChatStreamReturnsHTTPErrorWithTrimmedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "  unavailable  ", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIConfig{APIKey: "test-key", Model: "gpt-4.1", BaseURL: server.URL})
	_, err := provider.ChatStream(context.Background(), &ChatRequest{Messages: []models.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("ChatStream error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *httpError", err)
	}
	if httpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", httpErr.Status, http.StatusServiceUnavailable)
	}
	if httpErr.Body != "unavailable" {
		t.Fatalf("body = %q, want trimmed body", httpErr.Body)
	}
}
