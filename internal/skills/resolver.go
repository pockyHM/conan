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
