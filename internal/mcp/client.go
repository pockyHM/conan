package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type Config struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	nextID  int64
}

type toolsListResult struct {
	Tools []mcpproto.ToolDefinition `json:"tools"`
}

func NewClient(cfg Config) *Client {
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    httpClient,
	}
}

func URL(host string, port int, tls bool) string {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health check failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) Initialize(ctx context.Context) (*mcpproto.InitializeResult, error) {
	data, err := c.rpc(ctx, "initialize", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var result mcpproto.InitializeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListTools(ctx context.Context) ([]mcpproto.ToolDefinition, error) {
	data, err := c.rpc(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result toolsListResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*mcpproto.ToolResult, error) {
	data, err := c.rpc(ctx, "tools/call", mcpproto.ToolCallParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var result mcpproto.ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) rpc(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	reqBody, err := json.Marshal(mcpproto.JSONRPCRequest{
		JSONRPC: mcpproto.JSONRPCVersion,
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsData,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      json.RawMessage        `json:"id"`
		Result  json.RawMessage        `json:"result"`
		Error   *mcpproto.JSONRPCError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if envelope.JSONRPC != mcpproto.JSONRPCVersion {
		return nil, fmt.Errorf("invalid jsonrpc version: %s", envelope.JSONRPC)
	}
	if string(envelope.ID) != fmt.Sprintf("%d", id) {
		return nil, fmt.Errorf("mismatched response id: got %s want %d", string(envelope.ID), id)
	}
	if envelope.Error != nil {
		if envelope.Result != nil {
			return nil, fmt.Errorf("json-rpc response contains both result and error")
		}
		return nil, envelope.Error
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("json-rpc response missing result")
	}
	return envelope.Result, nil
}

func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
