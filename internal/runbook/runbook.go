package runbook

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type StepKind string

const (
	StepRead        StepKind = "read"
	StepConfirm     StepKind = "confirm"
	StepDestructive StepKind = "destructive"
)

type Step struct {
	Kind StepKind
	Text string
}

type Runbook struct {
	Title          string
	SourceIncident string
	Cluster        string
	Tags           []string
	CreatedAt      time.Time
	Scenario       string
	Prechecks      string
	Steps          []Step
	Verification   string
	Risks          string
}

func ParseMarkdown(md string) (Runbook, error) {
	var rb Runbook
	body := md
	if strings.HasPrefix(md, "---\n") {
		end := strings.Index(md[len("---\n"):], "\n---")
		if end >= 0 {
			fm := md[len("---\n") : len("---\n")+end]
			body = strings.TrimSpace(md[len("---\n")+end+len("\n---"):])
			parseFrontmatter(fm, &rb)
		}
	}
	if rb.Title == "" {
		rb.Title = firstHeading(body)
	}
	sections := markdownSections(body)
	rb.Scenario = sections["适用场景"]
	rb.Prechecks = sections["前置检查"]
	rb.Verification = sections["验证"]
	rb.Risks = sections["风险"]
	rb.Steps = parseSteps(sections["步骤"])
	if rb.Title == "" {
		return rb, fmt.Errorf("runbook title is required")
	}
	return rb, nil
}

func RenderMarkdown(rb Runbook) string {
	if rb.CreatedAt.IsZero() {
		rb.CreatedAt = time.Now().UTC()
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + rb.Title + "\n")
	if rb.SourceIncident != "" {
		b.WriteString("source_incident: " + rb.SourceIncident + "\n")
	}
	if rb.Cluster != "" {
		b.WriteString("cluster: " + rb.Cluster + "\n")
	}
	if len(rb.Tags) > 0 {
		b.WriteString("tags: " + strings.Join(rb.Tags, ", ") + "\n")
	}
	b.WriteString("created_at: " + rb.CreatedAt.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + rb.Title + "\n\n")
	b.WriteString("## 适用场景\n\n" + strings.TrimSpace(rb.Scenario) + "\n\n")
	b.WriteString("## 前置检查\n\n" + strings.TrimSpace(rb.Prechecks) + "\n\n")
	b.WriteString("## 步骤\n\n")
	for i, step := range rb.Steps {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, step.Kind, strings.TrimSpace(step.Text)))
	}
	b.WriteString("\n## 验证\n\n" + strings.TrimSpace(rb.Verification) + "\n\n")
	b.WriteString("## 风险\n\n" + strings.TrimSpace(rb.Risks) + "\n")
	return b.String()
}

func Slug(title string) string {
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
		return "runbook"
	}
	return regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
}

func parseFrontmatter(fm string, rb *Runbook) {
	for _, line := range strings.Split(fm, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "title":
			rb.Title = value
		case "source_incident":
			rb.SourceIncident = value
		case "cluster":
			rb.Cluster = value
		case "tags":
			rb.Tags = splitList(value)
		case "created_at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				rb.CreatedAt = t
			}
		}
	}
}

func firstHeading(md string) string {
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func markdownSections(md string) map[string]string {
	sections := make(map[string]string)
	current := ""
	var b strings.Builder
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimSpace(b.String())
			b.Reset()
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if current != "" {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	flush()
	return sections
}

func parseSteps(text string) []Step {
	var steps []Step
	re := regexp.MustCompile(`^\s*\d+\.\s*\[(read|confirm|destructive)\]\s*(.*)$`)
	for _, line := range strings.Split(text, "\n") {
		match := re.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		steps = append(steps, Step{Kind: StepKind(match[1]), Text: strings.TrimSpace(match[2])})
	}
	return steps
}

func splitList(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
