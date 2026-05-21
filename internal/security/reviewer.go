package security

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	"tool_search": true,
}

// ReviewerConfig holds configuration for creating a new Reviewer.
type ReviewerConfig struct {
	NodeWhitelists map[string][]string
	Blacklist      []string
	Provider       llm.Provider
	ModelName      string
}

// Reviewer implements the two-stage security review pipeline:
// 1. Read-only tools are auto-allowed
// 2. shell/run commands are checked against a whitelist
// 3. Everything else goes to an LLM-based risk assessment
type Reviewer struct {
	nodeWhitelists map[string]Whitelist
	blacklist      Blacklist
	provider       llm.Provider
	modelName      string
	cache          map[string]RiskAssessment
}

// NewReviewer creates a new Reviewer with the given configuration.
func NewReviewer(cfg ReviewerConfig) *Reviewer {
	nodeWhitelists := make(map[string]Whitelist, len(cfg.NodeWhitelists))
	for node, entries := range cfg.NodeWhitelists {
		nodeWhitelists[node] = NewWhitelist(entries)
	}
	return &Reviewer{
		nodeWhitelists: nodeWhitelists,
		blacklist:      NewBlacklist(cfg.Blacklist),
		provider:       cfg.Provider,
		modelName:      cfg.ModelName,
		cache:          make(map[string]RiskAssessment),
	}
}

// Review performs a two-stage security review of a tool call.
// Read-only tools are auto-allowed. Whitelisted shell commands are auto-allowed.
// Everything else is assessed by the LLM provider (with session caching).
func (r *Reviewer) Review(ctx context.Context, toolName, toolInput string, targetNodes []string) (RiskAssessment, error) {
	// Stage 1: read-only tools are always allowed
	if readOnlyTools[toolName] {
		return RiskAssessment{Level: RiskAllow, Reason: "read-only tool"}, nil
	}

	// Stage 2: whitelist check for shell commands
	if toolName == "shell/run" || toolName == "exec" {
		cmd, err := extractCommand(toolInput)
		if err == nil && !r.blacklist.Match(cmd) && r.allTargetsWhitelisted(targetNodes, cmd) {
			return RiskAssessment{Level: RiskAllow, Reason: "whitelisted"}, nil
		}
	}

	// call_tool delegates to an inner tool
	if toolName == "call_tool" {
		var args struct {
			Tool string `json:"tool"`
		}
		if err := json.Unmarshal([]byte(toolInput), &args); err == nil && readOnlyTools[args.Tool] {
			return RiskAssessment{Level: RiskAllow, Reason: "read-only tool"}, nil
		}
	}

	// Check session cache before calling the model
	cacheKey := toolName + ":" + toolInput + ":" + strings.Join(sortedCopy(targetNodes), ",")
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
			Reason: "risk assessment unavailable, requires confirmation",
		}, nil
	}

	r.cache[cacheKey] = assessment
	return assessment, nil
}

func (r *Reviewer) allTargetsWhitelisted(targetNodes []string, command string) bool {
	if len(targetNodes) == 0 {
		return false
	}
	for _, node := range targetNodes {
		whitelist, ok := r.nodeWhitelists[node]
		if !ok || !whitelist.Match(command) {
			return false
		}
	}
	return true
}

func (r *Reviewer) AddNodeWhitelist(node, command string) {
	node = strings.TrimSpace(node)
	command = strings.TrimSpace(command)
	if node == "" || command == "" {
		return
	}
	entries := append([]string(nil), r.nodeWhitelists[node].entries...)
	for _, entry := range entries {
		if entry == command {
			return
		}
	}
	entries = append(entries, command)
	r.nodeWhitelists[node] = NewWhitelist(entries)
}

func sortedCopy(values []string) []string {
	cp := append([]string(nil), values...)
	sort.Strings(cp)
	return cp
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
