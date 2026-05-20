# Phase 4: Production Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add production-grade error handling, logging, retry logic, and version checking to make Conan reliable for real operational use.

**Architecture:** Four focused additions: (1) structured file logging with slog, (2) audit trail for security decisions and tool calls, (3) retry wrappers for LLM and MCP calls with exponential backoff, (4) agent version mismatch detection at startup.

**Tech Stack:** Go `log/slog`, `internal/logging/` package (new), modifications to existing `internal/llm/`, `internal/mcp/`, `internal/tui/`, `cmd/conan/`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/logging/logging.go` | File handler setup, daily rotation, slog integration |
| `internal/logging/logging_test.go` | Logging tests |
| `internal/security/audit.go` | Audit logger for tool call decisions |
| `internal/security/audit_test.go` | Audit log tests |
| `internal/llm/retry.go` | Retry wrapper for Provider interface |
| `internal/llm/retry_test.go` | Retry wrapper tests |
| `internal/mcp/errors.go` | Error classification for MCP client |
| `internal/mcp/errors_test.go` | Error classification tests |
| `internal/tui/model.go` | Version check on startup, stream error recovery, retry status display |
| `cmd/conan/main.go` | Initialize logging, version check wiring |

---

### Task 1: Logging Foundation

**Files:**
- Create: `internal/logging/logging.go`
- Test: `internal/logging/logging_test.go`
- Modify: `cmd/conan/main.go` (logging init)

**Context:** The `configschema.LoggingConfig` already has `Level`, `File`, `Audit` fields but nothing uses them. The CLI currently has no file logging — all output goes to stdout/stderr. We need structured file logging for debugging and operational visibility.

- [ ] **Step 1: Write the failing test**

```go
// internal/logging/logging_test.go
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "conan.log")
	cfg := Config{Level: "info", File: logFile}
	if err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	dirOf := filepath.Dir(logFile)
	if _, err := os.Stat(dirOf); os.IsNotExist(err) {
		t.Fatal("Setup should create log directory")
	}
}

