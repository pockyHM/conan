# Skills System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add installable Conan skills from public GitHub repositories with global and cluster scope, slash invocation, deduplication, and model-driven `skill_read` selection.

**Architecture:** Create an `internal/skills` package that owns parsing, validation, registry persistence, repository fetching, visible skill resolution, and the model tool. Wire it into CLI commands in `cmd/conan/main.go` and into the TUI through `ModelConfig`, `availableToolDefs()`, `buildSystemPromptWithMemory()`, slash command parsing, and autocomplete. Keep model context short by injecting only a skill index and exposing full skill content through `skill_read`.

**Tech Stack:** Go, Cobra, Bubble Tea TUI, YAML via `gopkg.in/yaml.v3`, existing `llm.ToolDef`/tool dispatch patterns, local git command execution behind a testable interface.

---

## File Structure

- Create `internal/skills/types.go`: shared skill, registry, source, config, and scope types.
- Create `internal/skills/parser.go`: `SKILL.md` frontmatter parsing and validation.
- Create `internal/skills/parser_test.go`: parser unit tests.
- Create `internal/skills/registry.go`: global/cluster registry read/write and atomic saves.
- Create `internal/skills/registry_test.go`: registry and dedup tests.
- Create `internal/skills/resolver.go`: visible skill resolution for `global + current cluster`.
- Create `internal/skills/resolver_test.go`: scope priority and index rendering tests.
- Create `internal/skills/installer.go`: GitHub URL normalization, repo fetch abstraction, skill discovery, install/update/remove.
- Create `internal/skills/installer_test.go`: installer tests using fixture directories.
- Create `internal/skills/tools.go`: `skill_read` tool definition and handler.
- Create `internal/skills/tools_test.go`: `skill_read` behavior tests.
- Modify `pkg/configschema/config.go`: add `SkillsConfig` to `GlobalConfig`.
- Modify `internal/config/loader.go`: set skills defaults.
- Modify `internal/config/loader_test.go`: config default coverage.
- Modify `cmd/conan/main.go`: add `conan skills` command group and pass visible skills into TUI.
- Modify `cmd/conan/main_test.go`: CLI command tests.
- Modify `internal/tui/command.go`: parse `/skills` and `/skill`.
- Modify `internal/tui/command_test.go`: slash command tests.
- Modify `internal/tui/autocomplete.go`: show `/skills`, `/skill`, and skill name completions.
- Modify `internal/tui/model.go`: store visible skills/config, inject skill index, expose and dispatch `skill_read`, handle skill slash invocation.
- Modify `internal/tui/metatools.go`: add `metaToolSkillRead` constant if the tool is defined on the TUI side, or use `skills.ToolName`.
- Modify `internal/tui/model_test.go`: model prompt/tool/dispatch tests.

## Task 1: Skills Config Defaults

**Files:**
- Modify: `pkg/configschema/config.go`
- Modify: `internal/config/loader.go`
- Test: `internal/config/loader_test.go`

- [ ] **Step 1: Write the failing config default test**

Add this test to `internal/config/loader_test.go`:

```go
func TestLoadGlobalAppliesSkillsDefaults(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	cfg, err := loader.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Skills.Enabled {
		t.Fatal("Skills.Enabled = false, want true")
	}
	if cfg.Skills.IndexTokenBudget != 800 {
		t.Fatalf("IndexTokenBudget = %d, want 800", cfg.Skills.IndexTokenBudget)
	}
	if cfg.Skills.MaxSkillChars != 6000 {
		t.Fatalf("MaxSkillChars = %d, want 6000", cfg.Skills.MaxSkillChars)
	}
	if cfg.Skills.MaxVisibleSkills != 50 {
		t.Fatalf("MaxVisibleSkills = %d, want 50", cfg.Skills.MaxVisibleSkills)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/config -run TestLoadGlobalAppliesSkillsDefaults -count=1
```

Expected: FAIL because `GlobalConfig` has no `Skills` field.

- [ ] **Step 3: Add the config schema**

In `pkg/configschema/config.go`, add `Skills` to `GlobalConfig`:

```go
type GlobalConfig struct {
	DefaultModel   string            `yaml:"default_model"`
	DefaultCluster string            `yaml:"default_cluster"`
	UILanguage     string            `yaml:"ui_language,omitempty"`
	Models         []ModelConfig     `yaml:"models"`
	Security       SecurityConfig    `yaml:"security"`
	Memory         MemoryConfig      `yaml:"memory"`
	Logging        LoggingConfig     `yaml:"logging"`
	AgentDeploy    AgentDeployConfig `yaml:"agent_deploy"`
	Subagents      SubagentConfig    `yaml:"subagents"`
	Vision         VisionConfig      `yaml:"vision"`
	Skills         SkillsConfig      `yaml:"skills"`
}
```

Add the struct near `VisionConfig`:

```go
type SkillsConfig struct {
	Enabled          bool `yaml:"enabled"`
	IndexTokenBudget int  `yaml:"index_token_budget"`
	MaxSkillChars    int  `yaml:"max_skill_chars"`
	MaxVisibleSkills int  `yaml:"max_visible_skills"`
}
```

- [ ] **Step 4: Add loader defaults**

In `internal/config/loader.go`, append to `applyGlobalDefaults`:

```go
	if cfg.Skills.IndexTokenBudget == 0 {
		cfg.Skills.IndexTokenBudget = 800
	}
	if cfg.Skills.MaxSkillChars == 0 {
		cfg.Skills.MaxSkillChars = 6000
	}
	if cfg.Skills.MaxVisibleSkills == 0 {
		cfg.Skills.MaxVisibleSkills = 50
	}
	cfg.Skills.Enabled = true
```

