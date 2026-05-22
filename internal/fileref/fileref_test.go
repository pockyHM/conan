package fileref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReferencesRecognizesPlainQuotedAndEscapedAt(t *testing.T) {
	refs := Parse(`read @README.md and @"docs/with spaces.md" but not @@literal`)
	if len(refs) != 2 {
		t.Fatalf("refs = %#v, want 2", refs)
	}
	if refs[0].Path != "README.md" {
		t.Fatalf("first path = %q", refs[0].Path)
	}
	if refs[1].Path != "docs/with spaces.md" {
		t.Fatalf("second path = %q", refs[1].Path)
	}
}

func TestLoadReferencesReadsFilesAndListsDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("a"), 0644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}

	loaded, err := Load(root, []Reference{{Path: "README.md"}, {Path: "docs/"}}, Limits{MaxFileBytes: 100, MaxTotalBytes: 1000})
	if err != nil {
		t.Fatalf("load refs: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded = %#v, want 2", loaded)
	}
	if loaded[0].Content != "hello file" {
		t.Fatalf("file content = %q", loaded[0].Content)
	}
	if !loaded[1].Directory || !strings.Contains(loaded[1].Content, "a.md") {
		t.Fatalf("directory context = %#v", loaded[1])
	}

	formatted := Format(loaded)
	if !strings.Contains(formatted, `<file path="README.md">`) || !strings.Contains(formatted, `<directory path="docs">`) {
		t.Fatalf("formatted context missing references:\n%s", formatted)
	}
}

func TestLoadReferencesRejectsOutsideWorkspaceAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := Load(root, []Reference{{Path: "../secret.txt"}}, Limits{}); err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("outside err = %v", err)
	}
	if _, err := Load(root, []Reference{{Path: "link.txt"}}, Limits{}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink err = %v", err)
	}
}

func TestLoadReferencesTruncatesLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte("0123456789"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	loaded, err := Load(root, []Reference{{Path: "big.txt"}}, Limits{MaxFileBytes: 4, MaxTotalBytes: 100})
	if err != nil {
		t.Fatalf("load refs: %v", err)
	}
	if loaded[0].Content != "0123\n[truncated]" || !loaded[0].Truncated {
		t.Fatalf("loaded = %#v", loaded[0])
	}
}
