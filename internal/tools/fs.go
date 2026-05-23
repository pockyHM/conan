package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pockyHM/conan/internal/fileguard"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

// fs/read
type fsReadTool struct{}

func (f *fsReadTool) Name() string { return "fs/read" }
func (f *fsReadTool) Description() string {
	return "Read text file contents. Binary and image files are refused. Output is capped at 64KiB unless a smaller max_bytes is provided."
}
func (f *fsReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"offset":{"type":"integer","description":"Line offset (0-based)"},"limit":{"type":"integer","description":"Max lines to read"},"max_bytes":{"type":"integer","description":"Max output bytes, capped at 65536"}},"required":["path"]}`)
}
func (f *fsReadTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path     string `json:"path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if err := fileguard.ValidateTextFile(args.Path); err != nil {
		return toolError(err.Error()), nil
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	lines := strings.Split(string(data), "\n")
	if args.Offset > 0 {
		if args.Offset >= len(lines) {
			lines = nil
		} else {
			lines = lines[args.Offset:]
		}
	}
	if args.Limit > 0 && args.Limit < len(lines) {
		lines = lines[:args.Limit]
	}
	out, _ := fileguard.LimitTextOutput(strings.Join(lines, "\n"), args.MaxBytes)
	return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(out)}}, nil
}

// fs/write
type fsWriteTool struct{}

func (f *fsWriteTool) Name() string { return "fs/write" }
func (f *fsWriteTool) Description() string {
	return "Write text content to a file. Binary and image paths/content are refused."
}
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
	if err := fileguard.ValidateTextPath(args.Path); err != nil {
		return toolError(err.Error()), nil
	}
	if err := fileguard.ValidateTextContent(args.Content); err != nil {
		return toolError(err.Error()), nil
	}
	if _, err := os.Stat(args.Path); err == nil {
		if err := fileguard.ValidateTextFile(args.Path); err != nil {
			return toolError(err.Error()), nil
		}
	} else if !os.IsNotExist(err) {
		return toolError(err.Error()), nil
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

func (f *fsEditTool) Name() string { return "fs/edit" }
func (f *fsEditTool) Description() string {
	return "Edit a text file. Use old_text/new_text for exact replacement, or start_line/end_line/content for 1-based inclusive line range replacement. Binary and image files are refused."
}
func (f *fsEditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"old_text":{"type":"string","description":"Text to replace"},"new_text":{"type":"string","description":"Replacement text"},"start_line":{"type":"integer","description":"1-based first line to replace"},"end_line":{"type":"integer","description":"1-based last line to replace, inclusive. Defaults to start_line."},"content":{"type":"string","description":"Replacement content for line range mode"}},"required":["path"]}`)
}
func (f *fsEditTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	var args struct {
		Path      string `json:"path"`
		OldText   string `json:"old_text"`
		NewText   string `json:"new_text"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if err := fileguard.ValidateTextFile(args.Path); err != nil {
		return toolError(err.Error()), nil
	}
	replacement := args.NewText
	if args.StartLine > 0 {
		replacement = args.Content
	}
	if err := fileguard.ValidateTextContent(replacement); err != nil {
		return toolError(err.Error()), nil
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
	}
	content := string(data)
	if args.StartLine > 0 {
		updated, err := fileguard.ReplaceLineRange(content, args.StartLine, args.EndLine, args.Content)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if err := os.WriteFile(args.Path, []byte(updated), 0644); err != nil {
			return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.ErrorContent(err.Error())}, IsError: true}, nil
		}
		return &mcpproto.ToolResult{Content: []mcpproto.ContentBlock{mcpproto.TextContent(fmt.Sprintf("edited lines %d-%d successfully", args.StartLine, fileguard.EffectiveEndLine(args.StartLine, args.EndLine)))}}, nil
	}
	if args.OldText == "" && args.NewText == "" {
		return toolError("edit requires old_text/new_text or start_line/content"), nil
	}
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
