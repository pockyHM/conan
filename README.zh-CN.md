# Conan

[English](README.md) | [简体中文](README.zh-CN.md)

Conan 是一个运行在终端里的 AI 运维助手。主要入口是 `conan` TUI：你可以和模型对话、选择节点、引用本地文件、附加图片、安装 skills，并让 Conan 通过 `conan-agent` 调用安全的节点工具。

## 安装

### 一键安装（Linux & macOS）

```bash
curl -fsSL https://raw.githubusercontent.com/pockyHM/conan/main/install.sh | bash
```

自动检测系统和架构，下载 `conan` 到 `/usr/local/bin`，Linux 上还会下载 `conan-agent` 到 `~/.conan/agent/<架构>/conan-agent`。安装完成后自动进入 `conan model add` 配置第一个模型。

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/pockyHM/conan/main/install.sh | bash -s -- 0.0.1
```

### 手动下载

从 [GitHub Releases](https://github.com/pockyHM/conan/releases) 下载二进制文件。

**conan（CLI — Linux & macOS）**

```bash
# Linux amd64
curl -fSL -o /usr/local/bin/conan https://github.com/pockyHM/conan/releases/latest/download/conan-linux-amd64
chmod +x /usr/local/bin/conan

# Linux arm64
curl -fSL -o /usr/local/bin/conan https://github.com/pockyHM/conan/releases/latest/download/conan-linux-arm64
chmod +x /usr/local/bin/conan

# macOS arm64 (Apple Silicon)
curl -fSL -o /usr/local/bin/conan https://github.com/pockyHM/conan/releases/latest/download/conan-darwin-arm64
chmod +x /usr/local/bin/conan
```

**conan-agent（仅 Linux）**

```bash
# amd64
mkdir -p ~/.conan/agent/amd64
curl -fSL -o ~/.conan/agent/amd64/conan-agent https://github.com/pockyHM/conan/releases/latest/download/conan-agent-linux-amd64
chmod +x ~/.conan/agent/amd64/conan-agent

# arm64
mkdir -p ~/.conan/agent/arm64
curl -fSL -o ~/.conan/agent/arm64/conan-agent https://github.com/pockyHM/conan/releases/latest/download/conan-agent-linux-arm64
chmod +x ~/.conan/agent/arm64/conan-agent
```

然后在 `~/.conan/config.yaml` 里配置 agent 二进制路径：

```yaml
agent_deploy:
  binaries:
    amd64: ~/.conan/agent/amd64/conan-agent
    arm64: ~/.conan/agent/arm64/conan-agent
```

## 快速开始

添加模型：

```bash
conan model add
conan model use <名称>
```

添加节点并部署 `conan-agent`：

```bash
conan node add <hostname-or-ip> --user <ssh-user>
```

启动 Conan：

```bash
conan
```

不带子命令运行 `conan` 会进入交互式 TUI。使用 `--home <path>` 指定 Conan home 目录，使用 `--cluster <name>` 指定集群。

## TUI 日常使用

输入自然语言请求后按 Enter：

```text
检查选中节点上的 nginx 状态。
查找最近的 kubelet 错误并总结可能原因。
把 @deploy/nginx.conf 上传到 node-a 的 /etc/nginx/nginx.conf。
```

常用斜杠命令：

```text
/help                 显示可用命令
/lang                 切换界面语言
/model [名称]         显示或切换当前模型
/cluster [名称]       显示或切换集群
/nodes                选择目标节点
/skills               列出可见 skills
/skills install ...   在 TUI 内安装 skills
/skill <名称> ...     要求 Conan 使用指定 skill
/memory               查看记忆摘要
/resume [id]          恢复已保存会话
/compact [focus]      压缩对话上下文
/thinking <消息>      发送一条启用 thinking 的消息
/agent <角色> <任务>  运行本地只读 subagent 任务
/subagents            管理本地 subagents
/exit                 保存并退出
```

退出时 Conan 会输出会话 id：

```text
Session saved: <id>
Resume with: conan resume <id>
```

也可以直接恢复：

```bash
./bin/conan resume <id>
```

## 提示词里的引用

Conan 在 TUI 输入中支持 `@` 引用。

### 本地文件

使用 `@path` 把本地工作区文件作为上下文传给模型：

```text
阅读 @internal/tui/model.go 并解释会话恢复流程。
```

路径里有空格时使用引号：

```text
总结 @"docs/my runbook.md"。
```

引用目录时会传入目录列表：

```text
解释这个包的组织方式：@internal/skills
```

规则：

- 路径相对于启动 `conan` 的目录。
- 绝对路径和 `..` 路径会被拒绝。
- 包含符号链接的路径会被拒绝。
- 大文件会被截断。
- 在 TUI 中输入 `@` 可以触发路径补全。
- 如果需要输入普通的 `@`，使用 `@@`。

### 图片

PNG、JPEG、GIF 图片可以通过 `@path` 引用，也可以从剪贴板粘贴。Conan 会把它们保存为图片附件，并在需要查看像素内容时调用配置好的视觉模型。

```text
这张截图里有什么问题？@tmp/error.png
```

相关配置：

```yaml
vision:
  model: gpt-4o
  max_images: 10
  max_summary_chars_per_image: 1200
