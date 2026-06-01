# Generic Choice Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a generic `ask_choice` model tool that lets the model pause the TUI for a bounded user choice and resume with the selected value as a tool result.

**Architecture:** Implement `ask_choice` as a local meta tool, not assistant text JSON parsing. Keep argument parsing and choice state in a small `internal/tui/choice.go` unit, expose the tool in `internal/tui/metatools.go`, and wire a new `modeChoice` into `internal/tui/model.go` using the existing tool call/result/resume loop.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, existing Conan `llm.ToolCallEvent`, `multiToolResultMsg`, and conversation history plumbing.

---

## File Structure

- Create `internal/tui/choice.go`: choice argument structs, validation, result JSON builders, and state movement helpers.
- Create `internal/tui/choice_test.go`: focused tests for parsing, default selection, duplicate values, selection output, and cancellation output.
- Modify `internal/tui/metatools.go`: add the `ask_choice` constant and tool definition.
- Modify `internal/tui/metatools_test.go`: assert `ask_choice` is exposed and has the expected schema fields.
- Modify `internal/tui/model.go`: add `modeChoice`, `choiceState`, tool-call routing, key handling, footer rendering, and stream cleanup.
- Modify `internal/tui/model_test.go`: add TUI integration tests for valid calls, selecting an option, cancellation, and invalid arguments.

## Task 1: Choice Parser And Result Helpers

**Files:**
- Create: `internal/tui/choice.go`
- Test: `internal/tui/choice_test.go`

- [ ] **Step 1: Write failing parser/result tests**

Add `internal/tui/choice_test.go`:

