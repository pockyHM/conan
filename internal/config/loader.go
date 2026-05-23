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

func (l *Loader) ConfigPath() string {
	return filepath.Join(l.home, "config.yaml")
}

func (l *Loader) SaveGlobal(cfg *configschema.GlobalConfig) error {
	if cfg == nil {
		return fmt.Errorf("global config is nil")
	}
	toSave := *cfg
	toSave.Models = append([]configschema.ModelConfig(nil), cfg.Models...)
	normalizeModelsForSave(toSave.Models)
	preserveExistingModelAPIKeys(l.ConfigPath(), toSave.Models)

	path := l.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&toSave)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (l *Loader) LoadGlobal() (*configschema.GlobalConfig, error) {
	cfg := &configschema.GlobalConfig{}
	applyGlobalDefaults(cfg)
	applyAgentDeployDefaults(cfg, l.home)

	path := l.ConfigPath()
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
	skillsEnabledSet, err := yamlPathExists(data, "skills", "enabled")
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	skillsExplicitlyDisabled := skillsEnabledSet && !cfg.Skills.Enabled
	applyGlobalDefaults(cfg)
	if skillsExplicitlyDisabled {
		cfg.Skills.Enabled = false
	}
	applyAgentDeployDefaults(cfg, l.home)
	normalizeModelsForLoad(cfg.Models)
	return cfg, nil
}

func normalizeModelsForSave(models []configschema.ModelConfig) {
	for i := range models {
		models[i].Endpoint = strings.TrimRight(models[i].Endpoint, "/")
	}
}

func yamlPathExists(data []byte, path ...string) (bool, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false, err
	}
	if len(root.Content) == 0 {
		return false, nil
	}
	node := root.Content[0]
	for _, want := range path {
		if node.Kind != yaml.MappingNode {
			return false, nil
		}
		found := false
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value != want {
				continue
			}
			node = node.Content[i+1]
			found = true
			break
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func preserveExistingModelAPIKeys(path string, models []configschema.ModelConfig) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var existing configschema.GlobalConfig
	if err := yaml.Unmarshal(data, &existing); err != nil {
		return
	}
	apiKeys := make(map[string]string, len(existing.Models))
	for _, model := range existing.Models {
		apiKeys[model.Name] = model.APIKey
	}
	for i := range models {
		if apiKey, ok := apiKeys[models[i].Name]; ok && strings.Contains(apiKey, "${") {
			models[i].APIKey = apiKey
		}
	}
}

func normalizeModelsForLoad(models []configschema.ModelConfig) {
	for i := range models {
		models[i].APIKey = configschema.ExpandEnv(models[i].APIKey)
		models[i].Endpoint = strings.TrimRight(models[i].Endpoint, "/")
	}
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
		name = "default"
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
	if cfg.UILanguage == "" {
		cfg.UILanguage = "en-US"
	}
	if cfg.Security.RiskAssessmentModel == "" {
		cfg.Security.RiskAssessmentModel = "claude-sonnet"
	}
	if len(cfg.Security.CommandBlacklist) == 0 {
		cfg.Security.CommandBlacklist = []string{`.*\|\s*bash.*`}
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
	if cfg.Subagents.MaxParallel == 0 {
		cfg.Subagents.MaxParallel = 3
	}
	if cfg.Subagents.TimeoutSeconds == 0 {
		cfg.Subagents.TimeoutSeconds = 120
	}
	if cfg.Vision.MaxImages == 0 {
		cfg.Vision.MaxImages = 10
	}
	if cfg.Vision.MaxSummaryCharsPerImage == 0 {
		cfg.Vision.MaxSummaryCharsPerImage = 1200
	}
	if !cfg.Skills.Enabled {
		cfg.Skills.Enabled = true
	}
	if cfg.Skills.IndexTokenBudget == 0 {
		cfg.Skills.IndexTokenBudget = 800
	}
	if cfg.Skills.MaxSkillChars == 0 {
		cfg.Skills.MaxSkillChars = 6000
	}
	if cfg.Skills.MaxVisibleSkills == 0 {
		cfg.Skills.MaxVisibleSkills = 50
	}
}

func applyAgentDeployDefaults(cfg *configschema.GlobalConfig, home string) {
	if cfg.AgentDeploy.Binaries.AMD64 == "" {
		cfg.AgentDeploy.Binaries.AMD64 = filepath.Join(home, "agent", "amd64", "conan-agent")
	}
	if cfg.AgentDeploy.Binaries.ARM64 == "" {
		cfg.AgentDeploy.Binaries.ARM64 = filepath.Join(home, "agent", "arm64", "conan-agent")
	}
	if cfg.AgentDeploy.RemoteBinaryPath == "" {
		cfg.AgentDeploy.RemoteBinaryPath = "/usr/local/bin/conan-agent"
	}
	if cfg.AgentDeploy.RemoteConfigPath == "" {
		cfg.AgentDeploy.RemoteConfigPath = "/etc/conan-agent/config.yaml"
	}
	if cfg.AgentDeploy.SystemdUnitPath == "" {
		cfg.AgentDeploy.SystemdUnitPath = "/etc/systemd/system/conan-agent.service"
	}
	cfg.AgentDeploy.Binaries.AMD64 = expandPath(cfg.AgentDeploy.Binaries.AMD64)
	cfg.AgentDeploy.Binaries.ARM64 = expandPath(cfg.AgentDeploy.Binaries.ARM64)
}

func expandPath(path string) string {
	path = configschema.ExpandEnv(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func defaultClusterConfig() *configschema.ClusterConfig {
	return &configschema.ClusterConfig{
		Agent: configschema.AgentConfig{
			Listen:    "0.0.0.0:9280",
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
	mergeWeb(&base.Web, override.Web)
}

func mergeWeb(base *configschema.WebConfig, override configschema.WebConfig) {
	if override.SearchProvider != "" {
		base.SearchProvider = override.SearchProvider
	}
	if override.SearchAPIKey != "" {
		base.SearchAPIKey = override.SearchAPIKey
	}
	if override.SearchAPIKeyEnv != "" {
		base.SearchAPIKeyEnv = override.SearchAPIKeyEnv
	}
	if override.SearchEndpoint != "" {
		base.SearchEndpoint = override.SearchEndpoint
	}
	if override.FetchMaxBytes != 0 {
		base.FetchMaxBytes = override.FetchMaxBytes
	}
	if override.FetchMaxChars != 0 {
		base.FetchMaxChars = override.FetchMaxChars
	}
	if override.AllowPrivateNetwork {
		base.AllowPrivateNetwork = override.AllowPrivateNetwork
	}
}

func effectiveAgent(node configschema.NodeConfig, cluster configschema.ClusterConfig) EffectiveAgentConfig {
	port := portFromListen(cluster.Agent.Listen)
	user := cluster.NodeDefaults.User
	token := cluster.Agent.Token
	if node.Agent != nil {
		if node.Agent.Port != 0 {
			port = node.Agent.Port
		}
		if node.Agent.User != "" {
			user = node.Agent.User
		}
		if node.Agent.Token != "" {
			token = node.Agent.Token
		}
	}
	return EffectiveAgentConfig{
		Host:  node.Host,
		Port:  port,
		User:  user,
		TLS:   cluster.Agent.TLS,
		Token: configschema.ExpandEnv(token),
	}
}

func portFromListen(listen string) int {
	idx := strings.LastIndex(listen, ":")
	if idx == -1 || idx == len(listen)-1 {
		return 9280
	}
	port, err := strconv.Atoi(listen[idx+1:])
	if err != nil || port == 0 {
		return 9280
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