This first version always enables skills when the config is loaded. A later compatibility pass can distinguish explicit `enabled: false` by tracking YAML fields if the product needs a hard disable.

- [ ] **Step 5: Run the config test**

Run:

```bash
go test ./internal/config -run TestLoadGlobalAppliesSkillsDefaults -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/configschema/config.go internal/config/loader.go internal/config/loader_test.go
git commit -m "feat: add skills config defaults"
```

## Task 2: Parse And Validate SKILL.md

**Files:**
- Create: `internal/skills/types.go`
- Create: `internal/skills/parser.go`
- Test: `internal/skills/parser_test.go`

- [ ] **Step 1: Write parser tests**

Create `internal/skills/parser_test.go`:

```go
package skills

import (
	"strings"
	"testing"
)

func TestParseSkillMarkdown(t *testing.T) {
	raw := []byte(`---
name: k8s-debug
description: Use when diagnosing Kubernetes failures.
version: 1.0.0
tags:
  - k8s
  - ops
max_chars: 1200
---

# K8s Debug

Inspect pods, events, and logs before changing resources.
`)

	skill, err := ParseSkillMarkdown("skills/k8s-debug/SKILL.md", raw, 6000)
	if err != nil {
		t.Fatal(err)
	}

	if skill.Name != "k8s-debug" {
		t.Fatalf("Name = %q", skill.Name)
	}
	if skill.Description != "Use when diagnosing Kubernetes failures." {
		t.Fatalf("Description = %q", skill.Description)
	}
	if skill.Version != "1.0.0" {
		t.Fatalf("Version = %q", skill.Version)
	}
	if len(skill.Tags) != 2 || skill.Tags[0] != "k8s" || skill.Tags[1] != "ops" {
		t.Fatalf("Tags = %#v", skill.Tags)
	}
	if skill.MaxChars != 1200 {
		t.Fatalf("MaxChars = %d", skill.MaxChars)
	}
	if !strings.Contains(skill.Body, "Inspect pods") {
		t.Fatalf("Body missing markdown: %q", skill.Body)
	}
}

func TestParseSkillMarkdownRequiresNameAndDescription(t *testing.T) {
	_, err := ParseSkillMarkdown("SKILL.md", []byte("---\nname: \n---\nbody"), 6000)
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Fatalf("err = %v, want description validation", err)
	}
}

func TestParseSkillMarkdownRejectsOversizedFile(t *testing.T) {
	raw := []byte("---\nname: x\ndescription: y\n---\n" + strings.Repeat("a", 20))
	_, err := ParseSkillMarkdown("SKILL.md", raw, 10)
	if err == nil {
		t.Fatal("err = nil, want size error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want too large", err)
	}
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run:

```bash
go test ./internal/skills -run ParseSkill -count=1
```

Expected: FAIL because `internal/skills` does not exist.

- [ ] **Step 3: Create shared types**

Create `internal/skills/types.go`:

```go
package skills

import "time"

const (
	ScopeGlobal  = "global"
	ScopeCluster = "cluster"
	ToolName     = "skill_read"
)

type Skill struct {
	Name        string
	Description string
	Version     string
	Tags        []string
	MaxChars    int
	Body        string
	Path        string
	Scope       string
	Cluster     string
	Source      string
	Ref         string
	CachePath   string
	InstalledAt time.Time
}

type Registry struct {
	Skills []RegistryEntry `yaml:"skills"`
}

type RegistryEntry struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description,omitempty"`
	Version     string    `yaml:"version,omitempty"`
	Tags        []string  `yaml:"tags,omitempty"`
	MaxChars    int       `yaml:"max_chars,omitempty"`
	Source      string    `yaml:"source"`
	Ref         string    `yaml:"ref"`
	Path        string    `yaml:"path"`
	CachePath   string    `yaml:"cache_path"`
	InstalledAt time.Time `yaml:"installed_at"`
}
```

- [ ] **Step 4: Implement parser**

Create `internal/skills/parser.go`:

```go
package skills

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Tags        []string `yaml:"tags"`
	MaxChars    int      `yaml:"max_chars"`
}

func ParseSkillMarkdown(path string, data []byte, maxFileBytes int) (Skill, error) {
	if maxFileBytes > 0 && len(data) > maxFileBytes {
		return Skill{}, fmt.Errorf("%s too large: %d bytes exceeds %d", path, len(data), maxFileBytes)
	}
	meta, body, err := splitFrontmatter(data)
	if err != nil {
		return Skill{}, fmt.Errorf("parse %s: %w", path, err)
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal(meta, &fm); err != nil {
		return Skill{}, fmt.Errorf("parse %s frontmatter: %w", path, err)
	}
	fm.Name = strings.TrimSpace(fm.Name)
	fm.Description = strings.TrimSpace(fm.Description)
	if fm.Name == "" {
		return Skill{}, fmt.Errorf("%s missing required name", path)
	}
	if fm.Description == "" {
		return Skill{}, fmt.Errorf("%s missing required description", path)
	}
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Version:     strings.TrimSpace(fm.Version),
		Tags:        append([]string(nil), fm.Tags...),
		MaxChars:    fm.MaxChars,
		Body:        strings.TrimSpace(string(body)),
		Path:        path,
	}, nil
}

