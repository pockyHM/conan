package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/pockyHM/conan/pkg/models"
)

type retryTestProvider struct {
	chatErrors   []error
	streamErrors []error
	chatCalls    int
	streamCalls  int
}

type retryCurlTestProvider struct {
	retryTestProvider
}

func (p *retryCurlTestProvider) CurlCommand(req *ChatRequest) string {
	return "curl -d 'full prompt and request body'"
}

func (p *retryTestProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	p.chatCalls++
	if p.chatCalls <= len(p.chatErrors) {
		return nil, p.chatErrors[p.chatCalls-1]
	}
	return &ChatResponse{Message: models.Message{Role: "assistant", Content: "ok"}, StopReason: StopEndTurn}, nil
}

func (p *retryTestProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	p.streamCalls++
	if p.streamCalls <= len(p.streamErrors) {
		return nil, p.streamErrors[p.streamCalls-1]
	}
	ch := make(chan ChatEvent, 2)
	ch <- TextDeltaEvent{Delta: "ok"}
	close(ch)
	return ch, nil
}

func TestRetryProviderRetriesRateLimitErrorsThreeTimes(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 429, Body: "rate limited"},
		&httpError{Status: 429, Body: "rate limited"},
		&httpError{Status: 429, Body: "rate limited"},
		&httpError{Status: 429, Body: "rate limited"},
	}}
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = time.Nanosecond
	provider := NewRetryProvider(inner, cfg)

	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) || httpErr.Status != 429 {
		t.Fatalf("error = %v, want http 429", err)
	}
	if inner.chatCalls != 4 {
		t.Fatalf("chat calls = %d, want 4", inner.chatCalls)
	}
}

func TestRetryProviderRetriesServerErrorsTwoTimes(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 500, Body: "server error"},
		&httpError{Status: 503, Body: "unavailable"},
		&httpError{Status: 502, Body: "bad gateway"},
		&httpError{Status: 504, Body: "gateway timeout"},
	}}
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = time.Nanosecond
	provider := NewRetryProvider(inner, cfg)

	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) || httpErr.Status != 502 {
		t.Fatalf("error = %v, want http 502", err)
	}
	if inner.chatCalls != 3 {
		t.Fatalf("chat calls = %d, want 3", inner.chatCalls)
	}
}

func TestRetryProviderDoesNotRetryStatus600(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 600, Body: "not a server error"},
	}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: time.Nanosecond})

	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) || httpErr.Status != 600 {
		t.Fatalf("error = %v, want http 600", err)
	}
	if inner.chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1", inner.chatCalls)
	}
}

func TestRetryProviderDoesNotRetryBadRequest(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 400, Body: "bad request"},
	}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: time.Nanosecond})

	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) || httpErr.Status != 400 {
		t.Fatalf("error = %v, want http 400", err)
	}
	if inner.chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1", inner.chatCalls)
	}
}

func TestRetryProviderDoesNotRetryUnauthorized(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 401, Body: "unauthorized"},
	}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: time.Nanosecond})

	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatalf("Chat error = nil, want error")
	}
	var httpErr *httpError
	if !errors.As(err, &httpErr) || httpErr.Status != 401 {
		t.Fatalf("error = %v, want http 401", err)
	}
	if inner.chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1", inner.chatCalls)
	}
}

func TestRetryProviderReturnsSuccessAfterTransientFailures(t *testing.T) {
	inner := &retryTestProvider{chatErrors: []error{
		&httpError{Status: 502, Body: "bad gateway"},
	}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: time.Nanosecond})

	resp, err := provider.Chat(context.Background(), &ChatRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if inner.chatCalls != 2 {
		t.Fatalf("chat calls = %d, want 2", inner.chatCalls)
	}
}

func TestRetryProviderRetriesStreams(t *testing.T) {
	inner := &retryTestProvider{streamErrors: []error{
		&httpError{Status: 429, Body: "rate limited"},
	}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: time.Nanosecond})

	stream, err := provider.ChatStream(context.Background(), &ChatRequest{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var text string
	for event := range stream {
		if delta, ok := event.(TextDeltaEvent); ok {
			text += delta.Delta
		}
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
	if inner.streamCalls != 2 {
		t.Fatalf("stream calls = %d, want 2", inner.streamCalls)
	}
}

func TestRetryProviderDoesNotPrintCurlRequestOnRetry(t *testing.T) {
	inner := &retryCurlTestProvider{retryTestProvider: retryTestProvider{chatErrors: []error{
		&httpError{Status: 502, Body: "bad gateway"},
	}}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 1, BaseDelay: time.Nanosecond})

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldStdout })
	_, chatErr := provider.Chat(context.Background(), &ChatRequest{
		SystemPrompt: "full system prompt",
		Messages:     []models.Message{{Role: "user", Content: "complex request"}},
	})
	os.Stdout = oldStdout
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close stdout pipe writer: %v", closeErr)
	}
	defer reader.Close()
	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	if chatErr != nil {
		t.Fatalf("Chat: %v", chatErr)
	}
	if output.Len() != 0 {
		t.Fatalf("retry should not print curl request to stdout, got %q", output.String())
	}
}
