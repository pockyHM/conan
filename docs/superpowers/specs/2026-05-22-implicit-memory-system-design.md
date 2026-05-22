# Implicit Memory System Design

## Goal

Conan's memory system should be useful without becoming part of the user's normal chat experience. The assistant should automatically recall relevant context and quietly preserve durable facts, preferences, rules, topology, runbooks, and incident knowledge. Users should not need to see or invoke `memory/save` or `memory/search` during ordinary conversation.

## Current State

The existing system has two memory surfaces:

- Markdown behavioral rules loaded from `MEMORY.md` and `rules/*.md`.
- SQLite memories and conversation records exposed through explicit LLM tools such as `memory/save`, `memory/search`, `memory/update`, and `memory/delete`.

This works mechanically, but the interaction model is too visible. Memory tools look like normal tools, `/memory` exposes raw saved entries, and important durable facts such as user identity or coding rules are not naturally promoted into Markdown memory.

## Principles

- Memory is implicit by default. The model may use memory tools, but routine memory reads and writes should not render as visible tool messages.
- Important, durable, always-relevant knowledge belongs in Markdown, not only SQLite.
- Detailed or episodic knowledge belongs in indexed Markdown notes or SQLite and should be retrieved on demand.
- Memory editing must be restricted to the memory directory and mediated by purpose-built APIs. The LLM should not get arbitrary filesystem write access.
- Sensitive data must not be saved by default.
- The system should favor small, reviewable memory patches over wholesale rewrites.

## Memory Layers

### Core Markdown Memory

Path:

```text
~/.conan/memory/memory/MEMORY.md
```

Purpose:

- User name and preferred address.
- Stable interaction preferences.
- Global operating principles.
- High-level coding or operational rules that should be present in nearly every prompt.

This file is always considered for prompt injection, subject to token budget.

### Structured Markdown Memory

Path:

```text
~/.conan/memory/memory/
├── MEMORY.md
├── profile.md
├── rules/
│   ├── coding.md
│   └── ops.md
├── clusters/
│   └── <cluster>.md
├── runbooks/
│   └── <topic>.md
└── incidents/
    └── <date>-<slug>.md
```

Purpose:

- `profile.md`: user and team preferences that are more detailed than `MEMORY.md`.
- `rules/*.md`: durable behavior, coding, security, and operational rules.
- `clusters/*.md`: topology, node roles, service layout, and cluster-specific conventions.
- `runbooks/*.md`: reusable procedures.
- `incidents/*.md`: investigation summaries, symptoms, root cause, resolution, and follow-up.

These files are progressively exposed. The prompt builder injects only the files that match the current context, and memory tools can retrieve details as needed.

### SQLite Memory

Purpose:

- Conversation summaries.
- Searchable historical facts.
- Low-to-medium importance operational observations.
- Tool-result summaries that may be useful later but do not deserve a Markdown note.

SQLite remains the fast retrieval layer and can also act as a staging area before important facts are promoted into Markdown.

## Memory Classification

Each memory candidate should be classified before saving:

- `profile`: user identity, naming, preferences.
- `rule`: durable instruction, coding convention, operational policy.
- `topology`: cluster, node, service, dependency, network, or deployment layout.
- `runbook`: reusable procedure.
- `incident`: failure, symptom, investigation, root cause, fix, verification.
- `event`: time-bound historical event.
- `discard`: casual chat, greetings, transient content, duplicate facts, secrets.

Default destinations:

- `profile` -> `profile.md`, and optionally `MEMORY.md` if it is globally important.
- `rule` -> `rules/*.md`, and optionally `MEMORY.md` if short and always relevant.
- `topology` -> `clusters/<cluster>.md`.
- `runbook` -> `runbooks/<topic>.md`.
- `incident` -> `incidents/<date>-<slug>.md` plus SQLite index entry.
- `event` -> SQLite only.
- `discard` -> no persistence.

## Implicit Retrieval Flow

Before each model request, Conan builds memory context in this order:

1. Load bounded core context from `MEMORY.md`.
2. Load bounded rule context from `rules/*.md`.
3. If a cluster is selected, load `clusters/<cluster>.md` if present.
4. Run automatic retrieval against SQLite and Markdown index using the latest user request, selected cluster, selected nodes, and recent conversation summary.
5. Inject only the top relevant snippets into the prompt under a concise memory context section.

