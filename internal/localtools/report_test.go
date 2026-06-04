package localtools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestReportRejectsEmptyMarkdown(t *testing.T) {
	result := Handle(context.Background(), RootedFS{}, "web_report", json.RawMessage(`{"markdown":"   "}`))
	if result.Success {
		t.Fatalf("result = %#v, want failure for empty markdown", result)
	}
	if !strings.Contains(result.Output, "markdown is required") {
		t.Fatalf("output = %q, want markdown required error", result.Output)
	}
}

func TestReportRejectsUnknownToolName(t *testing.T) {
	result := Handle(context.Background(), RootedFS{}, "web_report_typo", json.RawMessage(`{"markdown":"# hi"}`))
	if result.Success {
		t.Fatalf("result = %#v, want failure for unknown tool", result)
	}
	if !strings.Contains(result.Output, "unknown local tool") {
		t.Fatalf("output = %q, want unknown local tool error", result.Output)
	}
}

func TestReportReturnsLoopbackURLs(t *testing.T) {
	result := Handle(context.Background(), RootedFS{}, "web_report", json.RawMessage(`{"title":"Ops Report","markdown":"# Status\n\nAll clear.","filename":"ops.md"}`))
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}

	var out struct {
		Title       string `json:"title"`
		ViewURL     string `json:"view_url"`
		DownloadURL string `json:"download_url"`
		Port        int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, result.Output)
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

func TestReportServesRenderedHTMLAndMarkdownDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	markdown := "# Status\n\n<script>alert(1)</script>\n\nAll clear."

	result := Handle(ctx, RootedFS{}, "web_report", json.RawMessage(`{"title":"Ops Report","markdown":`+string(mustJSON(t, markdown))+`,"filename":"ops report.md"}`))
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}

	var out struct {
		ViewURL     string `json:"view_url"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	htmlResp, err := http.Get(out.ViewURL)
	if err != nil {
		t.Fatalf("get view: %v", err)
	}
	defer htmlResp.Body.Close()
	htmlBody, _ := io.ReadAll(htmlResp.Body)
	body := string(htmlBody)
	if htmlResp.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d, body = %s", htmlResp.StatusCode, body)
	}
	if !strings.Contains(body, "<h1>Status</h1>") || !strings.Contains(body, "All clear.") {
		t.Fatalf("html did not contain rendered markdown: %s", body)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "alert(1)") {
		t.Fatalf("html should be sanitized: %s", body)
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

func TestReportIsReadOnlyAndLocal(t *testing.T) {
	if !IsLocalTool("web_report") {
		t.Fatal("web_report should be a local tool")
	}
	if !IsReadOnly("web_report") {
		t.Fatal("web_report should be read-only")
	}
	if PathFromCall("web_report", json.RawMessage(`{"markdown":"x"}`)) != "" {
		t.Fatal("PathFromCall should return empty for web_report")
	}
}

func TestReportHTMLUsesGitHubStyleAndHasDownloadButton(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	markdown := "# Status\n\nAll clear."

	result := Handle(ctx, RootedFS{}, "web_report", json.RawMessage(`{"title":"Ops Report","markdown":`+string(mustJSON(t, markdown))+`,"filename":"ops.md"}`))
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}

	var out struct {
		ViewURL string `json:"view_url"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	resp, err := http.Get(out.ViewURL)
	if err != nil {
		t.Fatalf("get view: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	for _, want := range []string{
		"markdown-body",            // GitHub-style content container
		`class="page-header"`,      // sticky top bar
		"Ops Report",               // title shown in header
		`class="download-btn"`,     // download button styled
		`href="/download"`,         // links to the download endpoint
		`download="ops.md"`,        // suggested filename via HTML attribute
		"#f6f8fa",                  // GitHub page background
		`download-btn-label`,       // button text wrapper
		"Download .md",             // button text
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("html missing %q\n--- body ---\n%s", want, body)
		}
	}

	// Sanity-check the existing rendering & sanitization contract still holds.
	if !strings.Contains(body, "<h1>Status</h1>") || !strings.Contains(body, "All clear.") {
		t.Fatalf("html did not contain rendered markdown: %s", body)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "alert(1)") {
		t.Fatalf("html should be sanitized: %s", body)
	}
}

func TestReportToolDefExposed(t *testing.T) {
	var found bool
	for _, def := range ToolDefs() {
		if def.Name == "web_report" {
			found = true
			if !strings.Contains(def.Description, "browser report") {
				t.Fatalf("web_report description = %q, want it to mention browser report", def.Description)
			}
			if !strings.Contains(string(def.InputSchema), `"markdown"`) {
				t.Fatalf("web_report InputSchema = %s, want markdown property", def.InputSchema)
			}
			break
		}
	}
	if !found {
		t.Fatal("web_report not exposed by ToolDefs()")
	}
}
