package skills

import (
	"fmt"
	"path"
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
		skillPath, cachePath, err := skillMarkdownPath(home, entry.CachePath)
		if err != nil {
			return fmt.Errorf("invalid cache path for skill %s: %w", entry.Name, err)
		}
		data, err := readFileUnderRoot(filepath.Join(home, "skills", "repos"), skillPath)
		if err != nil {
			return fmt.Errorf("read skill %s: %w", entry.Name, err)
		}
		skill, err := ParseSkillMarkdown(skillPath, data, opts.MaxSkillFileBytes)
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
		skill.CachePath = cachePath
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

func skillMarkdownPath(home string, cachePath string) (string, string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(cachePath), `\`, "/")
	if normalized == "" || filepath.IsAbs(cachePath) || path.IsAbs(normalized) {
		return "", "", fmt.Errorf("must be relative")
	}
	normalized = path.Clean(normalized)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", "", fmt.Errorf("must not escape cache root")
	}
	if normalized != "skills/repos" && !strings.HasPrefix(normalized, "skills/repos/") {
		return "", "", fmt.Errorf("must be under skills/repos")
	}

	cacheRoot := filepath.Join(home, "skills", "repos")
	fullPath := filepath.Join(home, filepath.FromSlash(normalized), "SKILL.md")
	rel, err := filepath.Rel(cacheRoot, filepath.Dir(fullPath))
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("must not escape cache root")
	}
	return fullPath, normalized, nil
}

func BuildSkillIndex(skills []Skill, charBudget int) string {
	if len(skills) == 0 {
		return ""
	}
	header := "Available skills:"
	guidance := "Call skill_read when one of these skills would materially improve the answer."
	var skillLines []string
	for _, skill := range skills {
		scope := "global"
		if skill.Scope == ScopeCluster {
			scope = "cluster:" + skill.Cluster
		}
		skillLines = append(skillLines, fmt.Sprintf("- %s: %s. scope=%s", skill.Name, skill.Description, scope))
	}
	index := header + "\n" + strings.Join(skillLines, "\n") + "\n\n" + guidance
	if charBudget > 0 && len([]rune(index)) > charBudget {
		return truncateSkillIndex(index, charBudget, header, strings.Join(skillLines, "\n"), guidance)
	}
	return index
}

func truncateSkillIndex(index string, charBudget int, header string, skillsText string, guidance string) string {
	if charBudget <= 0 {
		return index
	}
	runes := []rune(index)
	if len(runes) <= charBudget {
		return index
	}
	marker := "\n[truncated]"
	markerRunes := []rune(marker)
	guidanceSuffix := "\n\n" + guidance
	guidedMinimum := len([]rune(header)) + len(markerRunes) + len([]rune(guidanceSuffix))
	if charBudget >= guidedMinimum {
		skillsPrefix := "\n" + skillsText
		prefixBudget := charBudget - len([]rune(header)) - len(markerRunes) - len([]rune(guidanceSuffix))
		skillsRunes := []rune(skillsPrefix)
		if prefixBudget > len(skillsRunes) {
			prefixBudget = len(skillsRunes)
		}
		return header + string(skillsRunes[:prefixBudget]) + marker + guidanceSuffix
	}
	if charBudget <= len(markerRunes) {
		return string(runes[:charBudget])
	}
	return string(runes[:charBudget-len(markerRunes)]) + marker
}
