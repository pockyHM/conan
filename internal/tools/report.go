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
