package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pockyHM/conan/pkg/configschema"
	"gopkg.in/yaml.v3"
)

type Loader struct {
	home string
}

type Cluster struct {
	Cluster configschema.ClusterConfig
	Nodes   []Node
}

type Node struct {
	configschema.NodeConfig
	Agent EffectiveAgentConfig
}

type EffectiveAgentConfig struct {
	Host  string
	Port  int
	User  string
	TLS   bool
	Token string
}

func NewLoader(home string) *Loader {
	if home == "" {
		home = DefaultHome()
	}
	return &Loader{home: home}
}

func DefaultHome() string {
	if home := os.Getenv("CONAN_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".conan"
	}
	return filepath.Join(userHome, ".conan")
}

func (l *Loader) Home() string {
	return l.home
}

func (l *Loader) LoadGlobal() (*configschema.GlobalConfig, error) {
	cfg := &configschema.GlobalConfig{}
	applyGlobalDefaults(cfg)

	path := filepath.Join(l.home, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	applyGlobalDefaults(cfg)
	for i := range cfg.Models {
		cfg.Models[i].APIKey = configschema.ExpandEnv(cfg.Models[i].APIKey)
		cfg.Models[i].Endpoint = strings.TrimRight(cfg.Models[i].Endpoint, "/")
	}
	return cfg, nil
}

func (l *Loader) LoadCluster(name string) (*Cluster, error) {
	if name == "" {
		global, err := l.LoadGlobal()
		if err != nil {
			return nil, err
		}
		name = global.DefaultCluster
	}
	if name == "" {
		return nil, fmt.Errorf("cluster name is required")
	}

	cluster := defaultClusterConfig()
	basePath := filepath.Join(l.home, "clusters", "base.yaml")
	if err := readYAMLIfExists(basePath, cluster); err != nil {
		return nil, err
	}
	mergeClusterDefaults(cluster)

	clusterPath := filepath.Join(l.home, "clusters", name, "cluster.yaml")
	var override configschema.ClusterConfig
	overrideFields, err := readYAMLFieldsIfExists(clusterPath, &override)
	if err != nil {
		return nil, err
	}
	mergeCluster(cluster, override, overrideFields)
	if cluster.Name == "" {
		cluster.Name = name
	}
	mergeClusterDefaults(cluster)

	var nodeList configschema.NodeList
	nodesPath := filepath.Join(l.home, "clusters", name, "nodes.yaml")
	if err := readYAMLIfExists(nodesPath, &nodeList); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(nodeList.Nodes))
	for _, node := range nodeList.Nodes {
		nodes = append(nodes, Node{NodeConfig: node, Agent: effectiveAgent(node, *cluster)})
	}

	return &Cluster{Cluster: *cluster, Nodes: nodes}, nil
}

func (l *Loader) ListClusters() ([]string, error) {
	root := filepath.Join(l.home, "clusters")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	clusters := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "cluster.yaml")); err == nil {
			clusters = append(clusters, entry.Name())
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Strings(clusters)
	return clusters, nil
}

