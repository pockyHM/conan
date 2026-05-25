package skills

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	reg := Registry{Skills: []RegistryEntry{{
		Name: "k8s-debug", Description: "debug k8s", Source: "github.com/org/repo",
		Ref: "main", Path: "skills/k8s-debug", CachePath: "skills/repos/github.com/org/repo/main/skills/k8s-debug",
		InstalledAt: now,
	}}}

	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "k8s-debug" {
		t.Fatalf("registry = %#v", got)
	}
}

func TestLoadRegistryMissingFileReturnsEmpty(t *testing.T) {
	got, err := LoadRegistry(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 0 {
		t.Fatalf("Skills = %#v, want empty", got.Skills)
	}
}

func TestClusterRegistryPathSanitizesClusterSegment(t *testing.T) {
	home := t.TempDir()
	clustersRoot := filepath.Join(home, "clusters")

	for _, cluster := range []string{"../escape", "..", "prod/us-east", `prod\west`, ""} {
		got := ClusterRegistryPath(home, cluster)
		rel, err := filepath.Rel(clustersRoot, got)
		if err != nil {
			t.Fatalf("Rel(%q, %q): %v", clustersRoot, got, err)
		}
		if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			t.Fatalf("ClusterRegistryPath(%q) = %q, want path under %q", cluster, got, clustersRoot)
		}
		if filepath.Dir(filepath.Dir(got)) != clustersRoot {
			t.Fatalf("ClusterRegistryPath(%q) = %q, want direct cluster segment under %q", cluster, got, clustersRoot)
		}
	}
}
