package localtools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pockyHM/conan/internal/llm"
)

const maxReadBytes = 1024 * 1024

type Result struct {
	Output  string
	Success bool
}

type RootedFS struct {
	Root string
}

func ToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{Name: "local/fs/read", Description: "Read a local workspace file. Read-only; no confirmation required.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`)},
		{Name: "local/fs/list", Description: "List local workspace directory contents. Read-only; no confirmation required.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}},"required":["path"]}`)},
		{Name: "local/fs/stat", Description: "Get local workspace file metadata. Read-only; no confirmation required.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)},
		{Name: "local/fs/write", Description: "Create or overwrite a local workspace file. Requires user confirmation unless the file is allowlisted.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)},
		{Name: "local/fs/patch", Description: "Edit a local workspace file by replacing text. Requires user confirmation unless the file is allowlisted.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"}},"required":["path","old_text","new_text"]}`)},
		{Name: "local/fs/delete", Description: "Delete a local workspace file. Requires user confirmation unless the file is allowlisted.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)},
	}
}

func IsLocalTool(name string) bool {
	return strings.HasPrefix(name, "local/fs/")
}

func IsReadOnly(name string) bool {
	return name == "local/fs/read" || name == "local/fs/list" || name == "local/fs/stat"
}

func PathFromCall(name string, input json.RawMessage) string {
	if !IsLocalTool(name) {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(input, &args)
	return strings.TrimSpace(args.Path)
}

func Handle(fsys RootedFS, name string, input json.RawMessage) Result {
	switch name {
	case "local/fs/read":
		return fsys.read(input)
	case "local/fs/list":
		return fsys.list(input)
	case "local/fs/stat":
		return fsys.stat(input)
	case "local/fs/write":
		return fsys.write(input)
	case "local/fs/patch":
		return fsys.patch(input)
	case "local/fs/delete":
		return fsys.delete(input)
	default:
		return Result{Output: "unknown local tool: " + name, Success: false}
	}
}

func (f RootedFS) read(input json.RawMessage) Result {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	path, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	lines := strings.Split(string(data), "\n")
	if args.Offset > 0 && args.Offset < len(lines) {
		lines = lines[args.Offset:]
	}
	if args.Limit > 0 && args.Limit < len(lines) {
		lines = lines[:args.Limit]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxReadBytes {
		out = out[:maxReadBytes] + "\n[truncated]"
	}
	return Result{Output: out, Success: true}
}

func (f RootedFS) list(input json.RawMessage) Result {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	root, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	var entries []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != root {
			rel, _ := filepath.Rel(root, path)
			prefix := "  "
			if d.IsDir() {
				prefix = "d "
			}
			entries = append(entries, prefix+filepath.ToSlash(rel))
		}
		if !args.Recursive && d.IsDir() && path != root {
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	sort.Strings(entries)
	return Result{Output: strings.Join(entries, "\n"), Success: true}
}

func (f RootedFS) stat(input json.RawMessage) Result {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	path, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	out := fmt.Sprintf("path: %s\nsize: %d\nmode: %s\nis_dir: %v\nmod_time: %s", args.Path, info.Size(), info.Mode(), info.IsDir(), info.ModTime())
	return Result{Output: out, Success: true}
}

func (f RootedFS) write(input json.RawMessage) Result {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	path, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return fail(err)
	}
	return Result{Output: "wrote local file: " + args.Path, Success: true}
}

func (f RootedFS) patch(input json.RawMessage) Result {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	path, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	content := string(data)
	if !strings.Contains(content, args.OldText) {
		return Result{Output: "old_text not found in file", Success: false}
	}
	content = strings.Replace(content, args.OldText, args.NewText, 1)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fail(err)
	}
	return Result{Output: "patched local file: " + args.Path, Success: true}
}

func (f RootedFS) delete(input json.RawMessage) Result {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fail(err)
	}
	path, err := f.resolve(args.Path)
	if err != nil {
		return fail(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	if info.IsDir() {
		return Result{Output: "refusing to delete directory: " + args.Path, Success: false}
	}
	if err := os.Remove(path); err != nil {
		return fail(err)
	}
	return Result{Output: "deleted local file: " + args.Path, Success: true}
}

func (f RootedFS) resolve(path string) (string, error) {
	root := f.Root
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path outside workspace: %s", path)
	}
	target := filepath.Join(rootAbs, clean)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside workspace: %s", path)
	}
	if err := rejectSymlinkPath(rootAbs, targetAbs); err != nil {
		return "", err
	}
	return targetAbs, nil
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
			return fmt.Errorf("path contains symlink: %s", rel)
		}
	}
	return nil
}

func fail(err error) Result {
	return Result{Output: "Error: " + err.Error(), Success: false}
}