func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	trimmed := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(trimmed, []byte("---\n")) {
		return nil, nil, fmt.Errorf("missing YAML frontmatter")
	}
	rest := trimmed[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, nil, fmt.Errorf("unterminated YAML frontmatter")
	}
	return rest[:end], rest[end+len("\n---\n"):], nil
}
```

- [ ] **Step 5: Run parser tests**

Run:

```bash
go test ./internal/skills -run ParseSkill -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/skills/types.go internal/skills/parser.go internal/skills/parser_test.go
git commit -m "feat: parse skill markdown"
```

## Task 3: Registry Persistence And Visible Skill Resolution

**Files:**
- Create: `internal/skills/registry.go`
- Create: `internal/skills/resolver.go`
- Test: `internal/skills/registry_test.go`
- Test: `internal/skills/resolver_test.go`

- [ ] **Step 1: Write registry tests**

Create `internal/skills/registry_test.go`:

```go
package skills

import (
	"path/filepath"
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
```

- [ ] **Step 2: Write resolver tests**

Create `internal/skills/resolver_test.go`:

```go
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./internal/skills -run 'Registry|Resolve|BuildSkillIndex' -count=1
```

Expected: FAIL because registry and resolver functions are not defined.

- [ ] **Step 4: Implement registry persistence**

Create `internal/skills/registry.go`:

```go
package skills

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func LoadRegistry(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{}, nil
		}
		return Registry{}, err
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return Registry{}, err
	}
	return reg, nil
}

func SaveRegistry(path string, reg Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&reg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func GlobalRegistryPath(home string) string {
	return filepath.Join(home, "skills", "registry.yaml")
}

func ClusterRegistryPath(home string, cluster string) string {
	return filepath.Join(home, "clusters", cluster, "skills.yaml")
}
```

- [ ] **Step 5: Implement resolver and index rendering**

Create `internal/skills/resolver.go`:

```go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ResolveOptions struct {
	MaxSkillFileBytes int
	MaxVisibleSkills  int
	IndexCharBudget   int
}

func ResolveVisible(home string, cluster string, global Registry, clusterReg Registry, opts ResolveOptions) ([]Skill, []string, error) {
	var warnings []string
	byName := make(map[string]Skill)
	add := func(entry RegistryEntry, scope string) error {
		path := filepath.Join(home, entry.CachePath, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill %s: %w", entry.Name, err)
		}
		skill, err := ParseSkillMarkdown(path, data, opts.MaxSkillFileBytes)
		if err != nil {
			return err
		}
		skill.Scope = scope
		skill.Cluster = ""
		if scope == ScopeCluster {
			skill.Cluster = cluster
		}
		skill.Source = entry.Source
		skill.Ref = entry.Ref
		skill.CachePath = entry.CachePath
		skill.InstalledAt = entry.InstalledAt
		if existing, ok := byName[skill.Name]; ok && existing.Scope == scope {
			warnings = append(warnings, fmt.Sprintf("duplicate skill %s in %s scope; newest install wins", skill.Name, scope))
			if existing.InstalledAt.After(skill.InstalledAt) {
				return nil
			}
		}
		byName[skill.Name] = skill
		return nil
	}
	for _, entry := range global.Skills {
		if err := add(entry, ScopeGlobal); err != nil {
			return nil, nil, err
		}
	}
	for _, entry := range clusterReg.Skills {
		if err := add(entry, ScopeCluster); err != nil {
			return nil, nil, err
		}
	}
	result := make([]Skill, 0, len(byName))
	for _, skill := range byName {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope != result[j].Scope {
			return result[i].Scope == ScopeCluster
		}
		return result[i].Name < result[j].Name
	})
	if opts.MaxVisibleSkills > 0 && len(result) > opts.MaxVisibleSkills {
		warnings = append(warnings, fmt.Sprintf("visible skills limited to %d of %d", opts.MaxVisibleSkills, len(result)))
		result = result[:opts.MaxVisibleSkills]
	}
	return result, warnings, nil
}

func BuildSkillIndex(skills []Skill, charBudget int) string {
	if len(skills) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "Available skills:")
	for _, skill := range skills {
		scope := "global"
		if skill.Scope == ScopeCluster {
			scope = "cluster:" + skill.Cluster
		}
		lines = append(lines, fmt.Sprintf("- %s: %s. scope=%s", skill.Name, skill.Description, scope))
	}
	lines = append(lines, "")
	lines = append(lines, "Call skill_read when one of these skills would materially improve the answer.")
	index := strings.Join(lines, "\n")
	if charBudget > 0 && len([]rune(index)) > charBudget {
		runes := []rune(index)
		return string(runes[:charBudget]) + "\n[truncated]"
	}
	return index
}
```

- [ ] **Step 6: Run registry and resolver tests**

Run:

```bash
go test ./internal/skills -run 'Registry|Resolve|BuildSkillIndex' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/skills/registry.go internal/skills/resolver.go internal/skills/registry_test.go internal/skills/resolver_test.go
git commit -m "feat: resolve visible skills"
```

## Task 4: Install Skills From Repository Sources

**Files:**
- Create: `internal/skills/installer.go`
- Test: `internal/skills/installer_test.go`

- [ ] **Step 1: Write installer tests**

Create `internal/skills/installer_test.go`:

```go
package skills

import (
	"context"
	"os"
	"path/filepath"
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
}
```

- [ ] **Step 2: Run installer tests to verify they fail**

Run:

```bash
go test ./internal/skills -run 'NormalizeGitHubSource|InstallDiscovers' -count=1
```

Expected: FAIL because installer types and functions are missing.

- [ ] **Step 3: Implement installer**

Create `internal/skills/installer.go`:

```go
package skills

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RepoSource struct {
	Input    string
	HostPath string
	CloneURL string
	Ref      string
	Path     string
}

