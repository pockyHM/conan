package localtools

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
	"github.com/yuin/goldmark"
)

const maxReportMarkdownBytes = 1 << 20

var reportFilenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

const reportToolName = "web_report"

// githubMarkdownCSS renders Markdown in a GitHub-inspired light theme and
// embeds a sticky header bar with a primary "Download .md" button.
//
// This is intentionally a self-contained snippet (no external assets) so the
// loopback server has no third-party fetch dependencies and can be opened
// offline. Colors and metrics mirror github-markdown-css so the rendered
// output feels familiar.
const githubMarkdownCSS = `
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif;
  font-size: 16px;
  line-height: 1.5;
  color: #1f2328;
  background-color: #f6f8fa;
  -webkit-font-smoothing: antialiased;
}

.page-header {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 24px;
  background-color: rgba(255, 255, 255, 0.85);
  backdrop-filter: saturate(180%) blur(8px);
  -webkit-backdrop-filter: saturate(180%) blur(8px);
  border-bottom: 1px solid #d0d7de;
}

.page-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2328;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin: 0;
  min-width: 0;
}

.download-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  font-size: 14px;
  font-weight: 500;
  line-height: 20px;
  color: #ffffff;
  background-color: #1f883d;
  border: 1px solid rgba(31, 35, 40, 0.15);
  border-radius: 6px;
  box-shadow: 0 1px 0 rgba(31, 35, 40, 0.1);
  text-decoration: none;
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
  transition: background-color 0.12s ease;
}
.download-btn:hover { background-color: #1a7f37; }
.download-btn:active { background-color: #196c2e; }
.download-btn:focus-visible {
  outline: 2px solid #0969da;
  outline-offset: 2px;
}
.download-btn svg { display: block; }

.markdown-body {
  box-sizing: border-box;
  min-width: 200px;
  max-width: 1012px;
  margin: 24px auto;
  padding: 32px;
  background-color: #ffffff;
  border: 1px solid #d0d7de;
  border-radius: 6px;
}

.markdown-body > *:first-child { margin-top: 0 !important; }
.markdown-body > *:last-child { margin-bottom: 0 !important; }

.markdown-body h1, .markdown-body h2, .markdown-body h3,
.markdown-body h4, .markdown-body h5, .markdown-body h6 {
  margin-top: 24px;
  margin-bottom: 16px;
  font-weight: 600;
  line-height: 1.25;
}
.markdown-body h1 {
  font-size: 2em;
  border-bottom: 1px solid #d0d7de;
  padding-bottom: 0.3em;
}
.markdown-body h2 {
  font-size: 1.5em;
  border-bottom: 1px solid #d0d7de;
  padding-bottom: 0.3em;
}
.markdown-body h3 { font-size: 1.25em; }
.markdown-body h4 { font-size: 1em; }
.markdown-body h5 { font-size: 0.875em; }
.markdown-body h6 { font-size: 0.85em; color: #59636e; }

.markdown-body p { margin-top: 0; margin-bottom: 16px; }

.markdown-body a { color: #0969da; text-decoration: none; }
.markdown-body a:hover { text-decoration: underline; }

.markdown-body ul, .markdown-body ol {
  margin-top: 0;
  margin-bottom: 16px;
  padding-left: 2em;
}
.markdown-body li + li { margin-top: 4px; }
.markdown-body li > p { margin-top: 16px; }

.markdown-body blockquote {
  margin: 0 0 16px 0;
  padding: 0 1em;
  color: #59636e;
  border-left: 0.25em solid #d0d7de;
}
.markdown-body blockquote > :first-child { margin-top: 0; }
.markdown-body blockquote > :last-child { margin-bottom: 0; }

.markdown-body code, .markdown-body tt {
  padding: 0.2em 0.4em;
  font-size: 85%;
  background-color: rgba(175, 184, 195, 0.2);
  border-radius: 6px;
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
}
.markdown-body pre {
  padding: 16px;
  overflow: auto;
  font-size: 85%;
  line-height: 1.45;
  color: #1f2328;
  background-color: #f6f8fa;
  border-radius: 6px;
  margin-top: 0;
  margin-bottom: 16px;
}
.markdown-body pre code {
  padding: 0;
  font-size: 100%;
  background-color: transparent;
  border: 0;
  white-space: pre;
  word-wrap: normal;
}

.markdown-body table {
  display: block;
  width: max-content;
  max-width: 100%;
  overflow: auto;
  border-spacing: 0;
  border-collapse: collapse;
  margin-top: 0;
  margin-bottom: 16px;
}
.markdown-body table th, .markdown-body table td {
  padding: 6px 13px;
  border: 1px solid #d0d7de;
}
.markdown-body table tr { background-color: #ffffff; border-top: 1px solid #d8dee4; }
.markdown-body table tr:nth-child(2n) { background-color: #f6f8fa; }
.markdown-body table img { background-color: transparent; }

.markdown-body hr {
  height: 0.25em;
  padding: 0;
  margin: 24px 0;
  background-color: #d0d7de;
  border: 0;
}

.markdown-body img { max-width: 100%; }

.markdown-body strong { font-weight: 600; }
.markdown-body em { font-style: italic; }

.markdown-body kbd {
  display: inline-block;
  padding: 3px 5px;
  font-size: 11px;
  line-height: 10px;
  color: #1f2328;
  vertical-align: middle;
  background-color: #f6f8fa;
  border: solid 1px #d0d7de;
  border-bottom-color: rgba(175, 184, 195, 0.2);
  border-radius: 6px;
  box-shadow: inset 0 -1px 0 rgba(175, 184, 195, 0.2);
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
}

.markdown-body details { margin-bottom: 16px; }
.markdown-body details summary { cursor: pointer; font-weight: 600; }

@media (max-width: 767px) {
  .markdown-body { padding: 16px; margin: 16px; }
  .page-header { padding: 10px 16px; }
  .download-btn .download-btn-label { display: none; }
  .download-btn { padding: 5px 9px; }
}
`

