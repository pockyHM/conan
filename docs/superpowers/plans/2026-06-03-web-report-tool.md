# Web Report Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `web_report` tool that accepts Markdown, starts a random loopback HTTP port, renders the report for browser viewing, and exposes a Markdown download URL.

**Architecture:** Implement the tool in a focused `internal/tools/report.go` file and register it through `NewWebTools` so node discovery and `tool_search` can find it. Use `goldmark` for Markdown rendering, `bluemonday` for HTML sanitization, and a per-report `http.Server` bound to `127.0.0.1:0`.

**Tech Stack:** Go, `net/http`, `net.Listen`, `encoding/json`, `github.com/yuin/goldmark`, `github.com/microcosm-cc/bluemonday`, existing Conan MCP tool interfaces.

---

## File Structure

- Create `internal/tools/report.go`: `webReportTool`, Markdown rendering, HTML page generation, loopback server startup, filename sanitization, JSON response shape.
- Create `internal/tools/report_test.go`: focused behavior tests for validation, random loopback URLs, rendered HTML, sanitized output, and Markdown download.
- Modify `internal/tools/web.go`: include `web_report` in `NewWebTools`.
- Modify `internal/tools/web_test.go`: assert `NewWebTools` exposes `web_report`.
- Modify `internal/tools/metadata.go`: add `web_report` metadata.
- Modify `internal/tools/metadata_test.go`: include `web_report` in built-in metadata coverage.
- Modify `cmd/conan-agent/main_test.go`: assert default agent registration includes `web_report`.

### Task 1: Tool Skeleton and Registration

**Files:**
- Create: `internal/tools/report.go`
- Modify: `internal/tools/web.go`
- Modify: `internal/tools/web_test.go`

- [ ] **Step 1: Write the failing registration test**

Update `TestNewWebToolsExposesFetchWithoutSearchConfig` in `internal/tools/web_test.go`:

```go
func TestNewWebToolsExposesFetchAndReportWithoutSearchConfig(t *testing.T) {
	tools := NewWebTools(WebToolConfig{})

	if got := toolNames(tools); strings.Join(got, ",") != "web_fetch,web_report" {
		t.Fatalf("tools = %#v, want web_fetch and web_report", got)
	}
}
```

Update `TestNewWebToolsExposesSearchWhenConfigured`:

```go
func TestNewWebToolsExposesSearchWhenConfigured(t *testing.T) {
	tools := NewWebTools(WebToolConfig{SearchProvider: "brave", SearchAPIKey: "test-key"})

	if got := toolNames(tools); strings.Join(got, ",") != "web_fetch,web_report,web_search" {
		t.Fatalf("tools = %#v, want web_fetch, web_report, and web_search", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools -run 'TestNewWebToolsExposes'`

Expected: FAIL because `web_report` is not registered.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/report.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type webReportTool struct{}

func (w *webReportTool) Name() string { return "web_report" }

func (w *webReportTool) Description() string {
	return "Render Markdown as a local browser report and provide a Markdown download link"
}

func (w *webReportTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Optional report title"},"markdown":{"type":"string","description":"Markdown report content to render"},"filename":{"type":"string","description":"Optional Markdown download filename"}},"required":["markdown"]}`)
}

func (w *webReportTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	return toolError("web_report is not implemented yet"), nil
}
```

Modify `NewWebTools` in `internal/tools/web.go`:

```go
func NewWebTools(cfg WebToolConfig) []Tool {
	result := []Tool{&webFetchTool{cfg: cfg}, &webReportTool{}}
	if cfg.SearchProvider != "" && cfg.SearchAPIKey != "" {
		result = append(result, &webSearchTool{cfg: cfg})
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools -run 'TestNewWebToolsExposes'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/report.go internal/tools/web.go internal/tools/web_test.go
git commit -m "feat(tools): register web report tool"
```

### Task 2: Validation and Report Server

**Files:**
- Modify: `internal/tools/report.go`
- Create: `internal/tools/report_test.go`

- [ ] **Step 1: Write failing validation and URL tests**

Create `internal/tools/report_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWebReportRejectsEmptyMarkdown(t *testing.T) {
	tool := &webReportTool{}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"markdown":"   "}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "markdown is required") {
		t.Fatalf("result = %#v, want markdown required error", result)
	}
}

