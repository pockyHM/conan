package nodeupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pockyHM/conan/internal/agentupdate"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/pkg/mcpproto"
)

type AgentTarget struct {
	Host    string
	Port    int
	TLS     bool
	Token   string
	Request agentupdate.Request
}

type AgentUpdater interface {
	Update(ctx context.Context, target AgentTarget) error
}

type MCPAgentUpdater struct {
	HTTPClient     *http.Client
	BaseURL        func(AgentTarget) string
	HealthAttempts int
	HealthDelay    time.Duration
	// RestartDelay waits after a successful update RPC before health polling.
	// Zero uses the production default; a negative value skips the wait.
	RestartDelay time.Duration
}

func (u MCPAgentUpdater) Update(ctx context.Context, target AgentTarget) error {
	baseURL := mcp.URL(target.Host, target.Port, target.TLS)
	if u.BaseURL != nil {
		baseURL = u.BaseURL(target)
	}

	client := mcp.NewClient(mcp.Config{
		BaseURL: baseURL,
		Token:   target.Token,
		Client:  u.HTTPClient,
	})

	data, err := json.Marshal(target.Request)
	if err != nil {
		return err
	}
	result, err := client.CallTool(ctx, "agent_update", json.RawMessage(data))
	if err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("agent_update failed: %s", toolText(result))
	}

	if delay := u.restartDelay(); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	attempts := u.HealthAttempts
	if attempts == 0 {
		attempts = 10
	}
	delay := u.HealthDelay
	if delay == 0 {
		delay = 250 * time.Millisecond
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := client.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i == attempts-1 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("agent health check after update failed: %w", lastErr)
}

func (u MCPAgentUpdater) restartDelay() time.Duration {
	if u.RestartDelay < 0 {
		return 0
	}
	if u.RestartDelay == 0 {
		return 1500 * time.Millisecond
	}
	return u.RestartDelay
}

func toolText(result *mcpproto.ToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