```

如果 `vision.model` 为空，Conan 使用 `default_model`。

## Skills

Skills 是 Conan 可复用的指令包。一个 skill 是包含 `SKILL.md` 的目录，文件由 YAML frontmatter 和 Markdown 正文组成：

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

Conan 会把可见 skills 加载到会话索引中。模型可以在相关时调用内置 `skill_read` 工具读取完整指令；你也可以用 `/skill` 明确要求使用某个 skill。

### 安装 Skills

从公开 GitHub 仓库安装。默认会扫描仓库里的 `skills/` 目录，并发现所有 `SKILL.md`。

安装到某个集群：

```bash
./bin/conan skills install github.com/org/repo --cluster prod
```

全局安装：

```bash
./bin/conan skills install github.com/org/repo --global
```

从分支、tag 或自定义目录安装：

```bash
./bin/conan skills install org/repo --ref v1.2.0 --path conan-skills --global
```

也可以在 TUI 内完成同样操作：

```text
/skills install github.com/org/repo --cluster prod
/skills install org/repo --global --ref main --path skills
```

查看、更新、删除：

```bash
./bin/conan skills list --cluster prod
./bin/conan skills update --cluster prod
./bin/conan skills update k8s-debug --global
./bin/conan skills remove k8s-debug --cluster prod
```

### Skill 作用域

- global skill 对所有集群可用。
- cluster skill 只在对应集群激活时可用。
- cluster skill 注册在 `~/.conan/clusters/<cluster>/skills.yaml`。
- global skill 注册在 `~/.conan/skills/registry.yaml`。
- 仓库缓存放在 `~/.conan/skills/repos/`。

相关配置：

```yaml
skills:
  enabled: true
  index_token_budget: 800
  max_skill_chars: 6000
  max_visible_skills: 50
