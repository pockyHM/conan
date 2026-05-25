# Conan

[English](README.md) | [简体中文](README.zh-CN.md)

Conan is an AI operations assistant that runs in your terminal. The main entry is the `conan` TUI: you talk to the model, select nodes, reference local files, attach images, install skills, and let Conan call safe node tools through `conan-agent`.

## Quick Start

Build the binaries:

```bash
make build
```

Add a model:

```bash
./bin/conan model add
./bin/conan model use <name>
```

Add a node and deploy `conan-agent`:

```bash
./bin/conan node add <hostname-or-ip> --user <ssh-user>
```

Start Conan:

```bash
./bin/conan
```

`conan` without a subcommand opens the interactive TUI. Use `--home <path>` to choose a Conan home directory and `--cluster <name>` to choose a cluster.

## Daily TUI Usage

Type a natural-language request and press Enter:

```text
Check nginx status on the selected nodes.
Find recent kubelet errors and summarize the likely cause.
Upload @deploy/nginx.conf to /etc/nginx/nginx.conf on node-a.
```

Useful slash commands:

```text
/help                 Show available commands
/lang                 Switch UI language
/model [name]         Show or switch the active model
/cluster [name]       Show or switch cluster
/nodes                Select target nodes
/skills               List visible skills
/skills install ...   Install skills without leaving the TUI
/skill <name> ...     Ask Conan to use a specific skill
/memory               Show memory summary
/resume [id]          Resume a saved session
/compact [focus]      Compact conversation context
/thinking <message>   Send one message with thinking enabled
/agent <role> <task>  Run a local read-only subagent task
/subagents            Manage local subagents
/exit                 Save and exit
```

When you exit, Conan prints a session id:

```text
Session saved: <id>
Resume with: conan resume <id>
```

You can resume directly:

```bash
./bin/conan resume <id>
```

## References In Prompts

Conan supports `@` references in TUI input.

### Local Files

Use `@path` to include a local workspace file as prompt context:

```text
Review @internal/tui/model.go and explain the session resume flow.
```

Use quotes for paths with spaces:

```text
Summarize @"docs/my runbook.md".
```

Use a directory reference to include a directory listing:

```text
Explain how this package is organized: @internal/skills
```

Rules:

- Paths are relative to the directory where you started `conan`.
- Absolute paths and `..` paths are rejected.
- Symlink paths are rejected.
- Large files are truncated.
- Type `@` in the TUI to get path completion.
- Type `@@` if you want a literal `@`.

### Images

PNG, JPEG, and GIF images can be referenced with `@path` or pasted from the clipboard. Conan stores them as image attachments and uses the configured vision model when it needs to inspect pixels.

```text
What is wrong in this screenshot? @tmp/error.png
```

Relevant config:

```yaml
vision:
  model: gpt-4o
  max_images: 10
  max_summary_chars_per_image: 1200
```

If `vision.model` is empty, Conan uses `default_model`.

## Skills

Skills are reusable instruction packs for Conan. A skill is a directory containing `SKILL.md` with YAML frontmatter and a Markdown body:

```markdown
---
name: k8s-debug
description: Diagnose Kubernetes failures.
version: 0.1.0
tags: [kubernetes, debugging]
max_chars: 6000
---

Use this workflow when investigating Kubernetes incidents...
```

Conan loads visible skills into the session index. The model can call the built-in `skill_read` tool to load the full instructions when the skill is relevant, and you can explicitly request one with `/skill`.

### Install Skills

Install from a public GitHub repository. Conan looks under the repository `skills/` directory by default and discovers every `SKILL.md`.

Cluster-scoped install:

```bash
./bin/conan skills install github.com/org/repo --cluster prod
```

Global install:

```bash
./bin/conan skills install github.com/org/repo --global
```

Install from a branch, tag, or custom directory:

```bash
./bin/conan skills install org/repo --ref v1.2.0 --path conan-skills --global
```

The same operations work inside the TUI:

```text
/skills install github.com/org/repo --cluster prod
/skills install org/repo --global --ref main --path skills
```

List, update, and remove:

```bash
./bin/conan skills list --cluster prod
./bin/conan skills update --cluster prod
./bin/conan skills update k8s-debug --global
./bin/conan skills remove k8s-debug --cluster prod
```

### Skill Scopes

- Global skills are available in every cluster.
- Cluster skills are available only when that cluster is active.
- Cluster skills are registered in `~/.conan/clusters/<cluster>/skills.yaml`.
- Global skills are registered in `~/.conan/skills/registry.yaml`.
- Repository checkouts are cached under `~/.conan/skills/repos/`.

Relevant config:

