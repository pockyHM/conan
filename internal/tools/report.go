package tools

import (
	"context"
	"encoding/json"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type webReportTool struct{}

func (w *webReportTool) Name() string { return "web_report" }

func (w *webReportTool) Description() string {
	return "Render Markdown as a local browser report and provide a Markdown download link"
}

func (w *webReportTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Optional report title"},"markdown":{"type":"string","description":"Markdown report content to render"},"filename":{"type":"string","description":"Optional Markdown download filename"}},"required":["markdown"]}`)
}

func (w *webReportTool) Execute(ctx context.Context, input json.RawMessage) (*mcpproto.ToolResult, error) {
	return toolError("web_report is not implemented yet"), nil
}
