package skills

import (
	"context"
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
	if reg.Skills[0].CachePath != "skills/repos/github.com/org/repo/main/skills/k8s-debug" {
		t.Fatalf("CachePath = %q", reg.Skills[0].CachePath)
	}
}
