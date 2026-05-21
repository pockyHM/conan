package main

import "testing"

func TestModelPresetLookup(t *testing.T) {
	preset, ok := modelPresetByID("qwen")
	if !ok {
		t.Fatal("qwen preset not found")
	}
	if preset.Type != "openai" {
		t.Fatalf("qwen type = %q, want openai", preset.Type)
	}
	if preset.Endpoint != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("qwen endpoint = %q", preset.Endpoint)
	}
	if !preset.SupportsList {
		t.Fatal("qwen should support listing models")
	}

	if _, ok := modelPresetByID("nonexistent"); ok {
		t.Fatal("nonexistent preset should not be found")
	}
}
