package tools

import "testing"

func TestMetadataForKnownTools(t *testing.T) {
	tests := []struct {
		name       string
		safety     Safety
		capability string
	}{
		{name: "svc_status", safety: SafetyReadOnly, capability: "service"},
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
		"shell_run",
		"fs_read", "fs_list", "fs_stat",
		"sys_cpu", "sys_mem", "sys_disk", "sys_net", "sys_processes",
		"svc_list", "svc_status",
		"log_read", "log_journalctl",
		"net_ping", "net_traceroute", "net_portcheck", "net_curl",
		"web_search", "web_fetch",
		"docker_ps", "docker_images", "docker_logs",
		"k8s_pods", "k8s_logs", "k8s_events", "k8s_describe",
		"pkg_list", "pkg_search",
		"cron_list", "cron_show",
		"tool_search", "call_tool", "subagents_run",
		"file_put", "file_get", "node_add", "image_analyze",
		"memory_search", "memory_read", "memory_patch", "memory_write_note", "memory_promote",
		"local_fs_read", "local_fs_list", "local_fs_stat", "local_fs_write", "local_fs_patch", "local_fs_delete",
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
	if !IsReadOnly("log_read") {
		t.Fatal("log_read should be read-only")
	}
	if IsReadOnly("file_put") {
		t.Fatal("file_put should not be read-only")
	}
	if IsReadOnly("missing_tool") {
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
