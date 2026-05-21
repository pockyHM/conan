package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

type ErrorType int

const (
	ErrorConnection ErrorType = iota
	ErrorTimeout
	ErrorAuth
	ErrorRateLimit
	ErrorServer
	ErrorUnknown
)

func (t ErrorType) String() string {
	switch t {
	case ErrorConnection:
		return "connection"
	case ErrorTimeout:
		return "timeout"
	case ErrorAuth:
		return "auth"
	case ErrorRateLimit:
		return "rate limit"
	case ErrorServer:
		return "server"
	default:
		return "unknown"
	}
}

type ClassifiedError struct {
	Type      ErrorType
	Retryable bool
	Original  error
}

func (c *ClassifiedError) Error() string {
	if c == nil || c.Original == nil {
		return ""
	}
	return c.Original.Error()
}

func (c *ClassifiedError) Unwrap() error {
	if c == nil {
		return nil
	}
	return c.Original
}

type rpcError struct {
	Code       int
	HTTPStatus int
	Message    string
}

func (e *rpcError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("rpc http status %d", e.HTTPStatus)
	}
	return fmt.Sprintf("rpc error code %d", e.Code)
}

func ClassifyError(err error) *ClassifiedError {
	if err == nil {
		return &ClassifiedError{Type: ErrorUnknown, Retryable: false}
	}

	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified
	}

	var rpc *rpcError
	if errors.As(err, &rpc) {
		switch {
		case rpc.HTTPStatus == httpStatusUnauthorized || rpc.HTTPStatus == httpStatusForbidden:
			return &ClassifiedError{Type: ErrorAuth, Retryable: false, Original: err}
		case rpc.HTTPStatus == httpStatusTooManyRequests:
			return &ClassifiedError{Type: ErrorRateLimit, Retryable: true, Original: err}
		case rpc.HTTPStatus >= httpStatusInternalServerError && rpc.HTTPStatus < 600:
			return &ClassifiedError{Type: ErrorServer, Retryable: true, Original: err}
		}
	}

	var jsonRPC *mcpproto.JSONRPCError
	if errors.As(err, &jsonRPC) {
		msg := strings.ToLower(jsonRPC.Message)
		switch {
		case isAuthMessage(msg):
			return &ClassifiedError{Type: ErrorAuth, Retryable: false, Original: err}
		case jsonRPC.Code == jsonRPCInternalErrorCode:
			return &ClassifiedError{Type: ErrorServer, Retryable: true, Original: err}
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
		return &ClassifiedError{Type: ErrorTimeout, Retryable: true, Original: err}
	}

	msg := strings.ToLower(err.Error())

	var opErr *net.OpError
	if errors.As(err, &opErr) && isConnectionMessage(msg) {
		return &ClassifiedError{Type: ErrorConnection, Retryable: false, Original: err}
	}
	switch {
	case isConnectionMessage(msg):
		return &ClassifiedError{Type: ErrorConnection, Retryable: false, Original: err}
	case strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return &ClassifiedError{Type: ErrorTimeout, Retryable: true, Original: err}
	case isAuthMessage(msg):
		return &ClassifiedError{Type: ErrorAuth, Retryable: false, Original: err}
	default:
		return &ClassifiedError{Type: ErrorUnknown, Retryable: false, Original: err}
	}
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isConnectionMessage(msg string) bool {
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		msg == "eof" ||
		strings.HasSuffix(msg, ": eof") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host")
}

func isAuthMessage(msg string) bool {
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden")
}

const (
	httpStatusUnauthorized        = 401
	httpStatusForbidden           = 403
	httpStatusTooManyRequests     = 429
	httpStatusInternalServerError = 500
	jsonRPCInternalErrorCode      = -32603
)
