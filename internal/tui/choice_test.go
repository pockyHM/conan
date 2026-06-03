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

func TestNewChoiceStateSupportsMultipleDefaults(t *testing.T) {
	call := llm.ToolCall{
		ID:   "choice-1",
		Name: metaToolAskChoice,
		Arguments: json.RawMessage(`{
			"question":"Pick targets",
			"multiple":true,
			"options":[
				{"label":"Logs","value":"logs"},
				{"label":"Metrics","value":"metrics"},
				{"label":"Traces","value":"traces"}
			],
			"default_values":["logs","traces"]
		}`),
	}

	state, err := newChoiceState(7, call)
	if err != nil {
		t.Fatalf("newChoiceState returned error: %v", err)
	}
	if !state.multiple {
		t.Fatal("multiple should be true")
	}
	for _, want := range []string{"logs", "traces"} {
		if !state.selectedValues[want] {
			t.Fatalf("selectedValues = %#v, want %q selected", state.selectedValues, want)
		}
	}
	if state.selectedValues["metrics"] {
		t.Fatalf("selectedValues = %#v, metrics should not be selected", state.selectedValues)
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

func TestMultiChoiceResultJSON(t *testing.T) {
	state := choiceState{
		options: []choiceOption{
			{Label: "Logs", Value: "logs", Description: "Inspect logs"},
			{Label: "Metrics", Value: "metrics"},
			{Label: "Traces", Value: "traces"},
		},
		multiple:       true,
		selectedValues: map[string]bool{"logs": true, "traces": true},
	}

	selected := state.selectedResultJSON()
	want := `{"selected":true,"values":["logs","traces"],"labels":["Logs","Traces"],"options":[{"value":"logs","label":"Logs"},{"value":"traces","label":"Traces"}]}`
	if selected != want {
		t.Fatalf("selected result = %s, want %s", selected, want)
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

func TestMultiChoiceToggleSelectedValue(t *testing.T) {
	state := choiceState{
		options:        []choiceOption{{Label: "Logs", Value: "logs"}, {Label: "Metrics", Value: "metrics"}},
		multiple:       true,
		selectedValues: map[string]bool{"logs": true},
	}

	state.toggleSelectedValue()
	if state.selectedValues["logs"] {
		t.Fatalf("selectedValues = %#v, logs should be toggled off", state.selectedValues)
	}

	state.moveChoice(1)
	state.toggleSelectedValue()
	if !state.selectedValues["metrics"] {
		t.Fatalf("selectedValues = %#v, metrics should be toggled on", state.selectedValues)
	}
}
