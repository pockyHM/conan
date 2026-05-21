package mcp

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestClassifyConnectionRefused(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	ce := ClassifyError(err)
	if ce.Type != ErrorConnection {
		t.Fatalf("expected ErrorConnection, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("connection refused should not be retryable without user action")
	}
}

func TestClassifyNoSuchHost(t *testing.T) {
	err := errors.New("dial tcp: lookup node.invalid: no such host")
	ce := ClassifyError(err)
	if ce.Type != ErrorConnection {
		t.Fatalf("expected ErrorConnection, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("no such host should not be retryable without user action")
	}
}

func TestClassifyNetworkUnreachable(t *testing.T) {
	err := errors.New("dial tcp 10.0.0.1:9280: connect: network is unreachable")
	ce := ClassifyError(err)
	if ce.Type != ErrorConnection {
		t.Fatalf("expected ErrorConnection, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("network unreachable should not be retryable without user action")
	}
}

func TestClassifyCommonConnectionLossMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "connection reset by peer", err: errors.New("read tcp 10.0.0.2:443->10.0.0.1:54832: read: connection reset by peer")},
		{name: "broken pipe", err: errors.New("write tcp 10.0.0.1:54832->10.0.0.2:443: write: broken pipe")},
		{name: "unexpected EOF", err: errors.New("Post \"https://node/rpc\": unexpected EOF")},
		{name: "EOF", err: errors.New("Post \"https://node/rpc\": EOF")},
		{name: "no route to host", err: errors.New("dial tcp 10.0.0.2:443: connect: no route to host")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := ClassifyError(tt.err)
			if ce.Type != ErrorConnection {
				t.Fatalf("expected ErrorConnection, got %v", ce.Type)
			}
			if ce.Retryable {
				t.Fatal("connection loss should not be retryable without user action")
			}
		})
	}
}

func TestClassifyTimeout(t *testing.T) {
	ce := ClassifyError(context.DeadlineExceeded)
	if ce.Type != ErrorTimeout {
		t.Fatalf("expected ErrorTimeout, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("timeout should be retryable")
	}
}

func TestClassifyAuth(t *testing.T) {
	err := &rpcError{HTTPStatus: 401, Message: "unauthorized"}
	ce := ClassifyError(err)
	if ce.Type != ErrorAuth {
		t.Fatalf("expected ErrorAuth, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("auth error should not be retryable")
	}
}

func TestClassifyForbiddenMessage(t *testing.T) {
	err := errors.New("request forbidden")
	ce := ClassifyError(err)
	if ce.Type != ErrorAuth {
		t.Fatalf("expected ErrorAuth, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("forbidden error should not be retryable")
	}
}

func TestClassifyRateLimit(t *testing.T) {
	err := &rpcError{Code: -32000, HTTPStatus: 429, Message: "rate limit"}
	ce := ClassifyError(err)
	if ce.Type != ErrorRateLimit {
		t.Fatalf("expected ErrorRateLimit, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("rate limit should be retryable")
	}
}

func TestClassifyServerErr(t *testing.T) {
	err := &rpcError{Code: -32603, HTTPStatus: 500, Message: "internal error"}
	ce := ClassifyError(err)
	if ce.Type != ErrorServer {
		t.Fatalf("expected ErrorServer, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("server error should be retryable")
	}
}

func TestClassifyJSONRPCInternalError(t *testing.T) {
	err := &mcpproto.JSONRPCError{Code: -32603, Message: "Internal error: database unavailable"}
	ce := ClassifyError(err)
	if ce.Type != ErrorServer {
		t.Fatalf("expected ErrorServer, got %v", ce.Type)
	}
	if !ce.Retryable {
		t.Fatal("JSON-RPC internal error should be retryable")
	}
}

func TestClassifyJSONRPCAuthMessage(t *testing.T) {
	err := &mcpproto.JSONRPCError{Code: -32000, Message: "unauthorized request"}
	ce := ClassifyError(err)
	if ce.Type != ErrorAuth {
		t.Fatalf("expected ErrorAuth, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("JSON-RPC auth error should not be retryable")
	}
}

func TestClassifyUnknown(t *testing.T) {
	err := errors.New("something weird")
	ce := ClassifyError(err)
	if ce.Type != ErrorUnknown {
		t.Fatalf("expected ErrorUnknown, got %v", ce.Type)
	}
	if ce.Retryable {
		t.Fatal("unknown error should not be retryable")
	}
}
