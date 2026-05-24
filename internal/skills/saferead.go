package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readFileUnderRoot(root string, path string) ([]byte, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !pathWithinRoot(rootAbs, pathAbs) {
		return nil, fmt.Errorf("path escapes root: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return nil, err
	}
	if !pathWithinRoot(rootResolved, resolved) {
		return nil, fmt.Errorf("path escapes root through symlink: %s", path)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	return os.ReadFile(resolved)
}

func pathWithinRoot(rootAbs string, pathAbs string) bool {
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
