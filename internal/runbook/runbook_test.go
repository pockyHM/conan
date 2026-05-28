package runbook

import (
	"strings"
	"testing"
	"time"
)

func TestParseMarkdownRunbook(t *testing.T) {
	md := `---
title: Nginx 502 快速诊断
source_incident: incident-abc123
cluster: prod
tags: nginx, 502
created_at: 2026-05-23T10:00:00Z
---

# Nginx 502 快速诊断

## 适用场景

网关返回 502。

## 步骤

1. [read] 使用 ` + "`svc_status`" + ` 检查 nginx 状态。
2. [confirm] 使用 ` + "`svc_restart`" + ` 重启 nginx。
`

	rb, err := ParseMarkdown(md)
	if err != nil {
		t.Fatal(err)
	}
	if rb.Title != "Nginx 502 快速诊断" || rb.SourceIncident != "incident-abc123" || rb.Cluster != "prod" {
		t.Fatalf("metadata = %#v", rb)
	}
	if strings.Join(rb.Tags, ",") != "nginx,502" {
		t.Fatalf("tags = %#v", rb.Tags)
	}
	if len(rb.Steps) != 2 || rb.Steps[0].Kind != StepRead || rb.Steps[1].Kind != StepConfirm {
		t.Fatalf("steps = %#v", rb.Steps)
	}
	if !strings.Contains(rb.Scenario, "502") {
		t.Fatalf("scenario = %q", rb.Scenario)
	}
}

func TestRenderMarkdownRunbook(t *testing.T) {
	rb := Runbook{
		Title:          "Nginx 502 快速诊断",
		SourceIncident: "incident-abc123",
		Cluster:        "prod",
		Tags:           []string{"nginx", "502"},
		CreatedAt:      time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Scenario:       "网关返回 502。",
		Steps:          []Step{{Kind: StepRead, Text: "使用 `svc_status` 检查 nginx 状态。"}},
		Verification:   "请求恢复。",
		Risks:          "重启可能影响连接。",
	}

	md := RenderMarkdown(rb)

	for _, want := range []string{"title: Nginx 502 快速诊断", "source_incident: incident-abc123", "## 适用场景", "1. [read] 使用 `svc_status`", "## 验证", "请求恢复"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render missing %q:\n%s", want, md)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("API Latency Incident"); got != "api-latency-incident" {
		t.Fatalf("Slug = %q", got)
	}
}
