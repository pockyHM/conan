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
	writeSkillBody(t, home, "skills/repos/global/k8s", "---\nname: k8s-debug\ndescription: global k8s\n---\nglobal body")
	writeSkillBody(t, home, "skills/repos/cluster/k8s", "---\nname: k8s-debug\ndescription: cluster k8s\n---\ncluster body")
	now := time.Now().UTC()

	global := Registry{Skills: []RegistryEntry{{
		Name: "k8s-debug", Description: "global k8s", CachePath: "skills/repos/global/k8s", InstalledAt: now,
	}}}
	cluster := Registry{Skills: []RegistryEntry{{
		Name: "k8s-debug", Description: "cluster k8s", CachePath: "skills/repos/cluster/k8s", InstalledAt: now.Add(time.Second),
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

func TestResolveVisibleRejectsCachePathOutsideRepos(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	writeSkillBody(t, parent, "outside", "---\nname: escape\ndescription: outside\n---\noutside body")

	global := Registry{Skills: []RegistryEntry{{
		Name: "escape", Description: "outside", CachePath: "../outside", InstalledAt: time.Now().UTC(),
	}}}

	if _, _, err := ResolveVisible(home, "prod", global, Registry{}, ResolveOptions{MaxSkillFileBytes: 6000}); err == nil {
		t.Fatal("ResolveVisible succeeded with cache path outside skills/repos, want error")
	}
}

func TestResolveVisibleSameScopeDuplicateNewestWins(t *testing.T) {
	home := t.TempDir()
	writeSkillBody(t, home, "skills/repos/newer/dupe", "---\nname: dupe\ndescription: newer\n---\nnewer body")
	writeSkillBody(t, home, "skills/repos/older/dupe", "---\nname: dupe\ndescription: older\n---\nolder body")
	now := time.Now().UTC()
	global := Registry{Skills: []RegistryEntry{
		{Name: "dupe", Description: "newer", CachePath: "skills/repos/newer/dupe", InstalledAt: now.Add(time.Minute)},
		{Name: "dupe", Description: "older", CachePath: "skills/repos/older/dupe", InstalledAt: now},
	}}

	got, warnings, err := ResolveVisible(home, "prod", global, Registry{}, ResolveOptions{MaxSkillFileBytes: 6000, MaxVisibleSkills: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Description != "newer" || got[0].Body != "newer body" {
		t.Fatalf("resolved skill = %#v, want newest duplicate", got[0])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "duplicate skill dupe") {
		t.Fatalf("warnings = %#v, want duplicate warning", warnings)
	}
}

func TestResolveVisibleMaxVisibleSkillsTrimsResult(t *testing.T) {
	home := t.TempDir()
	writeSkillBody(t, home, "skills/repos/global/alpha", "---\nname: alpha\ndescription: alpha\n---\nalpha body")
	writeSkillBody(t, home, "skills/repos/global/bravo", "---\nname: bravo\ndescription: bravo\n---\nbravo body")
	writeSkillBody(t, home, "skills/repos/cluster/charlie", "---\nname: charlie\ndescription: charlie\n---\ncharlie body")
	now := time.Now().UTC()
	global := Registry{Skills: []RegistryEntry{
		{Name: "alpha", Description: "alpha", CachePath: "skills/repos/global/alpha", InstalledAt: now},
		{Name: "bravo", Description: "bravo", CachePath: "skills/repos/global/bravo", InstalledAt: now},
	}}
	cluster := Registry{Skills: []RegistryEntry{{
		Name: "charlie", Description: "charlie", CachePath: "skills/repos/cluster/charlie", InstalledAt: now,
	}}}

	got, warnings, err := ResolveVisible(home, "prod", global, cluster, ResolveOptions{MaxSkillFileBytes: 6000, MaxVisibleSkills: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "visible skills limited to 2 of 3") {
		t.Fatalf("warnings = %#v, want max-visible warning", warnings)
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

func TestBuildSkillIndexHonorsCharBudget(t *testing.T) {
	skills := []Skill{
		{Name: "k8s-debug", Description: strings.Repeat("Diagnose Kubernetes failures. ", 20), Scope: ScopeCluster, Cluster: "prod"},
	}

	for _, budget := range []int{1, 8, 32, 120} {
		index := BuildSkillIndex(skills, budget)
		if len([]rune(index)) > budget {
			t.Fatalf("len(BuildSkillIndex(..., %d)) = %d, want <= budget; index=%q", budget, len([]rune(index)), index)
		}
	}
}

func TestBuildSkillIndexPreservesGuidanceForPracticalBudget(t *testing.T) {
	index := BuildSkillIndex([]Skill{
		{Name: "k8s-debug", Description: strings.Repeat("Diagnose Kubernetes failures. ", 20), Scope: ScopeCluster, Cluster: "prod"},
	}, 220)

	if len([]rune(index)) > 220 {
		t.Fatalf("len(index) = %d, want <= 220", len([]rune(index)))
	}
	if !strings.Contains(index, "Call skill_read") {
		t.Fatalf("index missing tool guidance: %q", index)
	}
}
