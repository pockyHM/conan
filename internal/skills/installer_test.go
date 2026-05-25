package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureFetcher struct {
	src string
}

func (f fixtureFetcher) Fetch(ctx context.Context, source RepoSource, dest string) error {
	return copyDir(f.src, dest)
}

type recordingFetcher struct {
	calls int
	err   error
	src   string
}

func (f *recordingFetcher) Fetch(ctx context.Context, source RepoSource, dest string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return copyDir(f.src, dest)
}

func TestNormalizeGitHubSource(t *testing.T) {
	for _, input := range []string{"github.com/org/repo", "https://github.com/org/repo", "org/repo"} {
		src, err := NormalizeGitHubSource(input, "main", "skills")
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if src.HostPath != "github.com/org/repo" {
			t.Fatalf("%s HostPath = %q", input, src.HostPath)
		}
		if src.CloneURL != "https://github.com/org/repo.git" {
			t.Fatalf("%s CloneURL = %q", input, src.CloneURL)
		}
		if src.Ref != "main" {
			t.Fatalf("%s Ref = %q", input, src.Ref)
		}
		if src.Path != "skills" {
			t.Fatalf("%s Path = %q", input, src.Path)
		}
	}
}

func TestNormalizeGitHubSourceEmptyRefUsesRepositoryDefault(t *testing.T) {
	src, err := NormalizeGitHubSource("org/repo", "", "skills")
	if err != nil {
		t.Fatal(err)
	}
	if src.Ref != "" {
		t.Fatalf("Ref = %q, want empty default branch ref", src.Ref)
	}
}

