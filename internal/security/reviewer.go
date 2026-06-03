package security

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/localtools"
	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/models"
)

var readOnlyTools = map[string]bool{
	"fs_read": true, "fs_list": true, "fs_stat": true,
	"sys_cpu": true, "sys_mem": true, "sys_disk": true, "sys_net": true, "sys_processes": true,
	"svc_status": true, "svc_list": true,
	"log_read": true, "log_journalctl": true,
	"net_ping": true, "net_traceroute": true, "net_portcheck": true, "net_curl": true,
	"web_search": true, "web_fetch": true,
	"docker_ps": true, "docker_images": true, "docker_logs": true,
	"k8s_pods": true, "k8s_logs": true, "k8s_events": true, "k8s_describe": true,
	"pkg_list": true, "pkg_search": true,
	"cron_list": true, "cron_show": true,
	"tool_search":   true,
	"local_fs_read": true, "local_fs_list": true, "local_fs_stat": true,
}

// ReviewerConfig holds configuration for creating a new Reviewer.
type ReviewerConfig struct {
	NodeWhitelists     map[string][]string
	Blacklist          []string
	LocalFileWhitelist []string
	Provider           llm.Provider
	ModelName          string
}

// Reviewer implements the two-stage security review pipeline:
// 1. Read-only tools are auto-allowed
// 2. shell_run commands are checked against a whitelist
// 3. Everything else goes to an LLM-based risk assessment
type Reviewer struct {
	nodeWhitelists map[string]Whitelist
	blacklist      Blacklist
	localFiles     Whitelist
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
		localFiles:     NewWhitelist(normalizeLocalFileWhitelist(cfg.LocalFileWhitelist)),
		provider:       cfg.Provider,
		modelName:      cfg.ModelName,
		cache:          make(map[string]RiskAssessment),
	}
}

func (r *Reviewer) SetProvider(provider llm.Provider, modelName string) {
	if r == nil {
		return
	}
	r.provider = provider
	r.modelName = modelName
	r.cache = make(map[string]RiskAssessment)
}

// Review performs a two-stage security review of a tool call.
// Read-only tools are auto-allowed. Whitelisted shell commands are auto-allowed.
// Everything else is assessed by the LLM provider (with session caching).
func (r *Reviewer) Review(ctx context.Context, toolName, toolInput string, targetNodes []string) (RiskAssessment, error) {
	if localtools.IsLocalTool(toolName) {
		if tools.IsReadOnly(toolName) {
			return RiskAssessment{Level: RiskAllow, Reason: "read-only tool metadata"}, nil
		}
		path := normalizeLocalFilePath(localtools.PathFromCall(toolName, json.RawMessage(toolInput)))
		if path != "" && r.localFiles.Match(path) {
			return RiskAssessment{Level: RiskAllow, Reason: "local file allowlist"}, nil
		}
		return RiskAssessment{Level: RiskConfirm, Reason: "local file mutation requires confirmation"}, nil
	}

	if toolName == "file_put" || toolName == "file_get" {
		return RiskAssessment{Level: RiskConfirm, Reason: "managed file transfer requires confirmation"}, nil
	}

	if toolName != "node_add" {
		policyDecision := NewPolicy(tools.DefaultMetadata()).Evaluate(toolName, toolInput, targetNodes)
		if !policyDecision.ContinueLLM && policyDecision.Decided {
			return RiskAssessment{Level: policyDecision.Level, Reason: policyDecision.Reason}, nil
		}
	}

	// Stage 2: whitelist check for shell commands
	if toolName == "shell_run" || toolName == "exec" {
		cmd, err := extractCommand(toolInput)
		if err == nil && !r.blacklist.Match(cmd) && r.allTargetsWhitelisted(targetNodes, cmd) {
			return RiskAssessment{Level: RiskAllow, Reason: "whitelisted"}, nil
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

func (r *Reviewer) AddLocalFileWhitelist(path string) {
	path = normalizeLocalFilePath(path)
	if path == "" {
		return
	}
	entries := append([]string(nil), r.localFiles.entries...)
	for _, entry := range entries {
		if entry == path {
			return
		}
	}
	entries = append(entries, path)
	r.localFiles = NewWhitelist(entries)
}

func normalizeLocalFileWhitelist(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if normalized := normalizeLocalFilePath(path); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func normalizeLocalFilePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || strings.HasPrefix(path, "../") || path == ".." || strings.HasPrefix(path, "/") {
		return ""
	}
	return path
}

func sortedCopy(values []string) []string {
	cp := append([]string(nil), values...)
	sort.Strings(cp)
	return cp
}

// extractCommand parses the command field from a shell_run tool input JSON.
func extractCommand(toolInput string) (string, error) {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolInput), &input); err != nil {
		return "", err
	}
	return input.Command, nil
}