func TestSetupDefaultLevel(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "conan.log")
	cfg := Config{Level: "", File: logFile}
	if err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSetupNoFile(t *testing.T) {
	cfg := Config{Level: "debug", File: ""}
	if err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLogFileWritten(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "conan.log")
	cfg := Config{Level: "info", File: logFile}
	if err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	Write("test message")
	Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test message") {
		t.Fatalf("log file should contain test message, got:\n%s", string(data))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/logging/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Write implementation**

```go
// internal/logging/logging.go
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

type Config struct {
	Level string
	File  string
}

var fileWriter io.Writer

func Setup(cfg Config) error {
	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler

	if cfg.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		fileWriter = f
		handler = slog.NewJSONHandler(f, opts)
	} else {
		fileWriter = nil
		handler = slog.NewJSONHandler(io.Discard, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return nil
}

func Close() {
	if closer, ok := fileWriter.(interface{ Close() error }); ok {
		closer.Close()
	}
}

func Write(msg string) {
	slog.Info(msg)
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/logging/ -v`
Expected: PASS

- [ ] **Step 5: Wire logging into cmd/conan/main.go**

In `tuiCmd.RunE`, before loading config, add logging initialization:

```go
// At the start of tuiCmd.RunE, after loading global config:
if global.Logging.File != "" {
    logFile := global.Logging.File
    if strings.HasPrefix(logFile, "~/") {
        homeDir, _ := os.UserHomeDir()
        logFile = filepath.Join(homeDir, logFile[2:])
    }
    if err := logging.Setup(logging.Config{
        Level: global.Logging.Level,
        File:  logFile,
    }); err != nil {
        fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not setup logging: %v\n", err)
    }
    defer logging.Close()
}
```

Add `"github.com/pockyHM/conan/internal/logging"` to imports in `cmd/conan/main.go`.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 7: Commit**

```bash
git add internal/logging/ cmd/conan/main.go
git commit -m "feat: add structured file logging with slog"
```

---

### Task 2: Audit Trail

**Files:**
- Create: `internal/security/audit.go`
- Test: `internal/security/audit_test.go`
- Modify: `internal/security/reviewer.go` (audit on decisions)
- Modify: `internal/tui/model.go` (audit on tool dispatch)

**Context:** The design spec requires an audit log that records every tool call decision with format: `2026-05-19T14:30:22Z [ALLOW] shell/run node-01 "free -h"`. Currently there is no audit logging — security decisions happen silently. We need to log all security decisions (ALLOW/CONFIRM/DENY) and actual tool executions with results.

- [ ] **Step 1: Write the failing test**

```go
// internal/security/audit_test.go
package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLogWrites(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:    "shell/run",
		Input:   "free -h",
		Risk:    "ALLOW",
		Nodes:   []string{"node-01"},
	})

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "ALLOW") {
		t.Fatalf("audit log should contain ALLOW, got:\n%s", content)
	}
	if !strings.Contains(content, "shell/run") {
		t.Fatalf("audit log should contain tool name, got:\n%s", content)
	}
	if !strings.Contains(content, "free -h") {
		t.Fatalf("audit log should contain input, got:\n%s", content)
	}
}

func TestAuditLogCreatesDir(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "subdir", "audit.log")
	_, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(logPath)); os.IsNotExist(err) {
		t.Fatal("NewAuditLogger should create parent directory")
	}
}

func TestAuditLogDeny(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:    "shell/run",
		Input:   "rm -rf /",
		Risk:    "DENY",
		Reason:  "Destructive",
		Nodes:   []string{"node-01"},
	})

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "DENY") {
		t.Fatalf("audit log should contain DENY")
	}
}

func TestAuditLogConfirmApproved(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:    "shell/run",
		Input:   "systemctl restart nginx",
		Risk:    "CONFIRM",
		Outcome: "approved",
		Reason:  "Restarts service",
		Nodes:   []string{"node-01", "node-02"},
	})

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "CONFIRM") {
		t.Fatalf("should contain CONFIRM")
	}
	if !strings.Contains(content, "approved") {
		t.Fatalf("should contain outcome")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/security/ -run TestAudit -v`
Expected: FAIL — `NewAuditLogger` undefined

- [ ] **Step 3: Write implementation**

```go
// internal/security/audit.go
package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AuditEntry struct {
	Tool     string   `json:"tool"`
	Input    string   `json:"input"`
	Risk     string   `json:"risk"`     // ALLOW, CONFIRM, DENY
	Outcome  string   `json:"outcome"`  // approved, denied, cancelled (for CONFIRM)
	Reason   string   `json:"reason"`
	Nodes    []string `json:"nodes"`
}

type AuditLogger struct {
	file *os.File
}

func NewAuditLogger(path string) (*AuditLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &AuditLogger{file: f}, nil
}

func (l *AuditLogger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

func (l *AuditLogger) Log(entry AuditEntry) {
	nodes := strings.Join(entry.Nodes, ",")
	entryJSON, _ := json.Marshal(entry)
	line := fmt.Sprintf("%s [%s] %s %s %q",
		time.Now().Format(time.RFC3339),
		entry.Risk,
		entry.Tool,
		nodes,
		truncateAuditInput(entry.Input, 200),
	)
	if entry.Reason != "" {
		line += " (" + entry.Reason + ")"
	}
	if entry.Outcome != "" {
		line += " [" + entry.Outcome + "]"
	}
	// Write both human-readable line and JSON
	fmt.Fprintf(l.file, "%s  %s\n", line, string(entryJSON))
}
```

Add helper at bottom of `audit.go`:

```go
func truncateAuditInput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/security/ -run TestAudit -v`
Expected: PASS

- [ ] **Step 5: Wire audit logger into reviewer and TUI**

Add `AuditLogger *AuditLogger` field to `ModelConfig` in `internal/tui/model.go`.

Add `auditLog *security.AuditLogger` field to `Model` struct.

In `NewModel`, assign `auditLog: cfg.AuditLogger`.

In `riskAssessmentMsg` handler, after each decision (ALLOW/DENY/CONFIRM), call `m.auditLog.Log(...)`:

After ALLOW: `m.auditLog.Log(AuditEntry{Tool: msg.call.Name, Input: string(msg.call.Arguments), Risk: "ALLOW", Nodes: nodeNames})`

After DENY: `m.auditLog.Log(AuditEntry{Tool: msg.call.Name, Input: string(msg.call.Arguments), Risk: "DENY", Reason: msg.assessment.Reason, Nodes: nodeNames})`

After CONFIRM in handleConfirmKey: Log with Outcome="approved" or Outcome="denied".

In `multiToolResultMsg` handler, log the actual tool execution result.

In `cmd/conan/main.go`, create the audit logger from config and pass it to ModelConfig:

```go
var auditLogger *security.AuditLogger
if global.Logging.Audit {
    auditPath := filepath.Join(loader.Home(), "audit.log")
    if global.Logging.File != "" {
        auditPath = filepath.Join(filepath.Dir(global.Logging.File), "audit.log")
    }
    al, err := security.NewAuditLogger(auditPath)
    if err != nil {
        fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not open audit log: %v\n", err)
    } else {
        auditLogger = al
        defer auditLogger.Close()
    }
}
// Add to ModelConfig: AuditLogger: auditLogger,
```

Add node names helper in `internal/tui/model.go`:

```go
func (m Model) selectedNodeNames() []string {
	names := make([]string, 0, len(m.selectedNodes))
	for n := range m.selectedNodes {
		names = append(names, n)
	}
	return names
}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 7: Commit**

```bash
git add internal/security/audit.go internal/security/audit_test.go internal/tui/model.go cmd/conan/main.go
git commit -m "feat: add audit trail for security decisions and tool calls"
```

---

### Task 3: LLM Retry Wrapper

**Files:**
- Create: `internal/llm/retry.go`
- Test: `internal/llm/retry_test.go`
- Modify: `cmd/conan/main.go` (wrap provider with retry)

**Context:** Currently LLM calls fail immediately on 429 (rate limit) or 5xx (server error). The design spec requires exponential backoff: 429 → 3 retries with 2s/4s/8s backoff, 5xx → 2 retries, stream errors → preserve content. This task creates a retry decorator that wraps any Provider.

- [ ] **Step 1: Write the failing test**

```go
// internal/llm/retry_test.go
package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type errorProvider struct {
	calls    int
	err      error
	streamCh <-chan ChatEvent
}

func (e *errorProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	e.calls++
	return nil, e.err
}

func (e *errorProvider) ChatStream(_ context.Context, _ *ChatRequest) (<-chan ChatEvent, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	return e.streamCh, nil
}

func TestRetryOnRateLimit(t *testing.T) {
	ep := &errorProvider{err: &httpError{Status: 429, Body: "rate limited"}}
	provider := NewRetryProvider(ep, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond})
	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if ep.calls != 4 {
		t.Fatalf("expected 4 calls (1 + 3 retries), got %d", ep.calls)
	}
}

