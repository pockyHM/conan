# Conan Trace Command Design

## Context

Conan already has several session-level views in the Bubble Tea TUI:

- `/subagents` opens a full-window list/detail page for subagent runs.
- Subagent detail pages aggregate runner events into turns and tool calls.
- The main conversation persists user, assistant, tool-call, and tool-result
  records through `internal/conversation`.

The existing storage is enough to replay completed conversation history, but it
does not represent in-flight state such as streaming assistant output, pending
risk confirmation, executing tools, or running subagents. `/trace` must show the
current session chain in real time, so it needs a first-class TUI trace state
rather than a pure `conversation.Messages()` projection.

Out of scope:

- Persisting trace UI state as a new durable artifact.
- Changing provider interfaces or the saved conversation schema.
- Exposing `/trace` as a model tool.
- Replacing the existing `/subagents` page.

## Goal

Add a `/trace` slash command that opens a polished current-session trace window.
The window shows a full-width arrow timeline of user messages, assistant
streaming/final replies, tool calls, tool results, and subagent activity. Users
can move through nodes with keyboard navigation and open a selected node to view
details.

The design follows the confirmed visual direction:

```text
●
│
↓  01 user       check nginx on selected nodes
◆
│
↓  02 assistant  streaming... / final reply
▶
│
↓  03 tool call  shell_run {"command":"..."}
✓
│
↓  04 result     node-01 OK, node-02 failed
◇
│
↓  05 subagent   investigator[node-02] running / done
■  06 assistant  final diagnosis
```

The chain must remain readable without color. Colors improve scanning but do
not carry the only signal.

## Architecture

Add a small trace model inside `internal/tui`, owned by the Bubble Tea `Model`:

```go
type traceKind string

const (
    traceUser       traceKind = "user"
    traceAssistant  traceKind = "assistant"
    traceToolCall   traceKind = "tool_call"
    traceToolResult traceKind = "tool_result"
    traceSubagent   traceKind = "subagent"
)

type traceStatus string

const (
    tracePending traceStatus = "pending"
    traceRunning traceStatus = "running"
    traceDone    traceStatus = "done"
    traceFailed  traceStatus = "failed"
    traceBlocked traceStatus = "blocked"
)

type traceNode struct {
    ID        string
    ParentID  string
    Kind      traceKind
    Status    traceStatus
    Title     string
    Summary   string
    Detail    string
    StartedAt time.Time
    EndedAt   time.Time

    ToolCallID string
    ToolName   string
    SubagentID string
}
```

`Model` gains:

```go
traceNodes         []traceNode
traceCursor        int
traceDetailVisible bool
activeTraceAssistantID string
```

The trace state is intentionally presentation-oriented. It records compact,
sanitized summaries and details for the current TUI session. The durable source
of truth remains `conversation.Conversation`.

## Commands and Modes

Add `CommandTrace` to `internal/tui/command.go`:

- `/trace` opens `modeTrace`.
- `/help` includes `/trace`.
- Unknown command behavior remains unchanged.

Add `modeTrace` next to the existing full-window modes. `Model.View()` renders
`renderTracePage()` when active.

Keyboard behavior:

- Timeline page: `↑/k` moves up, `↓/j` moves down.
- `Enter` opens selected-node detail.
- Detail page: `Esc` returns to timeline.
- Timeline page: `Esc` returns to chat.
- `Ctrl+C` keeps the existing quit behavior. Trace mode is closed with `Esc`,
  matching the existing subagent page behavior.

If no trace nodes exist, `/trace` still opens a clear empty state:

```text
No trace nodes yet.

Send a message or run a tool to populate the current-session trace.
```

## Rendering

Create `internal/tui/tracepage.go` and `internal/tui/tracepage_test.go`.

The list view is a full-width timeline inside a bordered box. Each node renders
with a chain rail:

- User: `●`
- Assistant: `◆`
- Tool call: `▶`
- Successful result: `✓`
- Failed result: `✗`
- Subagent: `◇`
- Terminal/final assistant row can use `■` only if it improves clarity; normal
  assistant rows can stay `◆`.

Each non-last node shows a vertical continuation and down arrow. The selected
row gets the existing selected-row treatment used by subagent pages plus a
leading cursor marker. The text line includes ordinal, kind, status, elapsed
time if known, and summary.

The detail page shows:

- Node type and status.
- Started/ended/elapsed when available.
- Parent ID or related tool/subagent ID when useful.
- Summary.
- Detail text, truncated to a bounded size for display.
- For subagent nodes, a compact nested section can reuse
  `collectSubagentTurns` and `renderSubagentDetailTurn` behavior where
  practical, but `/trace` should not depend on opening `/subagents`.

