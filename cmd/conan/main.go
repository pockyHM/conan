package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/credentials"
	"github.com/pockyHM/conan/internal/deploy"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/internal/logging"
	"github.com/pockyHM/conan/internal/mcp"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/nodeadd"
	"github.com/pockyHM/conan/internal/nodeupdate"
	"github.com/pockyHM/conan/internal/security"
	"github.com/pockyHM/conan/internal/skills"
	"github.com/pockyHM/conan/internal/tui"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var version = "dev"

var runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
	return tea.NewProgram(model, teaProgramOptions(in, out)...).Run()
}

func teaProgramOptions(in io.Reader, out io.Writer) []tea.ProgramOption {
	options := []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	}
	if tuiMouseEnabled() {
		options = append(options, tea.WithMouseCellMotion())
	}
	return options
}

func tuiMouseEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("CONAN_TUI_MOUSE")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

type conversationSaver interface {
	SaveCurrentConversation() (string, error)
}

func printResumeHint(out io.Writer, model tea.Model) {
	saver, ok := model.(conversationSaver)
	if !ok {
		return
	}
	id, err := saver.SaveCurrentConversation()
	if err != nil {
		fmt.Fprintf(out, "Session save failed: %v\n", err)
		return
	}
	fmt.Fprintf(out, "Session saved: %s\nResume with: conan resume %s\n", id, id)
}

type cliPrompter struct {
	in  io.Reader
	out io.Writer
}

