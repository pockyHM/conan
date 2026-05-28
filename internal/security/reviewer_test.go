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
		NodeWhitelists: map[string][]string{"node-01": {"cat /etc/hosts", "ls", "kubectl get"}},
		Provider:       &stubProvider{},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"cat /etc/hosts"}`, []string{"node-01"})
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

func TestReviewerRequiresConfirmationForManagedFileTransfer(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Provider: &stubProvider{response: `{"risk_level":"allow","reason":"provider would allow"}`},
	})

	for _, toolName := range []string{"file_put", "file_get"} {
		assessment, err := r.Review(context.Background(), toolName, `{"node":"node-a","local_path":"x","remote_path":"/tmp/x"}`, []string{"node-a"})
		if err != nil {
			t.Fatalf("%s review: %v", toolName, err)
		}
		if assessment.Level != RiskConfirm {
			t.Fatalf("%s level = %v, want confirm", toolName, assessment.Level)
		}
		if assessment.Reason != "managed file transfer requires confirmation" {
			t.Fatalf("%s reason = %q", toolName, assessment.Reason)
		}
	}
}

func TestReviewerWhitelistRequiresExactCommand(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		NodeWhitelists: map[string][]string{"node-01": {"cat"}},
		Provider: &stubProvider{
			response: `{"risk_level":"confirm","reason":"not exact"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"cat test.sh | bash"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("prefix-like command should go to model assessment, got %v", result.Level)
	}
}

func TestReviewerBlacklistOverridesNodeWhitelist(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		NodeWhitelists: map[string][]string{"node-01": {"cat test.sh | bash"}},
		Blacklist:      []string{`.*\|\s*bash.*`},
		Provider: &stubProvider{
			response: `{"risk_level":"confirm","reason":"pipe to bash"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"cat test.sh | bash"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm || result.Reason != "pipe to bash" {
		t.Fatalf("blacklisted command should be model-assessed, got %#v", result)
	}
}

func TestReviewerWhitelistRequiresEveryTargetNode(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		NodeWhitelists: map[string][]string{
			"node-01": {"uptime"},
			"node-02": {"df -h"},
		},
		Provider: &stubProvider{
			response: `{"risk_level":"confirm","reason":"node missing allowlist entry"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"uptime"}`, []string{"node-01", "node-02"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("command not whitelisted on every target should be assessed, got %v", result.Level)
	}
}

func TestReviewerAlwaysAllowReadOnlyTools(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Provider: &stubProvider{},
	})
	readOnlyTools := []struct {
		name  string
		input string
	}{
		{"fs_read", `{"path":"/etc/hosts"}`},
		{"fs_list", `{"path":"/var/log"}`},
		{"fs_stat", `{"path":"/tmp/file"}`},
		{"sys_cpu", `{}`},
		{"sys_mem", `{}`},
		{"sys_disk", `{}`},
		{"svc_status", `{"name":"nginx"}`},
		{"svc_list", `{}`},
		{"log_read", `{"path":"/var/log/syslog"}`},
		{"net_ping", `{"host":"10.0.1.1"}`},
		{"docker_ps", `{}`},
		{"docker_images", `{}`},
		{"k8s_pods", `{}`},
		{"k8s_logs", `{"pod":"nginx"}`},
	}
	for _, tc := range readOnlyTools {
		result, err := r.Review(context.Background(), tc.name, tc.input, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result.Level != RiskAllow {
			t.Fatalf("%s should be auto-allowed, got %v", tc.name, result.Level)
		}
	}
}

func TestReviewerLocalFileReadAllowedAndWriteRequiresConfirm(t *testing.T) {
	r := NewReviewer(ReviewerConfig{})

	read, err := r.Review(context.Background(), "local_fs_read", `{"path":"README.md"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if read.Level != RiskAllow {
		t.Fatalf("local read should be allowed, got %#v", read)
	}

	write, err := r.Review(context.Background(), "local_fs_write", `{"path":"README.md","content":"x"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if write.Level != RiskConfirm {
		t.Fatalf("local write should require confirmation, got %#v", write)
	}
}

func TestReviewerAllowsWhitelistedLocalFileMutation(t *testing.T) {
	r := NewReviewer(ReviewerConfig{LocalFileWhitelist: []string{"README.md"}})

	result, err := r.Review(context.Background(), "local_fs_patch", `{"path":"README.md","old_text":"a","new_text":"b"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskAllow {
		t.Fatalf("whitelisted local file should allow mutation, got %#v", result)
	}

	r.AddLocalFileWhitelist("docs/spec.md")
	result, err = r.Review(context.Background(), "local_fs_delete", `{"path":"docs/spec.md"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskAllow {
		t.Fatalf("runtime local file allowlist should allow mutation, got %#v", result)
	}
}

func TestReviewerModelAssessment(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Provider: &stubProvider{
			response: `{"risk_level":"confirm","reason":"Restarts service causing downtime","suggestion":"Use rolling restart"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"systemctl restart nginx"}`, []string{"node-01"})
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
		Provider: &stubProvider{
			response: `{"risk_level":"deny","reason":"Destructive operation"}`,
		},
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"rm -rf /"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskDeny {
		t.Fatalf("expected deny, got %v", result.Level)
	}
}

func TestReviewerSessionCache(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		Provider: &stubProvider{
			response: `{"risk_level":"allow","reason":"ok"}`,
		},
	})
	result1, err := r.Review(context.Background(), "shell_run", `{"command":"uptime"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	result2, err := r.Review(context.Background(), "shell_run", `{"command":"uptime"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if result1.Level != result2.Level {
		t.Fatal("cached result should match original")
	}
}

func TestReviewerNoProviderDefaultsToConfirm(t *testing.T) {
	r := NewReviewer(ReviewerConfig{
		NodeWhitelists: map[string][]string{"node-01": {"cat"}},
		Provider:       nil,
	})
	result, err := r.Review(context.Background(), "shell_run", `{"command":"systemctl restart nginx"}`, []string{"node-01"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("without provider, non-whitelisted should default to confirm, got %v", result.Level)
	}
}

func TestReviewerNodeAddNoProviderRequiresConfirm(t *testing.T) {
	r := NewReviewer(ReviewerConfig{Provider: nil})
	result, err := r.Review(context.Background(), "node_add", `{"host":"10.0.0.12","user":"deploy","password":"[REDACTED]"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("without provider, node_add should require confirmation, got %v", result.Level)
	}
}
