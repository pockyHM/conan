package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

type Role string

const (
	RoleInvestigator Role = "investigator"
	RoleReviewer     Role = "reviewer"
	RoleSummarizer   Role = "summarizer"
)

type Task struct {
	ID    string   `json:"id,omitempty"`
	Role  Role     `json:"role"`
	Task  string   `json:"task"`
	Nodes []string `json:"nodes,omitempty"`
}

type Request struct {
	ID            string
	Role          Role
	Task          string
	Cluster       string
	Nodes         []string
	Model         string
	Context       []models.Message
	MemoryContext string
	Timeout       time.Duration
}

type ToolCall struct {
	Name      string
	Arguments string
	Output    string
	Success   bool
}

type Result struct {
	ID        string
	Role      Role
	Task      string
	Nodes     []string
	Summary   string
	ToolCalls []ToolCall
	Err       error
	Elapsed   time.Duration
}

type ToolExecutor interface {
	ExecuteSubagentTool(context.Context, llm.ToolCall) (string, bool)
}

type Runner struct {
	Provider     llm.Provider
	Tools        []llm.ToolDef
	Executor     ToolExecutor
	MaxTurns     int
	MaxToolCalls int
}

func (r Runner) Run(ctx context.Context, req Request) Result {
	start := time.Now()
	result := Result{
		ID:    req.ID,
		Role:  normalizeRole(req.Role),
		Task:  strings.TrimSpace(req.Task),
		Nodes: append([]string(nil), req.Nodes...),
	}
	if result.ID == "" {
		result.ID = models.NewID()
	}
	if r.Provider == nil {
		result.Err = fmt.Errorf("subagent provider is nil")
		result.Elapsed = time.Since(start)
		return result
	}
	if result.Task == "" {
		result.Err = fmt.Errorf("subagent task is required")
		result.Elapsed = time.Since(start)
		return result
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxTurns := r.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 4
	}
	maxToolCalls := r.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 8
	}

	messages := buildMessages(req)
	tools := allowedTools(result.Role, r.Tools)
	toolCalls := 0
	for turn := 0; turn < maxTurns; turn++ {
		resp, err := r.Provider.Chat(ctx, &llm.ChatRequest{
			SystemPrompt: rolePrompt(result.Role, req),
			Messages:     messages,
			Tools:        tools,
			MaxTokens:    1800,
		})
		if err != nil {
			result.Err = err
			result.Elapsed = time.Since(start)
			return result
		}
		if strings.TrimSpace(resp.Message.Content) != "" {
			messages = append(messages, models.Message{Role: "assistant", Content: resp.Message.Content})
			result.Summary = strings.TrimSpace(resp.Message.Content)
		}
		if len(resp.ToolCalls) == 0 {
			result.Elapsed = time.Since(start)
			return result
		}
		if r.Executor == nil {
			result.Err = fmt.Errorf("subagent requested tools but no executor is configured")
			result.Elapsed = time.Since(start)
			return result
		}
		for _, call := range resp.ToolCalls {
			if toolCalls >= maxToolCalls {
				result.Err = fmt.Errorf("subagent exceeded tool call limit")
				result.Elapsed = time.Since(start)
				return result
			}
			toolCalls++
			output, success := r.Executor.ExecuteSubagentTool(ctx, call)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				Name:      call.Name,
				Arguments: string(call.Arguments),
				Output:    output,
				Success:   success,
			})
			messages = append(messages,
				models.Message{Role: "assistant", ToolCallID: call.ID, ToolName: call.Name, ToolInput: string(call.Arguments)},
				models.Message{Role: "tool", ToolCallID: call.ID, Content: output, ToolOutput: output},
			)
		}
	}
	result.Err = fmt.Errorf("subagent reached turn limit")
	result.Elapsed = time.Since(start)
	return result
}