```yaml
skills:
  enabled: true
  index_token_budget: 800
  max_skill_chars: 6000
  max_visible_skills: 50
```

## Models

Interactive model setup:

```bash
./bin/conan model add
./bin/conan model list
./bin/conan model use <name>
./bin/conan model remove <name>
```

Conan supports Anthropic and OpenAI-compatible providers. API keys in config can use environment references such as `${OPENAI_API_KEY}`.

## Nodes And Agents

`conan-agent` runs on managed nodes and exposes MCP tools for operations tasks. Add and deploy a node:

```bash
./bin/conan node add 10.0.0.12 --name web-1 --user root
```

Useful variants:

```bash
./bin/conan node add web-1.example.com --no-deploy
./bin/conan node add web-1.example.com --update
./bin/conan node add web-1.example.com --update --rotate-token
./bin/conan node add web-1.example.com --agent-bin ./bin/conan-agent-linux-amd64
```

Check inventory and connectivity:

```bash
./bin/conan clusters
./bin/conan nodes --cluster prod
./bin/conan ping
./bin/conan ping web-1
./bin/conan tools list web-1
```

Run the agent manually:

```bash
./bin/conan-agent run --config /etc/conan-agent/config.yaml
```

Example agent config:

```yaml
listen: "0.0.0.0:9280"
token: "changeme"
tls: false
audit_log: /var/log/conan-agent/audit.log
rate_limit: 10
disabled_tools: []
log_level: info
```

## File Transfer

Use first-class file transfer instead of `scp` or shell commands:

```bash
./bin/conan files put web-1 ./local.conf /etc/app/app.conf
./bin/conan files get web-1 /var/log/app.log ./downloads/app.log
```

Inside the TUI, ask naturally:

```text
Download /etc/nginx/nginx.conf from web-1 to @downloads/nginx.conf.
Upload @configs/app.yaml to /etc/app/app.yaml on web-1.
```

## Configuration

Default home:

```text
~/.conan
```

Override with:

```bash
CONAN_HOME=/path/to/home ./bin/conan
./bin/conan --home /path/to/home
```

Common files:

```text
~/.conan/config.yaml
~/.conan/clusters/base.yaml
~/.conan/clusters/<cluster>/cluster.yaml
~/.conan/clusters/<cluster>/nodes.yaml
~/.conan/clusters/<cluster>/skills.yaml
~/.conan/skills/registry.yaml
~/.conan/memory/
```

Global config example:

```yaml
default_model: gpt-4o
default_cluster: prod
ui_language: en-US

models:
  - name: gpt-4o
    type: openai
    endpoint: https://api.openai.com/v1
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

logging:
  level: info
  file: ~/.conan/conan.log
  audit: true

security:
  command_blacklist:
    - '.*\|\s*bash.*'
  local_file_whitelist:
    - README.md

skills:
  enabled: true

subagents:
  enabled: true
  max_parallel: 3
  timeout_seconds: 120
```

Cluster config is merged in this order:

```text
clusters/base.yaml -> clusters/<cluster>/cluster.yaml -> node overrides
```

## Safety Model

Conan prefers specialized tools over raw shell execution:

1. It searches node tool metadata with `tool_search`.
2. It calls specialized tools with `call_tool` when possible.
3. It uses `exec` only when a shell command is explicitly requested or no specialized tool fits.

Risk controls include:

- Per-node command whitelist.
- Global command blacklist.
- Local file whitelist for local file writes.
- Model-assisted risk review for operations that need confirmation.
- Agent bearer token authentication.
- Agent rate limiting and audit logging.
- Optional agent TLS.

## Project Structure

```text
cmd/conan/              CLI entry point
cmd/conan-agent/        Managed-node agent entry point
internal/tui/           Bubble Tea terminal UI
internal/skills/        Skill install, registry, resolution, and skill_read tool
internal/fileref/       @file reference parsing and loading
internal/llm/           Anthropic and OpenAI-compatible clients
internal/mcp/           MCP client
internal/agent/         MCP server, file endpoints, and HTTP middleware
internal/tools/         Agent tools
internal/config/        Config loading and cluster inheritance
internal/security/      Whitelist, policy, risk review, and audit logging
internal/memory/        Memory rules, SQLite store, and memory tools
internal/subagent/      Local read-only subagent runner
internal/runbook/       Runbook draft and preview support
internal/evidence/      Evidence and incident report support
pkg/configschema/       Shared config structs
pkg/mcpproto/           Shared MCP JSON-RPC types
pkg/models/             Shared data models
```

## Build And Test

```bash
make build
make build-linux
make build-darwin
make test
```

Development helpers:

```bash
go test ./...
go run ./cmd/conan --help
go run ./cmd/conan-agent --help
```
