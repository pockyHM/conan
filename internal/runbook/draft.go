package runbook

import (
	"fmt"
	"strings"
	"time"
)

func DraftFromIncident(path string, markdown string, now time.Time) (Runbook, error) {
	if containsSecretLike(markdown) {
		return Runbook{}, fmt.Errorf("incident report appears to contain secret-like data")
	}
	sections := markdownSections(markdown)
	title := firstHeading(markdown)
	if title == "" {
		title = "Incident"
	}
	cluster := metadataLine(markdown, "cluster")
	rb := Runbook{
		Title:          title + " Runbook",
		SourceIncident: path,
		Cluster:        cluster,
		Tags:           []string{"incident", "draft"},
		CreatedAt:      now,
		Scenario:       strings.TrimSpace(sections["摘要"] + "\n\n" + sections["影响范围"]),
		Verification:   sections["验证结果"],
		Risks:          sections["后续项"],
	}
	for _, line := range strings.Split(sections["证据"], "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		if strings.Contains(line, "tool=") {
			rb.Steps = append(rb.Steps, Step{Kind: StepRead, Text: line})
		}
	}
	for _, line := range strings.Split(sections["执行动作"], "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		if strings.Contains(line, "outcome=approved") {
			rb.Steps = append(rb.Steps, Step{Kind: StepConfirm, Text: line})
		}
	}
	return rb, nil
}

func metadataLine(markdown string, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
		}
	}
	return ""
}

func containsSecretLike(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"token", "password", "passwd", "pwd=", "secret", "api key", "api_key", "apikey", "private key", "bearer ", "authorization:"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
