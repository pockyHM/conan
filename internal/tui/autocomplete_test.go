package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAutocompleteShowsOnSlash(t *testing.T) {
	a := newAutocomplete().update("/")
	if !a.visible {
		t.Fatal("autocomplete should be visible after /")
	}
}

func TestAutocompleteHidesOnNonSlash(t *testing.T) {
	a := newAutocomplete().update("hello")
	if a.visible {
		t.Fatal("autocomplete should not be visible for non-slash input")
	}
}

func TestAutocompleteHidesAfterSpace(t *testing.T) {
	a := newAutocomplete().update("/help ")
	if a.visible {
		t.Fatal("autocomplete should hide after space")
	}
}

func TestAutocompleteFilters(t *testing.T) {
	a := newAutocomplete().update("/cl")
	filtered := a.filtered()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 matches (clear, cluster), got %d: %v", len(filtered), filtered)
	}
	if filtered[0].Name != "clear" || filtered[1].Name != "cluster" {
		t.Fatalf("expected clear+cluster, got %v", filtered)
	}
}

func TestAutocompleteNoMatch(t *testing.T) {
	a := newAutocomplete().update("/zzz")
	filtered := a.filtered()
	if len(filtered) != 0 {
		t.Fatalf("expected no matches, got %v", filtered)
	}
}

func TestAutocompleteNavigation(t *testing.T) {
	a := newAutocomplete().update("/")
	if a.selected != 0 {
		t.Fatal("should start at 0")
	}
	a = a.moveDown()
	if a.selected != 1 {
		t.Fatalf("after down: selected=%d, want 1", a.selected)
	}
	a = a.moveUp()
	if a.selected != 0 {
		t.Fatalf("after up: selected=%d, want 0", a.selected)
	}
}

func TestAutocompleteCompletion(t *testing.T) {
	a := newAutocomplete().update("/")
	a = a.moveDown()
	comp := a.completion()
	if !strings.HasPrefix(comp, "/") {
		t.Fatalf("completion = %q, should start with /", comp)
	}
}

func TestAutocompleteExactPrefixWins(t *testing.T) {
	a := newAutocomplete().update("/skill")
	if got := a.completion(); got != "/skill " {
		t.Fatalf("completion = %q, want /skill", got)
	}
}

func TestAutocompleteRenders(t *testing.T) {
	a := newAutocomplete().update("/")
	view := a.View(0)
	if view == "" {
		t.Fatal("autocomplete view should not be empty")
	}
	if !strings.Contains(view, "/help") {
		t.Fatalf("autocomplete view should contain /help:\n%s", view)
	}
	if !strings.Contains(view, "Legend:") || !strings.Contains(view, "Built-in") || !strings.Contains(view, "System") || !strings.Contains(view, "Skill") {
		t.Fatalf("autocomplete view should contain category legend:\n%s", view)
	}
}

func TestAutocompleteTruncatesLongCommandRowsToWidth(t *testing.T) {
	a := newAutocomplete().withCommands([]commandInfo{{
		Name:        "very-long-skill-name-that-would-overflow-the-terminal-window",
		Description: "Skill: " + strings.Repeat("diagnose deeply nested infrastructure state ", 4),
		ArgHint:     "[arguments]",
		Skill:       true,
	}}).update("/very")

	view := a.View(50)
	if view == "" {
		t.Fatal("autocomplete view should not be empty")
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 50 {
			t.Fatalf("autocomplete line width = %d, want <= 50:\n%s", got, view)
		}
	}
	if strings.Contains(view, "very-long-skill-name-that-would-overflow") {
		t.Fatalf("autocomplete should truncate long skill command rows:\n%s", view)
	}
	if got := a.completion(); got != "/very-long-skill-name-that-would-overflow-the-terminal-window " {
		t.Fatalf("completion = %q, want full command despite truncated view", got)
	}
}

