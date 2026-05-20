package security

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

var readOnlyTools = map[string]bool{
	"fs/read": true, "fs/list": true, "fs/stat": true, "fs/download": true,
	"sys/cpu": true, "sys/mem": true, "sys/disk": true, "sys/net": true, "sys/processes": true,
	"svc/status": true, "svc/list": true,
	"log/read": true, "log/journalctl": true,
	"net/ping": true, "net/traceroute": true, "net/portcheck": true, "net/curl": true,
	"docker/ps": true, "docker/images": true, "docker/logs": true,
	"k8s/pods": true, "k8s/logs": true, "k8s/events": true, "k8s/describe": true,
	"pkg/list": true, "pkg/search": true,
	"cron/list": true, "cron/show": true,
}

// ReviewerConfig holds configuration for creating a new Reviewer.
type ReviewerConfig struct {
	Whitelist []string
	Provider  llm.Provider
	ModelName string
}

// Reviewer implements the two-stage security review pipeline:
// 1. Read-only tools are auto-allowed
// 2. shell/run commands are checked against a whitelist
// 3. Everything else goes to an LLM-based risk assessment
type Reviewer struct {
	whitelist Whitelist
	provider  llm.Provider
	modelName string
	cache     map[string]RiskAssessment
}

// NewReviewer creates a new Reviewer with the given configuration.
func NewReviewer(cfg ReviewerConfig) *Reviewer {
	return &Reviewer{
		whitelist: NewWhitelist(cfg.Whitelist),
		provider:  cfg.Provider,
		modelName: cfg.ModelName,
		cache:     make(map[string]RiskAssessment),
	}
}

// Review performs a two-stage security review of a tool call.
// Read-only tools are auto-allowed. Whitelisted shell commands are auto-allowed.
// Everything else is assessed by the LLM provider (with session caching).
func (r *Reviewer) Review(ctx context.Context, toolName, toolInput string) (RiskAssessment, error) {
	// Stage 1: read-only tools are always allowed
	if readOnlyTools[toolName] {
		return RiskAssessment{Level: RiskAllow, Reason: "read-only tool"}, nil
	}

	// Stage 2: whitelist check for shell commands
	if toolName == "shell/run" {
		cmd, err := extractCommand(toolInput)
		if err == nil && r.whitelist.Match(cmd) {
			return RiskAssessment{Level: RiskAllow, Reason: "whitelisted"}, nil
		}
	}

	// Check session cache before calling the model
	cacheKey := toolName + ":" + toolInput
	if cached, ok := r.cache[cacheKey]; ok {
		return cached, nil
	}

	// No provider configured — default to confirm
	if r.provider == nil {
		return RiskAssessment{
			Level:  RiskConfirm,
			Reason: "no risk assessment model configured — requiring confirmation",
		}, nil
	}

	// Stage 3: LLM-based risk assessment
	prompt := BuildRiskPrompt(toolName, toolInput)
	resp, err := r.provider.Chat(ctx, &llm.ChatRequest{
		SystemPrompt: prompt,
		Messages:     []models.Message{{Role: "user", Content: "Assess the risk of this tool call."}},
		MaxTokens:    256,
	})
	if err != nil {
		return RiskAssessment{}, fmt.Errorf("risk assessment failed: %w", err)
	}

	assessment, err := ParseRiskResponse(resp.Message.Content)
	if err != nil {
		return RiskAssessment{
			Level:  RiskConfirm,
			Reason: "could not parse risk assessment: " + err.Error(),
		}, nil
	}

	r.cache[cacheKey] = assessment
	return assessment, nil
}

// extractCommand parses the command field from a shell/run tool input JSON.
func extractCommand(toolInput string) (string, error) {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolInput), &input); err != nil {
		return "", err
	}
	return input.Command, nil
}