type RepoFetcher interface {
	Fetch(ctx context.Context, source RepoSource, dest string) error
}

type GitFetcher struct{}

func (GitFetcher) Fetch(ctx context.Context, source RepoSource, dest string) error {
	args := []string{"clone", "--depth", "1"}
	if source.Ref != "" {
		args = append(args, "--branch", source.Ref)
	}
	args = append(args, source.CloneURL, dest)
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

type Installer struct {
	Home              string
	Fetcher           RepoFetcher
	MaxSkillFileBytes int
}

type InstallRequest struct {
	Source  RepoSource
	Scope   string
	Cluster string
}

func NormalizeGitHubSource(input string, ref string, skillPath string) (RepoSource, error) {
	raw := strings.TrimSpace(input)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimSuffix(raw, ".git")
	if !strings.HasPrefix(raw, "github.com/") {
		parts := strings.Split(raw, "/")
		if len(parts) == 2 {
			raw = "github.com/" + raw
		}
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 3 || parts[0] != "github.com" || parts[1] == "" || parts[2] == "" {
		return RepoSource{}, fmt.Errorf("invalid GitHub repository %q; use github.com/org/repo, https://github.com/org/repo, or org/repo", input)
	}
	if ref == "" {
		ref = "main"
	}
	if skillPath == "" {
		skillPath = "skills"
	}
	return RepoSource{Input: input, HostPath: raw, CloneURL: "https://" + raw + ".git", Ref: ref, Path: skillPath}, nil
}

func (i Installer) Install(ctx context.Context, req InstallRequest) ([]Skill, error) {
	if req.Scope != ScopeGlobal && req.Scope != ScopeCluster {
		return nil, fmt.Errorf("invalid scope %q", req.Scope)
	}
	if req.Scope == ScopeCluster && strings.TrimSpace(req.Cluster) == "" {
		return nil, fmt.Errorf("cluster is required for cluster-scoped install")
	}
	fetcher := i.Fetcher
	if fetcher == nil {
		fetcher = GitFetcher{}
	}
	cacheRel := filepath.Join("skills", "repos", filepath.FromSlash(req.Source.HostPath), sanitizeRef(req.Source.Ref))
	cacheAbs := filepath.Join(i.Home, cacheRel)
	_ = os.RemoveAll(cacheAbs)
	if err := os.MkdirAll(filepath.Dir(cacheAbs), 0755); err != nil {
		return nil, err
	}
	if err := fetcher.Fetch(ctx, req.Source, cacheAbs); err != nil {
		return nil, err
	}
	skills, err := discoverSkills(cacheAbs, req.Source.Path, i.MaxSkillFileBytes)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no valid skills found under %s", req.Source.Path)
	}
	regPath := GlobalRegistryPath(i.Home)
	if req.Scope == ScopeCluster {
		regPath = ClusterRegistryPath(i.Home, req.Cluster)
	}
	reg, err := LoadRegistry(regPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, skill := range skills {
		entry := RegistryEntry{
			Name: skill.Name, Description: skill.Description, Version: skill.Version, Tags: skill.Tags, MaxChars: skill.MaxChars,
			Source: req.Source.HostPath, Ref: req.Source.Ref, Path: filepath.ToSlash(skill.Path),
			CachePath: filepath.ToSlash(filepath.Join(cacheRel, filepath.Dir(skill.Path))),
			InstalledAt: now,
		}
		reg.Skills = upsertEntry(reg.Skills, entry)
	}
	if err := SaveRegistry(regPath, reg); err != nil {
		return nil, err
	}
	return skills, nil
}

func discoverSkills(repoRoot string, skillRoot string, maxFileBytes int) ([]Skill, error) {
	root := filepath.Join(repoRoot, filepath.Clean(skillRoot))
	var result []Skill
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		skill, err := ParseSkillMarkdown(filepath.ToSlash(rel), data, maxFileBytes)
		if err != nil {
			return err
		}
		result = append(result, skill)
		return nil
	})
	return result, err
}

func upsertEntry(entries []RegistryEntry, next RegistryEntry) []RegistryEntry {
	for idx := range entries {
		if entries[idx].Name == next.Name {
			entries[idx] = next
			return entries
		}
	}
	return append(entries, next)
}

func sanitizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.ReplaceAll(ref, "/", "_")
	if ref == "" {
		return "main"
	}
	return ref
}

func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
```

- [ ] **Step 4: Run installer tests**

Run:

```bash
go test ./internal/skills -run 'NormalizeGitHubSource|InstallDiscovers' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/skills/installer.go internal/skills/installer_test.go
git commit -m "feat: install skills from repositories"
```

## Task 5: skill_read Tool

**Files:**
- Create: `internal/skills/tools.go`
- Test: `internal/skills/tools_test.go`

- [ ] **Step 1: Write tool tests**

Create `internal/skills/tools_test.go`:

```go
package skills

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDefsIncludesSkillRead(t *testing.T) {
	defs := ToolDefs()
	if len(defs) != 1 || defs[0].Name != ToolName {
		t.Fatalf("defs = %#v", defs)
	}
	if !strings.Contains(defs[0].Description, "Load") {
		t.Fatalf("description = %q", defs[0].Description)
	}
}

func TestHandleSkillReadReturnsCappedBody(t *testing.T) {
	handler := NewToolHandler([]Skill{{Name: "k8s-debug", Scope: ScopeCluster, Cluster: "prod", Body: strings.Repeat("a", 20)}}, 8)
	out := handler.Handle(json.RawMessage(`{"name":"k8s-debug","reason":"diagnose pods"}`))
	if !strings.Contains(out, "Skill: k8s-debug") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatalf("output missing truncation: %q", out)
	}
}

