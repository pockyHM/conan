# Node Add Agent Deploy Design

## Overview

Add `conan node add` to register a node in a cluster and, by default, deploy or update `conan-agent` on that node over SSH. The command should make the common path a single operation: resolve the node address, collect SSH credentials, write cluster node configuration, install/update the agent as a systemd service, and verify the agent health endpoint.

The first implementation supports Go-native SSH/SFTP with password authentication, systemd installation, encrypted local credential storage, and per-node agent tokens.

## Goals

- Add `conan node add --cluster <cluster> <hostname-or-ip>`.
- Use the input as the default node name and host.
- Prompt interactively for missing SSH username or password.
- If a hostname cannot be resolved, prompt for an IP address and use the hostname as the node name.
- Write or update `clusters/<cluster>/nodes.yaml`.
- Deploy or update `conan-agent` over Go-native SSH/SFTP by default.
- Install the remote agent as a systemd service.
- Store SSH credentials encrypted locally so later updates do not require re-entry.
- Generate a unique random token per node and configure the CLI and agent to use it.
- Provide `--no-deploy` for configuration-only changes.

## Non-goals

- SSH key authentication in the first version.
- Non-systemd service managers in the first version.
- TLS certificate provisioning for agent HTTP in the first version.
- Automatic firewall configuration.
- Rollback of remote system changes after partial deploy failure.
- Cross-cluster node name disambiguation; commands still operate within the selected cluster.

## Command UX

Add a singular `node` command namespace with an `add` subcommand:

```bash
conan node add --cluster prod <hostname-or-ip> -u <user> -p <password>
```

Flags:

- `--cluster, -c`: existing persistent cluster flag; required if no default cluster is configured.
- `--user, -u`: SSH username. If omitted, prompt interactively.
- `--password, -p`: SSH password. If omitted, prompt interactively with hidden input.
- `--ssh-port`: SSH port. Default comes from `node_defaults.ssh_port`, then `22`.
- `--port`: agent listen port. Default is `9200`.
- `--name`: explicit node name override.
- `--agent-bin`: local `conan-agent` binary path override for this run.
- `--no-deploy`: only write node configuration.
- `--update`: update an existing node and redeploy it instead of failing on duplicate name.
- `--rotate-token`: generate a new node token when updating.

Name and host behavior:

- If the positional argument is an IP address, use it as both `name` and `host` unless `--name` is provided.
- If it is a hostname and DNS resolves, use it as both `name` and `host` unless `--name` is provided.
- If it is a hostname and DNS does not resolve, prompt for an IP address. The default `name` remains the hostname and `host` becomes the entered IP.

Duplicate behavior:

- If the node name already exists in the selected cluster, fail unless `--update` is set.
- With `--update`, update `host`, `agent.user`, and `agent.port`, then redeploy unless `--no-deploy` is set.
- With `--update`, preserve the existing node token unless `--rotate-token` is set.

## Config schema

### Global deploy configuration

Extend global config with `agent_deploy`:

```yaml
agent_deploy:
  binaries:
    amd64: ~/.conan/agent/amd64/conan-agent
    arm64: ~/.conan/agent/arm64/conan-agent
  remote_binary_path: /usr/local/bin/conan-agent
  remote_config_path: /etc/conan-agent/config.yaml
  systemd_unit_path: /etc/systemd/system/conan-agent.service
```

Defaults:

- `binaries.amd64`: `$HOME/.conan/agent/amd64/conan-agent`
- `binaries.arm64`: `$HOME/.conan/agent/arm64/conan-agent`
- `remote_binary_path`: `/usr/local/bin/conan-agent`
- `remote_config_path`: `/etc/conan-agent/config.yaml`
- `systemd_unit_path`: `/etc/systemd/system/conan-agent.service`

The binary paths are global because agent artifacts are shared across clusters.

### Node token override

Extend node-level agent override with `token`:

```yaml
nodes:
  - name: web-1
    host: 10.0.0.11
    agent:
      user: deploy
      port: 9200
      token: <random-node-token>
```

Effective token precedence:

1. `node.agent.token`
2. `cluster.agent.token`
3. built-in default

New nodes created by `node add` always receive a generated node token. This avoids using the current built-in `changeme` default and limits a token leak to one agent.

## Credential storage

Store SSH credentials locally in encrypted files under Conan home:

```text
~/.conan/credentials.key
~/.conan/credentials.enc
```

Rules:

- `credentials.key` is generated automatically if missing.
- `credentials.key` permissions are `0600`.
- `credentials.enc` permissions are `0600`.
- Encryption uses AES-GCM.
- The encrypted payload is JSON.
- Credential records are keyed by `ssh/<cluster>/<node>`.

Credential record shape:

```json
{
  "username": "deploy",
  "password": "..."
}
```

Flow:

1. For deploy/update, first look for `ssh/<cluster>/<node>`.
2. If found, try SSH with the stored credentials.
3. If missing or authentication fails, prompt for username/password.
4. After a successful SSH authentication, save the credentials encrypted.
5. Never write passwords to `nodes.yaml`, logs, command output, or errors.

## Deploy flow

Default `node add` performs configuration and deployment.

