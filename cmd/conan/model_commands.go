package main

import (
	"fmt"
	"strings"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/spf13/cobra"
)

type modelCommandConfig struct {
	home *string
}

func newModelCommand(cfg modelCommandConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage model configurations",
	}

	cmd.AddCommand(
		newModelListCommand(cfg),
		newModelUseCommand(cfg),
		newModelRemoveCommand(cfg),
		newModelAddCommand(cfg),
	)

	return cmd
}

func newModelListCommand(cfg modelCommandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(*cfg.home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			if len(global.Models) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No models configured.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-12s %-30s %-40s %s\n", "NAME", "TYPE", "MODEL", "ENDPOINT", "DEFAULT")
			for _, m := range global.Models {
				marker := ""
				if m.Name == global.DefaultModel {
					marker = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-12s %-30s %-40s %s\n", m.Name, m.Type, m.Model, m.Endpoint, marker)
			}
			return nil
		},
	}
}

func newModelUseCommand(cfg modelCommandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			loader := cfgloader.NewLoader(*cfg.home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			if !modelExists(global.Models, name) {
				return fmt.Errorf("model %q not found", name)
			}
			global.DefaultModel = name
			if err := loader.SaveGlobal(global); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default model set to %s\n", name)
			return nil
		},
	}
}

func newModelRemoveCommand(cfg modelCommandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a model configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			loader := cfgloader.NewLoader(*cfg.home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			idx := -1
			for i, m := range global.Models {
				if m.Name == name {
					idx = i
					break
				}
			}
			if idx == -1 {
				return fmt.Errorf("model %q not found", name)
			}
			global.Models = append(global.Models[:idx], global.Models[idx+1:]...)
			if global.DefaultModel == name {
				global.DefaultModel = ""
			}
			if err := loader.SaveGlobal(global); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed model %s\n", name)
			return nil
		},
	}
}

func newModelAddCommand(cfg modelCommandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Interactively add a model configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(*cfg.home)
			return runModelAdd(cmd.InOrStdin(), cmd.OutOrStdout(), loader, OpenAIModelLister{})
		},
	}
}

func modelExists(models []configschema.ModelConfig, name string) bool {
	for _, m := range models {
		if strings.EqualFold(m.Name, name) {
			return true
		}
	}
	return false
}
