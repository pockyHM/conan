package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestAutocompleteRenders(t *testing.T) {
	a := newAutocomplete().update("/")
	view := a.View(0)
	if view == "" {
		t.Fatal("autocomplete view should not be empty")
	}
	if !strings.Contains(view, "/help") {
		t.Fatalf("autocomplete view should contain /help:\n%s", view)
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
