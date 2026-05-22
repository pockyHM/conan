package configschema

import (
	"os"
	"regexp"
	"strings"
)

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func ExpandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		return os.Getenv(key)
	})
}

// --- Agent-side config ---

type AgentConfig struct {
	Listen        string    `yaml:"listen"`
	Token         string    `yaml:"token"`
	TLS           bool      `yaml:"tls"`
	TLSCert       string    `yaml:"tls_cert,omitempty"`
	TLSKey        string    `yaml:"tls_key,omitempty"`
	AuditLog      string    `yaml:"audit_log"`
	RateLimit     int       `yaml:"rate_limit"`
	DisabledTools []string  `yaml:"disabled_tools"`
	LogLevel      string    `yaml:"log_level"`
	Web           WebConfig `yaml:"web"`
}

func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		Listen:    "0.0.0.0:9280",
		Token:     "changeme",
		RateLimit: 10,
		LogLevel:  "info",
	}
}

type WebConfig struct {
	SearchProvider      string `yaml:"search_provider"`
	SearchAPIKey        string `yaml:"search_api_key,omitempty"`
	SearchAPIKeyEnv     string `yaml:"search_api_key_env,omitempty"`
	SearchEndpoint      string `yaml:"search_endpoint,omitempty"`
	FetchMaxBytes       int64  `yaml:"fetch_max_bytes,omitempty"`
	FetchMaxChars       int    `yaml:"fetch_max_chars,omitempty"`
	AllowPrivateNetwork bool   `yaml:"allow_private_network,omitempty"`
}

// --- CLI-side config ---

type GlobalConfig struct {
	DefaultModel   string            `yaml:"default_model"`
	DefaultCluster string            `yaml:"default_cluster"`
	Models         []ModelConfig     `yaml:"models"`
	Security       SecurityConfig    `yaml:"security"`
	Memory         MemoryConfig      `yaml:"memory"`
	Logging        LoggingConfig     `yaml:"logging"`
	AgentDeploy    AgentDeployConfig `yaml:"agent_deploy"`
	Subagents      SubagentConfig    `yaml:"subagents"`
}

type ModelConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // "anthropic" or "openai"
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"`
	Thinking *bool  `yaml:"thinking,omitempty"`
}

type SecurityConfig struct {
	RiskAssessmentModel string   `yaml:"risk_assessment_model"`
	CommandBlacklist    []string `yaml:"command_blacklist"`
	LocalFileWhitelist  []string `yaml:"local_file_whitelist"`
}

type MemoryConfig struct {
	RulesTokenBudget     int `yaml:"rules_token_budget"`
	KnowledgeTokenBudget int `yaml:"knowledge_token_budget"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
	Audit bool   `yaml:"audit"`
}

type AgentDeployConfig struct {
	Binaries         AgentBinaryConfig `yaml:"binaries"`
	RemoteBinaryPath string            `yaml:"remote_binary_path"`
	RemoteConfigPath string            `yaml:"remote_config_path"`
	SystemdUnitPath  string            `yaml:"systemd_unit_path"`
}

type AgentBinaryConfig struct {
	AMD64 string `yaml:"amd64"`
	ARM64 string `yaml:"arm64"`
}

type SubagentConfig struct {
	Enabled        bool   `yaml:"enabled"`
	MaxParallel    int    `yaml:"max_parallel"`
	DefaultModel   string `yaml:"default_model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	Debug          bool   `yaml:"debug"`
}

// --- Cluster config ---

type ClusterConfig struct {
	Name         string       `yaml:"name"`
	Description  string       `yaml:"description"`
	Inherits     string       `yaml:"inherits"`
	Agent        AgentConfig  `yaml:"agent"`
	NodeDefaults NodeDefaults `yaml:"node_defaults"`
}

type NodeDefaults struct {
	User    string `yaml:"user"`
	SSHPort int    `yaml:"ssh_port"`
}

type NodeList struct {
	Nodes []NodeConfig `yaml:"nodes"`
}

type NodeConfig struct {
	Name             string             `yaml:"name"`
	Host             string             `yaml:"host"`
	Labels           []string           `yaml:"labels"`
	Zone             string             `yaml:"zone,omitempty"`
	CommandWhitelist []string           `yaml:"command_whitelist,omitempty"`
	Agent            *NodeAgentOverride `yaml:"agent,omitempty"`
}

type NodeAgentOverride struct {
	User  string `yaml:"user"`
	Port  int    `yaml:"port"`
	Token string `yaml:"token"`
}