1. Resolve the requested node name and host.
2. Load global config and the selected cluster.
3. Create or update the node entry in `nodes.yaml`.
4. Generate a per-node token for new nodes, or preserve/rotate the existing token for updates.
5. Resolve SSH credentials from encrypted storage or prompts.
6. Open a Go-native SSH connection using password authentication.
7. Run `uname -m` remotely and map architecture:
   - `x86_64`, `amd64` -> `amd64`
   - `aarch64`, `arm64` -> `arm64`
8. Select the local agent binary:
   - `--agent-bin` if provided
   - otherwise `agent_deploy.binaries.<arch>`
9. Upload the binary, generated agent config, and generated systemd unit to temporary remote paths with SFTP.
10. Use sudo on the remote host to install files and restart the service.
11. Verify `GET /health` through the existing MCP client.

Remote install commands are equivalent to:

```bash
install -m 0755 /tmp/conan-agent.<pid> /usr/local/bin/conan-agent
mkdir -p /etc/conan-agent
install -m 0600 /tmp/conan-agent-config.<pid> /etc/conan-agent/config.yaml
install -m 0644 /tmp/conan-agent.service.<pid> /etc/systemd/system/conan-agent.service
systemctl daemon-reload
systemctl enable --now conan-agent
systemctl restart conan-agent
```

The actual implementation should run these through a remote executor abstraction and pass the password to sudo only through stdin. Passwords must not appear in command strings.

Generated agent config:

```yaml
listen: 0.0.0.0:9200
token: <node-token>
tls: false
rate_limit: 10
log_level: info
```

The listen port comes from `--port` or the default. Later versions can expose more agent config fields.

Generated systemd unit:

```ini
[Unit]
Description=Conan Agent
After=network-online.target

[Service]
ExecStart=/usr/local/bin/conan-agent run -c /etc/conan-agent/config.yaml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

If global deploy config customizes remote paths, the generated unit must use those paths.

## Error handling

- Missing cluster: return an error; do not create a cluster implicitly.
- Hostname cannot resolve: prompt for IP address.
- Duplicate node without `--update`: return an error.
- Unsupported remote architecture: return an error before uploading.
- Missing local agent binary: return an error with the expected path and mention `--agent-bin`.
- SSH authentication failure with saved credentials: prompt again and overwrite credentials after successful authentication.
- Sudo failure: return an error without printing the password.
- systemd failure: return an error and include remote stderr.
- Health check failure: return an error after deploy, but keep the node configuration so the user can fix the remote issue and rerun with `--update`.

The first implementation does not attempt full rollback. Local config changes remain after deploy failures.

## Components

### `cmd/conan`

- Add `node` command group and `node add` subcommand.
- Parse flags and perform interactive prompts.
- Keep command code thin by delegating to service types.

### `internal/config`

- Add a writer for `clusters/<cluster>/nodes.yaml`.
- Support append and update flows.
- Preserve stable YAML output.
- Extend `NodeAgentOverride` with `Token`.
- Update effective node agent config to prefer node token over cluster token.

### `internal/credentials`

- Own encrypted local credential storage.
- Expose `Get` and `Put` operations by logical key.
- Handle key creation, AES-GCM encryption, JSON serialization, and permissions.

### `internal/deploy`

- Own Go-native SSH/SFTP deployment.
- Provide interfaces that can be faked in tests.
- Responsibilities include architecture detection, upload, sudo install, systemd restart, and deploy artifact rendering.

### `internal/mcp`

- Reuse existing client health check after deploy.

## Testing

### Config tests

- Appending a node writes the expected `nodes.yaml`.
- Duplicate node without `--update` fails.
- `--update` updates host/user/port.
- `--update` preserves token by default.
- `--rotate-token` replaces token.
- Node-level token overrides cluster token in effective config.

### Credential tests

- Missing key file is created with `0600` permissions.
- Credentials save and load correctly.
- Encrypted file does not contain plaintext username/password.
- Corrupt encrypted file returns an error.

### Deploy tests

Use fake SSH/SFTP implementations rather than real network connections.

- Architecture mapping selects the expected local binary.
- Unsupported architecture fails before upload.
- Missing local binary fails before remote changes.
- Generated agent config contains the node token and selected port.
- Generated systemd unit uses configured remote paths.
- Remote command sequence installs files and restarts systemd.
- Passwords are not included in command strings.

### CLI tests

- Missing required positional argument fails.
- `--no-deploy` writes config and does not call deployer.
- Missing username/password uses prompts.
- Existing node requires `--update`.
- `--update` triggers deploy by default.
- Failed saved credentials prompt for new credentials.

## Security considerations

- Generate node tokens with cryptographically secure randomness.
- Do not log or print passwords or tokens.
- Store SSH credentials encrypted and restrict file permissions.
- Avoid putting passwords in process arguments or shell command strings.
- Verify SSH host keys against a Conan-managed known-hosts file at `~/.conan/known_hosts`.
- On first connection to a host, show the host key fingerprint and require interactive confirmation before storing it.
- In non-interactive mode, unknown host keys fail unless a future explicit trust flag is added.

## Implementation note

The existing agent has a config field named `audit_log`, but current middleware logs through `slog` rather than writing that file directly. This feature should not rely on remote `audit_log` behavior for deployment success.
