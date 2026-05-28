# Conan Project Audit - 2026-05-28

## Scope

This audit reviewed Conan's current runtime design and implementation across:

- TUI model selection/configuration and LLM request flow.
- OpenAI/Anthropic provider behavior.
- Agent/MCP tool naming, tool metadata, and security review paths.
- Local tools, memory tools, subagent tool allow rules, and node management commands.
- Regression tests covering the changed behavior.

## Findings And Fixes

### 1. OpenAI tool names were incompatible with stricter Chat Completions validation

**Problem:** Many exposed tools used slash-separated names such as `shell/run`, `fs/read`, and `local/fs/write`. Newer OpenAI chat models reject tool names that do not match `^[a-zA-Z0-9_-]+$`, causing requests to fail before the model runs.

**Fix:** Renamed runtime tool names to underscore form across agent tools, local tools, metadata, TUI dispatch, security review, and tests. Examples:

- `shell/run` -> `shell_run`
- `fs/read` -> `fs_read`
- `svc/status` -> `svc_status`
- `local/fs/write` -> `local_fs_write`

Added a regression test that checks exposed TUI/local/memory/skill tool definitions against the OpenAI tool-name pattern.

### 2. Legacy slash names could still break existing configs and resumed conversations

**Problem:** Existing `disabled_tools` configs or saved conversation tool-call history may still contain slash tool names. Without compatibility handling, old configs would silently fail to disable tools, and resumed OpenAI conversations could still emit invalid tool-call names in request history.

**Fix:** Added slash-to-underscore normalization in:

- Tool registry lookup/disable paths.
- OpenAI provider message/tool serialization for legacy tool names.

Current exposed names remain underscore-only; normalization is only compatibility glue.

### 3. LLM HTTP requests had no setup timeout

**Problem:** OpenAI and Anthropic providers used `http.DefaultClient`. TUI streaming had an idle event timeout after a stream exists, but connection/TLS/response-header setup could hang indefinitely.

**Fix:** Added a dedicated default LLM HTTP client with dial, TLS handshake, and response-header timeouts. It intentionally leaves `Client.Timeout` unset so long streaming responses are not cut off after a fixed wall-clock duration.

### 4. Editing `default_model` in `/config` did not switch the active provider

**Problem:** The TUI config screen applied `default_model` by changing only the displayed model name. The active provider stayed on the previous model, so subsequent requests could use the wrong backend.

**Fix:** Runtime global-config application now refreshes model configs and rebuilds the active provider when `default_model` changes. Added a regression test for this path.

### 5. System prompt used ambiguous local tool shorthand

**Problem:** The system prompt said `local_fs_read, list, and stat`, which could lead the model to call nonexistent tools named `list` or `stat`.

**Fix:** Reworded the prompt to list exact tool names: `local_fs_read`, `local_fs_list`, `local_fs_stat`, `local_fs_write`, `local_fs_patch`, and `local_fs_delete`.

### 6. `/model` without an argument should present a model selector

**Problem:** `/model` without a name only showed the current model. This did not match the expected interactive TUI behavior.

**Fix:** Added a model selector mode for `/model`, wired configured models into the TUI, and made selection rebuild the active provider.

## Validation

Commands run:

```bash
go test ./...
rg -n '"(shell|fs|sys|svc|log|net|web|docker|k8s|pkg|cron|local)/[a-z_]+"|Name\(\) string.*"[^"]*/[^"]*"|Name: "[^"]*/[^"]*"' internal cmd pkg
```

Result:

- `go test ./...` passes.
- The slash-name search has no runtime tool-definition hits. Remaining matches are imports, HTTP strings, or explicit legacy-normalization regression test fixtures.

## Operational Notes

- Existing deployed `conan-agent` processes must be updated/restarted before they expose the new underscore tool names.
- Existing configs that still use old slash names in `disabled_tools` continue to work through normalization, but should be migrated to underscore names when touched.
- Historical plans/specs under `docs/superpowers/` still mention older slash names as historical design records; runtime code and current tests now use underscore names.
