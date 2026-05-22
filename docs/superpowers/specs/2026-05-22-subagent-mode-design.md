# Conan Subagent Mode Design

## Current state

Conan currently has one interactive assistant loop in `internal/tui/model.go`.
The remote `conan-agent` process is a node-side MCP tool server, not a reasoning
agent. Tool execution is routed from the TUI model through meta tools:

- `tool_search` searches cached node tools.
- `call_tool` calls a specialized node tool.
- `exec` runs shell commands through risk review.
- memory tools are available to the model but hidden from normal chat.

There is no independent subagent session, no background LLM worker, and no
orchestrator that can split a user request into parallel tasks.

## Goal

Add a subagent mode where the main Conan assistant can delegate bounded work to
local virtual subagents while keeping one user-facing conversation.

Subagents should help with investigation, planning, verification, and summary
work. They must not become autonomous operators with uncontrolled write access.
The main assistant remains responsible for user-visible decisions, final
answers, and destructive operations.

## Non-goals

- Do not turn node-side `conan-agent` into an LLM process.
- Do not expose subagent transcripts as normal chat by default.
- Do not let subagents bypass existing tool safety, risk review, memory policy,
  or selected-node scope.
- Do not require new provider integrations for the first version.

## Terminology

- **Main agent:** the existing TUI conversation and primary LLM stream.
- **Subagent:** a local LLM task session created by the main agent or by a user
  command. It has its own short conversation, tool scope, timeout, and result.
- **Orchestrator:** the local Go component that manages subagent lifecycle,
  tool gates, result collection, and UI events.
- **Node agent:** the existing remote `conan-agent` MCP server. It remains a
  tool endpoint.

## User experience

### Default behavior

Subagent mode should be opt-in initially:

```yaml
subagents:
  enabled: false
  max_parallel: 3
  default_model: ""
  timeout_seconds: 120
```

When enabled, the main assistant may delegate if the task benefits from
parallel or independent work. Examples:

- "查一下这几个节点为什么负载高" can spawn per-node investigators.
- "帮我排查并给出修复方案" can spawn an investigator and a reviewer.
- "执行重启 nginx" should not spawn workers that directly restart services.
  The main agent should handle confirmation and execution.

### Commands

- `/subagents` shows active and recent subagents.
- `/subagents on|off` toggles session-level delegation.
- `/subagents limit <n>` changes max parallel workers for the current session.
- `/agent <role> <task>` manually starts one subagent and returns a compact
  result into the main chat.
- `/agents` can be kept as an alias if the UI copy reads better.

### TUI display

Subagent activity should appear as compact status below or near the current
assistant generation:

```text
thinking 12.4s | esc to interrupt
  investigator[node-01] running sys/cpu, sys/processes
  investigator[node-02] done
```

Final subagent output should be collapsed by default:

```text
* 3 subagents completed in 18.2s
  node-01: load caused by postgres checkpoint
  node-02: normal
  reviewer: restart is not needed
```

Full transcripts can be available through `/subagents` or debug logs, not shown
inline by default.

## Execution model

The first implementation should use local goroutines and the existing LLM
provider interface. A subagent is not a process.

```go
type SubagentRole string

const (
    SubagentInvestigator SubagentRole = "investigator"
    SubagentReviewer     SubagentRole = "reviewer"
    SubagentSummarizer   SubagentRole = "summarizer"
)

type SubagentRequest struct {
    ID            string
    Role          SubagentRole
    Task          string
    Cluster       string
    Nodes         []string
    Model         string
    ToolPolicy    SubagentToolPolicy
    Context       []models.Message
    MemoryContext string
    Timeout       time.Duration
}

type SubagentResult struct {
    ID          string
    Role        SubagentRole
    Summary     string
    Evidence    []SubagentEvidence
    ToolCalls   []SubagentToolCall
    Err         error
    Elapsed     time.Duration
}
```

The orchestrator should live outside the Bubble Tea model, for example:

```text
internal/subagent/
  manager.go       lifecycle, queue, cancellation
  runner.go        LLM loop for one subagent
  policy.go        role and tool policy
  prompt.go        role prompts
  transcript.go    debug transcript and result compaction
```

The TUI model should only receive typed messages such as:

- `subagentStartedMsg`
- `subagentProgressMsg`
- `subagentResultMsg`
- `subagentBatchDoneMsg`

This keeps Bubble Tea update logic event-driven and avoids embedding long
workflow code in `internal/tui/model.go`.

## Tool policy

Subagents should receive a narrower tool surface than the main agent.

### Investigator

Allowed:

- `tool_search`
- `call_tool` for read-only tools only
- `memory_search`
- `memory_read`

Blocked:

- `exec` by default
- memory writes
- resource-changing node tools

Optional fallback:

- `exec` may be allowed only with `read_only_exec: true`, where the command is
  checked by a strict read-only allowlist and still goes through existing risk
  review.

### Reviewer

Allowed:

- no node tools by default
- memory read/search
- the investigator summaries and evidence bundle

The reviewer checks gaps, risky assumptions, and whether the proposed action is
safe.

### Summarizer

Allowed:

- no tools by default

The summarizer compresses long outputs into a user-facing result or memory
candidate.

### Executor

Do not add executor subagents in the first version. Destructive or
state-changing operations should stay in the main agent path so confirmation,
audit, and user intent remain simple.

