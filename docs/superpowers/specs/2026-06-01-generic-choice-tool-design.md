# Generic Choice Tool Design

## Goal

Add a generic model-driven choice interaction to the Conan TUI. The model should be able to ask the user to choose from a bounded set of options through normal tool calling, while the TUI renders the choice UI and returns the selected option as a tool result.

## Context

Conan already normalizes OpenAI and Anthropic tool calls into `llm.ToolCallEvent`. The TUI records assistant tool calls with `Conversation.AddToolCall`, records tool results with `Conversation.AddToolResult`, and resumes the model through `completeToolAndResume`. It also already has modal input flows for security confirmation, node credential prompts, and selectors.

This feature should follow that existing architecture instead of parsing JSON from assistant text.

## Architecture

Add a local meta tool named `ask_choice`.

The tool schema accepts:

- `question`: text shown above the choices.
- `options`: two to ten options, each with `label`, `value`, and optional `description`.
- `default_value`: optional value to preselect.
- `allow_cancel`: optional boolean that allows Esc cancellation.

When the model calls `ask_choice`, Conan should validate the arguments and enter a new `modeChoice` TUI mode. This mode is local-only: it does not run risk review, does not dispatch to node agents, and does not write security audit entries.

## User Experience

The footer is replaced with a compact choice panel:

- Question at the top.
- One option per row.
- The selected row uses the same arrow-highlight convention as existing selectors.
- `description` is shown beside or below an option when present.
- Up/Down moves the cursor.
- Enter submits the selected option.
- Esc cancels only when `allow_cancel` is true.

After a selection, the TUI returns a JSON tool result to the model:

```json
{"selected":true,"value":"continue","label":"Continue"}
```

If the user cancels:

```json
{"selected":false,"cancelled":true}
```

The model then continues from the normal tool-result loop.

## Data Flow

1. `ask_choice` is included in the model tool definitions.
2. The provider streams an `llm.ToolCallEvent`.
3. The TUI records the assistant tool call using existing conversation plumbing.
4. The TUI detects `ask_choice` before risk assessment and validates its arguments.
5. Valid arguments populate a `choiceState` and switch to `modeChoice`.
6. User selection creates a `multiToolResultMsg` containing the JSON result.
7. Existing tool-result handling records the result and resumes the model.

Invalid arguments should return a tool result error immediately and should not open the choice UI.

## Error Handling

Argument validation should reject:

- Invalid JSON.
- Blank `question`.
- Fewer than two options or more than ten options.
- Blank option `label` or `value`.
- Duplicate option `value`.

If `default_value` does not match an option, select the first option.

If the active stream is interrupted while a choice is open, cancel the stream and clear the choice state. When Esc is allowed and used, return the cancellation result to the model instead of treating it as a stream interrupt.

## Testing

Add focused TUI tests for:

- `ask_choice` appears in the tool definitions.
- A valid `ask_choice` tool call enters `modeChoice`.
- `default_value` preselects the matching option.
- Up/Down and Enter return the selected JSON tool result and resume the stream.
- Esc returns cancellation when `allow_cancel` is true.
- Esc is ignored or leaves a status message when cancellation is disabled.
- Invalid arguments return an error tool result without entering `modeChoice`.

No provider-level tests are needed unless tool name normalization changes, because existing OpenAI and Anthropic provider tests already cover generic tool-call streaming and history encoding.
