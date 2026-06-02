# Conan Subagent Quality-of-Life Design

## Context

Conan shipped its first subagent implementation in May 2026 (see
`2026-05-22-subagent-mode-design.md`). The runner is in production use but has
three user-visible pain points that this spec addresses:

1. **Turn budget is hardcoded.** `internal/tui/model.go:3583-3584` passes
   `MaxTurns: 4, MaxToolCalls: 8` directly to the runner, ignoring config.
   Subagents abort with `subagent reached turn limit` before finishing
   non-trivial investigations.
2. **Context isolation is partly accidental.** The subagent's `Request.Context`
   is filled with `recentConversationContext(conv, 3000)`, and the `MemoryContext`
   field on the request is defined but never set. The 3000-character budget
   is too wide and not configurable; `MemoryContext` is a half-built feature.
3. **UI status display is start/end only.** The runner runs synchronously in a
   goroutine, the TUI only sees `subagentCommandResultMsg` once the batch
   finishes, and `subagentStatus` shows aggregate counts without per-subagent
   tool or turn progress. Users see "Subagent X running..." for tens of
   seconds with no visible activity.

Additional gaps found while reading the code:

- The `subagentStatusLabel` map defines a `"tool"` status that is never set
  anywhere.
- `Request.MemoryContext` is defined in the struct but no caller populates it.
- `~/.conan/logs/subagents/<session>/<id>.jsonl` was promised in the original
  design but never implemented; no transcript is written.
- Cancellation is batch-level only: there is no way to cancel one specific
  in-flight subagent.

This spec delivers: configurable per-role turn budgets, configurable inbound
context with a default of zero parent history, a `MemoryContext` filled from
the main memory store, channel-based event streaming from the runner to the
TUI, a persistent subagent panel with live turn/tool/elapsed progress, debug
transcript persistence, and per-subagent cancellation.

Out of scope: resume across sessions, fork mode, subagent-to-subagent
communication, opening memory writes to subagents, changing the public
`subagents_run` tool schema.

## Architecture

```
LLM stream emits subagents_run tool call
  │
  ▼
TUI model.dispatchSubagentsRun
  │  · parse tasks
  │  · for each task: subagent.Manager.Submit(ctx, request)
  │  · appends a subagentRuns row with Status="queued"
  │  · wraps the events channel and result channel in two tea.Cmd values
  │
  ▼
subagent.Manager (new)
  │  · owns map[id]cancelFunc under a mutex
  │  · spawns one goroutine per Submit that calls Runner.Run
  │  · exposes Submit / Cancel / CancelAll
  │
  ▼
subagent.Runner (signature change)
  │  · Run returns (<-chan Event, <-chan Result) instead of Result
  │  · emits events at turn start, LLM response, tool call, tool result
  │  · respects per-role MaxTurns and MaxToolCalls from Request
  │  · writes events to Transcript sink if Request.DebugTranscript is true
  │
  ▼
subagent.Transcript (new)
  │  · file-backed JSONL writer at ~/.conan/logs/subagents/<session>/<id>.jsonl
  │  · each event is one line; no PII filtering beyond what already exists
  │
  ▼
TUI Update
  · subagentEventMsg  → look up row by ID → update Turn / ToolName / Elapsed
  · subagentResultMsg → update Summary / Err / Elapsed / Status
  · 250ms tick        → recompute elapsed for all running subagents
  · recompute aggregate subagentStatus ("2/4 done · 1 active · 6.2s")
```

## Components

### internal/subagent/runner.go (changed)

- New `Event` struct with `ID, Kind, Turn, Tool, Args, Out, OK, Elapsed`.
- `EventKind` enum: `TurnStart, TurnEnd, ToolCall, ToolResult, Done`.
- `Run` returns `(<-chan Event, <-chan Result)`. Internally still a synchronous
  loop; events are emitted between iterations. The `Done` event is the last
  event before the result channel is closed.
- `Request` struct gains `DebugTranscript bool` and `SessionID string` fields.
- `MaxTurns` and `MaxToolCalls` are no longer defaulted inside the runner; the
  caller is responsible for filling them from per-role config before
  constructing the request.
- `rolePrompt` and `allowedTools` are unchanged.

### internal/subagent/manager.go (new)

