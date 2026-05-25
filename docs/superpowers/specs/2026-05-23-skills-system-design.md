# Conan Skills System Design

## Goal

Add a skills system to Conan so reusable agent instructions can be installed from public GitHub repositories, scoped globally or to selected clusters, invoked explicitly from slash commands, and selected automatically by the model when useful.

The design intentionally avoids permanently injecting full skill content into the main agent context. The model should see a short skill index and use a dedicated `skill_read` tool to load the full skill only when it is relevant.

## Non-Goals

- Project/workspace-scoped skills. Conan skills are scoped only to `global` and clusters.
- Automatic execution of scripts inside skill repositories.
- Private GitHub repository authentication in the first version.
- Running skills as separate agents. A skill is reusable instruction/context, not an execution sandbox.

## Skill Package Format

A GitHub repository can contain one or more skills. The default discovery root is `skills/`, with an override available through `--path`.

```text
repo/
  skills/
    k8s-debug/
      SKILL.md
      references/
      scripts/
```

`SKILL.md` is required and starts with YAML frontmatter:

```md
---
name: k8s-debug
description: Use when diagnosing Kubernetes pod, service, ingress, node, or workload failures.
version: 1.0.0
---

Skill instructions...
```

Required fields:

- `name`: stable skill identifier used by slash commands and `skill_read`.
- `description`: short selection guidance shown to the model in the skill index.

Optional fields:

- `version`: informational version string.
- `tags`: keywords used for list and search.
- `max_chars`: per-skill override for returned content length, capped by global defaults.

Only `SKILL.md` content is loaded automatically. Files under `references/` and `scripts/` are retained on disk for future features, but are not injected into context or executed by this design.

## Storage Layout

Use Conan home for all skill state:

```text
~/.conan/
  skills/
    repos/
      github.com/org/repo/<ref-or-hash>/
    registry.yaml
  clusters/
    prod/
      skills.yaml
```

`skills/registry.yaml` stores global installations and cached repo metadata. `clusters/<name>/skills.yaml` stores cluster-scoped installations. Repo content is cached once and referenced by scope registries, so installing the same repo/ref to several clusters does not duplicate files.

Global registry entry:

```yaml
skills:
  - name: k8s-debug
    source: github.com/org/repo
    ref: v1.2.0
    path: skills/k8s-debug
    cache_path: skills/repos/github.com/org/repo/v1.2.0/skills/k8s-debug
    installed_at: "2026-05-23T12:00:00Z"
```

Cluster registry uses the same entry shape under each cluster.

## Scope And Deduplication

Visible skills for a TUI session are:

```text
visible = global skills + current cluster skills
```

Deduplication rules:

1. Deduplicate by `name`.
2. If global and current cluster contain the same `name`, the cluster-scoped skill wins.
3. If multiple entries with the same `name` exist in the same scope, the newest installed entry wins and `/skills` shows a conflict warning.
4. The model receives only the deduplicated visible list.

This means cluster skills can intentionally override global skills for cluster-specific operational practices.

## CLI Commands

Add a Cobra command group:

```text
conan skills install <github-url> [--global | --cluster prod,staging] [--ref main] [--path skills/]
conan skills list [--global | --cluster prod]
conan skills remove <name> [--global | --cluster prod]
conan skills update [<name>] [--global | --cluster prod]
```

Install behavior:

- Accept `github.com/org/repo`, `https://github.com/org/repo`, and `org/repo`.
- Default `--ref` is the repository default branch.
- If neither `--global` nor `--cluster` is provided, ask interactively where to install.
- Validate discovered skills before writing registry changes.
- Reject repositories with no valid skills.

## TUI Slash Commands

Add TUI commands:

```text
/skills                         List visible skills for current cluster
/skills install <github-url>     Install with interactive scope selection
/skills remove <name>            Remove from selected scope
/skills update [name]            Update installed skills
/skill <name> [arguments]        Explicitly load and apply a skill
/<skill-name> [arguments]        Shortcut for visible skills
```

Slash command precedence:

1. Built-in slash commands always win.
2. If no built-in command matches, try visible skill shortcut by name.
3. If no skill matches, return the existing unknown command behavior.