func TestRetryOnServerErr(t *testing.T) {
	ep := &errorProvider{err: &httpError{Status: 503, Body: "unavailable"}}
	provider := NewRetryProvider(ep, RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond})
	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.calls != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", ep.calls)
	}
}

func TestNoRetryOnAuthError(t *testing.T) {
	ep := &errorProvider{err: &httpError{Status: 401, Body: "unauthorized"}}
	provider := NewRetryProvider(ep, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond})
	_, err := provider.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.calls != 1 {
		t.Fatalf("should not retry on 401, got %d calls", ep.calls)
	}
}

func TestRetrySuccessAfterFailures(t *testing.T) {
	ep := &errorProvider{}
	ep.err = &httpError{Status: 502, Body: "bad gateway"}
	ep.calls = 0
	provider := NewRetryProvider(ep, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond})

	// Override to succeed on 3rd call
	origCalls := 0
	origChat := ep.Chat
	ep.calls = 0
	_ = origChat
	// This test checks that retries happen; we'll use a provider that flips to success
	ep2 := &flipProvider{failCount: 2, err: &httpError{Status: 502, Body: "bad gateway"}}
	provider2 := NewRetryProvider(ep2, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond})
	resp, err := provider2.Chat(context.Background(), &ChatRequest{})
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if ep2.calls != 3 {
		t.Fatalf("expected 3 calls (2 fail + 1 success), got %d", ep2.calls)
	}
}

