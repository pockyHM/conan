package runbook

import (
	"strings"
	"testing"
)

func TestPreviewClassifiesSteps(t *testing.T) {
	rb := Runbook{
		Title:   "Nginx 502 快速诊断",
		Cluster: "prod",
		Steps: []Step{
			{Kind: StepRead, Text: "read status"},
			{Kind: StepConfirm, Text: "restart nginx"},
			{Kind: StepDestructive, Text: "delete bad pod"},
		},
	}

	preview := BuildPreview(rb)

	if len(preview.ReadSteps) != 1 || len(preview.ConfirmSteps) != 1 || len(preview.DestructiveSteps) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	for _, want := range []string{"Nginx 502", "prod", "read=1", "confirm=1", "destructive=1"} {
		if !strings.Contains(preview.Summary, want) {
			t.Fatalf("summary missing %q: %s", want, preview.Summary)
		}
	}
	rendered := RenderPreview(preview)
	for _, want := range []string{"Read steps", "Confirm steps", "Destructive steps", "restart nginx"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered preview missing %q:\n%s", want, rendered)
		}
	}
}