The model can still call `memory_search` or `memory_read` when it needs more detail, but common recall should happen before the model answers.

## Implicit Save Flow

After each assistant turn completes, Conan runs a memory extraction step:

1. Build a compact extraction input from the latest user message, assistant answer, relevant tool results, cluster, nodes, and model.
2. Ask the model or a configured extraction provider to return structured memory candidates.
3. Validate candidates:
   - reject secrets, credentials, private keys, tokens, and obvious sensitive values;
   - reject casual chat and duplicates;
   - cap size and normalize category;
   - require evidence from the current turn.
4. Route each candidate to Markdown or SQLite using the classification rules.
5. Apply Markdown changes through controlled patch operations.
6. In normal mode, do not render memory tool messages. In debug mode, log candidate decisions and applied patches.

Explicit user instructions such as "记住...", "以后记得...", or "remember that..." should bypass ambiguity checks but still pass sensitivity validation and destination routing.

## LLM Memory Tools

Replace visible slash-style memory tools with OpenAI-compatible, implicit tools:

- `memory_search`
  - Search SQLite plus Markdown index.
  - Returns concise snippets, source paths, categories, and timestamps.

- `memory_read`
  - Reads an allowed Markdown memory file or section.
  - Allowed paths are restricted to the configured memory root.

- `memory_patch`
  - Applies a section-level patch to an existing Markdown memory file.
  - Rejects path traversal, whole-file deletion, oversized diffs, and edits outside allowed directories.

- `memory_write_note`
  - Creates a new structured Markdown note for incidents, runbooks, topology, or profile details.
  - Requires category, title, summary, content, and tags.

- `memory_promote`
  - Promotes a SQLite memory or conversation summary into Markdown.
  - Useful when repeated historical facts become durable rules or runbooks.

Backward-compatible aliases may keep accepting `memory/save`, `memory/search`, `memory/update`, and `memory/delete` internally, but new requests should expose only underscore tool names.

## UI Behavior

Normal chat:

- Memory reads and writes are hidden.
- No "Saved memory" assistant message is shown.
- Answers may naturally use remembered context without explaining retrieval mechanics.

Debug mode:

- Log memory extraction input, candidates, routing decisions, rejected candidates, tool calls, and Markdown patches.
- Avoid logging sensitive raw values when validation marks them secret-like.

Manual management:

- `/memory` should become a management view, not the primary interaction model.
- It can show summaries, recent changes, Markdown files, and SQLite entries.
- Future commands may include `/memory search`, `/memory open`, and `/memory diff`.

## Prompt Injection Strategy

The prompt should include a concise memory policy:

- Use memory implicitly.
- Search memory when previous context may matter.
- Save durable facts and reusable operational knowledge.
- Prefer Markdown for stable identity, preferences, rules, topology, and runbooks.
- Use SQLite for episodic or historical entries.
- Never save secrets.
- Do not mention memory operations unless the user asks.

The prompt should not expose memory as a user-facing feature in normal responses.

## Safety Rules

- Memory tools cannot access files outside the memory root.
- `memory_patch` must operate on known Markdown categories.
- Secret-like values are rejected or redacted by default.
- Destructive operations require either an explicit user request or a higher-friction management flow.
- Markdown patch failures do not block the assistant answer; they are logged and surfaced only in debug or management views.

## Migration Plan

1. Keep existing SQLite schema for compatibility.
2. Introduce Markdown memory directories and an indexer.
3. Add underscore memory tools while keeping old tool names as internal aliases.
4. Stop rendering memory tools in the TUI during normal chat.
5. Add automatic pre-request retrieval.
6. Add post-turn memory extraction and routing.
7. Gradually migrate important SQLite memories into Markdown through `memory_promote`.

## Testing Strategy

- Unit test path validation for Markdown memory tools.
- Unit test classification-to-destination routing.
- Unit test secret rejection and redaction.
- Unit test prompt builder progressive injection by cluster and query.
- Unit test hidden TUI rendering for memory tool calls.
- Integration test explicit "记住..." saves to the correct Markdown destination.
- Integration test incident summaries create Markdown notes and SQLite index entries.
- Regression test old memory tool aliases still work internally.

## Open Decisions

- Whether memory extraction uses the active chat model or a separate low-cost configured model.
- Whether Markdown patches should require user confirmation for `MEMORY.md` and `rules/*.md`.
- Whether `/memory` should expose an editor-like UI or remain a read-only summary initially.