func TestRetryStreamOnRateLimit(t *testing.T) {
	ep := &errorProvider{err: &httpError{Status: 429, Body: "rate limited"}}
	provider := NewRetryProvider(ep, RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond})
	_, err := provider.ChatStream(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", ep.calls)
	}
}

type flipProvider struct {
	calls     int
	failCount int
	err       error
}

func (f *flipProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.err
	}
	return &ChatResponse{Message: models.Message{Role: "assistant", Content: "ok"}, StopReason: StopEndTurn}, nil
}

func (f *flipProvider) ChatStream(_ context.Context, _ *ChatRequest) (<-chan ChatEvent, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.err
	}
	ch := make(chan ChatEvent, 1)
	ch <- StopEvent{Reason: StopEndTurn}
	close(ch)
	return ch, nil
}
```

Also add to `llm.go` or a shared file the httpError type used by providers:

```go
// In internal/llm/llm.go, add:

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestRetry -v`
Expected: FAIL — `NewRetryProvider` undefined

- [ ] **Step 3: Write implementation**

```go
// internal/llm/retry.go
package llm

import (
	"context"
	"fmt"
	"math"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  2 * time.Second,
	}
}

type RetryProvider struct {
	inner Provider
	cfg   RetryConfig
}

func NewRetryProvider(inner Provider, cfg RetryConfig) *RetryProvider {
	return &RetryProvider{inner: inner, cfg: cfg}
}

