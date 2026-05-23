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
	if len(parts) != 3 || parts[0] != "github.com" || !validGitHubSegment(parts[1]) || !validGitHubSegment(parts[2]) {
		return RepoSource{}, fmt.Errorf("invalid GitHub repository %q; use github.com/org/repo, https://github.com/org/repo, or org/repo", input)
	}
	if ref == "" {
		ref = "main"
	}
	if skillPath == "" {
		skillPath = "skills"
	}
	return RepoSource{
		Input:    input,
		HostPath: raw,
		CloneURL: "https://" + raw + ".git",
		Ref:      ref,
		Path:     filepath.ToSlash(filepath.Clean(skillPath)),
	}, nil
}

func (i Installer) Install(ctx context.Context, req InstallRequest) ([]Skill, error) {
	if req.Scope != ScopeGlobal && req.Scope != ScopeCluster {
		return nil, fmt.Errorf("invalid scope %q", req.Scope)
	}
	if req.Scope == ScopeCluster && strings.TrimSpace(req.Cluster) == "" {
		return nil, fmt.Errorf("cluster is required for cluster-scoped install")
	}
	if req.Source.HostPath == "" || req.Source.CloneURL == "" {
		return nil, fmt.Errorf("source is required")
	}

	fetcher := i.Fetcher
	if fetcher == nil {
		fetcher = GitFetcher{}
	}

	cacheRel := filepath.Join("skills", "repos", filepath.FromSlash(req.Source.HostPath), sanitizeRef(req.Source.Ref))
	cacheAbs := filepath.Join(i.Home, cacheRel)
	if err := os.RemoveAll(cacheAbs); err != nil {
		return nil, err
	}
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
	for idx := range skills {
		skillDir := filepath.Dir(skills[idx].Path)
		cachePath := filepath.ToSlash(filepath.Join(cacheRel, skillDir))
		skills[idx].Scope = req.Scope
		if req.Scope == ScopeCluster {
			skills[idx].Cluster = req.Cluster
		}
		skills[idx].Source = req.Source.HostPath
		skills[idx].Ref = req.Source.Ref
		skills[idx].CachePath = cachePath
		skills[idx].InstalledAt = now

		entry := RegistryEntry{
			Name:        skills[idx].Name,
			Description: skills[idx].Description,
			Version:     skills[idx].Version,
			Tags:        append([]string(nil), skills[idx].Tags...),
			MaxChars:    skills[idx].MaxChars,
			Source:      req.Source.HostPath,
			Ref:         req.Source.Ref,
			Path:        filepath.ToSlash(skillDir),
			CachePath:   cachePath,
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
	rootRel, err := cleanRelativePath(skillRoot)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(repoRoot, filepath.FromSlash(rootRel))
	var result []Skill
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
	if ref == "" {
		return "main"
	}
	ref = strings.ReplaceAll(ref, `\`, "_")
	ref = strings.ReplaceAll(ref, "/", "_")
	if ref == "." || ref == ".." || strings.Contains(ref, "..") {
		return "_"
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		return nil
	})
}

func validGitHubSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	return segment != "" && segment != "." && segment != ".." && !strings.Contains(segment, "/") && !strings.Contains(segment, `\`)
}

func cleanRelativePath(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" {
		value = "."
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be relative", value)
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q must not escape repository", value)
	}
	return clean, nil
}