func TestHandleSkillReadRejectsHiddenSkill(t *testing.T) {
	handler := NewToolHandler([]Skill{{Name: "k8s-debug", Body: "body"}}, 100)
	out := handler.Handle(json.RawMessage(`{"name":"missing","reason":"x"}`))
	if !strings.Contains(out, "not visible") {
		t.Fatalf("output = %q", out)
	}
}
```

- [ ] **Step 2: Run tool tests to verify they fail**

Run:

```bash
go test ./internal/skills -run 'ToolDefs|HandleSkillRead' -count=1
```

Expected: FAIL because tool functions are missing.

- [ ] **Step 3: Implement tool definition and handler**

Create `internal/skills/tools.go`:

```go
package skills

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pockyHM/conan/internal/llm"
)

func ToolDefs() []llm.ToolDef {
	return []llm.ToolDef{{
		Name:        ToolName,
		Description: "Load a visible Conan skill by name when its instructions would materially improve the answer. Use only names from the Available skills index.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "Name of the visible skill to load"},
				"reason": {"type": "string", "description": "Brief reason this skill is relevant"}
			},
			"required": ["name", "reason"]
		}`),
	}}
}

type ToolHandler struct {
	byName        map[string]Skill
	maxSkillChars int
}

func NewToolHandler(visible []Skill, maxSkillChars int) ToolHandler {
	byName := make(map[string]Skill, len(visible))
	for _, skill := range visible {
		byName[skill.Name] = skill
	}
	return ToolHandler{byName: byName, maxSkillChars: maxSkillChars}
}

type readArgs struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (h ToolHandler) Handle(raw json.RawMessage) string {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Sprintf("skill_read error: invalid arguments: %v", err)
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return "skill_read error: name is required"
	}
	skill, ok := h.byName[name]
	if !ok {
		return fmt.Sprintf("skill_read error: skill %q is not visible in this session", name)
	}
	limit := h.maxSkillChars
	if skill.MaxChars > 0 && (limit == 0 || skill.MaxChars < limit) {
		limit = skill.MaxChars
	}
	body := limitRunes(skill.Body, limit)
	scope := skill.Scope
	if skill.Scope == ScopeCluster {
		scope = "cluster:" + skill.Cluster
	}
	return fmt.Sprintf("Skill: %s\nScope: %s\nReason: %s\n\n%s", skill.Name, scope, strings.TrimSpace(args.Reason), body)
}

func limitRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}
```

- [ ] **Step 4: Run tool tests**

Run:

```bash
go test ./internal/skills -run 'ToolDefs|HandleSkillRead' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/skills/tools.go internal/skills/tools_test.go
git commit -m "feat: add skill read tool"
```

## Task 6: CLI Skills Commands

**Files:**
- Modify: `cmd/conan/main.go`
- Test: `cmd/conan/main_test.go`

- [ ] **Step 1: Write CLI tests**

Add tests to `cmd/conan/main_test.go` using the existing root command test helpers. If the file has helper functions for command execution, use them. Otherwise add this local helper once near the tests:

```go
func executeCommandForSkillsTest(root *cobra.Command, args ...string) (string, string, error) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}
```

Add:

```go
func TestSkillsListEmpty(t *testing.T) {
	home := t.TempDir()
	root := newRootCommand()

	out, _, err := executeCommandForSkillsTest(root, "--home", home, "skills", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No skills installed") {
		t.Fatalf("out = %q", out)
	}
}

func TestSkillsInstallRejectsInvalidGitHubRepo(t *testing.T) {
	home := t.TempDir()
	root := newRootCommand()

	_, _, err := executeCommandForSkillsTest(root, "--home", home, "skills", "install", "not-a-valid-source", "--global")
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "invalid GitHub repository") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run CLI tests to verify they fail**

Run:

```bash
go test ./cmd/conan -run 'TestSkillsListEmpty|TestSkillsInstallRejectsInvalidGitHubRepo' -count=1
```

Expected: FAIL because the `skills` command group does not exist.

- [ ] **Step 3: Add command wiring**

In `cmd/conan/main.go`, import the package:

```go
	"github.com/pockyHM/conan/internal/skills"
```

Inside `newRootCommand()`, create a command group before final `rootCmd.AddCommand(...)` calls:

```go
	skillsCmd := &cobra.Command{Use: "skills", Short: "Skill commands"}
	var skillsGlobal bool
	var skillsCluster string
	var skillsRef string
	var skillsPath string

	skillsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(home)
			globalReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(loader.Home()))
			if err != nil {
				return err
			}
			selectedCluster := skillsCluster
			if selectedCluster == "" {
				selectedCluster = clusterName
			}
			var clusterReg skills.Registry
			if selectedCluster != "" {
				clusterReg, err = skills.LoadRegistry(skills.ClusterRegistryPath(loader.Home(), selectedCluster))
				if err != nil {
					return err
				}
			}
			if len(globalReg.Skills) == 0 && len(clusterReg.Skills) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No skills installed")
				return nil
			}
			for _, entry := range globalReg.Skills {
				fmt.Fprintf(cmd.OutOrStdout(), "global\t%s\t%s\n", entry.Name, entry.Description)
			}
			for _, entry := range clusterReg.Skills {
				fmt.Fprintf(cmd.OutOrStdout(), "cluster:%s\t%s\t%s\n", selectedCluster, entry.Name, entry.Description)
			}
			return nil
		},
	}

	skillsInstallCmd := &cobra.Command{
		Use:   "install <github-url>",
		Short: "Install skills from a public GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := skills.ScopeCluster
			targetCluster := skillsCluster
			if skillsGlobal {
				scope = skills.ScopeGlobal
			}
			if scope == skills.ScopeCluster {
				if targetCluster == "" {
					targetCluster = clusterName
				}
				if targetCluster == "" {
					globalCfg, err := cfgloader.NewLoader(home).LoadGlobal()
					if err != nil {
						return err
					}
					targetCluster = globalCfg.DefaultCluster
				}
				if targetCluster == "" {
					return fmt.Errorf("--cluster is required when installing a cluster-scoped skill")
				}
			}
			source, err := skills.NormalizeGitHubSource(args[0], skillsRef, skillsPath)
			if err != nil {
				return err
			}
			installer := skills.Installer{Home: cfgloader.NewLoader(home).Home(), MaxSkillFileBytes: 256 * 1024}
			installed, err := installer.Install(cmd.Context(), skills.InstallRequest{Source: source, Scope: scope, Cluster: targetCluster})
			if err != nil {
				return err
			}
			for _, skill := range installed {
				fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", skill.Name)
			}
			return nil
		},
	}
	skillsInstallCmd.Flags().BoolVar(&skillsGlobal, "global", false, "Install globally")
	skillsInstallCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to install into or list from")
	skillsInstallCmd.Flags().StringVar(&skillsRef, "ref", "main", "Git ref to install")
	skillsInstallCmd.Flags().StringVar(&skillsPath, "path", "skills", "Repository skills path")
	skillsListCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to list")

	skillsCmd.AddCommand(skillsListCmd, skillsInstallCmd)
