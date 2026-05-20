package memory

import (
	"os"
	"path/filepath"
	"strings"
)

type RulesContent struct {
	Core  string
	Rules map[string]string
}

func LoadRules(memoryDir string) (*RulesContent, error) {
	rc := &RulesContent{Rules: make(map[string]string)}

	corePath := filepath.Join(memoryDir, "MEMORY.md")
	data, err := os.ReadFile(corePath)
	if err != nil {
		if os.IsNotExist(err) {
			return rc, nil
		}
		return nil, err
	}
	rc.Core = string(data)

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
		data, err := os.ReadFile(filepath.Join(rulesDir, entry.Name()))
		if err != nil {
			continue
		}
		rc.Rules[entry.Name()] = string(data)
	}

	return rc, nil
}

func (rc *RulesContent) Format() string {
	var parts []string
	if rc.Core != "" {
		parts = append(parts, rc.Core)
	}
	for name, content := range rc.Rules {
		parts = append(parts, "\n## "+name+"\n"+content)
	}
	return strings.Join(parts, "\n")
}

func (rc *RulesContent) Empty() bool {
	return rc.Core == "" && len(rc.Rules) == 0
}

func EnsureMemoryDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rulesDir := filepath.Join(dir, "rules")
	return os.MkdirAll(rulesDir, 0o755)
}
