package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	memoryDirPerm  = 0700
	memoryFilePerm = 0600
)

type MarkdownStore struct {
	root string
}

func NewMarkdownStore(root string) *MarkdownStore {
	return &MarkdownStore{root: root}
}

func (s *MarkdownStore) Read(rel string) (string, error) {
	path, err := s.safePath(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *MarkdownStore) ReadLimited(rel string, limitBytes int64) (string, error) {
	if limitBytes <= 0 {
		return "", fmt.Errorf("read limit must be positive")
	}
	path, err := s.safePath(rel)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limitBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *MarkdownStore) PatchSection(rel string, heading string, content string) error {
	if err := validateMemorySection("heading", heading); err != nil {
		return err
	}
	if err := validateMemoryContent("content", content); err != nil {
		return err
	}
	if err := rejectSecretLikeMemoryText(heading + "\n" + content); err != nil {
		return err
	}
	path, err := s.safePath(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), memoryDirPerm); err != nil {
		return err
	}
	existingBytes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(existingBytes)
	section := "## " + strings.TrimSpace(heading) + "\n\n" + strings.TrimSpace(content) + "\n"
	updated := replaceMarkdownSection(existing, strings.TrimSpace(heading), section)
	return os.WriteFile(path, []byte(updated), memoryFilePerm)
}

func (s *MarkdownStore) WriteNote(category string, title string, summary string, content string, tags []string) (string, error) {
	if !allowedMemoryCategory(category) {
		return "", fmt.Errorf("unsupported memory note category: %s", category)
	}
	if err := validateMemoryTitle("title", title); err != nil {
		return "", err
	}
	if err := validateMemorySummary("summary", summary); err != nil {
		return "", err
	}
	if err := validateMemoryContent("content", content); err != nil {
		return "", err
	}
	if err := rejectSecretLikeMemoryText(title + "\n" + summary + "\n" + content); err != nil {
		return "", err
	}
	slug := slugify(title)
	if slug == "" {
		return "", fmt.Errorf("title is required")
	}
	body := "# " + strings.TrimSpace(title) + "\n\n" +
		"summary: " + strings.TrimSpace(summary) + "\n" +
		"tags: " + strings.Join(tags, ", ") + "\n\n" +
		strings.TrimSpace(content) + "\n"
	date := time.Now().Format("2006-01-02")
	for suffix := 0; ; suffix++ {
		name := date + "-" + slug
		if suffix > 0 {
			name += "-" + strconv.Itoa(suffix+1)
		}
		rel := filepath.ToSlash(filepath.Join(category, name+".md"))
		path, err := s.safePath(rel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), memoryDirPerm); err != nil {
			return "", err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, memoryFilePerm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.WriteString(body); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return rel, nil
	}
}

func (s *MarkdownStore) safePath(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("memory path outside root: %s", rel)
	}
	if !strings.HasSuffix(clean, ".md") {
		return "", fmt.Errorf("memory path must be markdown: %s", rel)
	}
	if !allowedMarkdownRelativePath(clean) {
		return "", fmt.Errorf("unsupported memory path: %s", rel)
	}
	path := filepath.Join(s.root, clean)
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("memory path outside root: %s", rel)
	}
	if err := rejectSymlinkComponents(rootAbs, clean); err != nil {
		return "", err
	}
	return path, nil
}

func allowedMarkdownRelativePath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if rel == "MEMORY.md" || rel == "profile.md" {
		return true
	}
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[1] == "" || parts[1] == ".md" {
		return false
	}
	switch parts[0] {
	case "rules", "clusters", "runbooks", "incidents":
		return strings.HasSuffix(parts[1], ".md")
	default:
		return false
	}
}

func rejectSymlinkComponents(root string, rel string) error {
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("memory path contains symlink: %s", rel)
		}
	}
	return nil
}

func replaceMarkdownSection(existing string, heading string, replacement string) string {
	lines := strings.Split(existing, "\n")
	start := -1
	end := len(lines)
	inFence := false
	for i, line := range lines {
		if isMarkdownFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, text, ok := markdownHeading(line)
		if ok && level == 2 && text == heading {
			start = i
			break
		}
	}
	if start == -1 {
		prefix := strings.TrimRight(existing, "\n")
		if prefix == "" {
			return replacement
		}
		return prefix + "\n\n" + replacement
	}
	inFence = false
	for i := start + 1; i < len(lines); i++ {
		if isMarkdownFence(lines[i]) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, _, ok := markdownHeading(lines[i])
		if ok && level <= 2 {
			end = i
			break
		}
	}
	before := strings.Join(lines[:start], "\n")
	after := strings.Join(lines[end:], "\n")
	parts := []string{}
	if strings.TrimSpace(before) != "" {
		parts = append(parts, strings.TrimRight(before, "\n"))
	}
	parts = append(parts, strings.TrimRight(replacement, "\n"))
	if strings.TrimSpace(after) != "" {
		parts = append(parts, strings.TrimLeft(after, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func isMarkdownFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(trimmed[level+1:]), true
}

func slugify(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug != "" {
		return slug
	}
	sum := sha1.Sum([]byte(normalized))
	return "note-" + hex.EncodeToString(sum[:])[:12]
}

func allowedMemoryCategory(category string) bool {
	switch category {
	case "rules", "clusters", "runbooks", "incidents":
		return true
	default:
		return false
	}
}
