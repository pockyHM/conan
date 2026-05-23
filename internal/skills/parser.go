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
	if fm.Description == "" {
		return Skill{}, fmt.Errorf("%s missing required description", path)
	}
	if fm.Name == "" {
		return Skill{}, fmt.Errorf("%s missing required name", path)
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