This avoids conflicts such as a skill named `model` shadowing `/model`; users can still invoke it with `/skill model`.

Explicit skill invocation should add a compact user-visible event and inject the selected skill content into the next model request with the user-provided arguments.

## Model Automatic Selection

Expose a model-callable meta tool only when visible skills exist:

```text
skill_read
```

Tool schema:

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Name of the visible skill to load"
    },
    "reason": {
      "type": "string",
      "description": "Brief reason this skill is relevant"
    }
  },
  "required": ["name", "reason"]
}
```

The system prompt receives only a short skill index:

```text
Available skills:
- k8s-debug: Diagnose Kubernetes pod/service/ingress failures. scope=cluster:prod
- incident-report: Write concise incident reports. scope=global

Call skill_read when one of these skills would materially improve the answer.
```

`skill_read` returns the selected `SKILL.md` body, capped by configuration. It does not return repository metadata, scripts, or unrelated files. If the skill is not visible in the current session, the tool returns an error.

## Context Budgeting

Add config:

```yaml
skills:
  enabled: true
  index_token_budget: 800
  max_skill_chars: 6000
  max_visible_skills: 50
```

Defaults:

- `enabled`: true
- `index_token_budget`: 800
- `max_skill_chars`: 6000
- `max_visible_skills`: 50

If visible skills exceed `max_visible_skills`, sort by scope priority and name, include the first entries, and show a status warning in `/skills`.

## Security

GitHub installation:

- First version supports only public GitHub repositories.
- Clone/fetch operations must write only under Conan's skills cache directory.
- Validate skill paths so registry entries cannot escape the cache root.
- Reject individual `SKILL.md` files above a configured maximum size.
- Do not execute repository scripts during install or use.
- Do not include secrets in registry files or logs.

Model use:

- `skill_read` can only read visible, validated skills.
- Returned content is capped.
- Tool result should identify the skill name and scope, but not expose arbitrary local paths.

## Error Handling

- Invalid GitHub URL: return a deterministic validation error with accepted formats.
- Git unavailable or clone failure: show the underlying command failure without dumping credentials.
- No skills found: report the searched path.
- Duplicate names in one install: install valid unique skills and report skipped duplicates, or fail the install if duplicates point to different directories in the same repo.
- Unknown `/skill <name>`: show nearest visible skill names when possible.
- `skill_read` for hidden or missing skill: return a tool error, not a panic.

## Components

Add a new package:

```text
internal/skills/
  registry.go      # registry read/write and dedupe
  installer.go     # GitHub install/update/remove
  parser.go        # SKILL.md frontmatter parsing and validation
  resolver.go      # visible skill resolution for current cluster
  tools.go         # skill_read tool definition and handler
```

TUI integration:

- `internal/tui/command.go`: parse `/skills` and `/skill`.
- `internal/tui/autocomplete.go`: complete skill commands and skill names.
- `internal/tui/model.go`: include skill index in model requests and dispatch `skill_read`.

CLI integration:

- `cmd/conan/main.go`: add `skills` command group.
- `internal/config/loader.go` and `pkg/configschema/config.go`: add `SkillsConfig`.

## Testing

Unit tests:

- Parse valid and invalid `SKILL.md` frontmatter.
- Discover skills from a repository fixture.
- Registry read/write preserves existing unrelated entries.
- Scope resolution returns `global ∪ current cluster`.
- Cluster entries override global entries with the same name.
- Slash command parser handles `/skills`, `/skill name`, and skill-name shortcuts without shadowing built-ins.
- `skill_read` returns capped content and rejects hidden skills.

Integration-style tests:

- Install from a local git fixture using the GitHub installer abstraction.
- Update an installed repo/ref and refresh registry metadata.
- TUI model request includes a compact skill index and exposes `skill_read` only when visible skills exist.

## Implementation Notes

Implement installation through an interface so tests do not depend on network access:

```go
type RepoFetcher interface {
    Fetch(ctx context.Context, source RepoSource, dest string) error
}
```

The production implementation can call `git clone --depth 1` for branch refs and normal fetch/checkout for tags or commits. Tests should use fixture directories or local git repositories.

The first implementation should keep skills as Markdown instructions only. Any future support for executing scripts or reading reference files should go through separate tools and permission checks.
