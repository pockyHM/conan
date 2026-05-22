package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/pockyHM/conan/internal/agent"
	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var version = "dev"

func main() {
	var configPath string

	rootCmd := &cobra.Command{
		Use:     "conan-agent",
		Short:   "Conan Agent — MCP server for managed nodes",
		Version: version,
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the agent server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			slog.SetLogLoggerLevel(slog.LevelInfo)
			slog.Info("conan-agent starting", "version", version)

			registry := tools.NewRegistry()
			registerAllTools(registry, cfg)
			registry.DisableAll(cfg.DisabledTools)

			srv := agent.NewServer(cfg, registry, version)
			go srv.WaitForSignal()
			return srv.Start()
		},
	}

	runCmd.Flags().StringVarP(&configPath, "config", "c", "/etc/conan-agent/config.yaml", "Config file path")
	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig(path string) (*configschema.AgentConfig, error) {
	cfg := configschema.DefaultAgentConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("config file not found, using defaults", "path", path)
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func registerAllTools(r *tools.Registry, cfg *configschema.AgentConfig) {
	r.Register(&tools.ShellTool{})
	registerToolsByName(r, tools.NewFsTools(), "fs/read", "fs/list", "fs/stat")
	for _, t := range tools.NewSysTools() {
		r.Register(t)
	}
	registerToolsByName(r, tools.NewSvcTools(), "svc/list", "svc/status")
	for _, t := range tools.NewLogTools() {
		r.Register(t)
	}
	registerToolsByName(r, tools.NewNetTools(), "net/ping", "net/traceroute", "net/portcheck")
	for _, t := range tools.NewWebTools(webToolConfig(cfg.Web)) {
		r.Register(t)
	}
	registerToolsByName(r, tools.NewK8sTools(), "k8s/pods", "k8s/logs", "k8s/events", "k8s/describe")
	registerToolsByName(r, tools.NewPkgTools(), "pkg/list", "pkg/search")
	registerToolsByName(r, tools.NewCronTools(), "cron/list", "cron/show")
	registerToolsByName(r, tools.NewDockerTools(), "docker/ps", "docker/images", "docker/logs")
}

func registerToolsByName(r *tools.Registry, candidates []tools.Tool, names ...string) {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	for _, t := range candidates {
		if allowed[t.Name()] {
			r.Register(t)
		}
	}
}

func webToolConfig(cfg configschema.WebConfig) tools.WebToolConfig {
	apiKey := configschema.ExpandEnv(cfg.SearchAPIKey)
	if apiKey == "" && cfg.SearchAPIKeyEnv != "" {
		apiKey = os.Getenv(cfg.SearchAPIKeyEnv)
	}
	return tools.WebToolConfig{
		SearchProvider:      cfg.SearchProvider,
		SearchAPIKey:        apiKey,
		SearchEndpoint:      cfg.SearchEndpoint,
		FetchMaxBytes:       cfg.FetchMaxBytes,
		FetchMaxChars:       cfg.FetchMaxChars,
		AllowPrivateNetwork: cfg.AllowPrivateNetwork,
	}
}
