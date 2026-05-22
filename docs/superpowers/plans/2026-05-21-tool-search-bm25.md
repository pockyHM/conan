# Tool Search BM25-Lite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace substring-only `tool_search` matching with BM25-lite ranked lexical search across names, descriptions, and schemas.

**Architecture:** Keep search local to `toolCache.Search`. Add private tokenizer and scoring helpers in `internal/tui/metatools.go`; keep the public result shape unchanged.

**Tech Stack:** Go standard library, existing `mcpproto.ToolDefinition`, package-level Go tests.

---

### Task 1: BM25-Lite Ranking

**Files:**
- Create: `internal/tui/metatools_test.go`
- Modify: `internal/tui/metatools.go`

- [ ] **Step 1: Write failing tests**

Add tests for schema matching, name-priority ranking, and duplicate node merging in `internal/tui/metatools_test.go`.

- [ ] **Step 2: Run tests to verify failure**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestToolCacheSearch' -count=1`

Expected: FAIL because current search only checks substring matches in name and description.

- [ ] **Step 3: Implement tokenizer and scorer**

In `internal/tui/metatools.go`, add helpers to tokenize text, build weighted document fields, compute BM25-style scores, apply substring boosts, merge duplicate tool names, and sort by score descending then name.

- [ ] **Step 4: Run focused tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -run 'TestToolCacheSearch' -count=1`

Expected: PASS.

- [ ] **Step 5: Run package tests**

Run: `GOROOT=/opt/homebrew/Cellar/go/1.26.1/libexec GOPROXY=https://proxy.golang.org,direct go test ./internal/tui -count=1`

Expected: PASS.
