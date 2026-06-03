package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
	"golang.org/x/net/html"
)

const (
	defaultWebFetchMaxBytes = int64(2 << 20)
	defaultWebFetchMaxChars = 12000
	defaultWebTimeout       = 10 * time.Second
	defaultBraveEndpoint    = "https://api.search.brave.com/res/v1/web_search"
)

type WebToolConfig struct {
	SearchProvider      string
	SearchAPIKey        string
	SearchEndpoint      string
	FetchMaxBytes       int64
	FetchMaxChars       int
	AllowPrivateNetwork bool
}

func NewWebTools(cfg WebToolConfig) []Tool {
	result := []Tool{&webFetchTool{cfg: cfg}, &webReportTool{}}
	if cfg.SearchProvider != "" && cfg.SearchAPIKey != "" {
		result = append(result, &webSearchTool{cfg: cfg})
	}
	return result
}

type webSearchTool struct {
	cfg WebToolConfig
}

func (w *webSearchTool) Name() string        { return "web_search" }
func (w *webSearchTool) Description() string { return "Search the web and return structured results" }
func (w *webSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"max_results":{"type":"integer","description":"Maximum number of results, default 5"},"domains":{"type":"array","items":{"type":"string"},"description":"Restrict results to these domains"},"recency_days":{"type":"integer","description":"Prefer results from this many recent days"},"locale":{"type":"string","description":"Search locale or language hint"}},"required":["query"]}`)
}

func (w *webSearchTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Query      string   `json:"query"`
		MaxResults int      `json:"max_results"`
		Domains    []string `json:"domains"`
		Locale     string   `json:"locale"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return toolError("query is required"), nil
	}
	if args.MaxResults <= 0 || args.MaxResults > 10 {
		args.MaxResults = 5
	}
	switch strings.ToLower(w.cfg.SearchProvider) {
	case "brave":
		return w.executeBrave(ctx, args.Query, args.MaxResults, args.Domains, args.Locale)
	default:
		return toolError("unsupported web search provider: " + w.cfg.SearchProvider), nil
	}
}

func (w *webSearchTool) executeBrave(ctx context.Context, query string, maxResults int, domains []string, locale string) (*mcpproto.ToolResult, error) {
	endpoint := w.cfg.SearchEndpoint
	if endpoint == "" {
		endpoint = defaultBraveEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return toolError(err.Error()), nil
	}
	q := u.Query()
	q.Set("q", queryWithDomains(query, domains))
	q.Set("count", fmt.Sprintf("%d", maxResults))
	if locale != "" {
		q.Set("search_lang", locale)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", w.cfg.SearchAPIKey)

	client := &http.Client{Timeout: defaultWebTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return toolError(err.Error()), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return toolError(err.Error()), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return toolError(fmt.Sprintf("search request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))), nil
	}

	var brave struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &brave); err != nil {
		return toolError(err.Error()), nil
	}
	results := make([]map[string]string, 0, len(brave.Web.Results))
	for i, item := range brave.Web.Results {
		if i >= maxResults {
			break
		}
		results = append(results, map[string]string{
			"title":     item.Title,
			"url":       item.URL,
			"snippet":   item.Description,
			"published": item.Age,
			"source":    "brave",
		})
	}
	out, _ := json.Marshal(map[string]any{"results": results})
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func queryWithDomains(query string, domains []string) string {
	var filters []string
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain != "" {
			filters = append(filters, "site:"+domain)
		}
	}
	if len(filters) == 0 {
		return query
	}
	return query + " " + strings.Join(filters, " OR ")
}

type webFetchTool struct {
	cfg WebToolConfig
}

func (w *webFetchTool) Name() string        { return "web_fetch" }
func (w *webFetchTool) Description() string { return "Fetch a web page and return cleaned text" }
func (w *webFetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"HTTP or HTTPS URL to fetch"},"max_chars":{"type":"integer","description":"Maximum characters to return"},"timeout_ms":{"type":"integer","description":"Request timeout in milliseconds"}},"required":["url"]}`)
}

func (w *webFetchTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		URL       string `json:"url"`
		MaxChars  int    `json:"max_chars"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if err := validateFetchURL(args.URL, w.cfg.AllowPrivateNetwork); err != nil {
		return toolError(err.Error()), nil
	}
	timeout := defaultWebTimeout
	if args.TimeoutMS > 0 {
		timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	maxBytes := w.cfg.FetchMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultWebFetchMaxBytes
	}
	maxChars := effectiveMaxChars(args.MaxChars, w.cfg.FetchMaxChars)

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return validateFetchURL(req.URL.String(), w.cfg.AllowPrivateNetwork)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,text/plain,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return toolError(err.Error()), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return toolError(err.Error()), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return toolError(fmt.Sprintf("fetch request failed: HTTP %d", resp.StatusCode)), nil
	}
	truncated := int64(len(body)) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	title := ""
	text := ""
	switch {
	case strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml") || contentType == "":
		title, text = extractHTMLText(body)
	case strings.Contains(contentType, "text/plain"):
		text = string(body)
	default:
		return toolError("unsupported content type: " + contentType), nil
	}
	text = strings.TrimSpace(collapseWhitespace(text))
	if truncatedText, ok := truncateRunes(text, maxChars); ok {
		text = truncatedText
		truncated = true
	}
	out, _ := json.Marshal(map[string]any{
		"url":          args.URL,
		"final_url":    resp.Request.URL.String(),
		"title":        title,
		"content_type": contentType,
		"text":         text,
		"truncated":    truncated,
	})
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(string(out))}}, nil
}

func effectiveMaxChars(requested int, configured int) int {
	maxChars := configured
	if maxChars <= 0 {
		maxChars = defaultWebFetchMaxChars
	}
	if requested > 0 && requested < maxChars {
		return requested
	}
	return maxChars
}

func truncateRunes(text string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return text, false
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	return string(runes[:maxChars]), true
}

func validateFetchURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("only http and https URLs are supported")
	}
	if u.Hostname() == "" {
		return errors.New("url host is required")
	}
	if allowPrivate {
		return nil
	}
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return errors.New("private network URLs are not allowed")
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func extractHTMLText(body []byte) (string, string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", string(body)
	}
	var title string
	var parts []string
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg":
				skip = true
			case "title":
				title = strings.TrimSpace(nodeText(n))
				skip = true
			}
		}
		if !skip && n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				parts = append(parts, text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}
	walk(doc, false)
	return title, strings.Join(parts, " ")
}

func nodeText(n *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			parts = append(parts, node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
