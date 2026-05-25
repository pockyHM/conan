package evidence

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

func RenderMarkdown(incident Incident, events []Event, model string) string {
	events = append([]Event(nil), events...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	var b strings.Builder
	title := strings.TrimSpace(incident.Title)
	if title == "" {
		title = "Incident " + incident.ID
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("incident_id: " + incident.ID + "\n")
	b.WriteString("status: " + incident.Status + "\n")
	b.WriteString("cluster: " + incident.Cluster + "\n")
	b.WriteString("nodes: " + strings.Join(incident.Nodes, ", ") + "\n")
	b.WriteString("model: " + model + "\n")
	b.WriteString("started_at: " + formatTime(incident.StartedAt) + "\n")
	if !incident.ClosedAt.IsZero() {
		b.WriteString("closed_at: " + formatTime(incident.ClosedAt) + "\n")
	}
	b.WriteString("exported_at: " + formatTime(time.Now().UTC()) + "\n\n")

	b.WriteString("## 摘要\n\n")
	b.WriteString(summaryFromEvents(events) + "\n\n")
	b.WriteString("## 影响范围\n\n")
	b.WriteString("- cluster: " + incident.Cluster + "\n")
	if len(incident.Nodes) > 0 {
		b.WriteString("- nodes: " + strings.Join(incident.Nodes, ", ") + "\n")
	}
	b.WriteString("\n")
	b.WriteString("## 时间线\n\n")
	for _, event := range events {
		b.WriteString("- " + formatTime(event.Timestamp) + " [" + string(event.Source) + "] " + renderEventSummary(event) + "\n")
	}
	b.WriteString("\n")
	b.WriteString("## 证据\n\n")
	for _, event := range events {
		if event.Source == SourceTool || event.Source == SourceObservability || event.Source == SourceSubagent {
			b.WriteString("- " + renderEvidenceLine(event) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("## 根因假设\n\n")
	b.WriteString("- 待人工确认。\n\n")
	b.WriteString("## 执行动作\n\n")
	for _, event := range events {
		if event.Source == SourceRisk && isActionOutcome(event.RiskOutcome) {
			b.WriteString("- " + renderEvidenceLine(event) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("## 验证结果\n\n")
	for _, event := range events {
		if event.Source == SourceAssistant {
			b.WriteString("- " + renderEventSummary(event) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("## 后续项\n\n")
	b.WriteString("- 待补充。\n")
	return b.String()
}

func ExportMarkdown(root string, incident Incident, events []Event, model string) (string, error) {
	date := incident.StartedAt
	if date.IsZero() {
		date = time.Now()
	}
	base := date.Format("2006-01-02") + "-" + slugify(incident.Title)
	body := []byte(RenderMarkdown(incident, events, model))
	for suffix := 0; ; suffix++ {
		name := base
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d", base, suffix+1)
		}
		rel := filepath.ToSlash(filepath.Join("incidents", name+".md"))
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(body); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return rel, nil
	}
}

func summaryFromEvents(events []Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Source == SourceAssistant && strings.TrimSpace(events[i].Summary) != "" {
			return "- " + renderEventSummary(events[i])
		}
	}
	return "- 待补充。"
}

func renderEvidenceLine(event Event) string {
	parts := []string{formatTime(event.Timestamp)}
	if event.ToolName != "" {
		parts = append(parts, "tool="+event.ToolName)
	}
	if event.RiskLevel != "" {
		parts = append(parts, "risk="+event.RiskLevel)
	}
	if event.RiskOutcome != "" {
		parts = append(parts, "outcome="+event.RiskOutcome)
	}
	if event.Success != nil {
		parts = append(parts, fmt.Sprintf("success=%t", *event.Success))
	}
	parts = append(parts, renderEventSummary(event))
	return strings.Join(parts, " ")
}

func renderEventSummary(event Event) string {
	summary := truncateRunes(strings.TrimSpace(event.Summary), maxSummaryRunes)
	if containsSecretLike(summary) {
		return "[REDACTED]"
	}
	return summary
}

func isActionOutcome(outcome string) bool {
	switch outcome {
	case "approved", "cancelled", "blocked", "dispatched":
		return true
	default:
		return false
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func slugify(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "incident"
	}
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
	return slug
}
