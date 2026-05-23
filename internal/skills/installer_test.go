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
