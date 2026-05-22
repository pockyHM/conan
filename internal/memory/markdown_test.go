package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMarkdownStoreRejectsPathTraversal(t *testing.T) {
	store := NewMarkdownStore(t.TempDir())

	if _, err := store.Read("../secret.md"); err == nil {
		t.Fatal("expected path traversal read to fail")
	}
	if err := store.PatchSection("../secret.md", "Rules", "content"); err == nil {
		t.Fatal("expected path traversal patch to fail")
	}
}

func TestMarkdownStoreRejectsAbsolutePathAndNonMarkdown(t *testing.T) {
	store := NewMarkdownStore(t.TempDir())

	if _, err := store.Read(filepath.Join(t.TempDir(), "secret.md")); err == nil {
		t.Fatal("expected absolute path read to fail")
	}
	if _, err := store.Read("notes.txt"); err == nil {
		t.Fatal("expected non-markdown read to fail")
	}
	if err := store.PatchSection("notes.txt", "Rules", "content"); err == nil {
		t.Fatal("expected non-markdown patch to fail")
	}
}

func TestMarkdownStoreRejectsUnsupportedMarkdownPaths(t *testing.T) {
	store := NewMarkdownStore(t.TempDir())

	for _, path := range []string{"scratch.md", "foo/bar.md"} {
		if _, err := store.Read(path); err == nil {
			t.Fatalf("expected read of %q to fail", path)
		}
		if err := store.PatchSection(path, "Rules", "content"); err == nil {
			t.Fatalf("expected patch of %q to fail", path)
		}
	}
}

func TestMarkdownStoreRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows systems")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	store := NewMarkdownStore(root)

	if _, err := store.Read("linked/secret.md"); err == nil {
		t.Fatal("expected symlink escape read to fail")
	}
	if err := store.PatchSection("linked/secret.md", "Rules", "content"); err == nil {
		t.Fatal("expected symlink escape patch to fail")
	}
}

func TestMarkdownStoreReadAllowedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("core memory"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMarkdownStore(root)

	got, err := store.Read("MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "core memory" {
		t.Fatalf("Read() = %q, want core memory", got)
	}
}

func TestMarkdownStoreReadLimitedBoundsFileContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewMarkdownStore(root)

	got, err := store.ReadLimited("MEMORY.md", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc" {
		t.Fatalf("ReadLimited() = %q, want abc", got)
	}
}

func TestMarkdownStorePatchSectionAddsAndReplacesSection(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	if err := store.PatchSection("rules/ops.md", "Restart Policy", "- restart only after health check"); err != nil {
		t.Fatal(err)
	}
	first, err := store.Read("rules/ops.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "## Restart Policy\n\n- restart only after health check") {
		t.Fatalf("missing section after first patch:\n%s", first)
	}

	if err := store.PatchSection("rules/ops.md", "Restart Policy", "- require approval for production"); err != nil {
		t.Fatal(err)
	}
	second, err := store.Read("rules/ops.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, "restart only after health check") {
		t.Fatalf("old section content was not replaced:\n%s", second)
	}
	if !strings.Contains(second, "## Restart Policy\n\n- require approval for production") {
		t.Fatalf("new section content missing:\n%s", second)
	}
}

func TestMarkdownStorePatchSectionRejectsOversizedContent(t *testing.T) {
	store := NewMarkdownStore(t.TempDir())

	err := store.PatchSection("rules/ops.md", "Restart Policy", strings.Repeat("x", 5000))
	if err == nil {
		t.Fatal("expected oversized patch content to fail")
	}
}

func TestMarkdownStorePatchSectionPreservesTopLevelAndIgnoresFencedHeadings(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)
	existing := strings.Join([]string{
		"# Current Top Level",
		"",
		"## Target",
		"",
		"old target",
		"```",
		"## Not A Boundary",
		"```",
		"still target",
		"",
		"# Next Top Level",
		"",
		"keep this",
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Join(root, "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules", "ops.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := store.PatchSection("rules/ops.md", "Target", "new target"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Read("rules/ops.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "old target") || strings.Contains(got, "still target") {
		t.Fatalf("old target section content was not fully replaced:\n%s", got)
	}
	for _, want := range []string{"# Current Top Level", "## Target\n\nnew target", "# Next Top Level", "keep this"} {
		if !strings.Contains(got, want) {
			t.Fatalf("patched document missing %q:\n%s", want, got)
		}
	}
}

func TestMarkdownStoreWriteNoteCreatesSluggedFile(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	path, err := store.WriteNote("incidents", "API OOM", "summary", "details", []string{"api", "oom"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "incidents/") || !strings.HasSuffix(path, "-api-oom.md") {
		t.Fatalf("path = %q, want incidents date slug", path)
	}
	content, err := store.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# API OOM", "summary", "details", "tags: api, oom"} {
		if !strings.Contains(content, want) {
			t.Fatalf("note missing %q:\n%s", want, content)
		}
	}
}

func TestMarkdownStoreWriteNoteCreatesUnicodeSluggedFile(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	path, err := store.WriteNote("incidents", "生产事故", "summary", "details", []string{"prod"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "incidents/") || !strings.HasSuffix(path, ".md") {
		t.Fatalf("path = %q, want incidents/*.md", path)
	}
	if !strings.Contains(path, "生产事故") {
		t.Fatalf("path = %q, want Unicode slug in filename", path)
	}
}

func TestMarkdownStoreWriteNoteCreatesUniqueSluggedFiles(t *testing.T) {
	root := t.TempDir()
	store := NewMarkdownStore(root)

	first, err := store.WriteNote("incidents", "API OOM", "summary", "first", []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.WriteNote("incidents", "API OOM", "summary", "second", []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("paths should differ for duplicate note title: %q", first)
	}
	firstContent, err := store.Read(first)
	if err != nil {
		t.Fatal(err)
	}
	secondContent, err := store.Read(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstContent, "first") || !strings.Contains(secondContent, "second") {
		t.Fatalf("duplicate notes were not preserved:\nfirst:\n%s\nsecond:\n%s", firstContent, secondContent)
	}
}

func TestMarkdownStoreUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not portable on Windows")
	}
	root := t.TempDir()
	store := NewMarkdownStore(root)

	if err := store.PatchSection("rules/ops.md", "Rules", "content"); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Join(root, "rules"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("directory mode = %o, want 0700", got)
	}
	fileInfo, err := os.Stat(filepath.Join(root, "rules", "ops.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
}
