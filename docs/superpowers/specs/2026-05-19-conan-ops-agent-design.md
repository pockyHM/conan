# Conan — AI-Powered Operations CLI Agent

## Overview

Conan is a TUI-based AI operations assistant CLI, similar to Claude Code but designed for multi-cluster, multi-server operations scenarios. It consists of a local CLI and remote Agents that run as persistent daemons on each managed node, exposing operations capabilities via the MCP protocol.

**Tech Stack:** Go, Bubble Tea (TUI), SQLite, MCP JSON-RPC over HTTP/SSE

---

## Architecture

Three-layer architecture:

```
┌─────────────────────────────────┐
│  CLI (conan)                    │
│  TUI + LLM Client + Memory     │
│  Tool Dispatch + Security + Config │
└──────────┬──────────────────────┘
           │ HTTP/SSE (MCP JSON-RPC)
┌──────────▼──────────────────────┐
│  Agent (conan-agent)            │
│  MCP Server + Tool Engine       │
│  Persistent daemon per node     │
└──────────┬──────────────────────┘
           │ shell / API
┌──────────▼──────────────────────┐
│  Target System                  │
│  Linux / K8s / Docker / ...     │
└─────────────────────────────────┘
```

---

## Project Structure

```
conan/
├── cmd/
│   ├── conan/              # CLI entry point
│   │   └── main.go
│   └── conan-agent/        # Agent entry point
│       └── main.go
├── internal/
│   ├── tui/                # TUI (bubbletea + lipgloss + glamour)
│   ├── llm/                # LLM client (Anthropic + OpenAI compatible)
│   ├── memory/             # Memory management (SQLite + Markdown)
│   ├── config/             # Config loading (~/.conan/clusters/)
│   ├── security/           # Security review (whitelist + model risk assessment)
│   ├── mcp/                # MCP client (connect to remote Agents)
│   ├── agent/              # Agent-only: MCP Server + tool engine
│   ├── tools/              # Tool implementations (10 categories)
│   └── conversation/       # Conversation context & message management
├── pkg/
│   ├── mcpproto/           # MCP protocol definitions (JSON-RPC types)
│   ├── configschema/       # Config struct definitions
│   └── models/             # Shared data models
├── configs/
│   └── example/            # Example config files
├── docs/
│   └── superpowers/specs/
├── Makefile
├── go.mod
└── go.sum
```

Single Go module. CLI and Agent share `pkg/` and have their own code under `internal/`. `internal/agent/` + `internal/tools/` are only imported by `conan-agent`; `internal/tui/` + `internal/llm/` only by `conan` CLI.

---

## Agent (conan-agent)

### Deployment

- **Install:** `conan agent install` generates systemd unit file and enables the service
- **Config:** `/etc/conan-agent/config.yaml` — listen port, TLS certs, auth token, allowed tools
- **Lifecycle:** systemd managed, supports config reload via SIGHUP, graceful shutdown on SIGTERM (waits for in-flight tool calls)

### MCP Server Interface

Agent exposes MCP tools over HTTP/SSE:

| Category | Tools | Description |
|----------|-------|-------------|
| Command Execution | `shell/run` | Execute shell commands with timeout |
| File Operations | `fs/read`, `fs/write`, `fs/edit`, `fs/list`, `fs/stat`, `fs/download`, `fs/upload` | Read, edit, upload, download files |
| Service Management | `svc/list`, `svc/status`, `svc/start`, `svc/stop`, `svc/restart` | systemd service management |
| Resource Monitoring | `sys/cpu`, `sys/mem`, `sys/disk`, `sys/net`, `sys/processes` | CPU, memory, disk, network, processes |
| Log Viewing | `log/read`, `log/stream`, `log/journalctl` | Read logs, SSE streaming, journalctl |
| Network Diagnostics | `net/ping`, `net/traceroute`, `net/portcheck`, `net/curl` | Ping, trace, port check, HTTP requests |
| K8s Operations | `k8s/pods`, `k8s/logs`, `k8s/events`, `k8s/describe`, `k8s/apply`, `k8s/delete` | Kubernetes management |
| Package Management | `pkg/install`, `pkg/update`, `pkg/list`, `pkg/search` | apt/yum/brew package operations |
| Cron Jobs | `cron/list`, `cron/add`, `cron/remove`, `cron/show` | Crontab management |
| Docker | `docker/ps`, `docker/logs`, `docker/run`, `docker/exec`, `docker/compose`, `docker/images` | Container management |

