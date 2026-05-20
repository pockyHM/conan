package memory

import (
	"os"
	"path/filepath"
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