```go
package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/llm"
)

func TestNewChoiceStateValidatesAndSelectsDefault(t *testing.T) {
	call := llm.ToolCall{
		ID:   "choice-1",
		Name: metaToolAskChoice,
		Arguments: json.RawMessage(`{
			"question":"How should I proceed?",
			"options":[
				{"label":"Continue","value":"continue","description":"Run the planned command"},
				{"label":"Revise","value":"revise"}
			],
			"default_value":"revise",
			"allow_cancel":true
		}`),
	}

	state, err := newChoiceState(7, call)
	if err != nil {
		t.Fatalf("newChoiceState returned error: %v", err)
	}
	if state.streamID != 7 || state.call.ID != "choice-1" {
		t.Fatalf("state identifiers = streamID %d call %s", state.streamID, state.call.ID)
	}
	if state.question != "How should I proceed?" {
		t.Fatalf("question = %q", state.question)
	}
	if len(state.options) != 2 {
		t.Fatalf("options = %#v", state.options)
	}
	if state.selected != 1 {
		t.Fatalf("selected = %d, want default option index 1", state.selected)
	}
	if !state.allowCancel {
		t.Fatal("allowCancel should be true")
	}
}

func TestNewChoiceStateRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "invalid json", raw: `{`, want: "invalid ask_choice arguments"},
		{name: "blank question", raw: `{"question":" ","options":[{"label":"A","value":"a"},{"label":"B","value":"b"}]}`, want: "question is required"},
		{name: "too few options", raw: `{"question":"Pick","options":[{"label":"A","value":"a"}]}`, want: "at least 2 options"},
		{name: "too many options", raw: `{"question":"Pick","options":[{"label":"1","value":"1"},{"label":"2","value":"2"},{"label":"3","value":"3"},{"label":"4","value":"4"},{"label":"5","value":"5"},{"label":"6","value":"6"},{"label":"7","value":"7"},{"label":"8","value":"8"},{"label":"9","value":"9"},{"label":"10","value":"10"},{"label":"11","value":"11"}]}`, want: "at most 10 options"},
		{name: "blank label", raw: `{"question":"Pick","options":[{"label":"","value":"a"},{"label":"B","value":"b"}]}`, want: "label is required"},
		{name: "blank value", raw: `{"question":"Pick","options":[{"label":"A","value":" "},{"label":"B","value":"b"}]}`, want: "value is required"},
		{name: "duplicate value", raw: `{"question":"Pick","options":[{"label":"A","value":"same"},{"label":"B","value":"same"}]}`, want: "duplicate option value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newChoiceState(1, llm.ToolCall{ID: "c1", Name: metaToolAskChoice, Arguments: json.RawMessage(tt.raw)})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestChoiceResultJSON(t *testing.T) {
	state := choiceState{
		options: []choiceOption{
			{Label: "Continue", Value: "continue", Description: "Run it"},
			{Label: "Revise", Value: "revise"},
		},
		selected: 1,
	}

	selected := state.selectedResultJSON()
	if selected != `{"selected":true,"value":"revise","label":"Revise"}` {
		t.Fatalf("selected result = %s", selected)
	}

	cancelled := choiceCancelledResultJSON()
	if cancelled != `{"selected":false,"cancelled":true}` {
		t.Fatalf("cancelled result = %s", cancelled)
	}
}

func TestChoiceMovementClampsToOptions(t *testing.T) {
	state := choiceState{options: []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}}

	state.moveChoice(-1)
	if state.selected != 0 {
		t.Fatalf("selected after up = %d, want 0", state.selected)
	}
	state.moveChoice(1)
	state.moveChoice(1)
	if state.selected != 1 {
		t.Fatalf("selected after down = %d, want 1", state.selected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestNewChoiceState|TestChoiceResultJSON|TestChoiceMovement' -count=1
```

Expected: FAIL because `choiceState`, `choiceOption`, `newChoiceState`, `selectedResultJSON`, `choiceCancelledResultJSON`, and `moveChoice` are undefined.

- [ ] **Step 3: Implement parser and helpers**

Create `internal/tui/choice.go`:

```go
package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pockyHM/conan/internal/llm"
)

const (
	minChoiceOptions = 2
	maxChoiceOptions = 10
)

type choiceOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type choiceArgs struct {
	Question     string         `json:"question"`
	Options      []choiceOption `json:"options"`
	DefaultValue string         `json:"default_value,omitempty"`
	AllowCancel  bool           `json:"allow_cancel,omitempty"`
}

type choiceState struct {
	streamID    uint64
	call        llm.ToolCall
	question    string
	options     []choiceOption
	selected    int
	allowCancel bool
}

func newChoiceState(streamID uint64, call llm.ToolCall) (choiceState, error) {
	var args choiceArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return choiceState{}, fmt.Errorf("invalid ask_choice arguments: %w", err)
	}
	args.Question = strings.TrimSpace(args.Question)
	if args.Question == "" {
		return choiceState{}, fmt.Errorf("question is required")
	}
	if len(args.Options) < minChoiceOptions {
		return choiceState{}, fmt.Errorf("at least 2 options are required")
	}
	if len(args.Options) > maxChoiceOptions {
		return choiceState{}, fmt.Errorf("at most 10 options are allowed")
	}

	seen := make(map[string]bool, len(args.Options))
	selected := 0
	for i := range args.Options {
		args.Options[i].Label = strings.TrimSpace(args.Options[i].Label)
		args.Options[i].Value = strings.TrimSpace(args.Options[i].Value)
		args.Options[i].Description = strings.TrimSpace(args.Options[i].Description)
		if args.Options[i].Label == "" {
			return choiceState{}, fmt.Errorf("option %d label is required", i+1)
		}
		if args.Options[i].Value == "" {
			return choiceState{}, fmt.Errorf("option %d value is required", i+1)
		}
		if seen[args.Options[i].Value] {
			return choiceState{}, fmt.Errorf("duplicate option value %q", args.Options[i].Value)
		}
		seen[args.Options[i].Value] = true
		if args.Options[i].Value == args.DefaultValue {
			selected = i
		}
	}

	return choiceState{
		streamID:    streamID,
		call:        call,
		question:    args.Question,
		options:     args.Options,
		selected:    selected,
		allowCancel: args.AllowCancel,
	}, nil
}

func (s *choiceState) moveChoice(delta int) {
	if len(s.options) == 0 {
		s.selected = 0
		return
	}
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.options) {
		s.selected = len(s.options) - 1
	}
}

func (s choiceState) selectedResultJSON() string {
	opt := s.options[s.selected]
	data, _ := json.Marshal(map[string]any{
		"selected": true,
		"value":    opt.Value,
		"label":    opt.Label,
	})
	return string(data)
}

func choiceCancelledResultJSON() string {
	data, _ := json.Marshal(map[string]any{
		"selected":  false,
		"cancelled": true,
	})
	return string(data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestNewChoiceState|TestChoiceResultJSON|TestChoiceMovement' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/choice.go internal/tui/choice_test.go
git commit -m "feat: add choice tool state parser"
```

## Task 2: Expose The `ask_choice` Meta Tool

**Files:**
- Modify: `internal/tui/metatools.go`
- Modify: `internal/tui/metatools_test.go`

- [ ] **Step 1: Write failing tool-definition tests**

In `internal/tui/metatools_test.go`, add:

```go
func TestAskChoiceToolDefinition(t *testing.T) {
	var def *llm.ToolDef
	for i := range metaToolDefs {
		if metaToolDefs[i].Name == metaToolAskChoice {
			def = &metaToolDefs[i]
			break
		}
	}
	if def == nil {
		t.Fatal("ask_choice meta tool is not exposed")
	}
	if !strings.Contains(def.Description, "choose one option") {
		t.Fatalf("description = %q, want choice guidance", def.Description)
	}
	schema := string(def.InputSchema)
	for _, want := range []string{`"question"`, `"options"`, `"label"`, `"value"`, `"description"`, `"default_value"`, `"allow_cancel"`, `"minItems": 2`, `"maxItems": 10`} {
		if !strings.Contains(schema, want) {
			t.Fatalf("ask_choice schema missing %s:\n%s", want, schema)
		}
	}
}

func TestAskChoiceAvailableByDefault(t *testing.T) {
	model := NewModel(ModelConfig{})
	for _, tool := range model.availableToolDefs() {
		if tool.Name == metaToolAskChoice {
			return
		}
	}
	t.Fatal("ask_choice should be available by default")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestAskChoice' -count=1
```

Expected: FAIL because `metaToolAskChoice` and the tool definition are missing.

- [ ] **Step 3: Add the tool constant and definition**

In `internal/tui/metatools.go`, extend the const block:

```go
	metaToolAskChoice    = "ask_choice"
```

Add this entry to `metaToolDefs`:

```go
	{
		Name:        metaToolAskChoice,
		Description: "Ask the user to choose one option in the TUI before continuing. Use this when the next step depends on a user decision, such as choosing a plan, approving a non-tool workflow choice, selecting a mode, or clarifying an ambiguous preference. Do not use for security approval of tool execution; Conan handles that separately.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"question": {"type": "string", "description": "The concise question to show to the user."},
				"options": {
					"type": "array",
					"minItems": 2,
					"maxItems": 10,
					"items": {
						"type": "object",
						"properties": {
							"label": {"type": "string", "description": "Short user-visible option label."},
							"value": {"type": "string", "description": "Stable machine-readable value returned to the model."},
							"description": {"type": "string", "description": "Optional short explanation of the option."}
						},
						"required": ["label", "value"]
					}
				},
				"default_value": {"type": "string", "description": "Optional option value to preselect."},
				"allow_cancel": {"type": "boolean", "description": "Whether Esc should return a cancellation result."}
			},
			"required": ["question", "options"]
		}`),
	},
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestAskChoice|TestExposedToolNamesMatchOpenAIPattern' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/metatools.go internal/tui/metatools_test.go
git commit -m "feat: expose ask choice meta tool"
```

## Task 3: Route `ask_choice` Tool Calls Into Choice Mode

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing TUI routing tests**

In `internal/tui/model_test.go`, add:

```go
func TestAskChoiceToolCallEntersChoiceMode(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{
			"question":"Pick a path",
			"options":[
				{"label":"Continue","value":"continue"},
				{"label":"Revise","value":"revise"}
			],
			"default_value":"revise",
			"allow_cancel":true
		}`),
	}})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("ask_choice should pause for user input without command, got %T", execCmd(t, cmd))
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if model.choice.question != "Pick a path" || model.choice.selected != 1 {
		t.Fatalf("choice state = %#v", model.choice)
	}
	if len(model.messages) != 1 || model.messages[0].toolCallID != "choice-1" || model.messages[0].toolName != metaToolAskChoice {
		t.Fatalf("messages = %#v, want recorded ask_choice placeholder", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].ToolCallID != "choice-1" || msgs[0].ToolName != metaToolAskChoice {
		t.Fatalf("conversation messages = %#v, want recorded ask_choice tool call", msgs)
	}
}

func TestAskChoiceInvalidArgumentsReturnToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1

	next, cmd := model.Update(streamEventMsg{streamID: 1, Event: llm.ToolCallEvent{
		ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{"question":"Pick","options":[]}`),
	}})
	model = next.(Model)

	if model.mode == modeChoice {
		t.Fatal("invalid ask_choice arguments should not open choice mode")
	}
	if cmd == nil {
		t.Fatal("invalid ask_choice arguments should return a tool result command")
	}
	msg := execCmd(t, cmd)
	result, ok := msg.(multiToolResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want multiToolResultMsg", msg)
	}
	if len(result.Results) != 1 || result.Results[0].Success {
		t.Fatalf("results = %#v, want one failed result", result.Results)
	}
	if !strings.Contains(result.Results[0].Output, "at least 2 options") {
		t.Fatalf("output = %q, want validation error", result.Results[0].Output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestAskChoiceToolCallEntersChoiceMode|TestAskChoiceInvalidArgumentsReturnToolResult' -count=1
```

Expected: FAIL because `modeChoice`, `Model.choice`, and routing are missing.

- [ ] **Step 3: Add mode and state fields**

In `internal/tui/model.go`, add `modeChoice` to the `tuiMode` const block after `modeConfirm`:

```go
	modeChoice
```

Add this field to `Model` near `confirmChoice`:

```go
	choice choiceState
```

- [ ] **Step 4: Route tool calls before risk assessment**

In the `llm.ToolCallEvent` branch in `Model.Update`, replace the final routing block with:

```go
			var toolCmd tea.Cmd
			if e.Name == metaToolAskChoice {
				choice, err := newChoiceState(msg.streamID, call)
				if err != nil {
					toolCmd = func() tea.Msg {
						return singleToolError(msg.streamID, call, err.Error())
					}
				} else {
					m.mode = modeChoice
					m.choice = choice
					m.input = ""
					m.ac = newAutocompleteWithLanguage(m.uiLanguage)
					m.status = m.uiLanguage.tr("Use ↑↓ to choose, Enter to confirm", "使用 ↑↓ 选择，Enter 确认")
					m.updateViewportContent()
					return m, nil
				}
			} else if memory.IsMemoryTool(e.Name) {
				toolCmd = m.handleMemoryTool(msg.streamID, call)
			} else if e.Name == metaToolSubagentsRun {
				toolCmd = m.dispatchSubagentsRun(msg.streamID, call)
			} else {
				toolCmd = m.assessToolRisk(msg.streamID, call)
			}
```

Keep the existing `m.updateViewportContent()` and `tea.Batch(...)` return after this block unchanged.

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestAskChoiceToolCallEntersChoiceMode|TestAskChoiceInvalidArgumentsReturnToolResult' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: route ask choice tool calls"
```

## Task 4: Render And Handle Choice Mode

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing interaction tests**

In `internal/tui/model_test.go`, add:

```go
func TestAskChoiceEnterReturnsSelectedToolResult(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question: "Pick one",
		options: []choiceOption{
			{Label: "Continue", Value: "continue"},
			{Label: "Revise", Value: "revise"},
		},
		selected:    0,
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.choice.selected != 1 {
		t.Fatalf("selected = %d, want 1", model.choice.selected)
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.choice.call.ID != "" {
		t.Fatalf("choice state should be cleared: %#v", model.choice)
	}
	if cmd == nil {
		t.Fatal("enter should continue after tool result")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].toolOutput, `"value":"revise"`) {
		t.Fatalf("messages = %#v, want selected tool output", model.messages)
	}
	msgs := conv.Messages()
	if len(msgs) != 1 || msgs[0].Role != conversation.RoleTool || !strings.Contains(msgs[0].Content, `"value":"revise"`) {
		t.Fatalf("conversation messages = %#v, want selected tool result", msgs)
	}
}

func TestAskChoiceEscCancelsWhenAllowed(t *testing.T) {
	conv := conversation.New("test", nil, "model")
	model := NewModel(ModelConfig{Cluster: "test", Model: "m", Conv: conv, Provider: &fakeProvider{}})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.streamEnded = true
	model.streamToolExpected = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID:    1,
		call:        llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice, Arguments: []byte(`{}`)},
		question:    "Pick one",
		options:     []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
		allowCancel: true,
	}
	model.messages = []chatMsg{{role: "tool", toolCallID: "choice-1", toolName: metaToolAskChoice}}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if cmd == nil {
		t.Fatal("cancel should continue after tool result")
	}
	if !strings.Contains(model.messages[0].toolOutput, `"cancelled":true`) {
		t.Fatalf("tool output = %q, want cancellation JSON", model.messages[0].toolOutput)
	}
}

func TestAskChoiceEscBlockedWhenCancelDisabled(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.mode = modeChoice
	model.choice = choiceState{
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice},
		question: "Pick one",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("cmd = %T, want nil when cancel is disabled", execCmd(t, cmd))
	}
	if model.mode != modeChoice {
		t.Fatalf("mode = %v, want modeChoice", model.mode)
	}
	if !strings.Contains(model.status, "Choose an option") && !strings.Contains(model.status, "请选择") {
		t.Fatalf("status = %q, want choose-option guidance", model.status)
	}
}

func TestAskChoiceViewRendersOptions(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.width = 80
	model.mode = modeChoice
	model.choice = choiceState{
		question: "Pick a path",
		options: []choiceOption{
			{Label: "Continue", Value: "continue", Description: "Run the planned command"},
			{Label: "Revise", Value: "revise"},
		},
		selected:    0,
		allowCancel: true,
	}

	view := model.View()
	for _, want := range []string{"Pick a path", "▶ Continue", "Run the planned command", "Revise", "Enter to choose", "Esc to cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'TestAskChoiceEnter|TestAskChoiceEsc|TestAskChoiceView' -count=1
```

Expected: FAIL because choice key handling and rendering are missing.

- [ ] **Step 3: Add key handling and result finalization**

In `handleKey`, before the `modeNodeSelect` check, add:

```go
	if m.mode == modeChoice {
		return m.handleChoiceKey(key)
	}
```

Add these methods near `handleConfirmKey`:

```go
func (m Model) handleChoiceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		m.choice.moveChoice(-1)
		return m, nil
	case tea.KeyDown:
		m.choice.moveChoice(1)
		return m, nil
	case tea.KeyEnter:
		return m.finishChoice(m.choice.selectedResultJSON())
	case tea.KeyEsc, tea.KeyCtrlC:
		if !m.choice.allowCancel {
			m.status = m.uiLanguage.tr("Choose an option to continue", "请选择一个选项继续")
			return m, nil
		}
		return m.finishChoice(choiceCancelledResultJSON())
	default:
		return m, nil
	}
}

func (m Model) finishChoice(output string) (tea.Model, tea.Cmd) {
	state := m.choice
	m.mode = modeChat
	m.choice = choiceState{}
	m.input = ""
	m.ac = newAutocompleteWithLanguage(m.uiLanguage)
	m.fillToolPlaceholder(state.call, output, []nodeToolResult{{Node: "local", Output: output, Success: true}})
	if m.conv != nil {
		m.conv.AddToolResult(state.call.ID, output)
	}
	m.status = m.uiLanguage.tr("Choice recorded", "已记录选择")
	return m.completeToolAndResume(state.streamID, state.call)
}
```

- [ ] **Step 4: Add footer rendering**

In `View`, after the `modeConfirm` footer branch, add:

```go
	} else if m.mode == modeChoice {
		footer = m.renderChoiceFooter()
```

Add this method near `renderConfirmFooter`:

```go
func (m Model) renderChoiceFooter() string {
	state := m.choice
	lines := []string{
		inputPromptStyle.Render(state.question),
	}
	for i, opt := range state.options {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		if i == state.selected {
			prefix = "\u25b6 "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
		}
		line := prefix + opt.Label
		if opt.Description != "" {
			line += "  " + opt.Description
		}
		lines = append(lines, style.Render(line))
	}
	help := m.uiLanguage.tr("Use ↑↓ to choose, Enter to choose", "使用 ↑↓ 选择，Enter 确认")
	if state.allowCancel {
		help += m.uiLanguage.tr(", Esc to cancel", "，Esc 取消")
	}
	lines = append(lines, statusStyle.Render(help))
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/tui -run 'TestAskChoiceEnter|TestAskChoiceEsc|TestAskChoiceView' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: render ask choice interactions"
```

## Task 5: Stream Cleanup And Regression Pass

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Write failing interrupt cleanup test**

In `internal/tui/model_test.go`, add:

```go
func TestAskChoiceStreamInterruptClearsChoiceState(t *testing.T) {
	model := NewModel(ModelConfig{Cluster: "test", Model: "m"})
	model.streaming = true
	model.streamID = 1
	model.activeStreamID = 1
	model.mode = modeChoice
	model.choice = choiceState{
		streamID: 1,
		call:     llm.ToolCall{ID: "choice-1", Name: metaToolAskChoice},
		question: "Pick one",
		options:  []choiceOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)

	if model.streaming {
		t.Fatal("streaming should be false after interrupt")
	}
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", model.mode)
	}
	if model.choice.call.ID != "" {
		t.Fatalf("choice should be cleared: %#v", model.choice)
	}
	if model.status != "Interrupted" && model.status != "已中断" {
		t.Fatalf("status = %q, want interrupted", model.status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/tui -run 'TestAskChoiceStreamInterruptClearsChoiceState' -count=1
```

Expected: FAIL because Ctrl-C in choice mode currently goes through choice cancellation rules instead of stream interruption cleanup.

- [ ] **Step 3: Add explicit stream-interrupt cleanup**

In `handleChoiceKey`, make Ctrl-C interrupt even when Esc cancellation is disabled:

```go
	case tea.KeyCtrlC:
		m.finishStream(true)
		m.mode = modeChat
		m.choice = choiceState{}
		m.status = m.uiLanguage.tr("Interrupted", "已中断")
		return m, nil
	case tea.KeyEsc:
		if !m.choice.allowCancel {
			m.status = m.uiLanguage.tr("Choose an option to continue", "请选择一个选项继续")
			return m, nil
		}
		return m.finishChoice(choiceCancelledResultJSON())
```

Remove `tea.KeyCtrlC` from the Esc case.

- [ ] **Step 4: Run the focused choice tests**

Run:

```bash
go test ./internal/tui -run 'TestAskChoice|TestNewChoiceState|TestChoiceResultJSON|TestChoiceMovement' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run package regression tests**

Run:

```bash
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "fix: clear ask choice state on interrupt"
```

## Task 6: Final Verification

**Files:**
- No source changes expected.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 2: Inspect git status**

Run:

```bash
git status --short
```

Expected: only unrelated pre-existing worktree changes may remain. The commits from this plan should contain only the choice feature files.

- [ ] **Step 3: Summarize implementation**

Report:

- The new tool name: `ask_choice`.
- The user flow: model tool call, TUI choice mode, JSON tool result, model resume.
- The verification command result from `go test ./... -count=1`.

## Self-Review

Spec coverage:

- Generic tool calling is covered by Tasks 2 and 3.
- Bounded options, default selection, cancellation, and validation are covered by Tasks 1 and 4.
- TUI rendering and keyboard behavior are covered by Task 4.
- Tool-result resume behavior is covered by Tasks 3 and 4.
- Stream interruption cleanup is covered by Task 5.
- Regression verification is covered by Task 6.

Placeholder scan: this plan uses concrete file paths, commands, expected outcomes, and code snippets for each code step.

Type consistency: the plan consistently uses `metaToolAskChoice`, `modeChoice`, `choiceState`, `choiceOption`, `newChoiceState`, `selectedResultJSON`, `choiceCancelledResultJSON`, and `finishChoice`.