### Agent-Side Security

- **Token auth:** JWT token verification on every request from CLI
- **Tool whitelist:** Admin can disable specific tools per node (e.g., disable `pkg/install` on production)
- **Audit logging:** All tool calls logged to `/var/log/conan-agent/audit.log`
- **Rate limiting:** Configurable max calls per second per connection

### Node Status (Pull Model)

CLI pulls node status on demand — no heartbeat push:

- **On startup:** CLI pings all configured nodes concurrently, marks online/offline
- **Pre-tool-call check:** Ping target node before dispatching, avoid calls to offline nodes
- **Session cache:** Node status cached in-memory with 60s TTL, re-probe on expiry
- **Passive detection:** Connection failures during tool calls immediately mark node offline

---

## CLI (conan)

### Main Flow

```
User input → Conversation Manager → LLM call
                                    ↓
                              Tool call intent?
                              ├── No → Markdown render output
                              └── Yes → Security review
                                        ├── Deny → Show rejection reason
                                        ├── Allow → Select target nodes → Call Agent MCP
                                        └── Confirm → Show risk → User confirms → Call Agent MCP
                                                                    ↓
                                                              LLM continues reasoning
                                                              ↓
                                                         Final response rendered
```

### TUI Layout

Based on Bubble Tea + Lipgloss (styling) + Glamour (Markdown rendering):

```
┌──────────────────────────────────────┐
│  Header Status Bar                   │
│  Cluster: production | Nodes: 3      │
│  Model: claude-sonnet | Memory: 42   │
├──────────────────────────────────────┤
│                                      │
│  Conversation Area                   │
│  Markdown rendered, tool calls       │
│  collapsed with expand on keypress   │
│                                      │
├──────────────────────────────────────┤
│  > User Input Area                   │
│  Multi-line (Ctrl+Enter to submit)   │
└──────────────────────────────────────┘
```

- **Tool call visualization:** Collapsed by default as `🔧 shell/run on node-01`, expand to see full command and output
- **Command palette:** `Ctrl+P` — switch node, cluster, model, etc.
- **Shortcuts:** `Ctrl+C` interrupt generation, `Ctrl+L` clear screen, `/` prefix for slash commands

### Slash Commands

```
/cluster [name]       Switch/display current cluster (single select)
/nodes                Open interactive node selector (multi-select with ↑↓ and Space, Enter to confirm)
/model [name]         Switch/display current model
/memory               View memory summary
/config               Edit configuration
/help                 Help information
/clear                Clear current conversation context
/resume               Open session history list
/resume [id]          Resume a specific historical session
/exit                 Exit
```

### `/nodes` Interactive Selection

```
┌─ Select Target Nodes ─────────────────────────────────────┐
│  ○ node-01  10.0.1.1  ● Online  CPU 45%  MEM 72%  L 0.52 │
│  ● node-02  10.0.1.2  ● Online  CPU 12%  MEM 38%  L 0.31 │
│  ● node-03  10.0.1.3  ● Online  CPU 67%  MEM 81%  L 1.24 │
│  ○ node-04  10.0.1.4  ○ Offline ---     ---     ---       │
├───────────────────────────────────────────────────────────┤
│  ↑↓ Move  Space Select  Enter Confirm                     │
└───────────────────────────────────────────────────────────┘
```

- Space toggles selection, Enter confirms and returns to conversation
- Offline nodes are unselectable (grayed out)
- CPU/MEM/Load data fetched concurrently via `sys/cpu`, `sys/mem` on open
- Header status bar always shows selected node count

### Multi-Node Execution

When multiple nodes are selected:

- Tool calls dispatched concurrently to all selected nodes, results aggregated
- Results grouped by node, abnormal nodes auto-highlighted (non-zero exit code, timeout, etc.)
- LLM receives aggregated results and can compare/analyze across nodes

```
🔧 shell/run on 3 nodes (node-01, node-02, node-03)
├── node-01 ✓  load average: 0.52, 0.48, 0.45
├── node-02 ✓  load average: 0.31, 0.29, 0.33
└── node-03 ✗  Connection timeout (5s)
```

### Conversation Context Management

- Full conversation history kept in memory for current session
- Token budget trimming when sending to LLM: system prompt + memory context + recent N turns + tool results
- `/clear` resets context but does not affect long-term memory
- `/resume` loads historical session from SQLite into context for continuation

---

