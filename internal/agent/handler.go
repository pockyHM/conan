package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type Handler struct {
	registry *tools.Registry
	version  string
}

func NewHandler(registry *tools.Registry, version string) *Handler {
	return &Handler{registry: registry, version: version}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var req mcpproto.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, mcpproto.NewParseError(json.RawMessage("null")))
		return
	}

	if req.JSONRPC != mcpproto.JSONRPCVersion {
		writeJSON(w, mcpproto.NewInvalidRequestError(req.ID))
		return
	}

	var resp *mcpproto.JSONRPCResponse
	switch req.Method {
	case "initialize":
		resp = h.handleInitialize(req.ID)
	case "tools/list":
		resp = h.handleToolsList(req.ID)
	case "tools/call":
		resp = h.handleToolsCall(r.Context(), req)
	default:
		resp = mcpproto.NewMethodNotFoundError(req.ID)
	}

	writeJSON(w, resp)
}

func (h *Handler) handleInitialize(id json.RawMessage) *mcpproto.JSONRPCResponse {
	result := mcpproto.InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: mcpproto.ServerCapabilities{
			Tools: &mcpproto.ToolsCapability{ListChanged: false},
		},
		ServerInfo: mcpproto.ServerInfo{
			Name:    "conan-agent",
			Version: h.version,
		},
	}
	return mcpproto.NewSuccessResponse(id, result)
}

func (h *Handler) handleToolsList(id json.RawMessage) *mcpproto.JSONRPCResponse {
	toolList := h.registry.List()
	defs := make([]mcpproto.ToolDefinition, len(toolList))
	for i, t := range toolList {
		defs[i] = mcpproto.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		}
	}
	return mcpproto.NewSuccessResponse(id, map[string]interface{}{"tools": defs})
}

func (h *Handler) handleToolsCall(ctx context.Context, req mcpproto.JSONRPCRequest) *mcpproto.JSONRPCResponse {
	var params mcpproto.ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return mcpproto.NewInvalidParamsError(req.ID)
	}

	tool, ok := h.registry.Get(params.Name)
	if !ok {
		return mcpproto.NewErrorResponse(req.ID, -32602, "tool not found: "+params.Name)
	}

	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		slog.Error("tool execution failed", "tool", params.Name, "error", err)
		return mcpproto.NewInternalError(req.ID, err.Error())
	}

	return mcpproto.NewSuccessResponse(req.ID, result)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
