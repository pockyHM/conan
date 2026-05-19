package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/tui"
	"github.com/spf13/cobra"
)

var version = "dev"

var runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) error {
	_, err := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var home string
	var clusterName string

	rootCmd := &cobra.Command{
		Use:           "conan",
		Short:         "Conan — AI operations assistant CLI",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.PersistentFlags().StringVar(&home, "home", cfgloader.DefaultHome(), "Conan home directory")
	rootCmd.PersistentFlags().StringVarP(&clusterName, "cluster", "c", "", "Cluster name")

	configCmd := &cobra.Command{Use: "config", Short: "Configuration commands"}
	configCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate Conan configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			if global.DefaultCluster != "" {
				if _, err := loader.LoadCluster(global.DefaultCluster); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config ok: %s\n", loader.Home())
			return nil
		},
	})

	clustersCmd := &cobra.Command{
		Use:   "clusters",
		Short: "List configured clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clusters, err := cfgloader.NewLoader(home).ListClusters()
			if err != nil {
				return err
			}
			for _, name := range clusters {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}

	nodesCmd := &cobra.Command{
		Use:   "nodes",
		Short: "List nodes in a cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cfgloader.NewLoader(home).LoadCluster(clusterName)
			if err != nil {
				return err
			}
			for _, node := range cluster.Nodes {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%d\n", node.Name, node.Host, strings.Join(node.Labels, ","), node.Agent.Port)
			}
			return nil
		},
	}

	pingCmd := &cobra.Command{
		Use:   "ping [node]",
		Short: "Ping agent health endpoints",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cfgloader.NewLoader(home).LoadCluster(clusterName)
			if err != nil {
				return err
			}
			return forEachNode(cmd.Context(), cluster, args, func(ctx context.Context, node cfgloader.Node) error {
				client := mcp.NewClient(mcp.Config{BaseURL: mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS), Token: node.Agent.Token})
				if err := client.Ping(ctx); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\toffline\t%v\n", node.Name, err)
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tonline\n", node.Name)
				return nil
			})
		},
	}

	toolsCmd := &cobra.Command{Use: "tools", Short: "Agent tool commands"}
	toolsCmd.AddCommand(&cobra.Command{
		Use:   "list <node>",
		Short: "List tools exposed by an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cfgloader.NewLoader(home).LoadCluster(clusterName)
			if err != nil {
				return err
			}
			return forEachNode(cmd.Context(), cluster, args, func(ctx context.Context, node cfgloader.Node) error {
				client := mcp.NewClient(mcp.Config{BaseURL: mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS), Token: node.Agent.Token})
				tools, err := client.ListTools(ctx)
				if err != nil {
					return err
				}
				for _, tool := range tools {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", node.Name, tool.Name, tool.Description)
				}
				return nil
			})
		},
	})

	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Start the interactive TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			selectedCluster := clusterName
			if selectedCluster == "" {
				selectedCluster = global.DefaultCluster
			}
			model := tui.NewModel(tui.ModelConfig{Cluster: selectedCluster, Model: global.DefaultModel})
			return runTeaProgram(model, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	rootCmd.AddCommand(configCmd, clustersCmd, nodesCmd, pingCmd, toolsCmd, tuiCmd)
	return rootCmd
}

func forEachNode(ctx context.Context, cluster *cfgloader.Cluster, args []string, fn func(context.Context, cfgloader.Node) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wanted := ""
	if len(args) == 1 {
		wanted = args[0]
	}
	matched := false
	for _, node := range cluster.Nodes {
		if wanted != "" && node.Name != wanted {
			continue
		}
		matched = true
		if err := fn(ctx, node); err != nil {
			return err
		}
	}
	if !matched && wanted != "" {
		return fmt.Errorf("node not found: %s", wanted)
	}
	return nil
}
