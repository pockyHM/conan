package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderMarkdownIncludesRequiredHeadingsAndMetadata(t *testing.T) {
	incident := Incident{
		ID:        "incident-1",
		Title:     "API latency incident",
		Cluster:   "prod",
		Nodes:     []string{"web-1"},
		Status:    StatusClosed,
		StartedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		ClosedAt:  time.Date(2026, 5, 23, 10, 30, 0, 0, time.UTC),
	}
	approved := true
	events := []Event{
		{Source: SourceAssistant, Timestamp: time.Date(2026, 5, 23, 10, 3, 0, 0, time.UTC), Summary: "Latency recovered"},
		{Source: SourceTool, Timestamp: time.Date(2026, 5, 23, 10, 1, 0, 0, time.UTC), ToolName: "svc/status", Summary: "nginx active", Success: &approved},
		{Source: SourceRisk, Timestamp: time.Date(2026, 5, 23, 10, 2, 0, 0, time.UTC), ToolName: "svc/restart", Summary: "restart approved", RiskLevel: "confirm", RiskOutcome: "approved"},
	}

	md := RenderMarkdown(incident, events, "claude-sonnet")

	for _, want := range []string{
		"# API latency incident",
		"## 摘要",
		"## 影响范围",
		"## 时间线",
		"## 证据",
		"## 根因假设",
		"## 执行动作",
		"## 验证结果",
		"## 后续项",
		"cluster: prod",
		"nodes: web-1",
		"model: claude-sonnet",
		"risk=confirm",
		"outcome=approved",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Index(md, "10:01:00") > strings.Index(md, "10:02:00") {
		t.Fatalf("timeline not sorted:\n%s", md)
	}
}

func TestExportMarkdownWritesIncidentReport(t *testing.T) {
	root := t.TempDir()
	incident := Incident{
		ID:        "incident-1",
		Title:     "API latency incident",
		Cluster:   "prod",
		Status:    StatusClosed,
		StartedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
	}

	rel, err := ExportMarkdown(root, incident, nil, "model")
	if err != nil {
		t.Fatal(err)
	}

	if rel != "incidents/2026-05-23-api-latency-incident.md" {
		t.Fatalf("rel = %q", rel)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		t.Fatalf("report was not written: %v", err)
	}
}

func TestExportMarkdownCreatesUniqueReports(t *testing.T) {
	root := t.TempDir()
	incident := Incident{
		ID:        "incident-1",
		Title:     "API latency incident",
		StartedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
	}

	first, err := ExportMarkdown(root, incident, nil, "model")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportMarkdown(root, incident, nil, "model")
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Fatalf("second export overwrote first path %q", first)
	}
	if second != "incidents/2026-05-23-api-latency-incident-2.md" {
		t.Fatalf("second = %q", second)
	}
}

func TestRenderMarkdownRedactsSecretLikeContent(t *testing.T) {
	incident := Incident{ID: "incident-1", Title: "Secret", StartedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)}
	md := RenderMarkdown(incident, []Event{{Source: SourceTool, Summary: "password=secret"}}, "model")

	if strings.Contains(md, "password=secret") {
		t.Fatalf("secret-like content leaked:\n%s", md)
	}
	if !strings.Contains(md, "[REDACTED]") {
		t.Fatalf("redaction marker missing:\n%s", md)
	}
}

func TestRenderMarkdownDoesNotMutateEventOrder(t *testing.T) {
	incident := Incident{ID: "incident-1", Title: "Order", StartedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)}
	events := []Event{
		{ID: "later", Source: SourceTool, Timestamp: time.Date(2026, 5, 23, 10, 2, 0, 0, time.UTC), Summary: "later"},
		{ID: "earlier", Source: SourceTool, Timestamp: time.Date(2026, 5, 23, 10, 1, 0, 0, time.UTC), Summary: "earlier"},
	}

	_ = RenderMarkdown(incident, events, "model")

	if events[0].ID != "later" || events[1].ID != "earlier" {
		t.Fatalf("RenderMarkdown mutated caller slice: %#v", events)
	}
}
