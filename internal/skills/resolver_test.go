package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSkillBody(t *testing.T, root string, rel string, body string) {
	t.Helper()
	path := filepath.Join(root, rel, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveVisibleSkillsClusterOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	writeSkillBody(t, home, "global/k8s", "---\nname: k8s-debug\ndescription: global k8s\n---\nglobal body")
	writeSkillBody(t, home, "cluster/k8s", "---\nname: k8s-debug\ndescription: cluster k8s\n---\ncluster body")
	now := time.Now().UTC()

	global := Registry{Skills: []RegistryEntry{{
		Name: "k8s-debug", Description: "global k8s", CachePath: "global/k8s", InstalledAt: now,
	}}}
	cluster := Registry{Skills: []RegistryEntry{{
		Name: "k8s-debug", Description: "cluster k8s", CachePath: "cluster/k8s", InstalledAt: now.Add(time.Second),
	}}}

	got, warnings, err := ResolveVisible(home, "prod", global, cluster, ResolveOptions{MaxSkillFileBytes: 6000, MaxVisibleSkills: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Scope != ScopeCluster || got[0].Description != "cluster k8s" || got[0].Body != "cluster body" {
		t.Fatalf("resolved skill = %#v", got[0])
	}
}

func TestBuildSkillIndex(t *testing.T) {
	index := BuildSkillIndex([]Skill{
		{Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Scope: ScopeCluster, Cluster: "prod"},
		{Name: "incident-report", Description: "Write incident reports.", Scope: ScopeGlobal},
	}, 800)
	if !strings.Contains(index, "Available skills:") {
		t.Fatalf("index missing header: %q", index)
	}
	if !strings.Contains(index, "scope=cluster:prod") || !strings.Contains(index, "scope=global") {
		t.Fatalf("index missing scopes: %q", index)
	}
	if !strings.Contains(index, "Call skill_read") {
		t.Fatalf("index missing tool guidance: %q", index)
	}
}
