package skills

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pockyHM/conan/internal/llm"
)

func ToolDefs() []llm.ToolDef {
	return []llm.ToolDef{{
		Name:        ToolName,
		Description: "Load a visible Conan skill by name when its instructions would materially improve the answer. Use only names from the Available skills index.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "Name of the visible skill to load"},
				"reason": {"type": "string", "description": "Brief reason this skill is relevant"}
			},
			"required": ["name", "reason"]
		}`),
	}}
}

type ToolHandler struct {
	byName        map[string]Skill
	maxSkillChars int
}

func NewToolHandler(visible []Skill, maxSkillChars int) ToolHandler {
	byName := make(map[string]Skill, len(visible))
	for _, skill := range visible {
		byName[skill.Name] = skill
	}
	return ToolHandler{byName: byName, maxSkillChars: maxSkillChars}
}

type readArgs struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (h ToolHandler) Handle(raw json.RawMessage) string {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Sprintf("skill_read error: invalid arguments: %v", err)
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return "skill_read error: name is required"
	}
	skill, ok := h.byName[name]
	if !ok {
		return fmt.Sprintf("skill_read error: skill %q is not visible in this session", name)
	}
	limit := h.maxSkillChars
	if skill.MaxChars > 0 && (limit == 0 || skill.MaxChars < limit) {
		limit = skill.MaxChars
	}
	body := limitRunes(skill.Body, limit)
	scope := skill.Scope
	if skill.Scope == ScopeCluster {
		scope = "cluster:" + skill.Cluster
	}
	return fmt.Sprintf("Skill: %s\nScope: %s\nReason: %s\n\n%s", skill.Name, scope, strings.TrimSpace(args.Reason), body)
}

func limitRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}