```

Add `skillsCmd` to the existing `rootCmd.AddCommand(...)`.

- [ ] **Step 4: Run CLI tests**

Run:

```bash
go test ./cmd/conan -run 'TestSkillsListEmpty|TestSkillsInstallRejectsInvalidGitHubRepo' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/conan/main.go cmd/conan/main_test.go
git commit -m "feat: add skills cli commands"
```

## Task 7: TUI Slash Commands And Autocomplete

**Files:**
- Modify: `internal/tui/command.go`
- Modify: `internal/tui/autocomplete.go`
- Test: `internal/tui/command_test.go`

- [ ] **Step 1: Write slash parser tests**

Add to `internal/tui/command_test.go`:

```go
func TestParseSkillsCommands(t *testing.T) {
	tests := []struct {
		input string
		kind  CommandKind
		arg   string
	}{
		{input: "/skills", kind: CommandSkills, arg: ""},
		{input: "/skills install github.com/org/repo", kind: CommandSkills, arg: "install github.com/org/repo"},
		{input: "/skill k8s-debug pods failing", kind: CommandSkill, arg: "k8s-debug pods failing"},
	}
	for _, tt := range tests {
		got, ok := ParseSlashCommand(tt.input)
		if !ok {
			t.Fatalf("%s not parsed", tt.input)
		}
		if got.Kind != tt.kind || got.Arg != tt.arg {
			t.Fatalf("%s = %#v, want kind=%s arg=%q", tt.input, got, tt.kind, tt.arg)
		}
	}
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run:

```bash
go test ./internal/tui -run TestParseSkillsCommands -count=1
```

Expected: FAIL because `CommandSkills` and `CommandSkill` are not defined.

- [ ] **Step 3: Add command kinds and parser cases**

In `internal/tui/command.go`, add constants:

```go
	CommandSkills    CommandKind = "skills"
	CommandSkill     CommandKind = "skill"
```

Add parser cases:

```go
	case "skills":
		return SlashCommand{Kind: CommandSkills, Arg: arg}, true
	case "skill":
		return SlashCommand{Kind: CommandSkill, Arg: arg}, true
```

- [ ] **Step 4: Add autocomplete registry entries**

In `internal/tui/autocomplete.go`, add to `commandRegistry`:

```go
	{Name: "skills", Description: "List and manage skills", ArgHint: "[install|remove|update]"},
	{Name: "skill", Description: "Use a skill", ArgHint: "<name> [arguments]"},
```

If `uiLanguage.commandDescription` has explicit command descriptions in `internal/tui/i18n.go`, add entries for `"skills"` and `"skill"` there as well.

- [ ] **Step 5: Run parser tests**

Run:

```bash
go test ./internal/tui -run TestParseSkillsCommands -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/command.go internal/tui/autocomplete.go internal/tui/command_test.go internal/tui/i18n.go
git commit -m "feat: parse skills slash commands"
```

## Task 8: TUI Skill Index, skill_read Dispatch, And Explicit Invocation

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `cmd/conan/main.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write model prompt and tool tests**

Add to `internal/tui/model_test.go`:

```go
func TestModelInjectsSkillIndexAndTool(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Model: "test",
		Provider: provider,
		Conv: conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Scope: skills.ScopeCluster, Cluster: "prod", Body: "body"}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, IndexTokenBudget: 800, MaxSkillChars: 6000, MaxVisibleSkills: 50},
	})
	model.input = "pods are failing"

	cmd := model.submitMessage()
	msg := execCmd(t, cmd)
	updated, cmd := model.Update(msg)
	if cmd != nil {
		_ = execCmd(t, cmd)
	}
	model = updated.(Model)

	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	if !strings.Contains(provider.req.SystemPrompt, "Available skills:") {
		t.Fatalf("system prompt missing skills index: %s", provider.req.SystemPrompt)
	}
	found := false
	for _, tool := range provider.req.Tools {
		if tool.Name == skills.ToolName {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill_read tool missing from %#v", provider.req.Tools)
	}
}