```

## 模型

交互式配置模型：

```bash
./bin/conan model add
./bin/conan model list
./bin/conan model use <名称>
./bin/conan model remove <名称>
```

Conan 支持 Anthropic 和 OpenAI 兼容提供方。配置中的 API Key 可以使用 `${OPENAI_API_KEY}` 这类环境变量引用。

在 `model add` 里，可以用上下键移动提供方和模型列表，按 Enter 确认。自定义提供方需要选择 OpenAI 兼容或 Anthropic 兼容协议。自定义端点会按输入值原样访问，Conan 不会再追加 `/chat/completions` 或 `/v1/messages`。如果无法获取模型列表，就手动输入模型名称。

## 节点和 Agent

`conan-agent` 运行在被管节点上，对外提供 MCP 运维工具。添加并部署节点：

```bash
./bin/conan node add 10.0.0.12 --name web-1 --user root
```

添加节点的常用变体：

```bash
./bin/conan node add web-1.example.com --no-deploy
./bin/conan node add 10.0.0.12,10.0.0.13 --name web-1,web-2 --no-deploy
./bin/conan node add web-1.example.com --update
./bin/conan node add web-1.example.com --update --rotate-token
./bin/conan node add web-1.example.com --agent-bin ./bin/conan-agent-linux-amd64
```

更新已有节点上的 `conan-agent`：

```bash
./bin/conan node update web-1.example.com --cluster prod
./bin/conan node update --all --cluster prod
./bin/conan node update --all-cluster
```

`node update` 默认使用 `--mode auto`：读取已配置节点和已保存的 SSH 凭据，先尝试 SSH/SFTP 更新；如果 SSH 无法完成，则回退到已鉴权的 agent 更新接口。可以用 `--mode ssh` 强制旧的仅 SSH 行为，也可以用 `--mode agent` 跳过 SSH 凭据、直接通过正在运行的 agent 更新。

可以用 `--agent-bin` 指定本地二进制覆盖路径。在 `auto` 和 `ssh` 模式下，`--user`、`--password` 和 `--ssh-port` 会覆盖 SSH 连接参数。

查看清单和连通性：

```bash
./bin/conan clusters
./bin/conan nodes --cluster prod
./bin/conan ping
./bin/conan ping web-1
./bin/conan tools list web-1
```

手动运行 agent：

```bash
./bin/conan-agent run --config /etc/conan-agent/config.yaml
```

Agent 示例配置：

```yaml
listen: "0.0.0.0:9280"
token: "changeme"
tls: false
audit_log: /var/log/conan-agent/audit.log
rate_limit: 10
disabled_tools: []
log_level: info
```

## 文件传输

优先使用 Conan 的一等文件传输能力，而不是 `scp` 或 shell：

```bash
./bin/conan files put web-1 ./local.conf /etc/app/app.conf
./bin/conan files get web-1 /var/log/app.log ./downloads/app.log
```

在 TUI 里可以自然语言描述：

```text
把 web-1 的 /etc/nginx/nginx.conf 下载到 @downloads/nginx.conf。
把 @configs/app.yaml 上传到 web-1 的 /etc/app/app.yaml。
```

## 配置

默认 home：

```text
~/.conan
```

覆盖方式：

```bash
CONAN_HOME=/path/to/home ./bin/conan
./bin/conan --home /path/to/home
```

常见文件：

```text
~/.conan/config.yaml
~/.conan/clusters/base.yaml
~/.conan/clusters/<cluster>/cluster.yaml
~/.conan/clusters/<cluster>/nodes.yaml
~/.conan/clusters/<cluster>/skills.yaml
~/.conan/skills/registry.yaml
~/.conan/memory/
```

全局配置示例：

```yaml
default_model: gpt-4o
default_cluster: prod
ui_language: zh-CN

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

集群配置按以下顺序合并：

```text
clusters/base.yaml -> clusters/<cluster>/cluster.yaml -> node overrides
```

## 安全模型

Conan 会优先使用专用工具，而不是直接执行 shell：

1. 先用 `tool_search` 搜索节点工具元数据。
2. 能匹配专用工具时用 `call_tool` 调用。
3. 只有在用户明确要求 shell，或没有合适专用工具时，才使用 `exec`。

风险控制包括：

- 节点级命令白名单。
- 全局命令黑名单。
- 本地文件写入白名单。
- 对需要确认的操作进行模型辅助风险审查。
- Agent bearer token 鉴权。
- Agent 限流和审计日志。
- 可选 Agent TLS。

## 项目结构

```text
cmd/conan/              CLI 入口
cmd/conan-agent/        被管节点 agent 入口
internal/tui/           Bubble Tea 终端 UI
internal/skills/        Skill 安装、注册、解析和 skill_read 工具
internal/fileref/       @file 引用解析和加载
internal/llm/           Anthropic 和 OpenAI 兼容客户端
internal/mcp/           MCP 客户端
internal/agent/         MCP 服务端、文件端点和 HTTP 中间件
internal/tools/         Agent 工具集合
internal/config/        配置加载和集群继承
internal/security/      白名单、策略、风险审查和审计日志
internal/memory/        记忆规则、SQLite 存储和记忆工具
internal/subagent/      本地只读 subagent runner
internal/runbook/       Runbook 草稿和预览支持
internal/evidence/      证据和 incident report 支持
pkg/configschema/       共享配置结构
pkg/mcpproto/           共享 MCP JSON-RPC 类型
pkg/models/             共享数据模型
```

## 编译和测试

```bash
make build
make build-linux
make build-darwin
make test
```

开发辅助命令：

```bash
go test ./...
go run ./cmd/conan --help
go run ./cmd/conan-agent --help
```
