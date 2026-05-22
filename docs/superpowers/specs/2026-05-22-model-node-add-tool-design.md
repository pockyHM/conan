# Model Node Add Tool Design

## Overview

Expose node addition and agent deployment to the model as a short-lived TUI capability. The model should be able to call a `node_add` tool that reuses the existing `internal/nodeadd.Service.Add` flow: write node configuration, deploy `conan-agent` over SSH, and verify the health endpoint.

The tool must not be part of the default model tool set. It is exposed only after the user explicitly enters `/node` in the TUI. This keeps high-impact node management out of normal diagnostic conversations.

## Goals

- Let the model add a node and deploy `conan-agent` through a tool call.
- Require explicit `/node` activation before the model can see node management tools.
- Keep `/node` activation short-lived; expose `node_add` for the next model response only.
- Reuse the existing node add service instead of duplicating deployment logic.
- Preserve the current security review and confirmation path for deployment.
- Refresh TUI node state after a successful add so the model can use the new node in later turns.
- Prevent SSH passwords and generated tokens from leaking into chat history, debug logs, audit logs, or visible placeholders.

## Non-goals

- Exposing `node_add` permanently.
- Adding node management tools to remote `conan-agent`.
- Creating a separate non-interactive provisioning system.
- Changing the existing `conan node add` CLI behavior.
- Adding multiple node management tools in the first implementation.

## User Experience

The default TUI model request does not include `node_add`.

When the user types:

```text
/node
```

the TUI enables node management tools for the next model response and shows a status message:

```text
Node management enabled for next model response
```

The user can then ask naturally:

```text
Add 10.0.0.12 to prod and deploy the agent as deploy.
```

Only this next model response receives the `node_add` tool definition. After that response completes, fails, is interrupted, or the pending operation is cancelled, node management tools are disabled again. To add another node, the user runs `/node` again.

The user can also disable the temporary exposure before using it:

```text
/node off
```

## Tool Definition

Add a TUI meta tool named `node_add`.

Input schema:

```json
{
  "type": "object",
  "properties": {
    "cluster": {
      "type": "string",
      "description": "Cluster name. Omit to use the current TUI cluster."
    },
    "host": {
      "type": "string",
      "description": "Hostname or IP address to add."
    },
    "name": {
      "type": "string",
      "description": "Node name override. Defaults to host."
    },
    "user": {
      "type": "string",
      "description": "SSH username. Omit to prompt or use saved credentials."
    },
    "password": {
      "type": "string",
      "description": "SSH password. Omit to prompt or use saved credentials."
    },
    "ssh_port": {
      "type": "integer",
      "description": "SSH port. Defaults to cluster node_defaults.ssh_port, then 22."
    },
    "agent_port": {
      "type": "integer",
      "description": "conan-agent listen port. Defaults to 9280."
    },
    "agent_bin": {
      "type": "string",
      "description": "Local conan-agent binary override for this deployment."
    },
    "update": {
      "type": "boolean",
      "description": "Update an existing node instead of failing on duplicate name."
    },
    "rotate_token": {
      "type": "boolean",
      "description": "Generate a new per-node agent token while updating."
    }
  },
  "required": ["host"]
}
```

The description should make the high-impact behavior explicit: this tool writes local cluster config, deploys or updates a remote agent over SSH, and performs a health check.

## Architecture

### TUI activation state

Add a short-lived flag to `tui.Model`, for example:

```go
nodeToolsEnabled bool
```

`availableToolDefs()` appends `node_add` only when this flag is true.

`/node` command handling sets the flag to true. `/node off` clears it. The flag is cleared automatically when the next model response reaches a terminal state, including success, stream error, interruption, denied confirmation, or cancelled confirmation.

### Tool dispatch

Extend `dispatchTool()` with:

```go
case metaToolNodeAdd:
    return m.dispatchNodeAdd(streamID, call)
```

`dispatchNodeAdd()` parses arguments, fills defaults from the current cluster and global config, then calls `nodeadd.Service.Add`.

The dispatcher should build the service from existing production dependencies:

- `credentials.NewStore(loader.Home())`
- TUI-compatible prompter for missing username, password, or unresolved host IP
- `nodeadd.NetResolver{}`
- `nodeadd.ConfigNodeWriter{Home: loader.Home()}`
- `deploy.NewNativeDeployer()`
- `nodeadd.MCPHealthChecker{}`

If `cluster` is omitted, use the current TUI cluster. If the selected cluster is empty, fall back to global default cluster, then `default`, matching the CLI behavior.

### State refresh

After a successful add or update, reload the cluster from config and rebuild:

- `m.clients`
- `m.nodes`
- node whitelists used by the reviewer
- selected node state, preserving existing selections and selecting the newly added node
- tool cache for the new node

The TUI should fetch tools from the new agent after the health check passes so the model can use specialized tools on the newly added node in later turns.

## Security

`/node` only exposes the tool to the model. It does not approve execution.

`node_add` remains a mutating, high-impact tool and must go through the normal risk review path. It must not be added to the read-only tool list. With no risk provider configured, the existing default-confirm behavior is acceptable.

The confirmation view should show a sanitized summary:

- cluster
- host
- node name, if provided
- SSH username, if provided
- SSH port
- agent port
- update
- rotate token
- agent binary override, if provided

It must not show the SSH password or generated agent token.

Audit and debug logs should also sanitize `node_add` arguments. The conversation history should store a redacted argument string for the tool call if a password was provided.

## Error Handling

- Missing `host`: return a tool error explaining that host is required.
- Missing or invalid cluster: return the loader error.
- Duplicate node without `update`: return the existing `nodeadd.Service` error.
- Missing username/password: prompt through the TUI prompter or use saved credentials.
- Unresolved hostname: prompt for an IP address, preserving current node add behavior.
- Deployment failure: return the deploy error; local config may remain written, matching existing CLI behavior.
- Health check failure: return the health error and state that the local config was written.
- Stream interruption or user cancellation: stop waiting on the operation context and clear node tool exposure.

## Testing

Add focused tests for:

- `/node` enables `node_add` for `availableToolDefs()`.
- `node_add` is absent by default.
- `/node off` disables the tool.
- node tool exposure clears after the next model response completes.
- `node_add` is not treated as read-only by the reviewer.
- `dispatchNodeAdd()` maps model arguments to `nodeadd.Request`.
- successful `node_add` refreshes clients, node infos, selected nodes, and tool cache.
- password redaction in tool placeholders, conversation entries, audit payloads, and debug payloads.
- duplicate node and deploy failure return visible tool errors.

## Implementation Notes

The existing `cliPrompter` lives in `cmd/conan`, while TUI dispatch lives in `internal/tui`. The implementation should introduce an internal prompter abstraction that operates safely inside Bubble Tea, rather than importing command code into TUI internals.

When `nodeadd.Service` needs a username, password, or unresolved-host IP address, `dispatchNodeAdd()` should pause the operation and surface a dedicated TUI prompt. Prompt responses are passed directly back to the service and are not added to the model transcript. Password input is masked on screen and redacted anywhere the pending tool call is rendered, logged, audited, or stored in conversation history.