func TestDispatchSkillRead(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Conv: conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{Name: "k8s-debug", Scope: skills.ScopeCluster, Cluster: "prod", Body: "skill body"}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})
	msg := execCmd(t, model.dispatchTool(7, llm.ToolCall{
		ID: "skill-1", Name: skills.ToolName, Arguments: json.RawMessage(`{"name":"k8s-debug","reason":"diagnose"}`),
	}))
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want multiToolResultMsg", msg)
	}
	if !strings.Contains(result.results[0].Output, "skill body") {
		t.Fatalf("output = %q", result.results[0].Output)
	}
}
```

Adjust helper names if `model_test.go` already uses a different captured provider helper. Add imports for `internal/skills` and `pkg/configschema` if missing.

- [ ] **Step 2: Run model tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestModelInjectsSkillIndexAndTool|TestDispatchSkillRead' -count=1
```

Expected: FAIL because `ModelConfig` does not contain skills fields and dispatch does not handle `skill_read`.

- [ ] **Step 3: Add fields to TUI model config and model**

In `internal/tui/model.go`, import:

```go
	"github.com/pockyHM/conan/internal/skills"
```

Add to `ModelConfig`:

```go
	Skills       []skills.Skill
	SkillsConfig configschema.SkillsConfig
	SkillWarnings []string
```

Add to `Model`:

```go
	skills        []skills.Skill
	skillsConfig  configschema.SkillsConfig
	skillWarnings []string
```

In `NewModel`, set:

```go
		skills:        cfg.Skills,
		skillsConfig:  normalizeSkillsConfig(cfg.SkillsConfig),
		skillWarnings: append([]string(nil), cfg.SkillWarnings...),
```

Add helper:

```go
func normalizeSkillsConfig(cfg configschema.SkillsConfig) configschema.SkillsConfig {
	if cfg.IndexTokenBudget == 0 {
		cfg.IndexTokenBudget = 800
	}
	if cfg.MaxSkillChars == 0 {
		cfg.MaxSkillChars = 6000
	}
	if cfg.MaxVisibleSkills == 0 {
		cfg.MaxVisibleSkills = 50
	}
	cfg.Enabled = true
	return cfg
}
```

- [ ] **Step 4: Add tools and prompt injection**

In `availableToolDefs()`, append after image tools or before memory tools:

```go
	if m.skillsConfig.Enabled && len(m.skills) > 0 {
		allTools = append(allTools, skills.ToolDefs()...)
	}
```

In `buildSystemPromptWithMemory()`, after the tool routing contract section:

```go
	if m.skillsConfig.Enabled && len(m.skills) > 0 {
		if index := skills.BuildSkillIndex(m.skills, m.skillsConfig.IndexTokenBudget); strings.TrimSpace(index) != "" {
			parts = append(parts, "\n[Skills]\n"+index)
		}
	}
```

- [ ] **Step 5: Add dispatch for skill_read**

In `dispatchTool`, add:

```go
	case skills.ToolName:
		return m.dispatchSkillRead(streamID, call)
```

Add method:

```go
func (m Model) dispatchSkillRead(streamID uint64, call llm.ToolCall) tea.Cmd {
	visible := append([]skills.Skill(nil), m.skills...)
	maxChars := m.skillsConfig.MaxSkillChars
	return func() tea.Msg {
		output := skills.NewToolHandler(visible, maxChars).Handle(call.Arguments)
		return multiToolResultMsg{
			streamID: streamID,
			results: []nodeToolResult{{
				Node:   "local",
				Tool:   call.Name,
				Output: output,
			}},
			Call: call,
		}
	}
}
```

- [ ] **Step 6: Wire visible skills into TUI startup**

In `cmd/conan/main.go`, after loading `global` and `selectedCluster` in `runTUI`, load registries:

```go
		globalSkillReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(loader.Home()))
		if err != nil {
			return err
		}
		clusterSkillReg, err := skills.LoadRegistry(skills.ClusterRegistryPath(loader.Home(), selectedCluster))
		if err != nil {
			return err
		}
		visibleSkills, skillWarnings, err := skills.ResolveVisible(loader.Home(), selectedCluster, globalSkillReg, clusterSkillReg, skills.ResolveOptions{
			MaxSkillFileBytes: 256 * 1024,
			MaxVisibleSkills:  global.Skills.MaxVisibleSkills,
			IndexCharBudget:   global.Skills.IndexTokenBudget,
		})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load skills: %v\n", err)
		}
```

Pass into `tui.NewModel`:

```go
			Skills:        visibleSkills,
			SkillsConfig:  global.Skills,
			SkillWarnings: skillWarnings,
```

Make sure `cmd/conan/main.go` imports `github.com/pockyHM/conan/internal/skills`.

- [ ] **Step 7: Run model tests**

Run:

```bash
go test ./internal/tui -run 'TestModelInjectsSkillIndexAndTool|TestDispatchSkillRead' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go cmd/conan/main.go
git commit -m "feat: expose skills to tui model"
```

## Task 9: TUI `/skills`, `/skill`, And Skill Shortcut Behavior

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write slash behavior tests**

Add to `internal/tui/model_test.go`:

