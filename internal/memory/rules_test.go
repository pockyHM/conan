package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadRulesEmpty(t *testing.T) {
	rc, err := LoadRules(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Empty() {
		t.Fatal("expected empty rules for empty dir")
	}
}

func TestLoadRulesCoreOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Core Rules\nAlways check disk"), 0o644)

	rc, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Core == "" {
		t.Fatal("expected core rules")
	}
	if !strings.Contains(rc.Core, "Always check disk") {
		t.Fatalf("core = %q", rc.Core)
	}
}

func TestLoadRulesWithRuleFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "rules"), 0o755)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("Core"), 0o644)
	os.WriteFile(filepath.Join(dir, "rules", "production.md"), []byte("Prod rules"), 0o644)
	os.WriteFile(filepath.Join(dir, "rules", "security.md"), []byte("Security rules"), 0o644)

	rc, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.Rules) != 2 {
		t.Fatalf("got %d rule files, want 2", len(rc.Rules))
	}
	if rc.Rules["production.md"] != "Prod rules" {
		t.Fatalf("production.md = %q", rc.Rules["production.md"])
	}
}

func TestLoadRulesSkipsSymlinkedRuleFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rulesDir, "linked.md")); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rc.Rules["linked.md"]; ok {
		t.Fatalf("symlinked rule file should be skipped: %#v", rc.Rules)
	}
	if strings.Contains(rc.Format(), "outside rule") {
		t.Fatalf("symlinked rule content leaked into format: %s", rc.Format())
	}
}

func TestLoadRulesLimitsLargeFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(strings.Repeat("x", 5000)+"TAIL"), 0o600); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rc.Core, "TAIL") {
		t.Fatalf("large core memory was not truncated")
	}
	if len([]rune(rc.Core)) > maxMemoryContentRunes+20 {
		t.Fatalf("core memory length = %d, want bounded", len([]rune(rc.Core)))
	}
}

func TestRulesFormat(t *testing.T) {
	rc := &RulesContent{
		Core:  "Core rules",
		Rules: map[string]string{"tips.md": "Some tips"},
	}
	formatted := rc.Format()
	if !strings.Contains(formatted, "Core rules") {
		t.Fatal("missing core rules")
	}
	if !strings.Contains(formatted, "tips.md") {
		t.Fatal("missing rule file header")
	}
	if !strings.Contains(formatted, "Some tips") {
		t.Fatal("missing rule content")
	}
}

func TestRulesFormatSortsRuleNames(t *testing.T) {
	rc := &RulesContent{
		Core: "Core rules",
		Rules: map[string]string{
			"z-last.md":  "Last rule",
			"a-first.md": "First rule",
		},
	}

	formatted := rc.Format()

	first := strings.Index(formatted, "## a-first.md")
	last := strings.Index(formatted, "## z-last.md")
	if first == -1 || last == -1 {
		t.Fatalf("formatted rules missing expected headers:\n%s", formatted)
	}
	if first > last {
		t.Fatalf("rules not sorted by filename:\n%s", formatted)
	}
}

func TestEnsureMemoryDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	if err := EnsureMemoryDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("memory dir not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "rules")); err != nil {
		t.Fatal("rules dir not created")
	}
}

func TestEnsureMemoryDirCreatesStructuredMarkdownDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")

	if err := EnsureMemoryDir(root); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("memory root not created: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("memory root permissions = %o, want 700", got)
		}
	}

	for _, name := range []string{"rules", "clusters", "runbooks", "incidents"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("%s dir not created: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s exists but is not a dir", name)
		}
		if runtime.GOOS != "windows" {
			if got := info.Mode().Perm(); got != 0o700 {
				t.Fatalf("%s dir permissions = %o, want 700", name, got)
			}
		}
	}
}
