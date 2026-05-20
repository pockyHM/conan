package security

import "testing"

func TestBuildRiskPromptContainsToolInfo(t *testing.T) {
	prompt := BuildRiskPrompt("shell/run", `{"command":"rm -rf /var/log"}`)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	for _, substr := range []string{"shell/run", "rm -rf", "risk_level"} {
		if !contains(prompt, substr) {
			t.Fatalf("prompt missing %q", substr)
		}
	}
}

func TestParseRiskResponseAllow(t *testing.T) {
	input := `{"risk_level":"allow","reason":"Low risk read operation"}`
	result, err := ParseRiskResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskAllow {
		t.Fatalf("level = %v, want RiskAllow", result.Level)
	}
	if result.Reason != "Low risk read operation" {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestParseRiskResponseConfirm(t *testing.T) {
	input := `{"risk_level":"confirm","reason":"Restarts service, may cause brief downtime","suggestion":"Consider rolling restart"}`
	result, err := ParseRiskResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskConfirm {
		t.Fatalf("level = %v, want RiskConfirm", result.Level)
	}
	if result.Suggestion != "Consider rolling restart" {
		t.Fatalf("suggestion = %q", result.Suggestion)
	}
}

func TestParseRiskResponseDeny(t *testing.T) {
	input := `{"risk_level":"deny","reason":"Destructive operation targeting critical path"}`
	result, err := ParseRiskResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskDeny {
		t.Fatalf("level = %v, want RiskDeny", result.Level)
	}
}

func TestParseRiskResponseWithMarkdownFence(t *testing.T) {
	input := "```json\n{\"risk_level\":\"allow\",\"reason\":\"ok\"}\n```"
	result, err := ParseRiskResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != RiskAllow {
		t.Fatalf("level = %v, want RiskAllow", result.Level)
	}
}

func TestParseRiskResponseInvalid(t *testing.T) {
	_, err := ParseRiskResponse("not json at all")
	if err == nil {
		t.Fatal("should error on invalid JSON")
	}
}

func TestParseRiskResponseInvalidLevel(t *testing.T) {
	input := `{"risk_level":"unknown","reason":"test"}`
	_, err := ParseRiskResponse(input)
	if err == nil {
		t.Fatal("should error on invalid risk level")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