Keep styles close to the existing subagent page but refine the visual language:
blue for user, purple for assistant, yellow for tool calls, green/red for
success/failure, cyan for subagents, muted gray for connective rails and help.

## Data Flow

Trace nodes are appended or updated from existing TUI event paths.

### User Submit

When `startSubmittedMessage` appends the visible user chat message, append a
`traceUser` node with `Status=done`, `Title="user"`, `Summary=visibleInput`,
and detail equal to the LLM input if it differs from visible input.

`/clear` clears both chat messages and trace nodes. `/compact` does not remove
trace nodes because it is a context-management operation, not a UI history
clear.

### Assistant Streaming

When a stream starts, create an active `traceAssistant` node with
`Status=running`. During content and reasoning events, update that same node's
detail and summary. The summary should stay short, such as the first non-empty
line or `streaming...`.

When `finishStream(false)` commits the assistant message, mark the active
assistant trace node `done`, copy the final assistant content into detail, and
clear `activeTraceAssistantID`.

On interruption or stream error, mark the active assistant node `failed` or
`blocked` with the final partial content.

### Tool Calls

When `llm.ToolCallEvent` arrives:

- Append a `traceToolCall` node with `Status=pending` or `running`.
- Store `ToolCallID`, `ToolName`, and sanitized arguments.
- Summary format: `<tool_name> <one-line args>`.

If risk review routes the call to confirmation mode, update the tool-call node
to `pending` and include the risk summary in detail. If the user denies the
call, mark it `blocked`.

### Tool Results

When `multiToolResultMsg` is handled:

- Update the matching tool-call node to `done` or `failed` if a call ID match
  exists.
- Append a `traceToolResult` node with the per-node result summary and full
  output in detail.
- If no matching call exists, append an orphan result node instead of dropping
  the event.

The result node summary should be compact:

```text
2 nodes · 1 ok · 1 failed
```

For single local tools:

```text
local · ok
```

### Subagents

When a `subagents_run` call creates run views, append one `traceSubagent` node
per subagent run. Link each node to `SubagentID`.

When subagent events are incorporated into `subagentRuns`, update the matching
trace node:

- Running turn: `turn N`
- Tool event: `turn N · <tool>`
- Completed: summary and elapsed
- Failed/cancelled: error detail

The detail view should include the subagent prompt, nodes, status, summary, and
captured turn/tool events.

### Resume

When loading a saved conversation, rebuild `traceNodes` from restored
conversation messages:

- user message -> `traceUser`
- assistant normal content -> `traceAssistant done`
- assistant tool-call record -> `traceToolCall done`
- tool result record -> `traceToolResult done`

After resume, new live events continue appending to the rebuilt trace.

## Helper API

Keep trace mutation helpers small and deterministic:

```go
func (m Model) appendTraceNode(node traceNode) Model
func (m Model) updateTraceNode(id string, fn func(*traceNode)) Model
func (m Model) findTraceByToolCallID(id string) int
func (m Model) findTraceBySubagentID(id string) int
func (m Model) rebuildTraceFromConversation() Model
```

Use value receivers consistently with the rest of `Model` update code, unless a
local call site already uses pointer mutation.

## Error Handling and Limits

- Trace rendering must not panic on malformed or missing IDs.
- Detail strings are truncated for display. The stored in-memory detail can be
  longer, but renderers should cap output to avoid slow terminal redraws.
- Tool arguments use the same sanitization path already used before adding tool
  calls to conversation history.
- Orphan tool results are shown instead of ignored.
- Empty trace state opens a page instead of only setting a status message.

## Testing

Add focused tests before implementation:

- `ParseSlashCommand("/trace")` returns `CommandTrace`.
- `/trace` opens `modeTrace` and renders the empty state when no nodes exist.
- Trace page navigation supports `↑↓/jk`, `Enter` detail, and `Esc` back/close.
- Submitting a user message records a `traceUser` node.
- A streaming assistant response updates one active assistant node rather than
  appending one node per token.
- A tool call plus tool result renders a `traceToolCall` and `traceToolResult`
  with arrow rail markers.
- A failed tool result renders `✗`.
- A subagent run/event/result updates the matching `traceSubagent` node.
- Resume rebuilds historical trace nodes from restored conversation messages.

Run at minimum:

```bash
go test ./internal/tui -run 'TestParseSlashCommand|TestTrace' -count=1
go test ./internal/tui -count=1
```

If shared model behavior changes more broadly, run:

```bash
go test ./...
```
