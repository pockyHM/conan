package memory

import "testing"

func TestMemoryDestinationForCategories(t *testing.T) {
	tests := []struct {
		category string
		cluster  string
		wantKind string
		wantPath string
	}{
		{"profile", "prod", "markdown", "profile.md"},
		{"rule", "prod", "markdown", "rules/ops.md"},
		{"topology", "prod", "markdown", "clusters/prod.md"},
		{"runbook", "prod", "markdown-note", "runbooks"},
		{"incident", "prod", "markdown-note", "incidents"},
		{"event", "prod", "sqlite", ""},
		{"discard", "prod", "discard", ""},
	}
	for _, tt := range tests {
		got := DestinationFor(MemoryCandidate{Category: tt.category}, tt.cluster)
		if got.Kind != tt.wantKind || got.Path != tt.wantPath {
			t.Fatalf("DestinationFor(%q) = %#v, want kind=%q path=%q", tt.category, got, tt.wantKind, tt.wantPath)
		}
	}
}

func TestExplicitRememberClassifiesAsProfileForName(t *testing.T) {
	got, ok := CandidateFromExplicitRemember("记住我叫小王", "prod")
	if !ok {
		t.Fatal("expected candidate")
	}
	if got.Category != "profile" {
		t.Fatalf("category = %q, want profile", got.Category)
	}
	if got.Content != "我叫小王" {
		t.Fatalf("content = %q, want 我叫小王", got.Content)
	}
}

func TestExplicitRememberClassifiesAsRuleForNorms(t *testing.T) {
	got, ok := CandidateFromExplicitRemember("以后记得代码必须 gofmt", "prod")
	if !ok {
		t.Fatal("expected candidate")
	}
	if got.Category != "rule" {
		t.Fatalf("category = %q, want rule", got.Category)
	}
}

func TestMemoryDestinationForUnicodeTopologyCluster(t *testing.T) {
	got := DestinationFor(MemoryCandidate{Category: "topology"}, "生产集群")
	if got.Kind != "markdown" {
		t.Fatalf("kind = %q, want markdown", got.Kind)
	}
	if got.Path == "" {
		t.Fatal("path is empty")
	}
	if got.Path == "clusters/.md" {
		t.Fatal("path must not be clusters/.md")
	}
	if len(got.Path) < len("clusters/.md") || got.Path[:len("clusters/")] != "clusters/" || got.Path[len(got.Path)-len(".md"):] != ".md" {
		t.Fatalf("path = %q, want clusters/*.md", got.Path)
	}
}

func TestExplicitRememberFuturePrefixClassifiesAsRule(t *testing.T) {
	got, ok := CandidateFromExplicitRemember("以后记得用中文回答", "prod")
	if !ok {
		t.Fatal("expected candidate")
	}
	if got.Category != "rule" {
		t.Fatalf("category = %q, want rule", got.Category)
	}
	if got.Content != "用中文回答" {
		t.Fatalf("content = %q, want 用中文回答", got.Content)
	}
}
