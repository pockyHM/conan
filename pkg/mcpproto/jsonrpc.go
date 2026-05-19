package mcpproto

import "encoding/json"

const JSONRPCVersion = "2.0"

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return e.Message
}

func NewSuccessResponse(id json.RawMessage, result interface{}) *JSONRPCResponse {
	return &JSONRPCResponse{JSONRPC: JSONRPCVersion, ID: id, Result: result}
}

func NewErrorResponse(id json.RawMessage, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}

func NewParseError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32700, "Parse error")
}

func NewInvalidRequestError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32600, "Invalid request")
}

func NewMethodNotFoundError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32601, "Method not found")
}

func NewInvalidParamsError(id json.RawMessage) *JSONRPCResponse {
	return NewErrorResponse(id, -32602, "Invalid params")
}

func NewInternalError(id json.RawMessage, msg string) *JSONRPCResponse {
	return NewErrorResponse(id, -32603, "Internal error: "+msg)
}
