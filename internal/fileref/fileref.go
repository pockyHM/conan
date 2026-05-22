package fileref

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultMaxFileBytes  = 256 * 1024
	defaultMaxTotalBytes = 512 * 1024
)

type Reference struct {
	Path string
}

type LoadedReference struct {
	Path      string
	Content   string
	Directory bool
	Truncated bool
}

type Limits struct {
	MaxFileBytes  int
	MaxTotalBytes int
}

func Parse(input string) []Reference {
	var refs []Reference
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if i+1 < len(runes) && runes[i+1] == '@' {
			i++
			continue
		}
		if i > 0 && !isRefBoundary(runes[i-1]) {
			continue
		}
		ref, next := parseAt(runes, i+1)
		if ref != "" {
			refs = append(refs, Reference{Path: ref})
			i = next - 1
		}
	}
	return refs
}

func parseAt(runes []rune, start int) (string, int) {
	if start >= len(runes) {
		return "", start
	}
	if runes[start] == '"' {
		var b strings.Builder
		for i := start + 1; i < len(runes); i++ {
			if runes[i] == '"' {
				return strings.TrimSpace(b.String()), i + 1
			}
			b.WriteRune(runes[i])
		}
		return "", start
	}
	var b strings.Builder
	i := start
	for ; i < len(runes); i++ {
		if unicode.IsSpace(runes[i]) {
			break
		}
		switch runes[i] {
		case ',', ';', ':', ')', ']', '}':
			return strings.TrimRight(b.String(), "."), i
		}
		b.WriteRune(runes[i])
	}
	return strings.TrimRight(strings.TrimSpace(b.String()), "."), i
}

func isRefBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("([{:，。；；", r)
}

func Load(root string, refs []Reference, limits Limits) ([]LoadedReference, error) {
	if limits.MaxFileBytes <= 0 {
		limits.MaxFileBytes = defaultMaxFileBytes
	}
	if limits.MaxTotalBytes <= 0 {
		limits.MaxTotalBytes = defaultMaxTotalBytes
	}
	var loaded []LoadedReference
	total := 0
	seen := make(map[string]bool)
	for _, ref := range refs {
		path, display, err := resolve(root, ref.Path)
		if err != nil {
			return nil, err
		}
		if seen[display] {
			continue
		}
		seen[display] = true
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("read @%s: %w", ref.Path, err)
		}
		if info.IsDir() {
			content, err := listDirectory(path)
			if err != nil {
				return nil, fmt.Errorf("list @%s: %w", ref.Path, err)
			}
			loaded = append(loaded, LoadedReference{Path: display, Content: content, Directory: true})
			continue
		}
		remaining := limits.MaxTotalBytes - total
		if remaining <= 0 {
			break
		}
		limit := min(limits.MaxFileBytes, remaining)
		data, truncated, err := readLimited(path, limit)
		if err != nil {
			return nil, fmt.Errorf("read @%s: %w", ref.Path, err)
		}
		total += len(data)
		content := string(data)
		if truncated {
			content += "\n[truncated]"
		}
		loaded = append(loaded, LoadedReference{Path: display, Content: content, Truncated: truncated})
	}
	return loaded, nil
}

func Format(refs []LoadedReference) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Referenced local workspace files:\n")
	for _, ref := range refs {
		if ref.Directory {
			fmt.Fprintf(&b, "<directory path=%q>\n%s\n</directory>\n", ref.Path, ref.Content)
			continue
		}
		fmt.Fprintf(&b, "<file path=%q>\n%s\n</file>\n", ref.Path, ref.Content)
	}
	return strings.TrimSpace(b.String())
}

func AppendContext(input string, refs []LoadedReference) string {
	formatted := Format(refs)
	if formatted == "" {
		return input
	}
	return input + "\n\n" + formatted
}

func resolve(root, raw string) (string, string, error) {
	if root == "" {
		root = "."
	}
	raw = strings.TrimSpace(raw)
	clean := filepath.Clean(raw)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("file reference outside workspace: %s", raw)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(rootAbs, clean)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(filepath.Separator)) {
		return "", "", fmt.Errorf("file reference outside workspace: %s", raw)
	}
	if err := rejectSymlinkPath(rootAbs, targetAbs); err != nil {
		return "", "", err
	}
	display, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", "", err
	}
	return targetAbs, filepath.ToSlash(display), nil
}

func rejectSymlinkPath(rootAbs, targetAbs string) error {
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("file reference contains symlink: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

func readLimited(path string, limit int) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if len(data) <= limit {
		return data, false, nil
	}
	return data[:limit], true, nil
}

func listDirectory(root string) (string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		prefix := "  "
		if d.IsDir() {
			prefix = "d "
		}
		entries = append(entries, prefix+filepath.ToSlash(rel))
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n"), nil
}
