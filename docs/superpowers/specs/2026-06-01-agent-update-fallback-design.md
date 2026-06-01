# Agent Update Fallback Design

## Overview

Add an agent-interface fallback for `conan node update`. The command should keep SSH/SFTP as the default update path because it can recover nodes even when the running agent is unhealthy. When SSH cannot complete, Conan should optionally update through the already-running `conan-agent` HTTP/MCP interface.

The default mode is `auto`: try SSH first, then fall back to the agent update tool if SSH fails. Operators can force one path with `--mode ssh` or `--mode agent`.

## Goals

- Preserve the current SSH update behavior as the first choice.
- Add an agent-side update tool that can install a new binary, config, and systemd unit from an authenticated agent call.
- Make `conan node update` succeed when SSH is unavailable but the agent HTTP endpoint is reachable.
- Return clear error context when both SSH and agent fallback fail.
- Keep node selection, credential loading, local artifact rendering, and output shape compatible with current commands.

## Non-goals

- Replacing SSH deployment for first-time `node add`.
- Updating an agent that is stopped or unreachable over HTTP.
- Supporting non-systemd service managers.
- Implementing release download logic inside the remote agent.
- Adding rollback for partially applied remote changes.

## Command UX

`conan node update` gains a mode flag:

```bash
conan node update web-1 --cluster prod --mode auto
conan node update web-1 --cluster prod --mode ssh
conan node update web-1 --cluster prod --mode agent
```

Modes:

- `auto`: default. Try SSH/SFTP first. If that update fails, call the remote agent update tool.
- `ssh`: use only the existing SSH/SFTP deployer. This is the compatibility mode for operators who do not want agent self-update.
- `agent`: use only the agent update tool. This mode does not prompt for SSH credentials.

Existing targeting flags continue to work: positional node selector, `--all`, `--all-cluster`, `--cluster`, `--agent-bin`, `--user`, `--password`, and `--ssh-port`.

## Agent Update Tool

Add a new MCP tool named `agent_update`, registered by `conan-agent` and disableable through `disabled_tools`.

Input shape:

```json
{
  "binary": "<optional base64 conan-agent binary override>",
  "binaries": {
    "amd64": "<base64 conan-agent linux amd64 binary>",
    "arm64": "<base64 conan-agent linux arm64 binary>"
  },
  "config": "<agent config yaml>",
  "systemd_unit": "<systemd unit text>",
  "remote_binary_path": "/usr/local/bin/conan-agent",
  "remote_config_path": "/etc/conan-agent/config.yaml",
  "systemd_unit_path": "/etc/systemd/system/conan-agent.service"
}
```

Behavior:

1. Select the binary payload. Use `binary` when present; otherwise choose from `binaries` using the agent process architecture.
2. Decode the selected binary payload and reject empty, invalid, or unsupported architecture data.
3. Write the binary, config, and unit to unique temporary files.
4. Install the files to the requested target paths with the expected permissions:
   - binary: `0755`
   - config: `0600`
   - systemd unit: `0644`
5. Run `systemctl daemon-reload`, `systemctl enable --now conan-agent`, and `systemctl restart conan-agent`.
6. Return a text result that identifies the installed binary path and selected architecture.

The tool may return before the restarted HTTP server is reachable because the process restarts itself. The CLI verifies health after the call with a short retry window.

## Security

`agent_update` is a high-impact mutating operation and should be classified as destructive in tool metadata. It is protected by the existing bearer token middleware and can be disabled with:

```yaml
disabled_tools:
  - agent_update
```

The tool should not accept shell fragments. It receives concrete file contents and paths, writes temporary files, and executes a fixed command sequence. Error messages must not include bearer tokens or SSH passwords.

## CLI And Service Flow

`internal/nodeupdate.Service` should gain:

- an update mode field in `nodeupdate.Request`;
- an `AgentUpdater` dependency for agent-interface updates;
- fallback logic that is explicit and testable.

Flow for `auto`:

1. Select target nodes as today.
2. Attempt the existing SSH deploy flow.
3. If SSH succeeds, save credentials as today and do not call the agent updater.
4. If SSH fails, build the same deployment artifacts locally and call `agent_update` through the node's configured agent URL and token.
5. If agent update succeeds, return the normal update result.
6. If agent update fails, return both errors, for example:

```text
ssh update failed: <ssh error>; agent update fallback failed: <agent error>
```

Flow for `ssh`:

1. Run only the existing SSH deploy flow.
2. Preserve current prompting, retry-on-auth-failure, and credential storage behavior.

Flow for `agent`:

1. Skip SSH credential lookup and prompts.
2. Build local artifacts for the node and call `agent_update`.
3. Verify the agent health endpoint if the update call returns.

## Artifact Handling

The CLI side remains responsible for loading local update artifacts from the existing deploy configuration:

- If `--agent-bin` is provided, send it as the single `binary` override.
- If `--agent-bin` is omitted, read `agent_deploy.binaries.amd64` and `agent_deploy.binaries.arm64` and send them in the `binaries` map.

SSH mode keeps the existing `uname -m` remote architecture detection. Agent mode does not call `shell_run` for architecture detection; `agent_update` uses the running agent's Go architecture to choose from the provided binary map.

## Testing

Service-level tests:

- `auto` uses SSH and does not call agent when SSH succeeds.
- `auto` calls agent when SSH fails.
- `auto` returns both SSH and agent errors when both paths fail.
- `ssh` does not call agent.
- `agent` does not prompt for SSH credentials.

Agent tool tests:

- rejects invalid base64 or empty binary data.
- writes files with expected permissions through a fake installer/executor boundary.
- returns clear command errors without exposing secrets.

CLI tests:

- `node update --help` includes `--mode`.
- invalid modes are rejected.
- `--mode agent` can be passed with existing selectors.

Docs:

- Update README and README.zh-CN node update sections to describe `auto`, `ssh`, and `agent`.
