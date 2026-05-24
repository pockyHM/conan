package security

import (
	"testing"

	"github.com/pockyHM/conan/internal/tools"
)

func TestPolicyEvaluateReadOnlyToolMetadata(t *testing.T) {
	decision := NewPolicy(tools.DefaultMetadata()).Evaluate("svc/status", `{"name":"nginx"}`, []string{"node-a"})

	if decision.Level != RiskAllow {
		t.Fatalf("level = %v, want allow", decision.Level)
	}
	if decision.Reason != "read-only tool metadata" {
		t.Fatalf("reason = %q", decision.Reason)
	}
	if decision.ContinueLLM {
		t.Fatal("read-only metadata should not continue to LLM")
	}
}

func TestPolicyEvaluateMutatingToolMetadata(t *testing.T) {
	decision := NewPolicy(tools.DefaultMetadata()).Evaluate("file_put", `{"node":"node-a"}`, []string{"node-a"})

	if decision.Level != RiskConfirm {
		t.Fatalf("level = %v, want confirm", decision.Level)
	}
	if decision.Reason != "mutating tool metadata requires confirmation" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestPolicyEvaluateCallToolInnerMetadata(t *testing.T) {
	policy := NewPolicy(tools.DefaultMetadata())

	read := policy.Evaluate("call_tool", `{"node":"node-a","tool":"svc/status","arguments":{"name":"nginx"}}`, []string{"node-a"})
	if read.Level != RiskAllow {
		t.Fatalf("call_tool svc/status = %#v, want allow", read)
	}

	missing := policy.Evaluate("call_tool", `{"node":"node-a","tool":"svc/restart","arguments":{"name":"nginx"}}`, []string{"node-a"})
	if missing.Level != RiskConfirm {
		t.Fatalf("call_tool svc/restart = %#v, want confirm", missing)
	}
}

func TestPolicyEvaluateDestructiveContinuesReview(t *testing.T) {
	decision := NewPolicy(tools.DefaultMetadata()).Evaluate("exec", `{"command":"uptime"}`, []string{"node-a"})

	if !decision.ContinueLLM {
		t.Fatal("destructive metadata should continue to whitelist/model review")
	}
	if decision.Decided {
		t.Fatalf("destructive decision should not be final: %#v", decision)
	}
}

func TestPolicyEvaluateUnknownToolConfirms(t *testing.T) {
	decision := NewPolicy(tools.DefaultMetadata()).Evaluate("missing/tool", `{}`, nil)

	if decision.Level != RiskConfirm {
		t.Fatalf("level = %v, want confirm", decision.Level)
	}
	if decision.Reason != "missing tool metadata" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}