func (p cliPrompter) PromptUsername(defaultValue string) (string, error) {
	fmt.Fprint(p.out, "SSH username: ")
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (p cliPrompter) PromptPassword() (string, error) {
	fmt.Fprint(p.out, "SSH password: ")
	if file, ok := p.in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(p.out)
		return string(data), err
	}
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (p cliPrompter) PromptIP(hostname string) (string, error) {
	fmt.Fprintf(p.out, "Hostname %s could not be resolved. Enter IP address: ", hostname)
	reader := bufio.NewReader(p.in)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
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

	nodeCmd := &cobra.Command{Use: "node", Short: "Node management commands"}
	var nodeAddUser string
	var nodeAddPassword string
	var nodeAddSSHPort int
	var nodeAddAgentPort int
	var nodeAddName string
	var nodeAddAgentBin string
	var nodeAddNoDeploy bool
	var nodeAddUpdate bool
	var nodeAddRotateToken bool
	nodeAddCmd := &cobra.Command{
		Use:   "add <hostname-or-ip>",
		Short: "Add a node and deploy conan-agent",
		Args:  cobra.ExactArgs(1),
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
			if selectedCluster == "" {
				selectedCluster = "default"
			}
			cluster, err := loader.LoadCluster(selectedCluster)
			if err != nil {
				return err
			}
			sshPort := nodeAddSSHPort
			if sshPort == 0 {
				sshPort = cluster.Cluster.NodeDefaults.SSHPort
			}
			if sshPort == 0 {
				sshPort = 22
			}
			agentPort := nodeAddAgentPort
			if agentPort == 0 {
				agentPort = 9280
			}
			service := nodeadd.Service{
				Credentials: credentials.NewStore(loader.Home()),
				Prompter:    cliPrompter{in: cmd.InOrStdin(), out: cmd.OutOrStdout()},
				Resolver:    nodeadd.NetResolver{},
				Writer:      nodeadd.ConfigNodeWriter{Home: loader.Home()},
				Deployer:    deploy.NewNativeDeployer(),
				Health:      nodeadd.MCPHealthChecker{},
			}
			result, err := service.Add(cmd.Context(), nodeadd.Request{
				Home:             loader.Home(),
				ClusterName:      selectedCluster,
				Input:            args[0],
				Name:             nodeAddName,
				Username:         nodeAddUser,
				Password:         nodeAddPassword,
				SSHPort:          sshPort,
				AgentPort:        agentPort,
				NoDeploy:         nodeAddNoDeploy,
				Update:           nodeAddUpdate,
				RotateToken:      nodeAddRotateToken,
				AgentBinOverride: nodeAddAgentBin,
				DeployConfig:     global.AgentDeploy,
				KnownHostsPath:   filepath.Join(loader.Home(), "known_hosts"),
				TLS:              cluster.Cluster.Agent.TLS,
			})
			if err != nil {
				return err
			}
			if result.Deployed {
				fmt.Fprintf(cmd.OutOrStdout(), "node added and deployed: %s\n", result.Node.Name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "node added: %s\n", result.Node.Name)
			}
			return nil
		},
	}
	nodeAddCmd.Flags().StringVarP(&nodeAddUser, "user", "u", "", "SSH username")
	nodeAddCmd.Flags().StringVarP(&nodeAddPassword, "password", "p", "", "SSH password")
	nodeAddCmd.Flags().IntVar(&nodeAddSSHPort, "ssh-port", 0, "SSH port")
	nodeAddCmd.Flags().IntVar(&nodeAddAgentPort, "port", 9280, "Agent listen port")
	nodeAddCmd.Flags().StringVar(&nodeAddName, "name", "", "Node name override")
	nodeAddCmd.Flags().StringVar(&nodeAddAgentBin, "agent-bin", "", "Local conan-agent binary path override")
	nodeAddCmd.Flags().BoolVar(&nodeAddNoDeploy, "no-deploy", false, "Only write node configuration")
	nodeAddCmd.Flags().BoolVar(&nodeAddUpdate, "update", false, "Update an existing node")
	nodeAddCmd.Flags().BoolVar(&nodeAddRotateToken, "rotate-token", false, "Rotate the node agent token while updating")
	nodeCmd.AddCommand(nodeAddCmd)

	var nodeUpdateAll bool
	var nodeUpdateAllCluster bool
	var nodeUpdateUser string
	var nodeUpdatePassword string
	var nodeUpdateSSHPort int
	var nodeUpdateAgentBin string
	var nodeUpdateMode string
	nodeUpdateCmd := &cobra.Command{
		Use:   "update [hostname-or-ip]",
		Short: "Update conan-agent on existing nodes",
		Args: func(cmd *cobra.Command, args []string) error {
			if nodeUpdateAllCluster {
				if nodeUpdateAll {
					return fmt.Errorf("--all and --all-cluster cannot be used together")
				}
				if len(args) != 0 {
					return fmt.Errorf("--all-cluster does not accept a node argument")
				}
				return nil
			}
			if nodeUpdateAll {
				if len(args) != 0 {
					return fmt.Errorf("--all does not accept a node argument")
				}
				if strings.TrimSpace(clusterName) == "" {
					return fmt.Errorf("--cluster is required when using --all")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("accepts 1 arg, received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := nodeupdate.UpdateMode(nodeUpdateMode)
			if mode == "" {
				mode = nodeupdate.ModeAuto
			}
			if mode != nodeupdate.ModeAuto && mode != nodeupdate.ModeSSH && mode != nodeupdate.ModeAgent {
				return fmt.Errorf("invalid update mode %q", nodeUpdateMode)
			}
			loader := cfgloader.NewLoader(home)
			global, err := loader.LoadGlobal()
			if err != nil {
				return err
			}
			service := nodeupdate.Service{
				Credentials:  credentials.NewStore(loader.Home()),
				Prompter:     cliPrompter{in: cmd.InOrStdin(), out: cmd.OutOrStdout()},
				Deployer:     deploy.NewNativeDeployer(),
				AgentUpdater: nodeupdate.MCPAgentUpdater{},
			}
			clusterNames, selector, all, err := nodeUpdateTargets(loader, global, clusterName, args, nodeUpdateAll, nodeUpdateAllCluster)
			if err != nil {
				return err
			}
			updatedCount := 0
			for _, selectedCluster := range clusterNames {
				cluster, err := loader.LoadCluster(selectedCluster)
				if err != nil {
					return err
				}
				if nodeUpdateAllCluster && len(cluster.Nodes) == 0 {
					continue
				}
				results, err := service.Update(cmd.Context(), nodeupdate.Request{
					Home:             loader.Home(),
					ClusterName:      selectedCluster,
					Cluster:          cluster,
					Selector:         selector,
					All:              all,
					Username:         nodeUpdateUser,
					Password:         nodeUpdatePassword,
					SSHPort:          nodeUpdateSSHPort,
					Mode:             mode,
					AgentBinOverride: nodeUpdateAgentBin,
					DeployConfig:     global.AgentDeploy,
					KnownHostsPath:   filepath.Join(loader.Home(), "known_hosts"),
				})
				if err != nil {
					return err
				}
				for _, result := range results {
					fmt.Fprintf(cmd.OutOrStdout(), "node updated: %s/%s\n", result.ClusterName, result.NodeName)
				}
				updatedCount += len(results)
			}
			if updatedCount == 0 {
				return fmt.Errorf("no nodes to update")
			}
			return nil
		},
	}
	nodeUpdateCmd.Flags().BoolVar(&nodeUpdateAll, "all", false, "Update all nodes in the selected cluster")
	nodeUpdateCmd.Flags().BoolVar(&nodeUpdateAllCluster, "all-cluster", false, "Update all nodes in all clusters")
	nodeUpdateCmd.Flags().StringVarP(&nodeUpdateUser, "user", "u", "", "SSH username override")
	nodeUpdateCmd.Flags().StringVarP(&nodeUpdatePassword, "password", "p", "", "SSH password override")
	nodeUpdateCmd.Flags().IntVar(&nodeUpdateSSHPort, "ssh-port", 0, "SSH port override")
	nodeUpdateCmd.Flags().StringVar(&nodeUpdateAgentBin, "agent-bin", "", "Local conan-agent binary path override")
	nodeUpdateCmd.Flags().StringVar(&nodeUpdateMode, "mode", "auto", "Update mode: auto, ssh, or agent")
	nodeCmd.AddCommand(nodeUpdateCmd)

	runTUI := func(cmd *cobra.Command, initialSessionID string) error {
		loader := cfgloader.NewLoader(home)
		global, err := loader.LoadGlobal()
		if err != nil {
			return err
		}
		logFile := global.Logging.File
		if strings.HasPrefix(logFile, "~/") {
			if userHome, err := os.UserHomeDir(); err == nil {
				logFile = filepath.Join(userHome, strings.TrimPrefix(logFile, "~/"))
			}
		}
		if err := logging.Setup(logging.Config{Level: global.Logging.Level, File: logFile}); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not initialize logging: %v\n", err)
		} else {
			defer logging.Close()
		}

		var auditLog *security.AuditLogger
		if global.Logging.Audit {
			auditPath := filepath.Join(loader.Home(), "audit.log")
			if logFile != "" {
				auditPath = filepath.Join(filepath.Dir(logFile), "audit.log")
			}
			var auditErr error
			auditLog, auditErr = security.NewAuditLogger(auditPath)
			if auditErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not initialize audit logging: %v\n", auditErr)
			} else {
				defer auditLog.Close()
			}
		}
		selectedCluster := clusterName
		if selectedCluster == "" {
			selectedCluster = global.DefaultCluster
		}
		if selectedCluster == "" {
			selectedCluster = "default"
		}

		var visibleSkills []skills.Skill
		var skillWarnings []string
		if global.Skills.Enabled {
			globalSkillReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(loader.Home()))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load global skills: %v\n", err)
			} else {
				clusterSkillReg, err := skills.LoadRegistry(skills.ClusterRegistryPath(loader.Home(), selectedCluster))
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load cluster skills: %v\n", err)
				} else {
					visibleSkills, skillWarnings, err = skills.ResolveVisible(loader.Home(), selectedCluster, globalSkillReg, clusterSkillReg, skills.ResolveOptions{
						MaxSkillFileBytes: 256 * 1024,
						MaxVisibleSkills:  global.Skills.MaxVisibleSkills,
						IndexCharBudget:   global.Skills.IndexTokenBudget,
					})
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load skills: %v\n", err)
						visibleSkills = nil
						skillWarnings = nil
					}
				}
			}
		}

		provider, modelName, err := llm.NewProvider(global.Models, global.DefaultModel)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
		}
		if provider != nil {
			provider = llm.NewRetryProvider(provider, llm.DefaultRetryConfig())
		}
		visionModel := strings.TrimSpace(global.Vision.Model)
		if visionModel == "" {
			visionModel = global.DefaultModel
		}
		visionProvider, _, visionErr := llm.NewVisionProvider(global.Models, visionModel)
		visionError := ""
		if visionErr != nil {
			visionError = visionErr.Error()
		} else if chatProvider, ok := visionProvider.(llm.Provider); ok {
			if retryProvider, ok := llm.NewRetryProvider(chatProvider, llm.DefaultRetryConfig()).(llm.VisionProvider); ok {
				visionProvider = retryProvider
			}
		}

		var clients map[string]*mcp.Client
		var agentTools []llm.ToolDef
		var cluster *cfgloader.Cluster
		if selectedCluster != "" {
			var err error
			cluster, err = loader.LoadCluster(selectedCluster)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load cluster %s: %v\n", selectedCluster, err)
			} else {
				clients = make(map[string]*mcp.Client)
				for _, node := range cluster.Nodes {
					url := mcp.URL(node.Agent.Host, node.Agent.Port, node.Agent.TLS)
					clients[node.Name] = mcp.NewClient(mcp.Config{
						BaseURL: url,
						Token:   node.Agent.Token,
					})
				}
				for _, client := range clients {
					tools, err := client.ListTools(cmd.Context())
					if err == nil {
						for _, t := range tools {
							agentTools = append(agentTools, llm.ToolDef(t))
						}
					}
					break
				}
			}
		}

		var nodeInfos []tui.NodeInfo
		nodeWhitelists := make(map[string][]string)
		if cluster != nil {
			for _, node := range cluster.Nodes {
				nodeInfos = append(nodeInfos, tui.NodeInfo{
					Name:             node.Name,
					Host:             node.Agent.Host,
					CommandWhitelist: node.CommandWhitelist,
				})
				nodeWhitelists[node.Name] = node.CommandWhitelist
			}
		}

		reviewer := security.NewReviewer(security.ReviewerConfig{
			NodeWhitelists:     nodeWhitelists,
			Blacklist:          global.Security.CommandBlacklist,
			LocalFileWhitelist: global.Security.LocalFileWhitelist,
			Provider:           provider,
			ModelName:          modelName,
		})
		workspaceRoot, err := os.Getwd()
		if err != nil {
			workspaceRoot = "."
		}

		memDir := filepath.Join(loader.Home(), "memory")
		memStore, err := memory.Open(memDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not open memory store: %v\n", err)
		}
		if err := memory.EnsureMemoryDir(filepath.Join(loader.Home(), "memory", "memory")); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not initialize memory directory: %v\n", err)
		}

		conv := conversation.New(selectedCluster, nil, modelName)
		model := tui.NewModel(tui.ModelConfig{
			Cluster:            selectedCluster,
			Model:              modelName,
			ModelConfigs:       global.Models,
			UILanguage:         global.UILanguage,
			InitialSessionID:   initialSessionID,
			Version:            version,
			Provider:           provider,
			VisionProvider:     visionProvider,
			VisionError:        visionError,
			Vision:             global.Vision,
			Conv:               conv,
			Clients:            clients,
			Tools:              agentTools,
			Nodes:              nodeInfos,
			Reviewer:           reviewer,
			AuditLogger:        auditLog,
			ConfigHome:         loader.Home(),
			IncidentDir:        filepath.Join(loader.Home(), "memory", "memory", "incidents"),
			MemoryStore:        memStore,
			Memory:             global.Memory,
			Subagents:          global.Subagents,
			LocalWorkspaceRoot: workspaceRoot,
			Skills:             visibleSkills,
			SkillsConfig:       global.Skills,
			SkillWarnings:      skillWarnings,
		})
		finalModel, err := runTeaProgram(model, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return err
		}
		printResumeHint(cmd.OutOrStdout(), finalModel)
		return nil
	}

	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runTUI(cmd, "")
	}

	resumeCmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume a saved TUI session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd, args[0])
		},
	}

	skillsCmd := &cobra.Command{Use: "skills", Short: "Skill commands"}
	var skillsGlobal bool
	var skillsCluster string
	var skillsRef string
	var skillsPath string
	resolveSkillsScope := func() (string, string, error) {
		scope := skills.ScopeCluster
		targetCluster := skillsCluster
		if skillsGlobal {
			return skills.ScopeGlobal, "", nil
		}
		if targetCluster == "" {
			targetCluster = clusterName
		}
		if targetCluster == "" {
			globalCfg, err := cfgloader.NewLoader(home).LoadGlobal()
			if err != nil {
				return "", "", err
			}
			targetCluster = globalCfg.DefaultCluster
		}
		if targetCluster == "" {
			return "", "", fmt.Errorf("--cluster is required when using cluster-scoped skills")
		}
		return scope, targetCluster, nil
	}

	skillsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := cfgloader.NewLoader(home)
			globalReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(loader.Home()))
			if err != nil {
				return err
			}
			selectedCluster := skillsCluster
			if selectedCluster == "" {
				selectedCluster = clusterName
			}
			if selectedCluster == "" {
				globalCfg, err := loader.LoadGlobal()
				if err != nil {
					return err
				}
				selectedCluster = globalCfg.DefaultCluster
			}
			var clusterReg skills.Registry
			if selectedCluster != "" {
				clusterReg, err = skills.LoadRegistry(skills.ClusterRegistryPath(loader.Home(), selectedCluster))
				if err != nil {
					return err
				}
			}
			if len(globalReg.Skills) == 0 && len(clusterReg.Skills) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No skills installed")
				return nil
			}
			for _, entry := range globalReg.Skills {
				fmt.Fprintf(cmd.OutOrStdout(), "global\t%s\t%s\n", entry.Name, entry.Description)
			}
			for _, entry := range clusterReg.Skills {
				fmt.Fprintf(cmd.OutOrStdout(), "cluster:%s\t%s\t%s\n", selectedCluster, entry.Name, entry.Description)
			}
			return nil
		},
	}

	skillsInstallCmd := &cobra.Command{
		Use:   "install <github-url>",
		Short: "Install skills from a public GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, targetCluster, err := resolveSkillsScope()
			if err != nil {
				return err
			}
			source, err := skills.NormalizeGitHubSource(args[0], skillsRef, skillsPath)
			if err != nil {
				return err
			}
			installer := skills.Installer{Home: cfgloader.NewLoader(home).Home(), MaxSkillFileBytes: 256 * 1024}
			installed, err := installer.Install(cmd.Context(), skills.InstallRequest{Source: source, Scope: scope, Cluster: targetCluster})
			if err != nil {
				return err
			}
			for _, skill := range installed {
				fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", skill.Name)
			}
			return nil
		},
	}

	skillsRemoveCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, targetCluster, err := resolveSkillsScope()
			if err != nil {
				return err
			}
			installer := skills.Installer{Home: cfgloader.NewLoader(home).Home(), MaxSkillFileBytes: 256 * 1024}
			removed, err := installer.Remove(skills.RemoveRequest{Name: args[0], Scope: scope, Cluster: targetCluster})
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("skill not found: %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}

	skillsUpdateCmd := &cobra.Command{
		Use:   "update [name]",
		Short: "Update installed skills",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, targetCluster, err := resolveSkillsScope()
			if err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			installer := skills.Installer{Home: cfgloader.NewLoader(home).Home(), MaxSkillFileBytes: 256 * 1024}
			updated, err := installer.Update(cmd.Context(), skills.UpdateRequest{Name: name, Scope: scope, Cluster: targetCluster})
			if err != nil {
				return err
			}
			for _, skill := range updated {
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s\n", skill.Name)
			}
			return nil
		},
	}
	skillsInstallCmd.Flags().BoolVar(&skillsGlobal, "global", false, "Install globally")
	skillsInstallCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to install into or list from")
	skillsInstallCmd.Flags().StringVar(&skillsRef, "ref", "", "Git ref to install (default repository branch)")
	skillsInstallCmd.Flags().StringVar(&skillsPath, "path", "skills", "Repository skills path")
	skillsListCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to list")
	skillsRemoveCmd.Flags().BoolVar(&skillsGlobal, "global", false, "Remove globally")
	skillsRemoveCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to remove from")
	skillsUpdateCmd.Flags().BoolVar(&skillsGlobal, "global", false, "Update globally")
	skillsUpdateCmd.Flags().StringVar(&skillsCluster, "cluster", "", "Cluster to update")

	skillsCmd.AddCommand(skillsListCmd, skillsInstallCmd, skillsRemoveCmd, skillsUpdateCmd)

	rootCmd.AddCommand(configCmd, clustersCmd, nodesCmd, pingCmd, toolsCmd, nodeCmd, resumeCmd, skillsCmd, newFilesCommand(&home, &clusterName), newModelCommand(modelCommandConfig{home: &home}))
	return rootCmd
}

func nodeUpdateTargets(loader *cfgloader.Loader, global *configschema.GlobalConfig, clusterName string, args []string, all bool, allCluster bool) ([]string, string, bool, error) {
	if allCluster {
		clusters, err := loader.ListClusters()
		if err != nil {
			return nil, "", false, err
		}
		if len(clusters) == 0 {
			return nil, "", false, fmt.Errorf("no clusters configured")
		}
		return clusters, "", true, nil
	}

	selectedCluster := strings.TrimSpace(clusterName)
	if selectedCluster == "" && global != nil {
		selectedCluster = global.DefaultCluster
	}
	if selectedCluster == "" {
		selectedCluster = "default"
	}

	selector := ""
	if !all && len(args) == 1 {
		selector = args[0]
	}
	return []string{selectedCluster}, selector, all, nil
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