```go
type Manager struct {
    mu      sync.Mutex
    running map[string]context.CancelFunc
}

func NewManager() *Manager
func (m *Manager) Submit(ctx context.Context, runner Runner, req Request, transcript *Transcript) (id string, events <-chan Event, results <-chan Result, err error)
func (m *Manager) Cancel(id string) error
func (m *Manager) CancelAll()
func (m *Manager) Running() []string
```

`Submit` derives a per-request context from `ctx`, starts a goroutine that
calls `runner.Run` and a teardown that deletes the cancel func from the map
when the goroutine returns. `Cancel` is safe to call on an unknown id and
returns `nil`. `CancelAll` cancels every tracked id.

### internal/subagent/transcript.go (new)

```go
type Transcript struct {
    sessionID string
    id        string
    file      *os.File
    encoder   *json.Encoder
    mu        sync.Mutex
}

func OpenTranscript(configHome, sessionID, id string) (*Transcript, error)
func (t *Transcript) Write(event Event) error
func (t *Transcript) Close() error
```

File path: `<configHome>/logs/subagents/<sessionID>/<id>.jsonl`. Each call to
`Write` writes one JSON object followed by a newline. `Close` flushes and
closes the file. `Write` is safe to call from a single goroutine only;
synchronization is the caller's responsibility.

### internal/tui/model.go (changed)

- New message types: `subagentEventMsg{ID string, Event subagent.Event}`,
  `subagentResultMsg{Result subagent.Result}`.
- `subagentRunView` gains `Turn int`, `CurrentTool string`,
  `CurrentToolStart time.Time`, `StartedAt time.Time`, `EventCount int`.
  The existing `Elapsed` field becomes a derived value (`time.Since(StartedAt)`
  for running rows, `Result.Elapsed` for completed rows).
- `subagentStatus string` becomes a derived field recomputed on every relevant
  Update call. The string format is
  `"<done>/<total> done · <active> active [(<node>,...)] · <aggregate-elapsed>"`.
  When no subagents are running it is `""`.
- `m.runSubagent` and `m.runSubagentBatch` are replaced by a single
  `m.dispatchSubagentsRun` that uses `subagent.Manager`. The `MaxTurns: 4,
  MaxToolCalls: 8` hardcoding is removed.
- `m.streamCancel` calls `m.subagentManager.CancelAll()` on the way down.
- A new `c` keybinding in expanded subagent view cancels the focused row by
  calling `m.subagentManager.Cancel(row.ID)`. The `c` keybinding is only
  active when `len(m.subagentRuns) > 0` and `m.subagentRunsExpanded == true`.
- `m.recomputeSubagentStatus()` is called from any code path that mutates
  `m.subagentRuns` and from the 250ms tick handler.

### internal/tui/configscreen.go (changed)

Add six new entries to the Subagents group: `subagents.max_inbound_chars`
(int), `subagents.debug_transcript` (bool), `subagents.per_role.investigator_turns`
(int), `subagents.per_role.investigator_tool_calls` (int),
`subagents.per_role.reviewer_turns` (int), `subagents.per_role.reviewer_tool_calls`
(int), `subagents.per_role.summarizer_turns` (int). The summarizer has zero
tool calls by design and does not need a tool-call config entry.

### pkg/configschema/config.go (changed)

`SubagentConfig` gains `MaxInboundChars int`, `DebugTranscript bool`,
`PerRole SubagentRoleLimits`. The existing `MaxTurns` and `MaxToolCalls`
fields are removed; their functionality is moved to `PerRole`. Old YAML
keys `subagents.max_turns` and `subagents.max_tool_calls` are no longer
recognized and produce a config-validation error if present.

### internal/subagent/limits.go (new)

```go
type RoleLimits struct {
    InvestigatorTurns    int
    InvestigatorToolCalls int
    ReviewerTurns        int
    ReviewerToolCalls    int
    SummarizerTurns      int
}

func NormalizeRoleLimits(cfg SubagentRoleLimits) RoleLimits
func (r RoleLimits) For(role Role) (turns, toolCalls int)
```

`NormalizeRoleLimits` applies defaults: 8/12 investigator, 4/6 reviewer,
2/0 summarizer. `For` returns the limits for a given role. If the caller
passes zero turns or tool calls for a role, `For` falls back to the
defaults so a misconfigured YAML does not silently disable the runner.

