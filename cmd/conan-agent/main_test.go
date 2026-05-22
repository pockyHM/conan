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
		"shell/run":      true,
		"fs/read":        true,
		"fs/list":        true,
		"fs/stat":        true,
		"sys/cpu":        true,
		"sys/mem":        true,
		"sys/disk":       true,
		"sys/net":        true,
		"sys/processes":  true,
		"svc/list":       true,
		"svc/status":     true,
		"log/read":       true,
		"log/stream":     true,
		"log/journalctl": true,
		"net/ping":       true,
		"net/traceroute": true,
		"net/portcheck":  true,
		"web/fetch":      true,
		"k8s/pods":       true,
		"k8s/logs":       true,
		"k8s/events":     true,
		"k8s/describe":   true,
		"pkg/list":       true,
		"pkg/search":     true,
		"cron/list":      true,
		"cron/show":      true,
		"docker/ps":      true,
		"docker/images":  true,
		"docker/logs":    true,
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
		"fs/write",
		"fs/edit",
		"fs/download",
		"fs/upload",
		"svc/start",
		"svc/stop",
		"svc/restart",
		"k8s/apply",
		"k8s/delete",
		"pkg/install",
		"pkg/update",
		"cron/add",
		"cron/remove",
		"net/curl",
		"web/search",
		"docker/exec",
		"docker/run",
		"docker/compose",
	} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("mutating tool %q should not be registered", name)
		}
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

	if _, ok := registry.Get("web/search"); !ok {
		t.Fatal("web/search should be registered when search provider and API key are configured")
	}
	if _, ok := registry.Get("web/fetch"); !ok {
		t.Fatal("web/fetch should be registered")
	}
}