## LLM Integration

### Provider Interface

```go
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error)
}

type ChatRequest struct {
    SystemPrompt string
    Messages     []Message
    Tools        []ToolDef
    MaxTokens    int
}

type ChatEvent interface {
    isChatEvent()
}

type TextDeltaEvent struct{ Delta string }
type ToolCallEvent struct{ Name, ID, Args string }
type StopEvent struct{ Reason string }
```

### Model Configuration

```yaml
# ~/.conan/config.yaml
default_model: claude-sonnet

models:
  - name: claude-sonnet
    type: anthropic
    endpoint: https://api.anthropic.com
    model: claude-sonnet-4-6
    api_key: ${ANTHROPIC_API_KEY}

  - name: deepseek-r1
    type: openai
    endpoint: https://api.deepseek.com
    model: deepseek-reasoner
    api_key: ${DEEPSEEK_API_KEY}

  - name: local-qwen
    type: openai
    endpoint: http://localhost:11434/v1
    model: qwen3:32b
```

- `type` determines request format: `anthropic` (Anthropic Messages API) or `openai` (OpenAI Chat Completions API)
- `api_key` supports `${ENV_VAR}` environment variable references
- Custom endpoints allow self-hosted models (Ollama, vLLM, etc.)

### System Prompt (English)

```
[Role] You are Conan, an AI operations assistant...
[Environment] Cluster: production, Nodes: node-01, node-02
[Available Tools] MCP tool list and descriptions
[Security Rules] Tool call safety policy
[Memory Context] Retrieved from long-term memory
```

All system prompts written in English for better model compatibility and reasoning quality.

---

## Security Review

Two-stage pipeline: whitelist pre-check → model risk assessment.

### Stage 1: Whitelist Pre-Check

```yaml
# ~/.conan/config.yaml
security:
  risk_assessment_model: claude-sonnet
  command_whitelist:
    - cat
    - ls
    - free
    - df
    - top -bn1
    - ps aux
    - uname -a
    - hostname
    - uptime
    - netstat -tlnp
    - ss -tlnp
    - ip addr
    - docker ps
    - kubectl get
```

- Prefix matching: `kubectl get` matches `kubectl get pods -n default`
- Whitelist hit → skip model assessment, execute directly

### Stage 2: Model Risk Assessment

Commands not in whitelist are sent to LLM for risk evaluation using a separate, lightweight prompt (not the conversation context):

```go
type RiskLevel int

const (
    RiskAllow   RiskLevel = iota  // Execute directly
    RiskConfirm                    // Show risk, wait for user confirmation
    RiskDeny                       // Refuse execution
)

type RiskAssessment struct {
    Level      RiskLevel
    Reason     string    // Risk explanation (English)
    Suggestion string    // Safer alternative (if any)
}
```

**Risk classification logic:**

- **Deny:** Destructive operations (`rm -rf /`, `iptables -F`, `DROP TABLE`, `reboot` on production masters, etc.)
- **Confirm:** Risky but reasonable operations (restart a service, modify config files, install packages)
- **Allow:** Low-risk routine operations

**Short-circuit:** If the same command was already assessed during this session, reuse the previous result without re-calling the LLM.

### Agent-Side Secondary Check

Agent's tool whitelist is an additional defense layer — even if CLI approves, Agent can still refuse (e.g., `pkg/install` disabled on production nodes).

---

## Memory Management

### MEMORY.md — Behavioral Rules Layer (LLM-managed)

```
~/.conan/memory/
├── MEMORY.md              # Index + core rules (always loaded, kept under ~100 lines)
├── rules/
│   ├── production.md      # Production-specific rules (loaded when connected to production cluster)
│   ├── security.md        # Security constraints (loaded when security topics arise)
│   └── workflow.md        # Workflow preferences (loaded when troubleshooting)
```