## Data flow

1. Main LLM emits a `subagents_run` tool call.
2. `m.handleToolCall` matches the meta-tool name and calls
   `m.dispatchSubagentsRun(streamID, call)`.
3. `dispatchSubagentsRun` parses tasks, fills `MemoryContext` from
   `m.memStore` (see "Memory context" below), and resolves per-role limits
   from `m.subagents.PerRole` via `subagent.NormalizeRoleLimits(...).For(role)`.
4. For each task, it calls `m.subagentManager.Submit(...)` and appends a row
   to `m.subagentRuns` with `Status="queued"`.
5. The events channel is wrapped in a `tea.Cmd` that reads one event at a
   time and returns a `subagentEventMsg`; the result channel is wrapped
   similarly and returns `subagentResultMsg`. Both are returned in a single
   `tea.Batch`.
6. The Update function handles each message type:
   - `subagentEventMsg`: look up the row by `ID`, update `Turn`,
     `CurrentTool`, `EventCount`, set `Status="running"` (or
     `Status="tool"` while a tool call is in flight), bump `lastBodyContent`
     to force a re-render.
   - `subagentResultMsg`: look up the row, set `Status="completed"` or
     `"failed"`, copy `Summary` and `Err` from the result, set
     `Elapsed` from the result, close the row.
7. `m.recomputeSubagentStatus()` is called after every relevant mutation and
   from the 250ms tick handler while any subagent is running.
8. On parent cancel (`m.streamCancel`): call
   `m.subagentManager.CancelAll()` and let in-flight results flow through
   normally; queued requests do not start.
9. On per-subagent cancel (`c` keybinding): look up the focused row's ID,
   call `m.subagentManager.Cancel(id)`, and let the runner emit `Done` and
   the final `Result` with `Err = context.Canceled`.

## Configuration

Defaults applied by `NormalizeSubagentConfig` and `NormalizeRoleLimits`:

| Key                                     | Default | Notes                          |
|-----------------------------------------|---------|--------------------------------|
| `subagents.enabled`                     | false   | unchanged                      |
| `subagents.max_parallel`                | 3       | unchanged                      |
| `subagents.timeout_seconds`             | 120     | unchanged                      |
| `subagents.max_inbound_chars`           | 0       | 0 means do not pass parent history |
| `subagents.default_model`               | ""      | empty means use current model  |
| `subagents.debug`                       | false   | unchanged                      |
| `subagents.debug_transcript`            | false   | new                            |
| `subagents.per_role.investigator_turns` | 8       | new                            |
| `subagents.per_role.investigator_tool_calls` | 12 | new                            |
| `subagents.per_role.reviewer_turns`     | 4       | new                            |
| `subagents.per_role.reviewer_tool_calls`| 6       | new                            |
| `subagents.per_role.summarizer_turns`   | 2       | new                            |

Breaking change: `subagents.max_turns` and `subagents.max_tool_calls` (if
present in any existing user `config.yaml`) will fail config validation
with a message pointing the user to the new `per_role` keys.

## Memory context

`m.dispatchSubagentsRun` calls a new helper
`m.buildSubagentMemoryContext(task, nodes)` that:

1. Builds a search query from `task` plus node names.
2. Calls `m.memStore.Search(query, 5)` to retrieve the top five matching
   memory entries.
3. Trims each entry to fit a per-task budget of 600 characters.
4. Joins entries with blank lines into a single string.
5. Returns the string. If `m.memStore` is nil or `Search` returns nothing,
   returns the empty string and the runner proceeds without memory context.

The result is set on `Request.MemoryContext` before `Submit`. The runner
already injects `MemoryContext` into the first user message via
`buildMessages`; the only change is that the field is now actually populated.

## Error handling

- Per-subagent cancel: `Manager.Cancel(id)` calls the per-request cancel
  function. The cancel propagates into `Provider.Chat` and
  `Executor.ExecuteSubagentTool` via the request's context. The runner
  emits a final `Done` event and a `Result` whose `Err` is
  `context.Canceled`.
