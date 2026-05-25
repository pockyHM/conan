# Runbook Draft 与 Preview 设计

## 目标

让 Conan 能从成功 incident 中沉淀 runbook 草案，并支持执行前 preview。这个设计对应 roadmap 阶段 1 的闭环目标：调查、建议、执行、验证、沉淀。

## 非目标

- 不让 runbook 自动执行 destructive 步骤。
- 不实现远程 runbook 仓库同步。
- 不实现复杂 DSL 解释器；第一阶段使用结构化 Markdown frontmatter 和 step 列表。
- 不绕过现有风险审查和用户确认。

## Runbook 格式

Runbook 存储在：

```text
~/.conan/memory/memory/runbooks/YYYY-MM-DD-<slug>.md
```

Markdown 使用 YAML frontmatter：

```markdown
---
title: Nginx 502 快速诊断
source_incident: incident-abc123
cluster: prod
tags: nginx, 502, gateway
created_at: 2026-05-23T10:00:00Z
---

# Nginx 502 快速诊断

## 适用场景

## 前置检查

## 步骤

1. [read] 使用 `svc/status` 检查 nginx 状态。
2. [read] 使用 `log/journalctl` 查看最近错误。
3. [confirm] 如服务卡死，使用 `svc/restart` 重启 nginx。

## 验证

## 风险
```

第一阶段 runbook 只要求 Markdown 可读、可预览；执行时由模型根据 preview 和现有工具调用完成，不引入完整 DSL。

## Draft 生成

新增 TUI 命令：

```text
/runbook draft                 从最近关闭的 incident 生成草案
/runbook draft <incident path>  从指定 incident 报告生成草案
```

生成草案时读取 incident report，提取：

- 症状
- 影响范围
- 关键证据
- 成功执行动作
- 验证动作
- 后续项

草案默认写入 `runbooks/`，状态是普通 Markdown 文件，不自动注入长期规则。

## Preview

新增命令：

```text
/runbook preview <path>
```

Preview 展示：

- runbook 标题
- 适用场景
- 将要读取的节点/服务/路径
- mutating 或 destructive 步骤
- 每个确认步骤的风险说明

Preview 不调用任何远端工具，也不修改本地记忆，只渲染计划。

## 执行

新增命令：

```text
/runbook run <path>
```

第一阶段执行策略：

- TUI 把 runbook 内容和 preview 注入下一次模型请求。
- 模型必须先执行 read-only 步骤收集证据。
- mutating 步骤继续走现有 `Reviewer` 和确认 UI。
- 每步工具输出进入 evidence model；如果 incident open，也进入 incident timeline。
- 执行结束后提示用户是否把结果追加到 runbook 的 `## 验证` 或 `## 风险` 章节。

## 与 Memory Promotion 的关系

Runbook 是 Markdown 记忆层的复用知识，不是 SQLite event。生成草案后：

- `memory_search` 应能检索 runbook 标题、summary 和 tags。
- `memory_read` 可读取 runbook。
- incident close 的候选记忆可以建议生成 runbook，但不自动覆盖已有 runbook。

## 安全

- 草案生成拒绝 secret-like 内容。
- Preview 必须明确列出 mutating/destructive 步骤。
- Runbook run 不提供“全部自动确认”选项。
- 子代理只能参与 runbook 草案审查和摘要，不执行写操作。

## 测试策略

- `internal/runbook` 测试覆盖 frontmatter 解析、草案生成、preview 分类和 secret-like 拒绝。
- `internal/tui` 测试覆盖 `/runbook draft|preview|run` 命令。
- 与 evidence 包集成测试覆盖 runbook run 的工具证据进入 timeline。
- 与 memory 包集成测试覆盖 runbook 文件可被 search/read。

## 验收标准

- 用户能从 incident report 生成一个可读 runbook 草案。
- 用户能 preview runbook，且 preview 不执行任何工具。
- runbook run 的 mutating 步骤仍然逐步确认。
- 生成的 runbook 能被 memory search/read 复用。