func TestAutocompleteOrdersBuiltinCommandsBeforeSkills(t *testing.T) {
	a := newAutocomplete().withCommands([]commandInfo{
		{Name: "skills", Description: "builtin", Category: commandCategoryBuiltin},
		{Name: "sys-debug", Description: "system", Category: commandCategorySystem},
		{Name: "skill", Description: "builtin", Category: commandCategoryBuiltin},
		{Name: "sre-debug", Description: "Skill: debug", Skill: true, Category: commandCategorySkill},
		{Name: "s", Description: "Skill: exact", Skill: true, Category: commandCategorySkill},
	}).update("/s")

	filtered := a.filtered()
	if len(filtered) != 5 {
		t.Fatalf("filtered = %#v, want 5 candidates", filtered)
	}
	if filtered[0].normalizedCategory() != commandCategoryBuiltin || filtered[1].normalizedCategory() != commandCategoryBuiltin {
		t.Fatalf("built-in commands should be listed first: %#v", filtered)
	}
	if filtered[2].normalizedCategory() != commandCategorySystem {
		t.Fatalf("system commands should be listed after built-ins: %#v", filtered)
	}
	if filtered[3].normalizedCategory() != commandCategorySkill || filtered[4].normalizedCategory() != commandCategorySkill {
		t.Fatalf("skills should be listed last: %#v", filtered)
	}
}

func TestAutocompleteCommandLegendUsesCategoryColors(t *testing.T) {
	a := newAutocomplete().withCommands([]commandInfo{
		{Name: "help", Description: "builtin", Category: commandCategoryBuiltin},
		{Name: "nodes", Description: "system", Category: commandCategorySystem},
		{Name: "k8s-debug", Description: "Skill: debug", Skill: true, Category: commandCategorySkill},
	}).update("/")

	lines := a.commandLines(a.filtered(), 80)
	if len(lines) < 4 {
		t.Fatalf("command lines = %#v, want legend plus candidates", lines)
	}
	if !strings.Contains(lines[0], "Built-in") || !strings.Contains(lines[0], "System") || !strings.Contains(lines[0], "Skill") {
		t.Fatalf("legend missing categories: %q", lines[0])
	}
	if got := lines[1]; !strings.Contains(got, "/help") {
		t.Fatalf("first candidate should be built-in help, got %q", got)
	}
	if got := lines[2]; !strings.Contains(got, "/nodes") {
		t.Fatalf("second candidate should be system nodes, got %q", got)
	}
	if got := lines[3]; !strings.Contains(got, "/k8s-debug") {
		t.Fatalf("third candidate should be skill, got %q", got)
	}
}

func TestAutocompleteEmptyForNoMatch(t *testing.T) {
	a := newAutocomplete().update("/zzz")
	view := a.View(0)
	if view != "" {
		t.Fatalf("autocomplete should be empty for no match, got:\n%s", view)
	}
}

func TestAutocompleteShowsFileRefsAfterAt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "tui"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	a := newAutocomplete().updateWithRoot("please read @RE", root)
	if !a.visible {
		t.Fatal("autocomplete should be visible for @ file ref")
	}
	if got := a.completion(); got != "please read @README.md " {
		t.Fatalf("completion = %q", got)
	}
	view := a.View(80)
	if !strings.Contains(view, "@README.md") {
		t.Fatalf("view missing file candidate:\n%s", view)
	}
}

func TestAutocompleteCompletesDirectoriesWithSlash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "tui"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	a := newAutocomplete().updateWithRoot("inspect @int", root)
	if got := a.completion(); got != "inspect @internal/" {
		t.Fatalf("completion = %q", got)
	}

	a = newAutocomplete().updateWithRoot("inspect @internal/t", root)
	if got := a.completion(); got != "inspect @internal/tui/" {
		t.Fatalf("nested completion = %q", got)
	}
}

func TestAutocompleteIgnoresEscapedAt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	a := newAutocomplete().updateWithRoot("literal @@RE", root)
	if a.visible {
		t.Fatal("autocomplete should ignore escaped @@ references")
	}
}