- Parent cancel: `m.streamCancel` is augmented to call
  `m.subagentManager.CancelAll()`. In-flight runners return their partial
  result with `Err = context.Canceled`; queued requests never start.
- Per-role turn / tool-call limits: the existing error message
  `"subagent reached turn limit"` and `"subagent exceeded tool call limit"`
  are preserved. With the new defaults these are unlikely to fire on
  reasonable investigations but are still tested.
- Transcript errors: `Transcript.Write` returns an error; the manager logs
  the error via `slog` and continues. A failing transcript never aborts a
  subagent.
- Channel close order: events channel is closed after the `Done` event is
  emitted. The result channel is closed after the result is sent. The TUI
  uses `tea.Cmd` wrappers that do not require explicit close handling.

## Testing

### internal/subagent/runner_test.go (changed)

- `TestRunnerLoopsThroughReadOnlyToolCalls` is rewritten against the new
  channel-based API. It drains both channels, asserts two events (a
  `ToolCall` and a `Done`) and one result with the expected summary.
- `TestRunnerEmitsTurnAndToolEvents` drives a provider that returns three
  tool calls in a row and asserts the events channel receives the correct
  `TurnStart`, `ToolCall`, `ToolResult` sequence in order.
- `TestRunnerRespectsPerRoleTurns` builds a request with
  `MaxTurns: 2, MaxToolCalls: 100` and a provider that always returns a
  tool call. Asserts the final result has `Err` matching
  `subagent reached turn limit` and exactly two tool-call events were
  emitted.
- `TestRunnerRespectsPerRoleToolCalls` is the tool-call counterpart.
- `TestRunnerExceedingLimitsReturnsResult` asserts that hitting a limit
  produces a `Result` with the error and that the events channel is
  closed before the result channel is closed.

### internal/subagent/manager_test.go (new)

- `TestManagerSubmitAndCancel` submits a request, cancels it via the
  returned id, and asserts the result channel receives a result with
  `Err = context.Canceled` within one second.
- `TestManagerCancelAllStopsAll` submits three requests, calls
  `CancelAll`, and asserts all three results carry `context.Canceled`.
- `TestManagerResultChannelClosesOnDone` asserts the result channel is
  closed after the result is sent.

### internal/subagent/transcript_test.go (new)

- `TestTranscriptWritesJSONL` opens a transcript, writes three events,
  closes it, and reads the file back to assert it is three newline-
  delimited JSON objects with the expected fields.
- `TestTranscriptDisabledWritesNothing` asserts that
  `OpenTranscript` is not called when `DebugTranscript` is false, by
  checking the absence of a file in the temp dir.

### internal/tui/model_test.go (changed)

- `TestSubagentRunsUpdatedByEvents` injects three `subagentEventMsg`
  values and asserts the corresponding `subagentRuns` row fields update
  correctly.
- `TestSubagentStatusAggregate` submits events for two running and one
  completed subagent and asserts the recomputed status string.
- `TestPerSubagentCancelKeybinding` simulates a `c` keypress in expanded
  subagent view and asserts `subagentManager.Cancel` is called with the
  focused row's ID.

## Migration

- `m.subagents.MaxTurns` and `m.subagents.MaxToolCalls` are removed.
  `configscreen.go` and `normalizeSubagentConfig` lose their handling for
  the removed keys.
- `m.runSubagent` and `m.runSubagentBatch` are deleted; the new
  `dispatchSubagentsRun` is the only path.
- `Runner.Run` signature change is a breaking change for any external
  callers. There are no other callers inside this repo.
- Existing tests that construct a `Runner` directly continue to work
  because the runner is a value type; the change is in the `Run` method
  signature, not the struct.

## Out of scope

- Resume of a subagent across sessions. The transcript is on disk but
  no API exists to feed it back into a new `Runner`.
- Fork mode. Subagents always start with a fresh context window derived
  from the explicit `Request.Context` and `Request.MemoryContext`.
- Subagent-to-subagent communication. Subagents only emit to the main
  thread.
- `subagents_run` tool schema changes. The public tool signature is
  unchanged; per-role limits are a server-side concern.
- Memory writes from subagents. The `subagentToolExecutor` continues to
  reject memory-write tools and any non-read-only node tools.

## Open questions

None. All scope decisions were resolved during brainstorming.