- **MEMORY.md always fully loaded** into System Prompt — contains index + core universal rules
- **rules/*.md loaded on demand** based on current cluster, topic, conversation context
- **LLM manages autonomously:** writes new rules, migrates detail rules to files when MEMORY.md exceeds threshold, updates rules when context changes
- Users influence through conversation ("always check disk IO first on this cluster" → LLM writes rule)

**MEMORY.md example:**

```markdown
## Core Rules
- Always check cluster health before any operation
- Production operations require step-by-step execution

## Rule Files
- [Production Rules](rules/production.md) — for production cluster
- [Security Rules](rules/security.md) — security constraints
- [Workflow Preferences](rules/workflow.md) — troubleshooting habits
```

### SQLite — Experience Knowledge Layer (LLM-managed)

```sql
CREATE TABLE memories (
    id          TEXT PRIMARY KEY,
    category    TEXT,        -- event / experience / troubleshooting / topology
    title       TEXT,
    content     TEXT,
    tags        TEXT,        -- JSON array
    source_conv TEXT,        -- source conversation id
    created_at  DATETIME,
    updated_at  DATETIME
);

CREATE TABLE conversations (
    id          TEXT PRIMARY KEY,
    cluster     TEXT,
    nodes       TEXT,        -- JSON array
    model       TEXT,
    created_at  DATETIME,
    updated_at  DATETIME,
    summary     TEXT,        -- LLM-generated session summary
    messages    TEXT         -- Full conversation JSON for resume
);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT REFERENCES conversations(id),
    role            TEXT,      -- user / assistant / tool
    content         TEXT,
    tool_name       TEXT,
    tool_input      TEXT,
    tool_output     TEXT,
    created_at      DATETIME
);

CREATE TABLE audit_log (
    id          TEXT PRIMARY KEY,
    node        TEXT,
    tool_name   TEXT,
    input       TEXT,
    risk_level  TEXT,         -- ALLOW / CONFIRM / DENY
    created_at  DATETIME
);
```

### LLM Memory Tools (built-in)

```
memory/save      Write new memory (category, title, content, tags)
memory/update    Update existing memory (by id)
memory/delete    Delete memory (by id)
memory/search    Search memories (LLM self-retrieval, e.g., recalling past troubleshooting)
```

### Prompt Injection Order

```
[System Prompt]
  → [MEMORY.md rules] (full, ~2K token budget)
  → [On-demand rules/*.md] (~2K token budget)
  → [SQLite memory retrieval] (relevance-ranked, ~4K token budget)
  → [Conversation context] (remaining budget)
```

### Retrieval Strategy

1. Before each user message, extract context keywords (cluster name, node names, topic)
2. Full-text search `memories` table by `category`, `tags`, `content`
3. Sort by `updated_at` and relevance, truncate to token budget
4. Inject into System Prompt `[Memory Context]` section

### Session Archive & Resume

- On session end or `/exit`, LLM generates summary, full conversation stored in `conversations` table
- `/resume` opens interactive session list:

```
┌─ Historical Sessions ────────────────────────────────────────┐
│  ▸ a3f2e1  2026-05-19 14:30  production cluster              │
│           Investigated node-03 memory leak, found cache...    │
│    b7c9d4  2026-05-19 10:15  staging cluster                 │
│           Deployed v2.3.1 to staging environment              │
│    e1a4f8  2026-05-18 16:42  production cluster               │
│           K8s pod CrashLoopBackOff investigation              │
├──────────────────────────────────────────────────────────────┤
│  ↑↓ Move  Enter Resume  Esc Cancel                           │
└──────────────────────────────────────────────────────────────┘
```

---

## Configuration Management

### Directory Structure

```
~/.conan/
├── config.yaml                    # Global config (models, security, memory)
├── conan.db                       # SQLite database
├── memory/                        # Memory system
├── clusters/                      # Cluster configs
│   ├── base.yaml                  # Base config (inherited by all clusters)
│   ├── production/
│   │   ├── cluster.yaml           # Cluster-level config
│   │   └── nodes.yaml             # Node list
│   ├── staging/
│   │   ├── cluster.yaml
│   │   └── nodes.yaml
│   └── bare-metal/
│       ├── cluster.yaml
│       └── nodes.yaml
├── conan.log                      # CLI log
└── audit.log                      # Audit log
```

### Global Config (`~/.conan/config.yaml`)

```yaml
default_model: claude-sonnet
default_cluster: production

models:
  - name: claude-sonnet
    type: anthropic
    endpoint: https://api.anthropic.com
    model: claude-sonnet-4-6
    api_key: ${ANTHROPIC_API_KEY}

  - name: deepseek-r1
    type: openai
    endpoint: https://api.deepseek.com
    model: deepseek-reasoner
    api_key: ${DEEPSEEK_API_KEY}

security:
  risk_assessment_model: claude-sonnet
  command_whitelist:
    - cat
    - ls
    - free
    - df
    - top -bn1
    - ps aux
    - uname -a
    - hostname
    - uptime
    - netstat -tlnp
    - ss -tlnp
    - docker ps
    - kubectl get

memory:
  rules_token_budget: 2000
  knowledge_token_budget: 4000

logging:
  level: info
  file: ~/.conan/conan.log
  audit: true
```

### Base Config (`clusters/base.yaml`)

```yaml
agent:
  port: 9200
  timeout: 30s
  tls: false

node_defaults:
  user: root
  ssh_port: 22
```

### Cluster Config (`clusters/production/cluster.yaml`)

```yaml
name: production
description: "Production K8s cluster"

inherits: base

agent:
  timeout: 60s
  tls: true
  ca_cert: /etc/conan/certs/ca.pem

node_defaults:
  user: deploy
```

### Node List (`clusters/production/nodes.yaml`)

```yaml
nodes:
  - name: master-01
    host: 10.0.1.1
    labels: [master, k8s, etcd]
    zone: us-east-1a

  - name: master-02
    host: 10.0.1.2
    labels: [master, k8s, etcd]
    zone: us-east-1b

  - name: worker-01
    host: 10.0.2.1
    labels: [worker, k8s]
    zone: us-east-1a

  - name: db-01
    host: 10.0.3.1
    labels: [database, mysql, bare-metal]
    agent:
      user: admin
      port: 9201

  - name: gateway-01
    host: 10.0.4.1
    labels: [gateway, nginx]
```

### Inheritance Rules

Priority (high to low):

```
Node-level config (agent field in individual node in nodes.yaml)
  > Cluster config (cluster.yaml)
    > Base config (base.yaml)
      > Built-in defaults
```

Deep merge for maps, arrays overwritten entirely (no merge).

---

## Agent Tool Definitions

### 1. shell/run

```json
{ "command": "free -h", "timeout": 30, "user": "root" }
→ { "exit_code": 0, "stdout": "...", "stderr": "", "timed_out": false }
```

### 2. File Operations

```json
// fs/read
{ "path": "/etc/nginx/nginx.conf", "offset": 0, "limit": 100 }
// fs/write
{ "path": "/etc/nginx/nginx.conf", "content": "...", "backup": true }
// fs/edit — precise edit, avoid overwriting entire file
{ "path": "/etc/nginx/nginx.conf", "old_text": "listen 80;", "new_text": "listen 443 ssl;" }
// fs/stat
{ "path": "/var/log/syslog" }
// fs/list
{ "path": "/var/log", "recursive": false }
// fs/download — Agent → CLI file transfer
{ "path": "/var/log/syslog" }
// fs/upload — CLI → Agent file transfer
{ "path": "/tmp/config.yaml", "content": "..." }
```

### 3. svc/* (systemd)

```json
{ "name": "nginx", "action": "status" }  // list/status/start/stop/restart
```

### 4. sys/* (Resource Monitoring)

```json
// sys/cpu → { "usage_percent": 45.2, "cores": 8, "load_avg": [0.52, 0.48, 0.45] }
// sys/mem → { "total": "32G", "used": "23G", "percent": 71.8, "swap": {...} }
// sys/disk → { "disks": [{ "mount": "/", "total": "500G", "used": "320G", "percent": 64 }] }
// sys/net → { "interfaces": [{ "name": "eth0", "rx": "12.3G", "tx": "5.1G" }] }
// sys/processes → { "processes": [{ "pid": 1234, "name": "nginx", "cpu": "2.3%", "mem": "1.5%" }] }
```

### 5. log/*

```json
// log/read — read log file
{ "path": "/var/log/syslog", "tail": 100, "filter": "error" }
// log/stream — SSE stream (CLI subscribes, Agent pushes until CLI disconnects)
{ "path": "/var/log/syslog", "filter": "error" }
// log/journalctl
{ "unit": "nginx", "since": "1h ago", "tail": 50 }
```

### 6. net/* (Network Diagnostics)

```json
// net/ping → { "host": "10.0.1.1", "count": 3, "results": [...] }
// net/traceroute → { "host": "google.com", "hops": [...] }
// net/portcheck → { "host": "10.0.1.1", "port": 443, "open": true }
// net/curl → { "url": "http://localhost:8080/health", "method": "GET" }
```

### 7. k8s/* (Kubernetes)

```json
// k8s/pods → { "namespace": "default", "label_selector": "app=nginx" }
// k8s/logs → { "namespace": "default", "pod": "nginx-abc123", "tail": 100, "follow": false }
// k8s/events → { "namespace": "default" }
// k8s/describe → { "resource": "pod", "name": "nginx-abc123", "namespace": "default" }
// k8s/apply — create/update from YAML content
// k8s/delete — delete resource (goes through security review, high risk)
```

### 8. pkg/* (Package Management)

```json
// pkg/list → { "name": "nginx" }
// pkg/install → { "name": "htop", "update_cache": true }
// pkg/update → { "name": "nginx" }  // empty name = update all
```

### 9. cron/* (Cron Jobs)

```json
// cron/list → { "user": "root" }
// cron/show → { "user": "root", "job_id": "..." }
// cron/add → { "schedule": "0 3 * * *", "command": "/opt/backup.sh", "user": "root" }
// cron/remove → { "job_id": "..." }
```

### 10. docker/* (Docker)

```json
// docker/ps → { "all": false, "filter": "name=nginx" }
// docker/images → { "filter": "nginx" }
// docker/logs → { "container": "nginx", "tail": 100 }
// docker/exec → { "container": "nginx", "command": "cat /etc/nginx/nginx.conf" }
// docker/run → { "image": "nginx:latest", "name": "test-nginx", "ports": ["80:80"], "detach": true }
// docker/compose → { "action": "up", "file": "/opt/app/docker-compose.yml" }
```

---

## Error Handling & Retry

### MCP Client (CLI → Agent)

| Error | Behavior |
|-------|----------|
| Connection refused / timeout | Mark node offline, prompt user; skip node in multi-node ops |
| TLS / auth failure | Abort immediately, prompt config/cert check |
| Tool execution timeout (Agent-side) | Show elapsed time, ask user: wait / terminate / extend timeout; Agent kills subprocess on timeout, returns captured output |
| 429 rate limit | Exponential backoff, max 3 retries: 2s / 4s / 8s |
| 5xx server error | Retry once, report failure |

### LLM Calls

| Error | Behavior |
|-------|----------|
| 401 / 403 | Abort, prompt API key invalid or expired |
| 429 rate limit | Exponential backoff, max 3 retries, show wait state in TUI |
| 500 / 502 / 503 | Retry twice, show "model service temporarily unavailable, retrying..." |
| Network timeout | Retry once, prompt network check on second failure |
| Stream interruption | Preserve received content, ask user: retry / keep / discard |

### Tool Execution Errors

When a tool succeeds but returns non-zero exit code:
- Result includes exit code, LLM judges whether to retry or change approach
- No automatic retry — command idempotency cannot be guaranteed

---

## Build & Distribution

### Build Targets

```makefile
build:          Build conan and conan-agent to ./bin/
build-linux:    Cross-compile Linux amd64/arm64
build-darwin:   Cross-compile macOS amd64/arm64
```

### Installation

```bash
# CLI install
curl -fsSL https://github.com/user/conan/releases/latest/download/conan-linux-amd64 -o /usr/local/bin/conan
chmod +x /usr/local/bin/conan

# Agent install (on target node)
conan agent install --config /path/to/agent-config.yaml
```

`conan agent install`:

1. Generate `/etc/conan-agent/config.yaml`
2. Generate systemd unit `/etc/systemd/system/conan-agent.service`
3. `systemctl daemon-reload && systemctl enable --now conan-agent`
4. Health check to confirm startup

### Versioning

CLI and Agent share the same repository tag (semantic versioning):

```
v1.2.0 releases both conan and conan-agent
```

CLI checks Agent versions on all connected nodes at startup, warns on version mismatch.

---

## Logging & Observability

### CLI Logging

```yaml
logging:
  level: info        # debug / info / warn / error
  file: ~/.conan/conan.log
  audit: true
```

Log file rotated daily, retained for 7 days.

### Audit Log Format

```
2026-05-19T14:30:22Z [ALLOW]   shell/run node-01 "free -h"
2026-05-19T14:31:05Z [CONFIRM] shell/run node-02 "systemctl restart nginx" (user approved)
2026-05-19T14:32:10Z [DENY]    shell/run node-03 "rm -rf /var/log/*" (high risk: destructive)
2026-05-19T14:33:00Z [ALLOW]   fs/read   node-01 "/etc/nginx/nginx.conf"
```

### Agent Logging

- `/var/log/conan-agent/agent.log` — service runtime log
- `/var/log/conan-agent/audit.log` — tool call audit log (JSON format, compatible with ELK/Loki)
