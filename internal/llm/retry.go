package llm

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{MaxRetries: 3, BaseDelay: 2 * time.Second}
}

type RetryProvider struct {
	inner Provider
	cfg   RetryConfig
}

func NewRetryProvider(inner Provider, cfg RetryConfig) Provider {
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 2 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return &RetryProvider{inner: inner, cfg: cfg}
}

func (p *RetryProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		resp, err := p.inner.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		maxRetries, retryable := maxRetriesForError(err, p.cfg.MaxRetries)
		if attempt == maxRetries || !retryable {
			return nil, err
		}
		if err := p.wait(ctx, attempt, maxRetries, err); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (p *RetryProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		stream, err := p.inner.ChatStream(ctx, req)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		maxRetries, retryable := maxRetriesForError(err, p.cfg.MaxRetries)
		if attempt == maxRetries || !retryable {
			return nil, err
		}
		if err := p.wait(ctx, attempt, maxRetries, err); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (p *RetryProvider) SupportsVision() bool {
	vision, ok := p.inner.(VisionProvider)
	return ok && vision.SupportsVision()
}

func (p *RetryProvider) DescribeImages(ctx context.Context, req *VisionRequest) (*VisionResponse, error) {
	vision, ok := p.inner.(VisionProvider)
	if !ok {
		return nil, errors.New("provider does not support image input")
	}
	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		resp, err := vision.DescribeImages(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		maxRetries, retryable := maxRetriesForError(err, p.cfg.MaxRetries)
		if attempt == maxRetries || !retryable {
			return nil, err
		}
		if err := p.wait(ctx, attempt, maxRetries, err); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (p *RetryProvider) wait(ctx context.Context, attempt int, maxRetries int, err error) error {
	delay := p.cfg.BaseDelay * time.Duration(1<<attempt)
	slog.Info("retrying llm request", "attempt", attempt+1, "max_retries", maxRetries, "delay", delay, "error", err)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func maxRetriesForError(err error, configuredMax int) (int, bool) {
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		return configuredMax, false
	}
	if httpErr.Status == 429 {
		return configuredMax, true
	}
	if httpErr.Status >= 500 && httpErr.Status < 600 {
		return min(configuredMax, 2), true
	}
	return configuredMax, false
}

func isRetryableError(err error) bool {
	_, retryable := maxRetriesForError(err, 0)
	return retryable
}
