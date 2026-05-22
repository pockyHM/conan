package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const memoryRuleReadLimitBytes = int64(maxMemoryContentRunes*4 + len("\n[truncated]"))

type RulesContent struct {
	Core  string
	Rules map[string]string
}

func LoadRules(memoryDir string) (*RulesContent, error) {
	rc := &RulesContent{Rules: make(map[string]string)}
	store := NewMarkdownStore(memoryDir)

	core, err := store.ReadLimited("MEMORY.md", memoryRuleReadLimitBytes)
	if err == nil {
		rc.Core = limitMemoryRuleContent(core)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	rulesDir := filepath.Join(memoryDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return rc, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content, err := store.ReadLimited(filepath.ToSlash(filepath.Join("rules", entry.Name())), memoryRuleReadLimitBytes)
		if err != nil {
			continue
		}
		rc.Rules[entry.Name()] = limitMemoryRuleContent(content)
	}

	return rc, nil
}

func limitMemoryRuleContent(content string) string {
	runes := []rune(content)
	if len(runes) <= maxMemoryContentRunes {
		return content
	}
	return string(runes[:maxMemoryContentRunes]) + "\n[truncated]"
}

func (rc *RulesContent) Format() string {
	var parts []string
	if rc.Core != "" {
		parts = append(parts, rc.Core)
	}
	names := make([]string, 0, len(rc.Rules))
	for name := range rc.Rules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := rc.Rules[name]
		parts = append(parts, "\n## "+name+"\n"+content)
	}
	return strings.Join(parts, "\n")
}

func (rc *RulesContent) Empty() bool {
	return rc.Core == "" && len(rc.Rules) == 0
}

func EnsureMemoryDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"rules", "clusters", "runbooks", "incidents"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			return err
		}
	}
	return nil
}
