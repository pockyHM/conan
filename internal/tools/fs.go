package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

// fs/read
type fsReadTool struct{}

func (f *fsReadTool) Name() string        { return "fs/read" }
func (f *fsReadTool) Description() string { return "Read file contents" }
func (f *fsReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"offset":{"type":"integer","description":"Line offset (0-based)"},"limit":{"type":"integer","description":"Max lines to read"}},"required":["path"]}`)
}
func (f *fsReadTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	lines := strings.Split(string(data), "\n")
	if args.Offset > 0 && args.Offset < len(lines) {
		lines = lines[args.Offset:]
	}
	if args.Limit > 0 && args.Limit < len(lines) {
		lines = lines[:args.Limit]
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(lines, "\n"))}}, nil
}

// fs/write
type fsWriteTool struct{}

func (f *fsWriteTool) Name() string        { return "fs/write" }
func (f *fsWriteTool) Description() string { return "Write content to file" }
func (f *fsWriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"File content"},"backup":{"type":"boolean","description":"Create backup before writing"}},"required":["path","content"]}`)
}
func (f *fsWriteTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Backup  bool   `json:"backup"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if args.Backup {
		if _, err := os.Stat(args.Path); err == nil {
			os.Rename(args.Path, args.Path+".bak")
		}
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("written successfully")}}, nil
}

// fs/edit
type fsEditTool struct{}

func (f *fsEditTool) Name() string        { return "fs/edit" }
func (f *fsEditTool) Description() string { return "Edit file by replacing text" }
func (f *fsEditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"old_text":{"type":"string","description":"Text to replace"},"new_text":{"type":"string","description":"Replacement text"}},"required":["path","old_text","new_text"]}`)
}
func (f *fsEditTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	content := string(data)
	if !strings.Contains(content, args.OldText) {
		return &mcpproto.ToolResult{
			Content: []mcpproto.ContentBlock{mcpproto.ErrorContent("old_text not found in file")},
			IsError: true,
		}, nil
	}
	content = strings.Replace(content, args.OldText, args.NewText, 1)
	if err := os.WriteFile(args.Path, []byte(content), 0644); err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent("edited successfully")}}, nil
}

// fs/list
type fsListTool struct{}

func (f *fsListTool) Name() string        { return "fs/list" }
func (f *fsListTool) Description() string { return "List directory contents" }
func (f *fsListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path"},"recursive":{"type":"boolean","description":"List recursively"}},"required":["path"]}`)
}
func (f *fsListTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	var entries []string
	filepath.WalkDir(args.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != args.Path {
			rel, _ := filepath.Rel(args.Path, path)
			prefix := "  "
			if d.IsDir() {
				prefix = "d "
			}
			entries = append(entries, prefix+rel)
		}
		if !args.Recursive && d.IsDir() && path != args.Path {
			return filepath.SkipDir
		}
		return nil
	})
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(strings.Join(entries, "\n"))}}, nil
}

// fs/stat
type fsStatTool struct{}

func (f *fsStatTool) Name() string        { return "fs/stat" }
func (f *fsStatTool) Description() string { return "Get file/directory metadata" }
func (f *fsStatTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"}},"required":["path"]}`)
}
func (f *fsStatTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	info, err := os.Stat(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	result := fmt.Sprintf("name: %s\nsize: %d\nmode: %s\nis_dir: %v\nmod_time: %s",
		info.Name(), info.Size(), info.Mode(), info.IsDir(), info.ModTime())
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(result)}}, nil
}

func toolError(message string) *mcpproto.ToolResult {
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(message)}, IsError: true}
}

func NewFsTools() []Tool {
	return []Tool{&fsReadTool{}, &fsWriteTool{}, &fsEditTool{}, &fsListTool{}, &fsStatTool{}}
}