func TestWebReportReturnsLoopbackURLs(t *testing.T) {
	tool := &webReportTool{}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"Ops Report","markdown":"# Status\n\nAll clear.","filename":"ops.md"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is error: %#v", result)
	}

	var out struct {
		Title       string `json:"title"`
		ViewURL     string `json:"view_url"`
		DownloadURL string `json:"download_url"`
		Port        int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, result.Content[0].Text)
	}
	if out.Title != "Ops Report" {
		t.Fatalf("title = %q, want Ops Report", out.Title)
	}
	if out.Port <= 0 {
		t.Fatalf("port = %d, want assigned port", out.Port)
	}
	for _, raw := range []string{out.ViewURL, out.DownloadURL} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse url %q: %v", raw, err)
		}
		if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			t.Fatalf("url = %q, want http loopback", raw)
		}
	}
}

func TestWebReportServesRenderedHTMLAndMarkdownDownload(t *testing.T) {
	tool := &webReportTool{}
	markdown := "# Status\n\n<script>alert(1)</script>\n\nAll clear."

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"Ops Report","markdown":`+mustJSONString(markdown)+`,"filename":"ops report.md"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is error: %#v", result)
	}

	var out struct {
		ViewURL     string `json:"view_url"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	htmlResp, err := http.Get(out.ViewURL)
	if err != nil {
		t.Fatalf("get view: %v", err)
	}
	defer htmlResp.Body.Close()
	htmlBody, _ := io.ReadAll(htmlResp.Body)
	html := string(htmlBody)
	if htmlResp.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d, body = %s", htmlResp.StatusCode, html)
	}
	if !strings.Contains(html, "<h1>Status</h1>") || !strings.Contains(html, "All clear.") {
		t.Fatalf("html did not contain rendered markdown: %s", html)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "alert(1)") {
		t.Fatalf("html should be sanitized: %s", html)
	}

	mdResp, err := http.Get(out.DownloadURL)
	if err != nil {
		t.Fatalf("get download: %v", err)
	}
	defer mdResp.Body.Close()
	mdBody, _ := io.ReadAll(mdResp.Body)
	if string(mdBody) != markdown {
		t.Fatalf("download = %q, want original markdown", string(mdBody))
	}
	if got := mdResp.Header.Get("Content-Disposition"); !strings.Contains(got, `attachment; filename="ops-report.md"`) {
		t.Fatalf("Content-Disposition = %q, want sanitized attachment filename", got)
	}
}

func mustJSONString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools -run 'TestWebReport'`

Expected: FAIL because `Execute` still returns an unimplemented error.

- [ ] **Step 3: Write minimal implementation**

Replace `internal/tools/report.go` with:

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/yuin/goldmark"
)

const maxReportMarkdownBytes = 1 << 20

var reportFilenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type webReportTool struct{}

func (w *webReportTool) Name() string { return "web_report" }

func (w *webReportTool) Description() string {
	return "Render Markdown as a local browser report and provide a Markdown download link"
}

func (w *webReportTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Optional report title"},"markdown":{"type":"string","description":"Markdown report content to render"},"filename":{"type":"string","description":"Optional Markdown download filename"}},"required":["markdown"]}`)
}

func (w *webReportTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Markdown) == "" {
		return toolError("markdown is required"), nil
	}
	if len([]byte(args.Markdown)) > maxReportMarkdownBytes {
		return toolError(fmt.Sprintf("markdown exceeds maximum size of %d bytes", maxReportMarkdownBytes)), nil
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "Report"
	}
	rendered, err := renderReportMarkdown(args.Markdown)
	if err != nil {
		return toolError(err.Error()), nil
	}
	filename := sanitizeReportFilename(args.Filename, title)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return toolError(err.Error()), nil
	}
	port := listener.Addr().(*net.TCPAddr).Port
	viewURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
	downloadURL := fmt.Sprintf("http://127.0.0.1:%d/download", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write([]byte(reportHTMLPage(title, rendered)))
	})
	mux.HandleFunc("/download", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		rw.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		_, _ = rw.Write([]byte(args.Markdown))
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	out, _ := json.Marshal(map[string]any{
		"title":        title,
		"view_url":     viewURL,
		"download_url": downloadURL,
		"port":         port,
	})
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func renderReportMarkdown(markdown string) (string, error) {
	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(markdown), &rendered); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return bluemonday.UGCPolicy().Sanitize(rendered.String()), nil
}

