package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/pockyHM/conan/internal/agent"
	"github.com/pockyHM/conan/internal/tools"
	"github.com/pockyHM/conan/pkg/configschema"
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
			registerAllTools(registry)
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

func registerAllTools(r *tools.Registry) {
	r.Register(&tools.ShellTool{})
	for _, t := range tools.NewFsTools() {
		r.Register(t)
	}
	for _, t := range tools.NewSysTools() {
		r.Register(t)
	}
	for _, t := range tools.NewSvcTools() {
		r.Register(t)
	}
	for _, t := range tools.NewLogTools() {
		r.Register(t)
	}
	for _, t := range tools.NewNetTools() {
		r.Register(t)
	}
	for _, t := range tools.NewK8sTools() {
		r.Register(t)
	}
	for _, t := range tools.NewPkgTools() {
		r.Register(t)
	}
	for _, t := range tools.NewCronTools() {
		r.Register(t)
	}
	for _, t := range tools.NewDockerTools() {
		r.Register(t)
	}
}
