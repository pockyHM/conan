package skills

import "time"

const (
	ScopeGlobal  = "global"
	ScopeCluster = "cluster"
	ToolName     = "skill_read"
)

type Skill struct {
	Name        string
	Description string
	Version     string
	Tags        []string
	MaxChars    int
	Body        string
	Path        string
	Scope       string
	Cluster     string
	Source      string
	Ref         string
	CachePath   string
	InstalledAt time.Time
}

type Registry struct {
	Skills []RegistryEntry `yaml:"skills"`
}

type RegistryEntry struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description,omitempty"`
	Version     string    `yaml:"version,omitempty"`
	Tags        []string  `yaml:"tags,omitempty"`
	MaxChars    int       `yaml:"max_chars,omitempty"`
	Source      string    `yaml:"source"`
	Ref         string    `yaml:"ref"`
	Path        string    `yaml:"path"`
	CachePath   string    `yaml:"cache_path"`
	InstalledAt time.Time `yaml:"installed_at"`
}
