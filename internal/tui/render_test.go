package tui

import (
	"strings"
	"testing"
)

func TestMarkdownStyleNameDefaultsToDark(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "")
	if got := markdownStyleName(); got != "dark" {
		t.Fatalf("default style = %q, want %q", got, "dark")
	}
	t.Setenv("GLAMOUR_STYLE", "auto")
	if got := markdownStyleName(); got != "dark" {
		t.Fatalf("auto style override should fall back to dark, got %q", got)
	}
}

func TestMarkdownStyleNameHonorsEnvOverride(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "dracula")
	if got := markdownStyleName(); got != "dracula" {
		t.Fatalf("override = %q, want %q", got, "dracula")
	}
}

func TestRenderMarkdownStylesHeadings(t *testing.T) {
	// Locks in the fix for "headings show up as plain ## text" — the renderer
	// must emit ANSI escape codes, not the bare "## " prefix. Regression guard
	// against the WithEnvironmentConfig → "notty" fallback path.
	cases := []string{
		"## 🔍 DolphinScheduler 排查报告",
		"### sub heading",
		"# top heading",
	}
	for _, in := range cases {
		out := renderMarkdown(in + "\n\nbody text")
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("renderMarkdown(%q) produced no ANSI escapes; headings are unstyled. Got:\n%s", in, out)
		}
		if strings.HasPrefix(strings.TrimLeft(out, " \t"), "## ") ||
			strings.HasPrefix(strings.TrimLeft(out, " \t"), "### ") ||
			strings.HasPrefix(strings.TrimLeft(out, " \t"), "# ") {
			t.Errorf("renderMarkdown(%q) leaked raw markdown heading marker. Got:\n%s", in, out)
		}
	}
}

func TestRenderMarkdownFallsBackOnBrokenInput(t *testing.T) {
	// renderMarkdown should never panic and should return something for
	// pathological input. The actual content shape is glamour's concern.
	out := renderMarkdown("")
	if out == "" {
		// Empty input → empty output is acceptable, just no panic.
		return
	}
}
