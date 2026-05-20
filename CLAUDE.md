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

### Phase 2: CLI Core — DONE

CLI config loader with cluster inheritance, MCP client, conversation manager, LLM provider interfaces, and minimal Cobra commands (`config validate`, `clusters`, `nodes`, `ping`, `tools list`).

### Phase 3A: TUI Shell & Slash Commands — DONE

Minimal Bubble Tea shell with header, conversation display, text input, slash command parsing, and `conan tui` command.

Plan: `docs/superpowers/plans/2026-05-19-tui-shell.md`

### Phase 3B: LLM Integration & Streaming — DONE

Anthropic + OpenAI providers, streaming text in TUI, agentic tool-call dispatch loop.

- `internal/llm/anthropic.go` — Anthropic Messages API provider (Chat + ChatStream)
- `internal/llm/openai.go` — OpenAI Chat Completions API provider (Chat + ChatStream)
- `internal/llm/sse.go` — Shared SSE stream reader
- `internal/llm/factory.go` — Provider factory (model name → Provider)
- `pkg/models/models.go` — Added ToolCallID to Message
- `internal/conversation/conversation.go` — Added AddToolCall, AddToolResult methods
- `internal/tui/model.go` — Refactored with streaming display + tool dispatch loop
- `cmd/conan/main.go` — Wired providers, MCP clients, tools into TUI

Plan: `docs/superpowers/plans/2026-05-20-llm-streaming-tui.md`

### Phase 3C: Node Selector & Multi-node — DONE

Interactive `/nodes` multi-select, concurrent multi-node tool dispatch, tree-style result visualization.

- `internal/tui/nodeselector.go` — Interactive multi-select node overlay component
- `internal/tui/model.go` — Mode switching, node state tracking, async ping, concurrent multi-node dispatch, tree visualization
- `cmd/conan/main.go` — Node info wiring from cluster config to TUI

Plan: `docs/superpowers/plans/2026-05-20-node-selector.md`

### Phase 3D: Security Review — DONE

Two-stage security pipeline (whitelist + model risk assessment) with TUI confirmation prompts.

- `internal/security/whitelist.go` — Command whitelist with prefix matching
- `internal/security/riskprompt.go` — Risk assessment prompt builder and JSON response parser
- `internal/security/reviewer.go` — Two-stage reviewer (whitelist bypass → model assessment) with session cache
- `internal/tui/model.go` — Confirmation mode, security gate in tool dispatch flow
- `cmd/conan/main.go` — Wired Reviewer from config into TUI

Plan: `docs/superpowers/plans/2026-05-20-security-review.md`

### Phase 3E: Memory & Session — NEXT

SQLite memory store, MEMORY.md rules, session archive and `/resume`.

## Design Spec

Full design document: `docs/superpowers/specs/2026-05-19-conan-ops-agent-design.md`
Implementation plan: `docs/superpowers/plans/2026-05-19-foundation-agent.md`