func (r *RetryProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		resp, err := r.inner.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
		if attempt < r.cfg.MaxRetries {
			delay := r.delay(attempt)
			slogInfo("llm retry", "attempt", attempt+1, "delay", delay, "error", err)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("llm call failed after %d retries: %w", r.cfg.MaxRetries, lastErr)
}

func (r *RetryProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		ch, err := r.inner.ChatStream(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
		if attempt < r.cfg.MaxRetries {
			delay := r.delay(attempt)
			slogInfo("llm stream retry", "attempt", attempt+1, "delay", delay, "error", err)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("llm stream failed after %d retries: %w", r.cfg.MaxRetries, lastErr)
}

func (r *RetryProvider) delay(attempt int) time.Duration {
	secs := r.cfg.BaseDelay.Seconds() * math.Pow(2, float64(attempt))
	return time.Duration(secs * float64(time.Second))
}

func isRetryable(err error) bool {
	if he, ok := err.(*httpError); ok {
		return he.Status == 429 || (he.Status >= 500 && he.Status < 600)
	}
	return false
}

func slogInfo(msg string, args ...interface{}) {
	// Use log/slog if available, otherwise no-op
	// This avoids importing slog in test files that can't use it
}
```

Update `slogInfo` to use actual slog:

```go
import "log/slog"

func slogInfo(msg string, args ...any) {
	slog.Info(msg, args...)
}
```

- [ ] **Step 4: Update anthropic.go and openai.go to return httpError**

In `internal/llm/anthropic.go`, update the Chat error path:

```go
// Replace: return nil, fmt.Errorf("anthropic api status %d: %s", httpResp.StatusCode, data)
// With:
return nil, &httpError{Status: httpResp.StatusCode, Body: strings.TrimSpace(string(data))}
```

Same for ChatStream:
```go
// Replace: return nil, fmt.Errorf("anthropic api status %d: %s", httpResp.StatusCode, data)
// With:
return nil, &httpError{Status: httpResp.StatusCode, Body: strings.TrimSpace(string(data))}
```

In `internal/llm/openai.go`, same changes for both Chat and ChatStream.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/ -v`
Expected: PASS (all existing tests + new retry tests)

Note: Existing anthropic/openai tests check error message format. Update them to use `httpError` type checks or match the new format.

- [ ] **Step 6: Wire retry provider in cmd/conan/main.go**

In `tuiCmd.RunE`, wrap the provider:

```go
// After creating provider:
if provider != nil {
    provider = llm.NewRetryProvider(provider, llm.DefaultRetryConfig())
}
```

- [ ] **Step 7: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 8: Commit**

```bash
git add internal/llm/ cmd/conan/main.go
git commit -m "feat: add LLM retry wrapper with exponential backoff"
```

---

### Task 4: MCP Client Error Handling

**Files:**
- Create: `internal/mcp/errors.go`
- Test: `internal/mcp/errors_test.go`
- Modify: `internal/mcp/client.go` (return classified errors)
- Modify: `internal/tui/model.go` (handle node offline, retry transient errors)

**Context:** Currently MCP client returns raw errors with no classification. The design spec requires: connection refused → mark node offline, TLS/auth failure → abort, rate limit → retry with backoff, 5xx → retry once. The TUI also needs to mark nodes offline when connection fails during tool dispatch.

- [ ] **Step 1: Write the failing test**

```go
// internal/mcp/errors_test.go
package mcp

import (
	"errors"
	"net"
	"testing"
)

func TestClassifyConnectionRefused(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	ce := ClassifyError(err)
	if ce.Type != ErrorConnection {
		t.Fatalf("expected ErrorConnection, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("connection refused should not be retryable without user action")
	}
}

func TestClassifyTimeout(t *testing.T) {
	err := errors.New("context deadline exceeded")
	ce := ClassifyError(err)
	if ce.Type != ErrorTimeout {
		t.Fatalf("expected ErrorTimeout, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("timeout should be retryable")
	}
}

func TestClassifyAuth(t *testing.T) {
	err := errors.New("unauthorized")
	ce := ClassifyError(err)
	if ce.Type != ErrorAuth {
		t.Fatalf("expected ErrorAuth, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("auth error should not be retryable")
	}
}

func TestClassifyRateLimit(t *testing.T) {
	err := &rpcError{Code: -32000, HTTPStatus: 429, Message: "rate limit"}
	ce := ClassifyError(err)
	if ce.Type != ErrorRateLimit {
		t.Fatalf("expected ErrorRateLimit, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("rate limit should be retryable")
	}
}

func TestClassifyServerErr(t *testing.T) {
	err := &rpcError{Code: -32603, HTTPStatus: 500, Message: "internal error"}
	ce := ClassifyError(err)
	if ce.Type != ErrorServer {
		t.Fatalf("expected ErrorServer, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("server error should be retryable")
	}
}

func TestClassifyUnknown(t *testing.T) {
	err := errors.New("something weird")
	ce := ClassifyError(err)
	if ce.Type != ErrorUnknown {
		t.Fatalf("expected ErrorUnknown, got %v", ce.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestClassify -v`
Expected: FAIL — types undefined

- [ ] **Step 3: Write implementation**

```go
// internal/mcp/errors.go
package mcp

import (
	"errors"
	"net"
	"strings"
)

type ErrorType int

const (
	ErrorConnection ErrorType = iota
	ErrorTimeout
	ErrorAuth
	ErrorRateLimit
	ErrorServer
	ErrorUnknown
)

type ClassifiedError struct {
	Type      ErrorType
	Retryable bool
	Original  error
}

func (c *ClassifiedError) Error() string {
	return c.Original.Error()
}

type rpcError struct {
	Code       int
	HTTPStatus int
	Message    string
}

func (e *rpcError) Error() string {
	return e.Message
}

func ClassifyError(err error) *ClassifiedError {
	// Check for net.OpError (connection refused, network unreachable)
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return &ClassifiedError{
			Type:      ErrorConnection,
			Retryable: false,
			Original:  err,
		}
	}

	// Check for rpcError (from our own HTTP responses)
	var rpc *rpcError
	if errors.As(err, &rpc) {
		switch {
		case rpc.HTTPStatus == 401 || rpc.HTTPStatus == 403:
			return &ClassifiedError{Type: ErrorAuth, Retryable: false, Original: err}
		case rpc.HTTPStatus == 429:
			return &ClassifiedError{Type: ErrorRateLimit, Retryable: true, Original: err}
		case rpc.HTTPStatus >= 500:
			return &ClassifiedError{Type: ErrorServer, Retryable: true, Original: err}
		}
	}

	// Check for timeout
	if errors.Is(err, context.DeadExceeded) || strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "timeout") {
		return &ClassifiedError{Type: ErrorTimeout, Retryable: true, Original: err}
	}

	// Check for auth-related strings
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") {
		return &ClassifiedError{Type: ErrorAuth, Retryable: false, Original: err}
	}

	return &ClassifiedError{Type: ErrorUnknown, Retryable: false, Original: err}
}
```

Add `context` to imports. Add `var context = context.Background()` — actually, use the standard `context` import properly:

```go
import (
	"context"
	"errors"
	"net"
	"strings"
)
```

Fix the `errors.Is` line:
```go
if errors.Is(err, context.DeadlineExceeded) || ...
```

- [ ] **Step 4: Update client.go to return rpcError for HTTP errors**

In `client.go`, update the `rpc` method to return classified errors:

```go
// In rpc(), after reading response, change the non-200 error:
if resp.StatusCode != http.StatusOK {
	return nil, &rpcError{
		Code:       resp.StatusCode,
		HTTPStatus: resp.StatusCode,
		Message:    fmt.Sprintf("rpc http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
	}
}
```

Same in `Ping`:
```go
if resp.StatusCode != http.StatusOK {
	body, _ := io.ReadAll(resp.Body)
	return &rpcError{
		Code:       resp.StatusCode,
		HTTPStatus: resp.StatusCode,
		Message:    fmt.Sprintf("health check failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
	}
}
```

- [ ] **Step 5: Update TUI model to mark nodes offline on connection errors**

In `dispatchTool` in `model.go`, when a tool call returns a connection error, mark the node offline:

```go
// In the goroutine in dispatchTool, after CallTool returns error:
if err != nil {
    ce := mcp.ClassifyError(err)
    if ce.Type == mcp.ErrorConnection {
        // Mark node offline - send pingResultMsg
        // We can't directly modify m.nodes here (goroutine), so we include
        // a signal in the result
        ch <- result{node: n, output: "Connection failed: " + err.Error(), success: false, connectionLost: true}
    } else {
        ch <- result{node: n, output: err.Error(), success: false}
    }
    return
}
```

Add `connectionLost bool` to the `result` struct in `dispatchTool`. When processing results in `multiToolResultMsg`, also send `pingResultMsg` for connection-lost nodes.

Actually, simpler approach: after building results, emit ping messages:

```go
// In dispatchTool, after collecting results:
var cmds []tea.Cmd
for _, r := range results {
    if !r.success {
        ce := mcp.ClassifyError(errors.New(r.output))
        if ce.Type == mcp.ErrorConnection {
            m.nodes — can't access here, return ping msgs
        }
    }
}
```

Simplest approach: add connection-lost detection in the TUI Update handler for `multiToolResultMsg`:

```go
// In the multiToolResultMsg handler, after processing results:
// Check for connection-lost nodes and send ping results
for _, r := range msg.Results {
    if !r.Success {
        if strings.Contains(r.Output, "connection refused") ||
           strings.Contains(r.Output, "deadline exceeded") ||
           strings.Contains(r.Output, "no such host") {
            // Mark node offline via pingResultMsg
            for i := range m.nodes {
                if m.nodes[i].Name == r.Node {
                    m.nodes[i].Online = false
                    break
                }
            }
        }
    }
}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/ internal/tui/model.go
git commit -m "feat: add MCP error classification and node offline detection"
```

---

### Task 5: Agent Version Checking

**Files:**
- Create: `internal/mcp/version.go`
- Test: `internal/mcp/version_test.go`
- Modify: `cmd/conan/main.go` (version check on TUI startup)
- Modify: `internal/tui/model.go` (version mismatch status warning)

**Context:** The design spec says "CLI checks Agent versions on all connected nodes at startup, warns on version mismatch." The MCP `Initialize` method already returns `ServerInfo.Version` but it's never called during TUI startup. We need to query all agents on startup and display a warning if versions don't match.

- [ ] **Step 1: Write the failing test**

```go
// internal/mcp/version_test.go
package mcp

import (
	"testing"
)

func TestCheckVersionsAllMatch(t *testing.T) {
	results := []VersionResult{
		{Node: "node-01", Version: "1.0.0", Error: nil},
		{Node: "node-02", Version: "1.0.0", Error: nil},
	}
	mismatches := CheckVersionMismatches("1.0.0", results)
	if len(mismatches) > 0 {
		t.Fatalf("expected no mismatches, got %v", mismatches)
	}
}

func TestCheckVersionsMismatch(t *testing.T) {
	results := []VersionResult{
		{Node: "node-01", Version: "1.0.0", Error: nil},
		{Node: "node-02", Version: "0.9.0", Error: nil},
	}
	mismatches := CheckVersionMismatches("1.0.0", results)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch, got %d", len(mismatches))
	}
	if mismatches[0].Node != "node-02" {
		t.Fatalf("expected node-02 mismatch, got %s", mismatches[0].Node)
	}
}

func TestCheckVersionsWithError(t *testing.T) {
	results := []VersionResult{
		{Node: "node-01", Version: "1.0.0", Error: nil},
		{Node: "node-02", Version: "", Error: fmt.Errorf("connection refused")},
	}
	mismatches := CheckVersionMismatches("1.0.0", results)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch (error node), got %d", len(mismatches))
	}
}

func TestCheckVersionsAllDev(t *testing.T) {
	results := []VersionResult{
		{Node: "node-01", Version: "dev", Error: nil},
	}
	mismatches := CheckVersionMismatches("dev", results)
	if len(mismatches) > 0 {
		t.Fatal("dev builds should not warn")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestCheck -v`
Expected: FAIL — types undefined

- [ ] **Step 3: Write implementation**

```go
// internal/mcp/version.go
package mcp

import (
	"context"
	"fmt"
	"sync"
)

type VersionResult struct {
	Node    string
	Version string
	Error   error
}

func CheckVersions(ctx context.Context, clients map[string]*Client) []VersionResult {
	results := make([]VersionResult, 0, len(clients))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, client := range clients {
		c := client
		n := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			initResult, err := c.Initialize(ctx)
			vr := VersionResult{Node: n}
			if err != nil {
				vr.Error = err
			} else {
				vr.Version = initResult.ServerInfo.Version
			}
			mu.Lock()
			results = append(results, vr)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

type Mismatch struct {
	Node      string
	Got       string
	Expected  string
	IsError   bool
}

func CheckVersionMismatches(cliVersion string, results []VersionResult) []Mismatch {
	if cliVersion == "dev" {
		return nil
	}
	var mismatches []Mismatch
	for _, r := range results {
		if r.Error != nil {
			mismatches = append(mismatches, Mismatch{Node: r.Node, IsError: true})
		} else if r.Version != cliVersion && r.Version != "dev" {
			mismatches = append(mismatches, Mismatch{
				Node:     r.Node,
				Got:      r.Version,
				Expected: cliVersion,
			})
		}
	}
	return mismatches
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/ -run TestCheck -v`
Expected: PASS

- [ ] **Step 5: Wire version check into TUI startup**

Add `Version string` to `ModelConfig` in `internal/tui/model.go`.

Add `cliVersion string` to `Model` struct.

In `NewModel`, assign `cliVersion: cfg.Version`.

Add `versionCheckMsg` type:

```go
type versionCheckMsg struct {
	mismatches []mcp.Mismatch
}
```

Add `versionCheckCmd` that calls `mcp.CheckVersions` and classifies results.

In `Init()`, return the version check command:

```go
func (m Model) Init() tea.Cmd {
	if len(m.clients) > 0 && m.cliVersion != "dev" {
		return m.checkVersions()
	}
	return nil
}
```

Handle `versionCheckMsg` in Update — if mismatches, set status to warn:

```go
case versionCheckMsg:
	if len(msg.mismatches) > 0 {
		var nodes []string
		for _, mm := range msg.mismatches {
			if mm.IsError {
				nodes = append(nodes, mm.Node+" (unreachable)")
			} else {
				nodes = append(nodes, fmt.Sprintf("%s (%s)", mm.Node, mm.Got))
			}
		}
		m.status = fmt.Sprintf("⚠ Version mismatch: %s", strings.Join(nodes, ", "))
	}
	return m, nil
```

In `cmd/conan/main.go`, add `Version: version` to `tui.ModelConfig`.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/version.go internal/mcp/version_test.go internal/tui/model.go cmd/conan/main.go
git commit -m "feat: check agent versions at TUI startup, warn on mismatch"
```

---

### Task 6: Stream Error Recovery

**Files:**
- Modify: `internal/tui/model.go` (handle stream interruption gracefully)
- Modify: `internal/tui/model_test.go` (add stream error recovery test)

**Context:** Currently when a stream ErrorEvent arrives, the TUI just shows "Stream error: ..." and stops. The design spec says "Preserve received content, ask user: retry / keep / discard." We also need to handle the case where the stream channel closes unexpectedly (network drop).

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/tui/model_test.go

func TestStreamErrorPreservesContent(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Provider: &fakeProvider{}, Conv: conv})
	model.streaming = true
	model.streamBuf = "Partial content"

	next, _ := model.Update(streamEventMsg{Event: llm.ErrorEvent{Err: fmt.Errorf("connection lost")}})
	model = next.(Model)

	if model.streaming {
		t.Fatal("streaming should stop on error")
	}
	// Content should be preserved
	if !strings.Contains(model.status, "Partial") {
		t.Fatalf("status should mention preserved content: %q", model.status)
	}
	// Check that partial content is visible
	view := model.View()
	if !strings.Contains(view, "Partial content") {
		t.Fatalf("view should show preserved content:\n%s", view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestStreamError -v`
Expected: FAIL — partial content not preserved

- [ ] **Step 3: Update ErrorEvent handler in model.go**

Change the `ErrorEvent` handler in `Update`:

```go
case llm.ErrorEvent:
	// Preserve any content received before the error
	if m.streamBuf != "" {
		if m.conv != nil {
			m.conv.AddAssistant(m.streamBuf)
		}
		m.messages = append(m.messages, chatMsg{role: "assistant", content: m.streamBuf})
		m.streamBuf = ""
	}
	m.streaming = false
	m.status = "Stream error (content preserved): " + e.Err.Error()
	return m, nil
```

Also update `streamDoneMsg` handler similarly — if `streamBuf` has content, it should already be handled, but ensure consistency.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestStreamError -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: preserve partial content on stream interruption"
```

---

### Task 7: Update CLAUDE.md and Final Cleanup

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update CLAUDE.md Phase 4 section**

Add after Phase 3F:

```markdown
### Phase 4: Production Readiness — DONE

Structured logging, audit trail, LLM/MCP retry, agent version checking, stream error recovery.

- `internal/logging/logging.go` — Structured file logging with slog, log dir creation, level parsing
- `internal/security/audit.go` — Audit logger for tool call decisions (ALLOW/CONFIRM/DENY) with JSON entries
- `internal/llm/retry.go` — Retry wrapper for Provider interface with exponential backoff (429, 5xx)
- `internal/llm/llm.go` — Added httpError type for HTTP status classification
- `internal/mcp/errors.go` — MCP error classification (connection, timeout, auth, rate-limit, server)
- `internal/mcp/version.go` — Agent version checking and mismatch detection
- `internal/tui/model.go` — Version check on startup, stream error recovery, node offline on connection failure, audit integration
- `cmd/conan/main.go` — Logging init, audit logger init, retry provider wrapper, version wiring

Plan: `docs/superpowers/plans/2026-05-20-production-readiness.md`
```

- [ ] **Step 2: Run go vet and full test suite**

Run: `go vet ./... && go test ./... -count=1`
Expected: Clean

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update progress — Phase 4 Production Readiness complete"
```
