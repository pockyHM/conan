package security

import (
	"encoding/json"

	"github.com/pockyHM/conan/internal/tools"
)

type PolicyDecision struct {
	Level       RiskLevel
	Reason      string
	ContinueLLM bool
	Decided     bool
}

type Policy struct {
	Metadata map[string]tools.Metadata
}

func NewPolicy(metadata map[string]tools.Metadata) Policy {
	if metadata == nil {
		metadata = tools.DefaultMetadata()
	}
	return Policy{Metadata: metadata}
}

func (p Policy) Evaluate(toolName string, toolInput string, targetNodes []string) PolicyDecision {
	if toolName == "call_tool" {
		var args struct {
			Tool string `json:"tool"`
		}
		if err := json.Unmarshal([]byte(toolInput), &args); err == nil && args.Tool != "" {
			return p.evaluateDirect(args.Tool)
		}
		return PolicyDecision{Level: RiskConfirm, Reason: "call_tool missing inner tool", Decided: true}
	}
	return p.evaluateDirect(toolName)
}

func (p Policy) evaluateDirect(toolName string) PolicyDecision {
	meta, ok := p.Metadata[toolName]
	if !ok {
		return PolicyDecision{Level: RiskConfirm, Reason: "missing tool metadata", Decided: true}
	}
	switch meta.Safety {
	case tools.SafetyReadOnly:
		return PolicyDecision{Level: RiskAllow, Reason: "read-only tool metadata", Decided: true}
	case tools.SafetyMutating:
		return PolicyDecision{Level: RiskConfirm, Reason: "mutating tool metadata requires confirmation", Decided: true}
	case tools.SafetyDestructive:
		return PolicyDecision{ContinueLLM: true, Reason: "destructive tool metadata requires review"}
	default:
		return PolicyDecision{Level: RiskConfirm, Reason: "unknown tool safety metadata", Decided: true}
	}
}
