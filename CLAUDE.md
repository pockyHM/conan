# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Conan is a TUI-based AI operations assistant CLI (similar to Claude Code) for multi-cluster, multi-server management. It consists of two binaries: `conan` (CLI) and `conan-agent` (persistent daemon on managed nodes).

## Language & Dependencies

- **Go** — single module, no workspace
- **TUI:** charmbracelet/bubbletea + lipgloss (styling) + glamour (Markdown rendering)
- **Storage:** SQLite (go-mattn/sqlite3 or modernc.org/sqlite for pure Go)
- **Protocol:** MCP JSON-RPC over HTTP/SSE between CLI and Agent

## Build

```bash
make build            # Build both binaries to ./bin/
make build-linux      # Cross-compile Linux amd64/arm64
make build-darwin     # Cross-compile macOS amd64/arm64
```

## Architecture

Single Go module with visibility managed by `internal/` and `pkg/`:

- `cmd/conan/` — CLI entry point
- `cmd/conan-agent/` — Agent entry point
- `internal/tui/` — CLI only: Bubble Tea TUI
- `internal/llm/` — CLI only: Anthropic + OpenAI-compatible LLM client
- `internal/memory/` — CLI only: SQLite + Markdown memory system
- `internal/security/` — CLI only: whitelist pre-check + model risk assessment
- `internal/mcp/` — CLI only: MCP client connecting to remote Agents
- `internal/conversation/` — CLI only: context & message management
- `internal/agent/` — Agent only: MCP Server implementation
- `internal/tools/` — Agent only: tool implementations (shell, fs, svc, sys, log, net, k8s, pkg, cron, docker)
- `internal/config/` — Shared: config loading with inheritance (base → cluster → node)
- `pkg/mcpproto/` — Shared: MCP JSON-RPC type definitions
- `pkg/configschema/` — Shared: config struct definitions
- `pkg/models/` — Shared: data models

## Key Design Decisions

- **Communication:** HTTP/SSE with MCP JSON-RPC, not gRPC or raw TCP
- **Node status:** Pull model — CLI pings on demand, no heartbeat push from Agents
- **Security:** Two-stage — whitelist prefix-match first, then LLM risk assessment (allow/confirm/deny)
- **Memory:** Two layers — MEMORY.md + rules/*.md for behavioral rules (always in prompt), SQLite for operational knowledge/experience (retrieved by relevance)
- **Multi-node:** `/nodes` opens interactive multi-select UI; tool calls fan out concurrently, results aggregated per-node
- **LLM providers:** `anthropic` type (Messages API) and `openai` type (Chat Completions API), both with configurable endpoints
- **Config inheritance:** `base.yaml` → `cluster.yaml` → per-node overrides, deep merge (maps merge, arrays overwrite)

## Implementation Progress

### Phase 1: Foundation & Agent — DONE

Shared types, agent binary with all 44 tools, HTTP server with JSON-RPC routing.

- `pkg/mcpproto/` — JSON-RPC 2.0 + MCP tool types
- `pkg/configschema/` — Agent + CLI config structs with YAML tags
- `pkg/models/` — Conversation, Message, Memory, AuditEntry, NodeStatus
- `internal/tools/` — 44 tools in 10 categories (shell, fs, sys, svc, log, net, k8s, pkg, cron, docker)
- `internal/agent/` — HTTP server, JSON-RPC handler, auth/audit/rate-limit middleware
- `cmd/conan-agent/` — Cobra CLI with `run` subcommand, config loading, graceful shutdown
- `configs/example/agent-config.yaml` — Example configuration

### Phase 2: CLI Core — TODO

LLM client, MCP client, config loading with inheritance, conversation manager.

### Phase 3: TUI & Features — TODO

Bubble Tea TUI, node selector, slash commands, memory system, security review, session resume.

## Design Spec

Full design document: `docs/superpowers/specs/2026-05-19-conan-ops-agent-design.md`
Implementation plan: `docs/superpowers/plans/2026-05-19-foundation-agent.md`
