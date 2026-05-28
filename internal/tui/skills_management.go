package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/skills"
	"github.com/pockyHM/conan/pkg/configschema"
)

type skillManagementResultMsg struct {
	status         string
	skills         []skills.Skill
	warnings       []string
	err            error
	keepSkillsOpen bool
}

type skillInstallPreviewMsg struct {
	source     skills.RepoSource
	scope      string
	cluster    string
	discovered []skills.Skill
	err        error
}

type pendingSkillInstall struct {
	source  skills.RepoSource
	scope   string
	cluster string
}

type pendingSkillRemove struct {
	entry skillManageEntry
}

type skillsCommandOptions struct {
	global  bool
	cluster string
	ref     string
	path    string
}

func parseSkillsCommandOptions(args []string) (skillsCommandOptions, error) {
	opts := skillsCommandOptions{path: "skills"}
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		switch {
		case arg == "--global":
			opts.global = true
		case arg == "--cluster":
			idx++
			if idx >= len(args) || strings.TrimSpace(args[idx]) == "" {
				return opts, fmt.Errorf("--cluster requires a value")
			}
			opts.cluster = args[idx]
		case strings.HasPrefix(arg, "--cluster="):
			opts.cluster = strings.TrimPrefix(arg, "--cluster=")
		case arg == "--ref":
			idx++
			if idx >= len(args) || strings.TrimSpace(args[idx]) == "" {
				return opts, fmt.Errorf("--ref requires a value")
			}
			opts.ref = args[idx]
		case strings.HasPrefix(arg, "--ref="):
			opts.ref = strings.TrimPrefix(arg, "--ref=")
		case arg == "--path":
			idx++
			if idx >= len(args) || strings.TrimSpace(args[idx]) == "" {
				return opts, fmt.Errorf("--path requires a value")
			}
			opts.path = args[idx]
		case strings.HasPrefix(arg, "--path="):
			opts.path = strings.TrimPrefix(arg, "--path=")
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}
	return opts, nil
}

func (m Model) skillsCommandScope(opts skillsCommandOptions) (string, string) {
	if opts.global {
		return skills.ScopeGlobal, ""
	}
	cluster := strings.TrimSpace(opts.cluster)
	if cluster == "" {
		cluster = m.cluster
	}
	return skills.ScopeCluster, cluster
}

func (m Model) applySkillsCommand(arg string) (Model, tea.Cmd) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return m.openSkillsManager()
	}
	switch fields[0] {
	case "install":
		if len(fields) < 2 {
			m.status = m.uiLanguage.tr("Usage: /skills install <github-url> [--global|--cluster name] [--ref ref] [--path path]", "用法: /skills install <github-url> [--global|--cluster 名称] [--ref ref] [--path path]")
			return m, nil
		}
		opts, err := parseSkillsCommandOptions(fields[2:])
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		scope, cluster := m.skillsCommandScope(opts)
		source, err := skills.NormalizeGitHubSource(fields[1], opts.ref, opts.path)
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.status = m.uiLanguage.tr("Discovering skills...", "正在发现技能...")
		return m, m.previewSkillInstall(source, scope, cluster)
	case "remove":
		if len(fields) < 2 {
			m.status = m.uiLanguage.tr("Usage: /skills remove <name> [--global|--cluster name]", "用法: /skills remove <名称> [--global|--cluster 名称]")
			return m, nil
		}
		opts, err := parseSkillsCommandOptions(fields[2:])
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		scope, cluster := m.skillsCommandScope(opts)
		name := fields[1]
		return m, m.runSkillsManagement(func(installer skills.Installer) (string, error) {
			removed, err := installer.Remove(skills.RemoveRequest{Name: name, Scope: scope, Cluster: cluster})
			if err != nil {
				return "", err
			}
			if !removed {
				return "", fmt.Errorf("skill not found: %s", name)
			}
			return m.uiLanguage.tr("Removed skill: ", "已移除技能: ") + name, nil
		})
	case "update":
		name := ""
		optionsStart := 1
		if len(fields) > 1 && !strings.HasPrefix(fields[1], "--") {
			name = fields[1]
			optionsStart = 2
		}
		opts, err := parseSkillsCommandOptions(fields[optionsStart:])
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		scope, cluster := m.skillsCommandScope(opts)
		m.status = m.uiLanguage.tr("Updating skills...", "正在更新技能...")
		return m, m.runSkillsManagement(func(installer skills.Installer) (string, error) {
			updated, err := installer.Update(context.Background(), skills.UpdateRequest{Name: name, Scope: scope, Cluster: cluster})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(m.uiLanguage.tr("Updated %d skill(s)", "已更新 %d 个技能"), len(updated)), nil
		})
	default:
		name, rest := splitSkillInvocationArg(arg)
		if skill, found := m.findSkill(name); found {
			visible := strings.TrimSpace("/skills " + name + " " + rest)
			return m.submitSkillMessage(visible, skill, rest)
		}
		m.status = m.uiLanguage.tr("Usage: /skills [install|remove|update|<skill>]", "用法: /skills [install|remove|update|<技能>]")
		return m, nil
	}
}