func RunBatch(ctx context.Context, runner Runner, requests []Request, maxParallel int) []Result {
	if maxParallel <= 0 {
		maxParallel = 1
	}
	results := make([]Result, len(requests))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := range requests {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runner.Run(ctx, requests[i])
		}()
	}
	wg.Wait()
	return results
}

func ParseTasks(raw json.RawMessage) ([]Task, error) {
	var args struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Tasks) == 0 {
		return nil, fmt.Errorf("tasks is required")
	}
	for i := range args.Tasks {
		args.Tasks[i].Role = normalizeRole(args.Tasks[i].Role)
		args.Tasks[i].Task = strings.TrimSpace(args.Tasks[i].Task)
		if args.Tasks[i].Task == "" {
			return nil, fmt.Errorf("tasks[%d].task is required", i)
		}
	}
	return args.Tasks, nil
}

func FormatResults(results []Result) string {
	if len(results) == 0 {
		return "No subagent results."
	}
	var lines []string
	for _, result := range results {
		status := "ok"
		if result.Err != nil {
			status = "error: " + result.Err.Error()
		}
		nodes := "local"
		if len(result.Nodes) > 0 {
			cp := append([]string(nil), result.Nodes...)
			sort.Strings(cp)
			nodes = strings.Join(cp, ",")
		}
		summary := strings.TrimSpace(result.Summary)
		if summary == "" {
			summary = "(no summary)"
		}
		lines = append(lines, fmt.Sprintf("[%s:%s:%s] %s", result.Role, nodes, status, summary))
	}
	return strings.Join(lines, "\n")
}

func buildMessages(req Request) []models.Message {
	messages := append([]models.Message(nil), req.Context...)
	var b strings.Builder
	b.WriteString("Task: ")
	b.WriteString(strings.TrimSpace(req.Task))
	if req.Cluster != "" {
		b.WriteString("\nCluster: ")
		b.WriteString(req.Cluster)
	}
	if len(req.Nodes) > 0 {
		nodes := append([]string(nil), req.Nodes...)
		sort.Strings(nodes)
		b.WriteString("\nNodes: ")
		b.WriteString(strings.Join(nodes, ", "))
	}
	if strings.TrimSpace(req.MemoryContext) != "" {
		b.WriteString("\nRelevant memory:\n")
		b.WriteString(strings.TrimSpace(req.MemoryContext))
	}
	messages = append(messages, models.Message{Role: "user", Content: b.String()})
	return messages
}

func normalizeRole(role Role) Role {
	switch role {
	case RoleReviewer, RoleSummarizer:
		return role
	default:
		return RoleInvestigator
	}
}

func allowedTools(role Role, tools []llm.ToolDef) []llm.ToolDef {
	if role == RoleSummarizer {
		return nil
	}
	allowed := map[string]bool{
		"memory_search": true,
		"memory_read":   true,
	}
	if role == RoleInvestigator {
		allowed["tool_search"] = true
		allowed["call_tool"] = true
	}
	var result []llm.ToolDef
	for _, tool := range tools {
		if allowed[tool.Name] {
			result = append(result, tool)
		}
	}
	return result
}

func rolePrompt(role Role, req Request) string {
	base := []string{
		"You are a Conan subagent.",
		"Return concise findings only.",
		"Include evidence from tools when available.",
		"Do not change resources, write memory, or ask the user questions.",
		"If the task requires a resource-changing operation, report that it must be escalated to the main agent.",
	}
	switch normalizeRole(role) {
	case RoleReviewer:
		base = append(base,
			"Role: reviewer.",
			"Check assumptions, missing evidence, and operational risk.",
		)
	case RoleSummarizer:
		base = append(base,
			"Role: summarizer.",
			"Compress the provided material into the smallest useful answer.",
		)
	default:
		base = append(base,
			"Role: investigator.",
			"Use read-only tools to inspect facts before concluding when tools are relevant.",
		)
	}
	if req.Cluster != "" {
		base = append(base, "Cluster: "+req.Cluster)
	}
	return strings.Join(base, "\n")
}
