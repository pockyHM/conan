package skills

import (
	"strings"
	"testing"
)

func TestParseSkillMarkdown(t *testing.T) {
	raw := []byte(`---
name: k8s-debug
description: Use when diagnosing Kubernetes failures.
version: 1.0.0
tags:
  - k8s
  - ops
max_chars: 1200
---

# K8s Debug

Inspect pods, events, and logs before changing resources.
`)

	skill, err := ParseSkillMarkdown("skills/k8s-debug/SKILL.md", raw, 6000)
	if err != nil {
		t.Fatal(err)
	}

	if skill.Name != "k8s-debug" {
		t.Fatalf("Name = %q", skill.Name)
	}
	if skill.Description != "Use when diagnosing Kubernetes failures." {
		t.Fatalf("Description = %q", skill.Description)
	}
	if skill.Version != "1.0.0" {
		t.Fatalf("Version = %q", skill.Version)
	}
	if len(skill.Tags) != 2 || skill.Tags[0] != "k8s" || skill.Tags[1] != "ops" {
		t.Fatalf("Tags = %#v", skill.Tags)
	}
	if skill.MaxChars != 1200 {
		t.Fatalf("MaxChars = %d", skill.MaxChars)
	}
	if !strings.Contains(skill.Body, "Inspect pods") {
		t.Fatalf("Body missing markdown: %q", skill.Body)
	}
}

func TestParseSkillMarkdownRequiresNameAndDescription(t *testing.T) {
	_, err := ParseSkillMarkdown("SKILL.md", []byte("---\nname: \n---\nbody"), 6000)
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Fatalf("err = %v, want description validation", err)
	}
}

func TestParseSkillMarkdownRejectsOversizedFile(t *testing.T) {
	raw := []byte("---\nname: x\ndescription: y\n---\n" + strings.Repeat("a", 20))
	_, err := ParseSkillMarkdown("SKILL.md", raw, 10)
	if err == nil {
		t.Fatal("err = nil, want size error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want too large", err)
	}
}