```go
func TestSlashSkillsListsVisibleSkills(t *testing.T) {
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Conv: conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Scope: skills.ScopeCluster, Cluster: "prod"}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true},
	})
	model.input = "/skills"
	cmd := model.submitMessage()
	msg := execCmd(t, cmd)
	updated, _ := model.Update(msg)
	model = updated.(Model)
	if !strings.Contains(model.status, "k8s-debug") && !strings.Contains(model.View(), "k8s-debug") {
		t.Fatalf("skill list not visible; status=%q view=%q", model.status, model.View())
	}
}

func TestSlashSkillInjectsSkillForNextRequest(t *testing.T) {
	provider := &captureStreamProvider{}
	model := NewModel(ModelConfig{
		Cluster: "prod",
		Provider: provider,
		Conv: conversation.New("prod", nil, "test"),
		Skills: []skills.Skill{{Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Scope: skills.ScopeCluster, Cluster: "prod", Body: "Use kubectl describe before changes."}},
		SkillsConfig: configschema.SkillsConfig{Enabled: true, MaxSkillChars: 6000},
	})
	model.input = "/skill k8s-debug pods failing"
	cmd := model.submitMessage()
	msg := execCmd(t, cmd)
	updated, cmd := model.Update(msg)
	if cmd != nil {
		_ = execCmd(t, cmd)
	}
	model = updated.(Model)
	if provider.req == nil {
		t.Fatal("provider request was not captured")
	}
	found := false
	for _, msg := range provider.req.Messages {
		if strings.Contains(msg.Content, "Use kubectl describe before changes.") && strings.Contains(msg.Content, "pods failing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("request messages missing explicit skill content: %#v", provider.req.Messages)
	}
}
```

- [ ] **Step 2: Run slash behavior tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestSlashSkillsListsVisibleSkills|TestSlashSkillInjectsSkillForNextRequest' -count=1
```

Expected: FAIL because slash handling has not been added.

- [ ] **Step 3: Add helper functions to find and format skills**

In `internal/tui/model.go`, add:

```go
func (m Model) findSkill(name string) (skills.Skill, bool) {
	for _, skill := range m.skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return skills.Skill{}, false
}

func (m Model) visibleSkillsSummary() string {
	if len(m.skills) == 0 {
		return "No skills available for this cluster"
	}
	var lines []string
	for _, skill := range m.skills {
		scope := "global"
		if skill.Scope == skills.ScopeCluster {
			scope = "cluster:" + skill.Cluster
		}
		lines = append(lines, fmt.Sprintf("%s [%s] - %s", skill.Name, scope, skill.Description))
	}
	return strings.Join(lines, "\n")
}

func formatExplicitSkillMessage(skill skills.Skill, args string, maxChars int) string {
	return fmt.Sprintf("Use Conan skill %q for this request.\n\nArguments: %s\n\n%s", skill.Name, strings.TrimSpace(args), skills.NewToolHandler([]skills.Skill{skill}, maxChars).Handle(json.RawMessage(fmt.Sprintf(`{"name":%q,"reason":"explicit slash invocation"}`, skill.Name))))
}
```

- [ ] **Step 4: Handle slash commands in submit path**

Find the existing slash command switch in `internal/tui/model.go` and add cases:

```go
	case CommandSkills:
		m.status = m.visibleSkillsSummary()
		m.input = ""
		m.ac = m.ac.hide()
		return commandResultMsg{model: m}
	case CommandSkill:
		name, rest, ok := strings.Cut(strings.TrimSpace(cmd.Arg), " ")
		if !ok {
			name = strings.TrimSpace(cmd.Arg)
			rest = ""
		}
		skill, found := m.findSkill(name)
		if !found {
			m.status = fmt.Sprintf("Unknown skill: %s", name)
			m.input = ""
			m.ac = m.ac.hide()
			return commandResultMsg{model: m}
		}
		m.input = formatExplicitSkillMessage(skill, rest, m.skillsConfig.MaxSkillChars)
```

For skill shortcut support, in the `CommandUnknown` branch, before reporting unknown command:

```go
		name, rest, _ := strings.Cut(strings.TrimSpace(cmd.Arg), " ")
		if skill, found := m.findSkill(name); found {
			m.input = formatExplicitSkillMessage(skill, rest, m.skillsConfig.MaxSkillChars)
			break
		}
```

Let the normal submit flow send `m.input` after it has been replaced with the explicit skill message.

- [ ] **Step 5: Run slash behavior tests**

Run:

```bash
go test ./internal/tui -run 'TestSlashSkillsListsVisibleSkills|TestSlashSkillInjectsSkillForNextRequest' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: support skill slash invocation"
```

## Task 10: Full Verification

**Files:**
- All files touched by prior tasks.

- [ ] **Step 1: Run targeted package tests**

Run:

```bash
go test ./internal/skills ./internal/config ./internal/tui ./cmd/conan -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Inspect git status**

Run:

```bash
git status --short
```

Expected: Only intentional skills-system files are modified or added. Existing unrelated dirty files may still appear; do not revert them.

- [ ] **Step 4: Commit final integration fixes if any were needed**

If Step 1 or Step 2 required small integration fixes, commit only those files:

```bash
git add internal/skills internal/tui cmd/conan pkg/configschema internal/config
git commit -m "test: verify skills system integration"
```

Expected: Either a small final commit is created, or there are no additional changes to commit.

## Self-Review

- Spec coverage: The plan covers public GitHub install, global and cluster scope, deduplication through resolver priority, `/skills`, `/skill`, skill shortcut invocation, model-visible short index, and `skill_read`.
- Scope: The plan keeps script execution and private GitHub authentication out of scope, matching the spec.
- Type consistency: `skills.Skill`, `skills.Registry`, `skills.ResolveVisible`, `skills.ToolDefs`, and `skills.NewToolHandler` are introduced before TUI and CLI tasks use them.
- Test strategy: Each behavior has a failing test before implementation, with package-level and full-suite verification at the end.