## Prompt policy

The main agent system prompt gets a short delegation policy only when subagents
are enabled:

```text
Subagent policy:
- Use subagents for independent investigation, cross-node comparison, review,
  or summarization when it reduces latency or improves reliability.
- Do not delegate destructive actions.
- Give each subagent a bounded task, selected node scope, and expected output.
- Use subagent results as evidence, then answer the user yourself.
```

Subagent prompts should be role-specific and strict:

```text
You are a Conan investigator subagent.
Return findings only. Include commands/tools used and evidence.
Do not change resources. Do not ask the user questions.
If the task requires a write operation, report that it must be escalated to the
main agent.
```

## Context and memory

Subagents should not receive the full conversation by default. They receive:

- current user request
- selected cluster and nodes
- relevant memory injected by the main memory retrieval layer
- a compact recent context window
- any explicit task-specific evidence

Subagent findings are not automatically persisted. The main agent can decide
whether to trigger normal implicit memory extraction after the final answer.

## Scheduling

Use a bounded worker pool:

- default `max_parallel = 3`
- queue excess tasks
- per-subagent timeout, default 120 seconds
- parent cancellation on `Esc`
- partial results are preserved if a batch is interrupted

The orchestrator should support these modes:

- `RunOne`
- `RunBatch`
- `Cancel(id)`
- `CancelAll(sessionID)`
- `List(sessionID)`

## Main-agent integration flow

For an automatic multi-node investigation:

1. User asks: "查一下 node-01 到 node-03 为什么负载高".
2. Main agent decides to delegate.
3. Orchestrator creates one investigator per node.
4. Each investigator uses `tool_search` and `call_tool` for read-only tools.
5. Optional reviewer receives all investigator summaries.
6. Main agent receives a compact evidence bundle as a synthetic tool result.
7. Main agent answers the user and, if useful, implicit memory extraction runs.

The main LLM should see subagent results as a tool result, for example tool name
`subagents/run`, rather than as normal assistant messages.

## Public tools for the main LLM

Expose one high-level meta tool instead of exposing low-level lifecycle controls
to the model:

```json
{
  "name": "subagents_run",
  "description": "Delegate bounded read-only investigation, review, or summarization tasks to local subagents. Do not use for destructive actions.",
  "input_schema": {
    "type": "object",
    "properties": {
      "tasks": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "role": { "type": "string", "enum": ["investigator", "reviewer", "summarizer"] },
            "task": { "type": "string" },
            "nodes": { "type": "array", "items": { "type": "string" } }
          },
          "required": ["role", "task"]
        }
      }
    },
    "required": ["tasks"]
  }
}
```

The implementation can translate this into orchestrator requests. The model
does not need direct `spawn`, `wait`, or `cancel` tools at first.

## Safety

- Subagents inherit selected node scope and cannot expand it.
- Subagents cannot call write memory tools in v1.
- Subagent tool calls are audited with `parent_message_id` and `subagent_id`.
- Read-only tool allowlisting should be explicit, not inferred only from names.
- `exec` remains disabled for subagents in v1 unless a later read-only shell
  policy is implemented.
- Any recommendation to mutate resources must be returned to the main agent for
  user confirmation and normal risk review.

## Configuration

Add to `pkg/configschema/config.go`:

```go
type GlobalConfig struct {
    // existing fields
    Subagents SubagentConfig `yaml:"subagents"`
}

type SubagentConfig struct {
    Enabled        bool   `yaml:"enabled"`
    MaxParallel    int    `yaml:"max_parallel"`
    DefaultModel   string `yaml:"default_model"`
    TimeoutSeconds int    `yaml:"timeout_seconds"`
    Debug          bool   `yaml:"debug"`
}
```

Defaults:

- disabled
- max parallel 3
- timeout 120 seconds
- empty default model means use current model
- debug false

## Logging and debug

When debug logging is enabled, write structured records:

- subagent start and stop
- prompt metadata, not secrets
- raw LLM chunks only if existing LLM debug raw-output mode is enabled
- tool calls and results with truncation
- cancellation and timeout reasons

Debug transcripts should be stored under:

```text
~/.conan/logs/subagents/<session-id>/<subagent-id>.jsonl
```

## Implementation phases

### Phase 1: Manual subagent command

- Add config schema and defaults.
- Add `internal/subagent` manager and runner.
- Add `/agent <role> <task>`.
- Allow investigator/reviewer/summarizer without exposing automatic delegation
  to the main LLM yet.
- UI shows compact progress and final summary.

### Phase 2: Main LLM delegation tool

- Add `subagents_run` meta tool.
- Add prompt policy when enabled.
- Return compact batch results as a tool result.
- Add tests for cancellation, timeout, hidden transcript, and result injection.

### Phase 3: Parallel investigation patterns

- Add helper planning for per-node fanout.
- Add reviewer pass for risky or ambiguous findings.
- Add debug transcript viewer in `/subagents`.

### Phase 4: Optional read-only shell fallback

- Add strict read-only exec policy for subagents if needed.
- Keep destructive shell and write tools reserved for the main agent.

## Recommended first version

Build Phase 1 and Phase 2 only.

This gives Conan useful subagent behavior without creating an unsafe autonomous
executor model. The main assistant remains the only actor that can perform
state-changing operations, while subagents improve investigation latency and
answer quality.
