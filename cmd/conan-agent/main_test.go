package main

import (
	"testing"

	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
)

func TestRegisterAllToolsOnlyExposesShellAndReadOnlyTools(t *testing.T) {
	registry := tools.NewRegistry()
	registerAllTools(registry, configschema.DefaultAgentConfig())

	allowed := map[string]bool{
		"shell_run":      true,
		"fs_read":        true,
		"fs_list":        true,
		"fs_stat":        true,
		"sys_cpu":        true,
		"sys_mem":        true,
		"sys_disk":       true,
		"sys_net":        true,
		"sys_processes":  true,
		"svc_list":       true,
		"svc_status":     true,
		"log_read":       true,
		"log_stream":     true,
		"log_journalctl": true,
		"net_ping":       true,
		"net_traceroute": true,
		"net_portcheck":  true,
		"web_fetch":      true,
		"web_report":     true,
		"k8s_pods":       true,
		"k8s_logs":       true,
		"k8s_events":     true,
		"k8s_describe":   true,
		"pkg_list":       true,
		"pkg_search":     true,
		"cron_list":      true,
		"cron_show":      true,
		"docker_ps":      true,
		"docker_images":  true,
		"docker_logs":    true,
		"agent_update":   true,
	}

	for _, tool := range registry.List() {
		name := tool.Name()
		if !allowed[name] {
			t.Fatalf("registered unexpected mutating tool %q", name)
		}
		delete(allowed, name)
	}

	for name := range allowed {
		t.Fatalf("expected tool %q to be registered", name)
	}

	for _, name := range []string{
		"fs_write",
		"fs_edit",
		"fs_download",
		"fs_upload",
		"svc_start",
		"svc_stop",
		"svc_restart",
		"k8s_apply",
		"k8s_delete",
		"pkg_install",
		"pkg_update",
		"cron_add",
		"cron_remove",
		"net_curl",
		"web_search",
		"docker_exec",
		"docker_run",
		"docker_compose",
	} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("mutating tool %q should not be registered", name)
		}
	}
}

func TestRegisterAllToolsCanDisableAgentUpdate(t *testing.T) {
	registry := tools.NewRegistry()
	registerAllTools(registry, &configschema.AgentConfig{DisabledTools: []string{"agent_update"}})
	registry.DisableAll([]string{"agent_update"})

	if _, ok := registry.Get("agent_update"); ok {
		t.Fatal("agent_update should be disabled")
	}
}

func TestRegisterAllToolsExposesWebSearchWhenConfigured(t *testing.T) {
	registry := tools.NewRegistry()
	registerAllTools(registry, &configschema.AgentConfig{
		Web: configschema.WebConfig{
			SearchProvider: "brave",
			SearchAPIKey:   "test-key",
		},
	})

	if _, ok := registry.Get("web_search"); !ok {
		t.Fatal("web_search should be registered when search provider and API key are configured")
	}
	if _, ok := registry.Get("web_fetch"); !ok {
		t.Fatal("web_fetch should be registered")
	}
}
