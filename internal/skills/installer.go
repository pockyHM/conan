package skills

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	Names   []string
}

type RemoveRequest struct {
	Name    string
	Scope   string
	Cluster string
}

type UpdateRequest struct {
	Name    string
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
	hostPath, err := validateGitHubHostPath(req.Source.HostPath)
	if err != nil {
		return nil, err
	}
	if req.Source.CloneURL != "https://"+hostPath+".git" {
		return nil, fmt.Errorf("invalid clone URL %q for %s", req.Source.CloneURL, hostPath)
	}
	skillRoot, err := cleanRelativePath(req.Source.Path)
	if err != nil {
		return nil, err
	}

	fetcher := i.Fetcher
	if fetcher == nil {
		fetcher = GitFetcher{}
	}

	cacheRel := filepath.Join("skills", "repos", filepath.FromSlash(hostPath), sanitizeRef(req.Source.Ref))
	cacheAbs := filepath.Join(i.Home, cacheRel)

	tmpRoot := filepath.Join(i.Home, "skills", "tmp")
	if err := os.MkdirAll(tmpRoot, 0755); err != nil {
		return nil, err
	}
	tmpCheckout, err := os.MkdirTemp(tmpRoot, "install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpCheckout)

	if err := fetcher.Fetch(ctx, req.Source, tmpCheckout); err != nil {
		return nil, err
	}

	skills, err := discoverSkills(tmpCheckout, skillRoot, i.MaxSkillFileBytes)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no valid skills found under %s", skillRoot)
	}
	skills, err = filterSkillsByName(skills, req.Names)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no skills selected")
	}

	if err := replaceDir(cacheAbs, tmpCheckout); err != nil {
		return nil, err
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
		skills[idx].Source = hostPath
		skills[idx].Ref = req.Source.Ref
		skills[idx].CachePath = cachePath
		skills[idx].InstalledAt = now

		entry := RegistryEntry{
			Name:        skills[idx].Name,
			Description: skills[idx].Description,
			Version:     skills[idx].Version,
			Tags:        append([]string(nil), skills[idx].Tags...),
			MaxChars:    skills[idx].MaxChars,
			Source:      hostPath,
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

func (i Installer) Discover(ctx context.Context, source RepoSource) ([]Skill, error) {
	if source.HostPath == "" || source.CloneURL == "" {
		return nil, fmt.Errorf("source is required")
	}
	hostPath, err := validateGitHubHostPath(source.HostPath)
	if err != nil {
		return nil, err
	}
	if source.CloneURL != "https://"+hostPath+".git" {
		return nil, fmt.Errorf("invalid clone URL %q for %s", source.CloneURL, hostPath)
	}
	skillRoot, err := cleanRelativePath(source.Path)
	if err != nil {
		return nil, err
	}

	fetcher := i.Fetcher
	if fetcher == nil {
		fetcher = GitFetcher{}
	}

	tmpRoot := filepath.Join(i.Home, "skills", "tmp")
	if err := os.MkdirAll(tmpRoot, 0755); err != nil {
		return nil, err
	}
	tmpCheckout, err := os.MkdirTemp(tmpRoot, "discover-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpCheckout)

	if err := fetcher.Fetch(ctx, source, tmpCheckout); err != nil {
		return nil, err
	}
	skills, err := discoverSkills(tmpCheckout, skillRoot, i.MaxSkillFileBytes)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no valid skills found under %s", skillRoot)
	}
	return skills, nil
}

func (i Installer) Remove(req RemoveRequest) (bool, error) {
	if strings.TrimSpace(req.Name) == "" {
		return false, fmt.Errorf("skill name is required")
	}
	regPath, err := registryPathForScope(i.Home, req.Scope, req.Cluster)
	if err != nil {
		return false, err
	}
	reg, err := LoadRegistry(regPath)
	if err != nil {
		return false, err
	}
	var next []RegistryEntry
	removed := false
	for _, entry := range reg.Skills {
		if entry.Name == req.Name {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	if !removed {
		return false, nil
	}
	reg.Skills = next
	return true, SaveRegistry(regPath, reg)
}

func (i Installer) Update(ctx context.Context, req UpdateRequest) ([]Skill, error) {
	regPath, err := registryPathForScope(i.Home, req.Scope, req.Cluster)
	if err != nil {
		return nil, err
	}
	reg, err := LoadRegistry(regPath)
	if err != nil {
		return nil, err
	}
	var entries []RegistryEntry
	for _, entry := range reg.Skills {
		if strings.TrimSpace(req.Name) != "" && entry.Name != req.Name {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		if strings.TrimSpace(req.Name) != "" {
			return nil, fmt.Errorf("skill not found: %s", req.Name)
		}
		return nil, fmt.Errorf("no skills installed")
	}
	var updated []Skill
	for _, entry := range entries {
		hostPath, err := validateGitHubHostPath(entry.Source)
		if err != nil {
			return nil, fmt.Errorf("invalid source for %s: %w", entry.Name, err)
		}
		source := RepoSource{
			Input:    hostPath,
			HostPath: hostPath,
			CloneURL: "https://" + hostPath + ".git",
			Ref:      entry.Ref,
			Path:     entry.Path,
		}
		installed, err := i.Install(ctx, InstallRequest{Source: source, Scope: req.Scope, Cluster: req.Cluster})
		if err != nil {
			return nil, err
		}
		updated = append(updated, installed...)
	}
	return updated, nil
}

func registryPathForScope(home string, scope string, cluster string) (string, error) {
	switch scope {
	case ScopeGlobal:
		return GlobalRegistryPath(home), nil
	case ScopeCluster:
		if strings.TrimSpace(cluster) == "" {
			return "", fmt.Errorf("cluster is required")
		}
		return ClusterRegistryPath(home, cluster), nil
	default:
		return "", fmt.Errorf("invalid scope %q", scope)
	}
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
		data, err := readFileUnderRoot(repoRoot, path)
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

func filterSkillsByName(discovered []Skill, names []string) ([]Skill, error) {
	if len(names) == 0 {
		return discovered, nil
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("no skills selected")
	}
	var result []Skill
	for _, skill := range discovered {
		if wanted[skill.Name] {
			result = append(result, skill)
			delete(wanted, skill.Name)
		}
	}
	if len(wanted) > 0 {
		var missing []string
		for name := range wanted {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("selected skill(s) not found: %s", strings.Join(missing, ", "))
	}
	return result, nil
}

func sanitizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "default"
	}
	hash := sha1.Sum([]byte(ref))
	suffix := hex.EncodeToString(hash[:])[:10]
	prefix := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, ref)
	prefix = strings.Trim(prefix, "._-")
	if prefix == "" || prefix == "." || prefix == ".." || strings.Contains(prefix, "..") {
		prefix = "ref"
	}
	if len(prefix) > 48 {
		prefix = strings.TrimRight(prefix[:48], "._-")
		if prefix == "" {
			prefix = "ref"
		}
	}
	return prefix + "-" + suffix
}

func validateGitHubHostPath(hostPath string) (string, error) {
	normalized := strings.TrimSpace(hostPath)
	parts := strings.Split(normalized, "/")
	if len(parts) != 3 || parts[0] != "github.com" || !validGitHubSegment(parts[1]) || !validGitHubSegment(parts[2]) {
		return "", fmt.Errorf("invalid GitHub repository %q; use normalized github.com/org/repo", hostPath)
	}
	if normalized != hostPath {
		return "", fmt.Errorf("invalid GitHub repository %q; use normalized github.com/org/repo", hostPath)
	}
	return normalized, nil
}

func replaceDir(dst string, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	backup := ""
	if _, err := os.Stat(dst); err == nil {
		var tmpErr error
		backup, tmpErr = os.MkdirTemp(filepath.Dir(dst), ".previous-*")
		if tmpErr != nil {
			return tmpErr
		}
		_ = os.Remove(backup)
		if err := os.Rename(dst, backup); err != nil {
			_ = os.RemoveAll(backup)
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(src, dst); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dst)
		}
		return err
	}
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return nil
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
	if segment == "" || strings.TrimSpace(segment) != segment || segment == "." || segment == ".." || strings.Contains(segment, "..") || strings.Contains(segment, "/") || strings.Contains(segment, `\`) {
		return false
	}
	for _, r := range segment {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
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
