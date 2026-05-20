package security

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RiskLevel int

const (
	RiskAllow   RiskLevel = iota
	RiskConfirm
	RiskDeny
)

type RiskAssessment struct {
	Level      RiskLevel
	Reason     string
	Suggestion string
}

func BuildRiskPrompt(toolName, toolInput string) string {
	return fmt.Sprintf(`You are a security reviewer for an operations assistant. Assess the risk of the following tool call.

Tool: %s
Input: %s

Classify the risk level as one of:
- "allow": Safe read-only or low-risk operation (e.g., reading files, checking status, listing resources)
- "confirm": Moderate risk that needs user approval (e.g., restarting services, modifying configs, installing packages)
- "deny": Destructive or dangerous operation that should be refused (e.g., rm -rf /, iptables -F, DROP TABLE, reboot on production)

Respond with JSON only:
{"risk_level":"allow|confirm|deny","reason":"...","suggestion":"safer alternative if applicable"}`, toolName, toolInput)
}

func ParseRiskResponse(input string) (RiskAssessment, error) {
	text := strings.TrimSpace(input)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var raw struct {
		RiskLevel  string `json:"risk_level"`
		Reason     string `json:"reason"`
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return RiskAssessment{}, fmt.Errorf("parse risk response: %w", err)
	}

	var level RiskLevel
	switch raw.RiskLevel {
	case "allow":
		level = RiskAllow
	case "confirm":
		level = RiskConfirm
	case "deny":
		level = RiskDeny
	default:
		return RiskAssessment{}, fmt.Errorf("unknown risk_level: %q", raw.RiskLevel)
	}

	return RiskAssessment{
		Level:      level,
		Reason:     raw.Reason,
		Suggestion: raw.Suggestion,
	}, nil
}
