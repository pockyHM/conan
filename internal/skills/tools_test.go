package skills

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDefsIncludesSkillRead(t *testing.T) {
	defs := ToolDefs()
	if len(defs) != 1 || defs[0].Name != ToolName {
		t.Fatalf("defs = %#v", defs)
	}
	if !strings.Contains(defs[0].Description, "Load") {
		t.Fatalf("description = %q", defs[0].Description)
	}

	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(defs[0].InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	if !required["name"] || !required["reason"] {
		t.Fatalf("required = %#v, want name and reason", schema.Required)
	}
}

func TestHandleSkillReadReturnsCappedBody(t *testing.T) {
	handler := NewToolHandler([]Skill{{Name: "k8s-debug", Scope: ScopeCluster, Cluster: "prod", Body: strings.Repeat("a", 20)}}, 8)
	out := handler.Handle(json.RawMessage(`{"name":"k8s-debug","reason":"diagnose pods"}`))
	if !strings.Contains(out, "Skill: k8s-debug") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatalf("output missing truncation: %q", out)
	}

	handler = NewToolHandler([]Skill{{Name: "short-skill", Body: strings.Repeat("b", 20), MaxChars: 5}}, 100)
	out = handler.Handle(json.RawMessage(`{"name":"short-skill","reason":"prefer local cap"}`))
	if !strings.Contains(out, "bbbbb\n[truncated]") {
		t.Fatalf("output did not honor per-skill MaxChars: %q", out)
	}
}

func TestHandleSkillReadRejectsHiddenSkill(t *testing.T) {
	handler := NewToolHandler([]Skill{{Name: "k8s-debug", Body: "body"}}, 100)
	out := handler.Handle(json.RawMessage(`{"name":"missing","reason":"x"}`))
	if !strings.Contains(out, "not visible") {
		t.Fatalf("output = %q", out)
	}
}