func TestInstallDiscoversSkillsAndWritesGlobalRegistry(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	body := "---\nname: k8s-debug\ndescription: debug k8s\n---\nbody"
	path := filepath.Join(fixture, "skills", "k8s-debug", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	installer := Installer{Home: home, Fetcher: fixtureFetcher{src: fixture}, MaxSkillFileBytes: 6000}
	src, err := NormalizeGitHubSource("org/repo", "main", "skills")
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(context.Background(), InstallRequest{Source: src, Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].Name != "k8s-debug" {
		t.Fatalf("installed = %#v", installed)
	}
	reg, err := LoadRegistry(GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Skills) != 1 || reg.Skills[0].Name != "k8s-debug" {
		t.Fatalf("registry = %#v", reg)
	}
	if !strings.HasPrefix(reg.Skills[0].CachePath, "skills/repos/") {
		t.Fatalf("CachePath = %q, want under skills/repos", reg.Skills[0].CachePath)
	}
	if !strings.HasPrefix(reg.Skills[0].CachePath, "skills/repos/github.com/org/repo/main-") {
		t.Fatalf("CachePath = %q", reg.Skills[0].CachePath)
	}
	if !strings.HasSuffix(reg.Skills[0].CachePath, "/skills/k8s-debug") {
		t.Fatalf("CachePath = %q", reg.Skills[0].CachePath)
	}
}

func TestInstallRejectsMaliciousHostPathBeforeSideEffects(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(home, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	fetcher := &recordingFetcher{src: t.TempDir()}
	installer := Installer{Home: home, Fetcher: fetcher, MaxSkillFileBytes: 6000}
	_, err := installer.Install(context.Background(), InstallRequest{
		Source: RepoSource{
			HostPath: "../outside",
			CloneURL: "https://github.com/org/repo.git",
			Ref:      "main",
			Path:     "skills",
		},
		Scope: ScopeGlobal,
	})
	if err == nil {
		t.Fatal("Install succeeded with malicious HostPath")
	}
	if fetcher.calls != 0 {
		t.Fatalf("Fetch called %d times, want 0", fetcher.calls)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel removed or inaccessible: %v", err)
	}
}

func TestInstallRejectsInvalidSourcePathBeforeFetch(t *testing.T) {
	home := t.TempDir()
	fetcher := &recordingFetcher{src: t.TempDir()}
	installer := Installer{Home: home, Fetcher: fetcher, MaxSkillFileBytes: 6000}
	src, err := NormalizeGitHubSource("org/repo", "main", "../skills")
	if err != nil {
		t.Fatal(err)
	}
	_, err = installer.Install(context.Background(), InstallRequest{Source: src, Scope: ScopeGlobal})
	if err == nil {
		t.Fatal("Install succeeded with invalid source path")
	}
	if fetcher.calls != 0 {
		t.Fatalf("Fetch called %d times, want 0", fetcher.calls)
	}
}

func TestInstallRejectsSkillMarkdownSymlinkOutsideRepo(t *testing.T) {
	fixture := t.TempDir()
	outside := t.TempDir()
	outsideSkill := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(outsideSkill, []byte("---\nname: escape\ndescription: outside\n---\noutside body"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture, "skills", "escape", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSkill, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := discoverSkills(fixture, "skills", 6000)
	if err == nil {
		t.Fatal("discoverSkills succeeded with SKILL.md symlink outside repo")
	}
	if !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("err = %v, want symlink escape error", err)
	}
}

func TestInstallFailedFetchPreservesExistingCache(t *testing.T) {
	home := t.TempDir()
	src, err := NormalizeGitHubSource("org/repo", "main", "skills")
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(home, "skills", "repos", "github.com", "org", "repo", sanitizeRef(src.Ref), "skills", "existing")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(cacheDir, "SKILL.md")
	if err := os.WriteFile(sentinel, []byte("---\nname: existing\ndescription: keep\n---\nbody"), 0644); err != nil {
		t.Fatal(err)
	}

	fetcher := &recordingFetcher{err: errors.New("network unavailable")}
	installer := Installer{Home: home, Fetcher: fetcher, MaxSkillFileBytes: 6000}
	_, err = installer.Install(context.Background(), InstallRequest{Source: src, Scope: ScopeGlobal})
	if err == nil {
		t.Fatal("Install succeeded with failed fetch")
	}
	if fetcher.calls != 1 {
		t.Fatalf("Fetch called %d times, want 1", fetcher.calls)
	}
	if got, err := os.ReadFile(sentinel); err != nil || !strings.Contains(string(got), "existing") {
		t.Fatalf("existing cache not preserved, data=%q err=%v", got, err)
	}
}

func TestInstallRefCacheAvoidsSanitizedCollisions(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	writeSkill(t, fixture, "skills", "collision", "first")

	installer := Installer{Home: home, Fetcher: fixtureFetcher{src: fixture}, MaxSkillFileBytes: 6000}
	srcSlash, err := NormalizeGitHubSource("org/repo", "feature/foo", "skills")
	if err != nil {
		t.Fatal(err)
	}
	first, err := installer.Install(context.Background(), InstallRequest{Source: srcSlash, Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	srcUnderscore, err := NormalizeGitHubSource("org/repo", "feature_foo", "skills")
	if err != nil {
		t.Fatal(err)
	}
	second, err := installer.Install(context.Background(), InstallRequest{Source: srcUnderscore, Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].CachePath == second[0].CachePath {
		t.Fatalf("CachePath collision: %q", first[0].CachePath)
	}
	if !strings.Contains(first[0].CachePath, "feature_foo-") || !strings.Contains(second[0].CachePath, "feature_foo-") {
		t.Fatalf("cache paths missing readable hashed ref: first=%q second=%q", first[0].CachePath, second[0].CachePath)
	}
}

func TestInstallWritesClusterRegistry(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	writeSkill(t, fixture, "skills", "cluster-debug", "cluster")

	installer := Installer{Home: home, Fetcher: fixtureFetcher{src: fixture}, MaxSkillFileBytes: 6000}
	src, err := NormalizeGitHubSource("org/repo", "main", "skills")
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(context.Background(), InstallRequest{Source: src, Scope: ScopeCluster, Cluster: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].Scope != ScopeCluster || installed[0].Cluster != "prod" {
		t.Fatalf("installed = %#v", installed)
	}
	reg, err := LoadRegistry(ClusterRegistryPath(home, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Skills) != 1 || reg.Skills[0].Name != "cluster-debug" {
		t.Fatalf("registry = %#v", reg)
	}
}

func TestInstallDuplicateSkillNameReplacesRegistryEntry(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	writeSkill(t, fixture, "skills", "one", "first")

	installer := Installer{Home: home, Fetcher: fixtureFetcher{src: fixture}, MaxSkillFileBytes: 6000}
	srcMain, err := NormalizeGitHubSource("org/repo", "main", "skills")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(context.Background(), InstallRequest{Source: srcMain, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(fixture, "skills")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, fixture, "skills", "one", "second")
	srcFeature, err := NormalizeGitHubSource("org/repo", "feature", "skills")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(context.Background(), InstallRequest{Source: srcFeature, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadRegistry(GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Skills) != 1 {
		t.Fatalf("registry = %#v", reg)
	}
	if reg.Skills[0].Ref != "feature" || !strings.Contains(reg.Skills[0].CachePath, "/feature-") {
		t.Fatalf("registry entry was not replaced: %#v", reg.Skills[0])
	}
}

func TestRemoveSkillFromGlobalRegistry(t *testing.T) {
	home := t.TempDir()
	reg := Registry{Skills: []RegistryEntry{
		{Name: "keep", Source: "github.com/org/repo", Ref: "main", Path: "skills/keep", CachePath: "skills/repos/github.com/org/repo/main/skills/keep"},
		{Name: "remove-me", Source: "github.com/org/repo", Ref: "main", Path: "skills/remove-me", CachePath: "skills/repos/github.com/org/repo/main/skills/remove-me"},
	}}
	if err := SaveRegistry(GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}

	removed, err := (Installer{Home: home}).Remove(RemoveRequest{Name: "remove-me", Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("removed = false, want true")
	}
	got, err := LoadRegistry(GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "keep" {
		t.Fatalf("registry = %#v", got)
	}
}

func TestUpdateSkillReinstallsFromRegistryEntry(t *testing.T) {
	home := t.TempDir()
	fixture := t.TempDir()
	writeSkill(t, fixture, "skills", "one", "updated")
	reg := Registry{Skills: []RegistryEntry{{
		Name: "one", Source: "github.com/org/repo", Ref: "main", Path: "skills/one", CachePath: "skills/repos/github.com/org/repo/main/skills/one",
	}}}
	if err := SaveRegistry(GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}

	installer := Installer{Home: home, Fetcher: fixtureFetcher{src: fixture}, MaxSkillFileBytes: 6000}
	updated, err := installer.Update(context.Background(), UpdateRequest{Name: "one", Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].Name != "one" || updated[0].Description != "updated" {
		t.Fatalf("updated = %#v", updated)
	}
	got, err := LoadRegistry(GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Description != "updated" {
		t.Fatalf("registry = %#v", got)
	}
}

func writeSkill(t *testing.T, root string, skillRoot string, name string, description string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody"
	path := filepath.Join(root, filepath.FromSlash(skillRoot), name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