func (m Model) openSkillsManager() (Model, tea.Cmd) {
	entries, err := loadSkillManageEntries(m.configHome, m.cluster)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	entries = mergeVisibleSkillEntries(entries, m.skills)
	m.mode = modeSkillsManage
	m.skillsManager = newSkillsManager(entries, m.uiLanguage)
	if len(entries) == 0 {
		m.status = m.uiLanguage.tr("No skills installed", "没有已安装技能")
	} else {
		m.status = fmt.Sprintf(m.uiLanguage.tr("%d skill(s) installed", "已安装 %d 个技能"), len(entries))
	}
	return m, nil
}

func loadSkillManageEntries(home string, cluster string) ([]skillManageEntry, error) {
	if strings.TrimSpace(home) == "" {
		home = cfgloader.DefaultHome()
	}
	globalReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		return nil, err
	}
	var entries []skillManageEntry
	for _, entry := range globalReg.Skills {
		entries = append(entries, skillManageEntry{
			Name:        entry.Name,
			Description: entry.Description,
			Scope:       skills.ScopeGlobal,
		})
	}
	if strings.TrimSpace(cluster) != "" {
		clusterReg, err := skills.LoadRegistry(skills.ClusterRegistryPath(home, cluster))
		if err != nil {
			return nil, err
		}
		for _, entry := range clusterReg.Skills {
			entries = append(entries, skillManageEntry{
				Name:        entry.Name,
				Description: entry.Description,
				Scope:       skills.ScopeCluster,
				Cluster:     cluster,
			})
		}
	}
	return entries, nil
}

func mergeVisibleSkillEntries(entries []skillManageEntry, visible []skills.Skill) []skillManageEntry {
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := entry.Scope + ":" + entry.Cluster + ":" + entry.Name
		seen[key] = true
	}
	for _, skill := range visible {
		key := skill.Scope + ":" + skill.Cluster + ":" + skill.Name
		if seen[key] {
			continue
		}
		entries = append(entries, skillManageEntry{
			Name:        skill.Name,
			Description: skill.Description,
			Scope:       skill.Scope,
			Cluster:     skill.Cluster,
		})
	}
	return entries
}

func (m Model) previewSkillInstall(source skills.RepoSource, scope string, cluster string) tea.Cmd {
	configHome := m.configHome
	fetcher := m.skillsFetcher
	return func() tea.Msg {
		loader := cfgloader.NewLoader(configHome)
		installer := skills.Installer{Home: loader.Home(), Fetcher: fetcher, MaxSkillFileBytes: 256 * 1024}
		discovered, err := installer.Discover(context.Background(), source)
		return skillInstallPreviewMsg{source: source, scope: scope, cluster: cluster, discovered: discovered, err: err}
	}
}

func (m Model) runSkillsManagement(fn func(skills.Installer) (string, error)) tea.Cmd {
	configHome := m.configHome
	cluster := m.cluster
	cfg := m.skillsConfig
	fetcher := m.skillsFetcher
	return func() tea.Msg {
		loader := cfgloader.NewLoader(configHome)
		installer := skills.Installer{Home: loader.Home(), Fetcher: fetcher, MaxSkillFileBytes: 256 * 1024}
		status, err := fn(installer)
		if err != nil {
			return skillManagementResultMsg{err: err}
		}
		visible, warnings, err := resolveVisibleSkillsForTUI(loader.Home(), cluster, cfg)
		if err != nil {
			return skillManagementResultMsg{err: err}
		}
		return skillManagementResultMsg{status: status, skills: visible, warnings: warnings}
	}
}

func (m Model) runSkillsManagementKeepOpen(fn func(skills.Installer) (string, error)) tea.Cmd {
	base := m.runSkillsManagement(fn)
	return func() tea.Msg {
		msg := base()
		result, ok := msg.(skillManagementResultMsg)
		if !ok {
			return msg
		}
		result.keepSkillsOpen = true
		return result
	}
}

func resolveVisibleSkillsForTUI(home string, cluster string, cfg configschema.SkillsConfig) ([]skills.Skill, []string, error) {
	globalReg, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		return nil, nil, err
	}
	var clusterReg skills.Registry
	if strings.TrimSpace(cluster) != "" {
		clusterReg, err = skills.LoadRegistry(skills.ClusterRegistryPath(home, cluster))
		if err != nil {
			return nil, nil, err
		}
	}
	return skills.ResolveVisible(home, cluster, globalReg, clusterReg, skills.ResolveOptions{
		MaxSkillFileBytes: 256 * 1024,
		MaxVisibleSkills:  cfg.MaxVisibleSkills,
		IndexCharBudget:   cfg.IndexTokenBudget,
	})
}

func (m Model) installSelectedSkills(names []string) tea.Cmd {
	pending := m.pendingSkillInstall
	if len(names) == 0 {
		return func() tea.Msg {
			return skillManagementResultMsg{err: fmt.Errorf("no skills selected")}
		}
	}
	return m.runSkillsManagement(func(installer skills.Installer) (string, error) {
		installed, err := installer.Install(context.Background(), skills.InstallRequest{
			Source:  pending.source,
			Scope:   pending.scope,
			Cluster: pending.cluster,
			Names:   names,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(m.uiLanguage.tr("Installed %d skill(s)", "已安装 %d 个技能"), len(installed)), nil
	})
}
