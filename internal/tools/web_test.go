package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWebToolsExposesFetchWithoutSearchConfig(t *testing.T) {
	tools := NewWebTools(WebToolConfig{})

	if len(tools) != 1 || tools[0].Name() != "web/fetch" {
		t.Fatalf("tools = %#v, want only web/fetch", toolNames(tools))
	}
}

func TestNewWebToolsExposesSearchWhenConfigured(t *testing.T) {
	tools := NewWebTools(WebToolConfig{SearchProvider: "brave", SearchAPIKey: "test-key"})

	if got := toolNames(tools); strings.Join(got, ",") != "web/fetch,web/search" {
		t.Fatalf("tools = %#v, want web/fetch and web/search", got)
	}
}

func TestWebSearchBraveReturnsStructuredResults(t *testing.T) {
	var sawKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("X-Subscription-Token")
		if got := r.URL.Query().Get("q"); got != "conan agent" {
			t.Fatalf("query = %q, want conan agent", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Conan Agent","url":"https://example.com/conan","description":"Agent docs","age":"May 22, 2026"}]}}`))
	}))
	defer srv.Close()

	tool := &webSearchTool{cfg: WebToolConfig{SearchProvider: "brave", SearchAPIKey: "test-key", SearchEndpoint: srv.URL}}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"conan agent","max_results":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is error: %#v", result)
	}
	if sawKey != "test-key" {
		t.Fatalf("subscription token = %q, want test-key", sawKey)
	}
	output := result.Content[0].Text
	if !strings.Contains(output, `"title":"Conan Agent"`) || !strings.Contains(output, `"url":"https://example.com/conan"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestWebFetchExtractsHTMLText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Example Page</title><script>ignore()</script></head><body><main><h1>Hello</h1><p>Useful content.</p></main></body></html>`))
	}))
	defer srv.Close()

	tool := &webFetchTool{cfg: WebToolConfig{AllowPrivateNetwork: true, FetchMaxChars: 2000, FetchMaxBytes: 1 << 20}}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"`+srv.URL+`","max_chars":500}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is error: %#v", result)
	}
	output := result.Content[0].Text
	if !strings.Contains(output, `"title":"Example Page"`) || !strings.Contains(output, "Useful content.") {
		t.Fatalf("output = %s", output)
	}
	if strings.Contains(output, "ignore()") {
		t.Fatalf("output should not include script text: %s", output)
	}
}

func TestWebFetchRejectsPrivateNetworkByDefault(t *testing.T) {
	tool := &webFetchTool{cfg: WebToolConfig{}}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"http://127.0.0.1:8080"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "private network") {
		t.Fatalf("result = %#v, want private network error", result)
	}
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}
