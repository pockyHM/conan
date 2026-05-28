package runbook

import (
	"strings"
	"testing"
	"time"
)

func TestDraftFromIncident(t *testing.T) {
	incident := `# API latency incident

incident_id: incident-abc123
cluster: prod

## 摘要

- API latency recovered.

## 影响范围

- cluster: prod
- nodes: web-1

## 时间线

- 2026-05-23T10:00:00Z [tool] svc_status active

## 证据

- 2026-05-23T10:00:00Z tool=svc_status success=true nginx active

## 根因假设

## 执行动作

- 2026-05-23T10:05:00Z tool=svc_restart risk=confirm outcome=approved restart nginx

## 验证结果

- Latency recovered after restart.
`

	rb, err := DraftFromIncident("incidents/2026-05-23-api.md", incident, time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if rb.Title != "API latency incident Runbook" {
		t.Fatalf("title = %q", rb.Title)
	}
	if rb.SourceIncident != "incidents/2026-05-23-api.md" || rb.Cluster != "prod" {
		t.Fatalf("source/cluster = %#v", rb)
	}
	if !strings.Contains(rb.Scenario, "API latency") || !strings.Contains(rb.Scenario, "web-1") {
		t.Fatalf("scenario = %q", rb.Scenario)
	}
	if len(rb.Steps) != 2 || rb.Steps[0].Kind != StepRead || rb.Steps[1].Kind != StepConfirm {
		t.Fatalf("steps = %#v", rb.Steps)
	}
	if !strings.Contains(rb.Verification, "Latency recovered") {
		t.Fatalf("verification = %q", rb.Verification)
	}
}

func TestDraftFromIncidentRejectsSecretLikeContent(t *testing.T) {
	_, err := DraftFromIncident("incidents/secret.md", "# Secret\n\n## 摘要\n\npassword=secret", time.Now())
	if err == nil {
		t.Fatal("expected secret-like incident content to be rejected")
	}
}
