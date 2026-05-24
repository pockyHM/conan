package runbook

import (
	"fmt"
	"strings"
)

type Preview struct {
	Title            string
	Cluster          string
	ReadSteps        []Step
	ConfirmSteps     []Step
	DestructiveSteps []Step
	Summary          string
}

func BuildPreview(rb Runbook) Preview {
	preview := Preview{Title: rb.Title, Cluster: rb.Cluster}
	for _, step := range rb.Steps {
		switch step.Kind {
		case StepRead:
			preview.ReadSteps = append(preview.ReadSteps, step)
		case StepConfirm:
			preview.ConfirmSteps = append(preview.ConfirmSteps, step)
		case StepDestructive:
			preview.DestructiveSteps = append(preview.DestructiveSteps, step)
		}
	}
	preview.Summary = fmt.Sprintf("%s cluster=%s read=%d confirm=%d destructive=%d", preview.Title, preview.Cluster, len(preview.ReadSteps), len(preview.ConfirmSteps), len(preview.DestructiveSteps))
	return preview
}

func RenderPreview(p Preview) string {
	var b strings.Builder
	b.WriteString("Runbook preview: " + p.Title + "\n")
	if p.Cluster != "" {
		b.WriteString("Cluster: " + p.Cluster + "\n")
	}
	b.WriteString(p.Summary + "\n\n")
	renderSteps(&b, "Read steps", p.ReadSteps)
	renderSteps(&b, "Confirm steps", p.ConfirmSteps)
	renderSteps(&b, "Destructive steps", p.DestructiveSteps)
	return b.String()
}

func renderSteps(b *strings.Builder, title string, steps []Step) {
	b.WriteString(title + "\n")
	if len(steps) == 0 {
		b.WriteString("- none\n\n")
		return
	}
	for _, step := range steps {
		b.WriteString("- " + step.Text + "\n")
	}
	b.WriteString("\n")
}
