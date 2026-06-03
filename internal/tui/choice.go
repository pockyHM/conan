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

type choiceState struct {
	streamID       uint64
	call           llm.ToolCall
	question       string
	options        []choiceOption
	selected       int
	multiple       bool
	selectedValues map[string]bool
	allowCancel    bool
}

type choiceArgs struct {
	Question      string         `json:"question"`
	Options       []choiceOption `json:"options"`
	DefaultValue  string         `json:"default_value"`
	DefaultValues []string       `json:"default_values"`
	Multiple      bool           `json:"multiple"`
	AllowCancel   bool           `json:"allow_cancel"`
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
		return choiceState{}, fmt.Errorf("at least %d options are required", minChoiceOptions)
	}
	if len(args.Options) > maxChoiceOptions {
		return choiceState{}, fmt.Errorf("at most %d options are allowed", maxChoiceOptions)
	}

	seen := make(map[string]struct{}, len(args.Options))
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
		if _, ok := seen[args.Options[i].Value]; ok {
			return choiceState{}, fmt.Errorf("duplicate option value %q", args.Options[i].Value)
		}
		seen[args.Options[i].Value] = struct{}{}
	}

	selected := 0
	defaultValue := strings.TrimSpace(args.DefaultValue)
	for i, opt := range args.Options {
		if opt.Value == defaultValue {
			selected = i
			break
		}
	}
	selectedValues := make(map[string]bool)
	if args.Multiple {
		defaultValues := append([]string(nil), args.DefaultValues...)
		if defaultValue != "" && len(defaultValues) == 0 {
			defaultValues = append(defaultValues, defaultValue)
		}
		for _, value := range defaultValues {
			value = strings.TrimSpace(value)
			if _, ok := seen[value]; ok {
				selectedValues[value] = true
			}
		}
	}

	return choiceState{
		streamID:       streamID,
		call:           call,
		question:       args.Question,
		options:        args.Options,
		selected:       selected,
		multiple:       args.Multiple,
		selectedValues: selectedValues,
		allowCancel:    args.AllowCancel,
	}, nil
}

func (s choiceState) selectedResultJSON() string {
	if s.multiple {
		return s.selectedValuesResultJSON()
	}
	if len(s.options) == 0 || s.selected < 0 || s.selected >= len(s.options) {
		return `{"selected":false}`
	}
	opt := s.options[s.selected]
	result, err := json.Marshal(struct {
		Selected bool   `json:"selected"`
		Value    string `json:"value"`
		Label    string `json:"label"`
	}{
		Selected: true,
		Value:    opt.Value,
		Label:    opt.Label,
	})
	if err != nil {
		return `{"selected":false}`
	}
	return string(result)
}

func (s choiceState) selectedValuesResultJSON() string {
	var values []string
	var labels []string
	var options []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	for _, opt := range s.options {
		if !s.selectedValues[opt.Value] {
			continue
		}
		values = append(values, opt.Value)
		labels = append(labels, opt.Label)
		options = append(options, struct {
			Value string `json:"value"`
			Label string `json:"label"`
		}{Value: opt.Value, Label: opt.Label})
	}
	if len(values) == 0 {
		return `{"selected":false,"values":[],"labels":[],"options":[]}`
	}
	result, err := json.Marshal(struct {
		Selected bool     `json:"selected"`
		Values   []string `json:"values"`
		Labels   []string `json:"labels"`
		Options  []struct {
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"options"`
	}{
		Selected: true,
		Values:   values,
		Labels:   labels,
		Options:  options,
	})
	if err != nil {
		return `{"selected":false,"values":[],"labels":[],"options":[]}`
	}
	return string(result)
}

func choiceCancelledResultJSON() string {
	result, err := json.Marshal(struct {
		Selected  bool `json:"selected"`
		Cancelled bool `json:"cancelled"`
	}{
		Selected:  false,
		Cancelled: true,
	})
	if err != nil {
		return `{"selected":false,"cancelled":true}`
	}
	return string(result)
}

func (s *choiceState) moveChoice(delta int) {
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	last := len(s.options) - 1
	if s.selected > last {
		s.selected = last
	}
}

func (s *choiceState) toggleSelectedValue() {
	if !s.multiple || len(s.options) == 0 || s.selected < 0 || s.selected >= len(s.options) {
		return
	}
	if s.selectedValues == nil {
		s.selectedValues = make(map[string]bool)
	}
	value := s.options[s.selected].Value
	if s.selectedValues[value] {
		delete(s.selectedValues, value)
		return
	}
	s.selectedValues[value] = true
}
