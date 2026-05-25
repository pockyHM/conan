package tools

type Safety string

const (
	SafetyReadOnly    Safety = "read-only"
	SafetyMutating    Safety = "mutating"
	SafetyDestructive Safety = "destructive"
)

type Scope string

const (
	ScopeLocal   Scope = "local"
	ScopeNode    Scope = "node"
	ScopeCluster Scope = "cluster"
)

type Metadata struct {
	Name        string
	Capability  []string
	Safety      Safety
	Scope       Scope
	Privileges  []string
	OutputShape string
	Tags        []string
}

func DefaultMetadata() map[string]Metadata {
	entries := []Metadata{
		meta("shell/run", SafetyDestructive, ScopeNode, []string{"shell"}, []string{"command", "exec"}),
		meta("exec", SafetyDestructive, ScopeNode, []string{"shell"}, []string{"command", "fallback"}),
		meta("fs/read", SafetyReadOnly, ScopeNode, []string{"filesystem"}, []string{"file", "read"}),
		meta("fs/list", SafetyReadOnly, ScopeNode, []string{"filesystem"}, []string{"file", "list"}),
		meta("fs/stat", SafetyReadOnly, ScopeNode, []string{"filesystem"}, []string{"file", "stat"}),
		meta("sys/cpu", SafetyReadOnly, ScopeNode, []string{"system"}, []string{"cpu"}),
		meta("sys/mem", SafetyReadOnly, ScopeNode, []string{"system"}, []string{"memory"}),
		meta("sys/disk", SafetyReadOnly, ScopeNode, []string{"system"}, []string{"disk"}),
		meta("sys/net", SafetyReadOnly, ScopeNode, []string{"system", "network"}, []string{"network"}),
		meta("sys/processes", SafetyReadOnly, ScopeNode, []string{"system", "process"}, []string{"process"}),
		meta("svc/list", SafetyReadOnly, ScopeNode, []string{"service"}, []string{"systemd", "list"}),
		meta("svc/status", SafetyReadOnly, ScopeNode, []string{"service"}, []string{"systemd", "status"}),
		meta("log/read", SafetyReadOnly, ScopeNode, []string{"logs"}, []string{"log", "file"}),
		meta("log/journalctl", SafetyReadOnly, ScopeNode, []string{"logs", "service"}, []string{"journalctl", "systemd"}),
		meta("net/ping", SafetyReadOnly, ScopeNode, []string{"network"}, []string{"ping"}),
		meta("net/traceroute", SafetyReadOnly, ScopeNode, []string{"network"}, []string{"traceroute"}),
		meta("net/portcheck", SafetyReadOnly, ScopeNode, []string{"network"}, []string{"port"}),
		meta("net/curl", SafetyReadOnly, ScopeNode, []string{"network", "web"}, []string{"http", "curl"}),
		meta("web/search", SafetyReadOnly, ScopeNode, []string{"web"}, []string{"search"}),
		meta("web/fetch", SafetyReadOnly, ScopeNode, []string{"web"}, []string{"fetch"}),
		meta("docker/ps", SafetyReadOnly, ScopeNode, []string{"container"}, []string{"docker", "ps"}),
		meta("docker/images", SafetyReadOnly, ScopeNode, []string{"container"}, []string{"docker", "images"}),
		meta("docker/logs", SafetyReadOnly, ScopeNode, []string{"container", "logs"}, []string{"docker", "logs"}),
		meta("k8s/pods", SafetyReadOnly, ScopeCluster, []string{"kubernetes"}, []string{"pods"}),
		meta("k8s/logs", SafetyReadOnly, ScopeCluster, []string{"kubernetes", "logs"}, []string{"pods", "logs"}),
		meta("k8s/events", SafetyReadOnly, ScopeCluster, []string{"kubernetes", "events"}, []string{"events"}),
		meta("k8s/describe", SafetyReadOnly, ScopeCluster, []string{"kubernetes"}, []string{"describe"}),
		meta("pkg/list", SafetyReadOnly, ScopeNode, []string{"package"}, []string{"packages", "list"}),
		meta("pkg/search", SafetyReadOnly, ScopeNode, []string{"package"}, []string{"packages", "search"}),
		meta("cron/list", SafetyReadOnly, ScopeNode, []string{"cron"}, []string{"crontab", "list"}),
		meta("cron/show", SafetyReadOnly, ScopeNode, []string{"cron"}, []string{"crontab", "show"}),
		meta("tool_search", SafetyReadOnly, ScopeLocal, []string{"tool"}, []string{"search", "metadata"}),
		meta("call_tool", SafetyMutating, ScopeNode, []string{"tool"}, []string{"delegate"}),
		meta("subagents_run", SafetyReadOnly, ScopeLocal, []string{"subagent"}, []string{"investigate", "review"}),
		meta("file_put", SafetyMutating, ScopeNode, []string{"filesystem", "transfer"}, []string{"upload", "copy"}),
		meta("file_get", SafetyMutating, ScopeLocal, []string{"filesystem", "transfer"}, []string{"download", "copy"}),
		meta("node_add", SafetyMutating, ScopeCluster, []string{"node", "deploy"}, []string{"agent", "ssh"}),
		meta("image_analyze", SafetyReadOnly, ScopeLocal, []string{"vision"}, []string{"image"}),
		meta("memory_search", SafetyReadOnly, ScopeLocal, []string{"memory"}, []string{"search"}),
		meta("memory_read", SafetyReadOnly, ScopeLocal, []string{"memory"}, []string{"read"}),
		meta("memory_patch", SafetyMutating, ScopeLocal, []string{"memory"}, []string{"patch"}),
		meta("memory_write_note", SafetyMutating, ScopeLocal, []string{"memory"}, []string{"write", "note"}),
		meta("memory_promote", SafetyMutating, ScopeLocal, []string{"memory"}, []string{"promote"}),
		meta("local/fs/read", SafetyReadOnly, ScopeLocal, []string{"filesystem"}, []string{"local", "read"}),
		meta("local/fs/list", SafetyReadOnly, ScopeLocal, []string{"filesystem"}, []string{"local", "list"}),
		meta("local/fs/stat", SafetyReadOnly, ScopeLocal, []string{"filesystem"}, []string{"local", "stat"}),
		meta("local/fs/write", SafetyMutating, ScopeLocal, []string{"filesystem"}, []string{"local", "write"}),
		meta("local/fs/patch", SafetyMutating, ScopeLocal, []string{"filesystem"}, []string{"local", "patch"}),
		meta("local/fs/delete", SafetyDestructive, ScopeLocal, []string{"filesystem"}, []string{"local", "delete"}),
	}
	result := make(map[string]Metadata, len(entries))
	for _, entry := range entries {
		result[entry.Name] = entry
	}
	return result
}

func MetadataFor(name string) (Metadata, bool) {
	meta, ok := DefaultMetadata()[name]
	return meta, ok
}

func IsReadOnly(name string) bool {
	meta, ok := MetadataFor(name)
	return ok && meta.Safety == SafetyReadOnly
}

func meta(name string, safety Safety, scope Scope, capabilities []string, tags []string) Metadata {
	return Metadata{
		Name:        name,
		Capability:  capabilities,
		Safety:      safety,
		Scope:       scope,
		Privileges:  []string{"user"},
		OutputShape: "text",
		Tags:        tags,
	}
}