func reportHTMLPage(title string, body string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>" + html.EscapeString(title) + "</title></head><body><main>" + body + "</main></body></html>"
}

func sanitizeReportFilename(filename string, title string) string {
	base := strings.TrimSpace(filename)
	if base == "" {
		base = strings.TrimSpace(title)
	}
	base = filepath.Base(base)
	base = strings.ToLower(base)
	base = reportFilenameUnsafe.ReplaceAllString(base, "-")
	base = strings.Trim(base, ".-_")
	if base == "" {
		base = "report"
	}
	if filepath.Ext(base) == "" {
		base += ".md"
	}
	if filepath.Ext(base) != ".md" {
		base = strings.TrimSuffix(base, filepath.Ext(base)) + ".md"
	}
	return base
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools -run 'TestWebReport|TestNewWebToolsExposes'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/report.go internal/tools/report_test.go
git commit -m "feat(tools): serve markdown reports locally"
```

### Task 3: Metadata and Agent Registration

**Files:**
- Modify: `internal/tools/metadata.go`
- Modify: `internal/tools/metadata_test.go`
- Modify: `cmd/conan-agent/main_test.go`

- [ ] **Step 1: Write failing metadata and registration tests**

In `internal/tools/metadata_test.go`, add `web_report` to the metadata coverage list near the other web tools:

```go
"web_search", "web_fetch", "web_report",
```

In `cmd/conan-agent/main_test.go`, add `web_report` to the `allowed` map in `TestRegisterAllToolsOnlyExposesShellAndReadOnlyTools`:

```go
"web_report": true,
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools ./cmd/conan-agent -run 'TestDefaultMetadataCoversBuiltInAndMetaTools|TestRegisterAllToolsOnlyExposesShellAndReadOnlyTools'`

Expected: FAIL because `web_report` metadata is missing or registration expectations have not been updated together.

- [ ] **Step 3: Add metadata**

Modify `internal/tools/metadata.go` near the existing web entries:

```go
meta("web_report", SafetyReadOnly, ScopeLocal, []string{"web", "report"}, []string{"markdown", "preview", "download", "html"}),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools ./cmd/conan-agent -run 'TestDefaultMetadataCoversBuiltInAndMetaTools|TestRegisterAllToolsOnlyExposesShellAndReadOnlyTools'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/metadata.go internal/tools/metadata_test.go cmd/conan-agent/main_test.go
git commit -m "feat(tools): add web report metadata"
```

### Task 4: Final Verification

**Files:**
- No new files.

- [ ] **Step 1: Run focused tests**

Run: `go test ./internal/tools ./cmd/conan-agent`

Expected: PASS.

- [ ] **Step 2: Run broad test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Inspect working tree**

Run: `git status --short`

Expected: only pre-existing unrelated user changes remain, or a clean tree if none existed.

- [ ] **Step 4: Commit any final polish if needed**

If final verification required small code polish, commit only the files changed by this feature:

```bash
git add internal/tools/report.go internal/tools/report_test.go internal/tools/web.go internal/tools/web_test.go internal/tools/metadata.go internal/tools/metadata_test.go cmd/conan-agent/main_test.go
git commit -m "test: verify web report tool"
```

Skip this commit if there are no additional changes after Task 3.

## Self-Review

- Spec coverage: `web_report` name, Markdown input, random loopback port, view URL, download URL, sanitized HTML, original Markdown download, metadata, tool-search discoverability, and registration are all covered by tasks.
- Placeholder scan: no unresolved placeholders remain.
- Type consistency: tests and implementation use `title`, `markdown`, `filename`, `view_url`, `download_url`, and `port` consistently.
