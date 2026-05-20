package security

import (
	"context"
	"testing"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

// stubProvider is a test double for llm.Provider.
type stubProvider struct {
	response string
	err      error
}

func (s *stubProvider) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Message:    models.Message{Role: "assistant", Content: s.response},
		StopReason: llm.StopEndTurn,
	}, s.err
}

func (s *stubProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	return nil, nil
}

func TestReviewerWhitelistBypass(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: []string{"cat", "ls", "kubectl get"},
		Provider:  &stubProvider{},
	})
	result, err := r.Review(context.Background(), "shell/run", `{"command":"cat /etc/hosts"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskAllow {
		t.Fatalf("whitelisted command should auto-allow, got %v", result.Level)
	}
	if result.Reason != "whitelisted" {
		t.Fatalf("reason = %q, want 'whitelisted'", result.Reason)
	}
}

func TestReviewerAlwaysAllowReadOnlyTools(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: nil,
		Provider:  &stubProvider{},
	})
	readOnlyTools := []struct {
		name  string
		input string
	}{
		{"fs/read", `{"path":"/etc/hosts"}`},
		{"fs/list", `{"path":"/var/log"}`},
		{"fs/stat", `{"path":"/tmp/file"}`},
		{"sys/cpu", `{}`},
		{"sys/mem", `{}`},
		{"sys/disk", `{}`},
		{"svc/status", `{"name":"nginx"}`},
		{"svc/list", `{}`},
		{"log/read", `{"path":"/var/log/syslog"}`},
		{"net/ping", `{"host":"10.0.1.1"}`},
		{"docker/ps", `{}`},
		{"docker/images", `{}`},
		{"k8s/pods", `{}`},
		{"k8s/logs", `{"pod":"nginx"}`},
	}
	for _, tc := range readOnlyTools {
		result, err := r.Review(context.Background(), tc.name, tc.input)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result.Level != RiskAllow {
			t.Fatalf("%s should be auto-allowed, got %v", tc.name, result.Level)
		}
	}
}

func TestReviewerModelAssessment(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: []string{"cat"},
		Provider: &stubProvider{
			response: `{"risk_level":"confirm","reason":"Restarts service causing downtime","suggestion":"Use rolling restart"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell/run", `{"command":"systemctl restart nginx"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("expected confirm, got %v", result.Level)
	}
	if result.Reason != "Restarts service causing downtime" {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestReviewerModelDeny(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: []string{},
		Provider: &stubProvider{
			response: `{"risk_level":"deny","reason":"Destructive operation"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell/run", `{"command":"rm -rf /"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskDeny {
		t.Fatalf("expected deny, got %v", result.Level)
	}
}

func TestReviewerSessionCache(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: []string{},
		Provider: &stubProvider{
			response: `{"risk_level":"allow","reason":"ok"}`,
		},
	})
	result1, err := r.Review(context.Background(), "shell/run", `{"command":"uptime"}`)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := r.Review(context.Background(), "shell/run", `{"command":"uptime"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result1.Level != result2.Level {
		t.Fatal("cached result should match original")
	}
}

func TestReviewerNoProviderDefaultsToConfirm(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Whitelist: []string{"cat"},
		Provider:  nil,
	})
	result, err := r.Review(context.Background(), "shell/run", `{"command":"systemctl restart nginx"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("without provider, non-whitelisted should default to confirm, got %v", result.Level)
	}
}
