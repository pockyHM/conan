package tools

import "testing"

func TestMetadataForKnownTools(t *testing.T) {
	tests := []struct {
		name       string
		safety     Safety
		capability string
	}{
		{name: "svc/status", safety: SafetyReadOnly, capability: "service"},
		{name: "node_add", safety: SafetyMutating, capability: "node"},
		{name: "exec", safety: SafetyDestructive, capability: "shell"},
	}

	for _, tt := range tests {
		meta, ok := MetadataFor(tt.name)
		if !ok {
			t.Fatalf("MetadataFor(%q) missing", tt.name)
		}
		if meta.Safety != tt.safety {
			t.Fatalf("MetadataFor(%q).Safety = %q, want %q", tt.name, meta.Safety, tt.safety)
		}
		if !containsString(meta.Capability, tt.capability) {
			t.Fatalf("MetadataFor(%q).Capability = %#v, want %q", tt.name, meta.Capability, tt.capability)
		}
	}
}

func TestDefaultMetadataCoversBuiltInAndMetaTools(t *testing.T) {
	for _, name := range []string{
		"shell/run",
		"fs/read", "fs/list", "fs/stat",
		"sys/cpu", "sys/mem", "sys/disk", "sys/net", "sys/processes",
		"svc/list", "svc/status",
		"log/read", "log/journalctl",
		"net/ping", "net/traceroute", "net/portcheck", "net/curl",
		"web/search", "web/fetch",
		"docker/ps", "docker/images", "docker/logs",
		"k8s/pods", "k8s/logs", "k8s/events", "k8s/describe",
		"pkg/list", "pkg/search",
		"cron/list", "cron/show",
		"tool_search", "call_tool", "subagents_run",
		"file_put", "file_get", "node_add", "image_analyze",
		"memory_search", "memory_read", "memory_patch", "memory_write_note", "memory_promote",
		"local/fs/read", "local/fs/list", "local/fs/stat", "local/fs/write", "local/fs/patch", "local/fs/delete",
	} {
		meta, ok := MetadataFor(name)
		if !ok {
			t.Fatalf("metadata missing for %s", name)
		}
		if meta.Name != name {
			t.Fatalf("%s metadata name = %q", name, meta.Name)
		}
		if meta.Safety == "" {
			t.Fatalf("%s metadata missing safety", name)
		}
		if meta.Scope == "" {
			t.Fatalf("%s metadata missing scope", name)
		}
		if len(meta.Capability) == 0 {
			t.Fatalf("%s metadata missing capability", name)
		}
	}
}

func TestIsReadOnlyUsesMetadata(t *testing.T) {
	if !IsReadOnly("log/read") {
		t.Fatal("log/read should be read-only")
	}
	if IsReadOnly("file_put") {
		t.Fatal("file_put should not be read-only")
	}
	if IsReadOnly("missing/tool") {
		t.Fatal("missing tool should not be read-only")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
