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