// downloadIconSVG is the GitHub Octicon "download" glyph, sized 16x16. The
// currentColor fill lets the button background drive the icon color.
const downloadIconSVG = `<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" focusable="false"><path d="M2.75 14A1.75 1.75 0 0 1 1 12.25v-2.5a.75.75 0 0 1 1.5 0v2.5c0 .138.112.25.25.25h10.5a.25.25 0 0 0 .25-.25v-2.5a.75.75 0 0 1 1.5 0v2.5A1.75 1.75 0 0 1 13.25 14Z"/><path d="M7.25 7.689V2a.75.75 0 0 1 1.5 0v5.689l1.97-1.969a.749.749 0 1 1 1.06 1.06l-3.25 3.25a.749.749 0 0 1-1.06 0L4.47 6.78a.749.749 0 1 1 1.06-1.06l1.97 1.969Z"/></svg>`

func handleReport(ctx context.Context, input json.RawMessage) Result {
	var args struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{Output: "Error: " + err.Error(), Success: false}
	}
	if strings.TrimSpace(args.Markdown) == "" {
		return Result{Output: "Error: markdown is required", Success: false}
	}
	if len([]byte(args.Markdown)) > maxReportMarkdownBytes {
		return Result{Output: fmt.Sprintf("Error: markdown exceeds maximum size of %d bytes", maxReportMarkdownBytes), Success: false}
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "Report"
	}
	rendered, err := renderReportMarkdown(args.Markdown)
	if err != nil {
		return Result{Output: "Error: " + err.Error(), Success: false}
	}
	filename := sanitizeReportFilename(args.Filename, title)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{Output: "Error: " + err.Error(), Success: false}
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
		_, _ = rw.Write([]byte(reportHTMLPage(title, filename, rendered)))
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
	return Result{Output: string(out), Success: true}
}

func renderReportMarkdown(markdown string) (string, error) {
	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(markdown), &rendered); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return bluemonday.UGCPolicy().Sanitize(rendered.String()), nil
}

// reportHTMLPage returns the full HTML document for the rendered report.
// It embeds the GitHub-style CSS, a sticky top bar (title left, primary
// download button right), and a centered white "markdown-body" card. The
// download button links to /download with the sanitized filename as a hint
// via the HTML download attribute; the server still sets the authoritative
// Content-Disposition header.
func reportHTMLPage(title string, filename string, body string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString("<html lang=\"en\">\n<head>\n")
	b.WriteString("  <meta charset=\"utf-8\">\n")
	b.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("  <title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title>\n")
	b.WriteString("  <style>")
	b.WriteString(githubMarkdownCSS)
	b.WriteString("</style>\n</head>\n<body>\n")
	b.WriteString("  <header class=\"page-header\">\n")
	b.WriteString("    <div class=\"page-title\">")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</div>\n")
	b.WriteString("    <a class=\"download-btn\" href=\"/download\" download=\"")
	b.WriteString(html.EscapeString(filename))
	b.WriteString("\" title=\"Download the original Markdown file\">\n      ")
	b.WriteString(downloadIconSVG)
	b.WriteString("\n      <span class=\"download-btn-label\">Download .md</span>\n")
	b.WriteString("    </a>\n")
	b.WriteString("  </header>\n")
	b.WriteString("  <article class=\"markdown-body\">\n")
	b.WriteString(body)
	b.WriteString("\n  </article>\n</body>\n</html>\n")
	return b.String()
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