func applyGlobalDefaults(cfg *configschema.GlobalConfig) {
	if cfg.Security.RiskAssessmentModel == "" {
		cfg.Security.RiskAssessmentModel = "claude-sonnet"
	}
	if len(cfg.Security.CommandWhitelist) == 0 {
		cfg.Security.CommandWhitelist = []string{"cat", "ls", "free", "df", "ps aux", "uname -a", "hostname", "uptime", "kubectl get"}
	}
	if cfg.Memory.RulesTokenBudget == 0 {
		cfg.Memory.RulesTokenBudget = 2000
	}
	if cfg.Memory.KnowledgeTokenBudget == 0 {
		cfg.Memory.KnowledgeTokenBudget = 4000
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
}

func defaultClusterConfig() *configschema.ClusterConfig {
	return &configschema.ClusterConfig{
		Agent: configschema.AgentConfig{
			Listen:    "0.0.0.0:9200",
			RateLimit: 10,
			LogLevel:  "info",
		},
		NodeDefaults: configschema.NodeDefaults{User: "root", SSHPort: 22},
	}
}

func mergeClusterDefaults(cfg *configschema.ClusterConfig) {
	defaults := defaultClusterConfig()
	if cfg.Agent.Listen == "" {
		cfg.Agent.Listen = defaults.Agent.Listen
	}
	if cfg.Agent.RateLimit == 0 {
		cfg.Agent.RateLimit = defaults.Agent.RateLimit
	}
	if cfg.Agent.LogLevel == "" {
		cfg.Agent.LogLevel = defaults.Agent.LogLevel
	}
	if cfg.NodeDefaults.User == "" {
		cfg.NodeDefaults.User = defaults.NodeDefaults.User
	}
	if cfg.NodeDefaults.SSHPort == 0 {
		cfg.NodeDefaults.SSHPort = defaults.NodeDefaults.SSHPort
	}
}

func mergeCluster(base *configschema.ClusterConfig, override configschema.ClusterConfig, fields map[string]map[string]bool) {
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.Inherits != "" {
		base.Inherits = override.Inherits
	}
	mergeAgent(&base.Agent, override.Agent, fields["agent"])
	if override.NodeDefaults.User != "" {
		base.NodeDefaults.User = override.NodeDefaults.User
	}
	if override.NodeDefaults.SSHPort != 0 {
		base.NodeDefaults.SSHPort = override.NodeDefaults.SSHPort
	}
}

func mergeAgent(base *configschema.AgentConfig, override configschema.AgentConfig, fields map[string]bool) {
	if override.Listen != "" {
		base.Listen = override.Listen
	}
	if override.Token != "" {
		base.Token = override.Token
	}
	if fields["tls"] {
		base.TLS = override.TLS
	}
	if override.TLSCert != "" {
		base.TLSCert = override.TLSCert
	}
	if override.TLSKey != "" {
		base.TLSKey = override.TLSKey
	}
	if override.AuditLog != "" {
		base.AuditLog = override.AuditLog
	}
	if override.RateLimit != 0 {
		base.RateLimit = override.RateLimit
	}
	if override.DisabledTools != nil {
		base.DisabledTools = override.DisabledTools
	}
	if override.LogLevel != "" {
		base.LogLevel = override.LogLevel
	}
}

func effectiveAgent(node configschema.NodeConfig, cluster configschema.ClusterConfig) EffectiveAgentConfig {
	port := portFromListen(cluster.Agent.Listen)
	user := cluster.NodeDefaults.User
	if node.Agent != nil {
		if node.Agent.Port != 0 {
			port = node.Agent.Port
		}
		if node.Agent.User != "" {
			user = node.Agent.User
		}
	}
	return EffectiveAgentConfig{
		Host:  node.Host,
		Port:  port,
		User:  user,
		TLS:   cluster.Agent.TLS,
		Token: configschema.ExpandEnv(cluster.Agent.Token),
	}
}

func portFromListen(listen string) int {
	idx := strings.LastIndex(listen, ":")
	if idx == -1 || idx == len(listen)-1 {
		return 9200
	}
	port, err := strconv.Atoi(listen[idx+1:])
	if err != nil || port == 0 {
		return 9200
	}
	return port
}

func readYAMLIfExists(path string, out interface{}) error {
	_, err := readYAMLFieldsIfExists(path, out)
	return err
}

func readYAMLFieldsIfExists(path string, out interface{}) (map[string]map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	fields := map[string]map[string]bool{}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		return fields, nil
	}
	collectFields(root.Content[0], fields)
	return fields, nil
}

func collectFields(node *yaml.Node, fields map[string]map[string]bool) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		if fields[key] == nil {
			fields[key] = map[string]bool{}
		}
		if value.Kind != yaml.MappingNode {
			fields[key][""] = true
			continue
		}
		for j := 0; j+1 < len(value.Content); j += 2 {
			fields[key][value.Content[j].Value] = true
		}
	}
}
