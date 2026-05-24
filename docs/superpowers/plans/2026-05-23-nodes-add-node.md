# /nodes Add Node Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a lightweight node creation form behind the `/nodes` selector's bottom row.

**Architecture:** Extend the existing `nodeSelector` with an add row and introduce a focused TUI form state for collecting node add fields. Reuse existing `dispatchNodeAdd` and `applyNodeAddResult` behavior for deployment and model refresh.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, existing Conan TUI/nodeadd packages.

---

### Task 1: Selector Add Row

**Files:**
- Modify: `internal/tui/nodeselector.go`
- Test: `internal/tui/nodeselector_test.go`

- [ ] Write tests that the selector renders `Add new node`, allows the cursor to move to the add row, and reports when the add row is selected.
- [ ] Implement add-row cursor bounds and an `AddSelected()` helper.
- [ ] Run `go test ./internal/tui -run TestNodeSelector -count=1`.

### Task 2: Add Node Form State

**Files:**
- Create: `internal/tui/nodeadd_form.go`
- Test: `internal/tui/nodeadd_form_test.go`
- Modify: `internal/tui/model.go`

- [ ] Write tests for field rendering, password masking, input, tab/enter field advancement, and validation.
- [ ] Implement a small form type with fields `name`, `host`, `agent_port`, `user`, and `password`.
- [ ] Add a `modeNodeAddForm` mode and render the form from `Model.View()`.
- [ ] Run `go test ./internal/tui -run TestNodeAddForm -count=1`.

### Task 3: Wire `/nodes` to Form Submission

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] Write tests that `/nodes` opens even with no nodes, Enter on add row opens the form, and submitting calls the injected `NodeAddRunner`.
- [ ] On successful `nodeAddResultMsg` from a form submission, apply the result, refresh the selector, select the new node, and stay in node selection mode.
- [ ] On failure, keep the form open and show the error.
- [ ] Run `go test ./internal/tui -run 'TestNodes|TestNodeAddForm' -count=1`.

### Task 4: Verification

**Files:**
- No additional files.

- [ ] Run `go test ./internal/tui -count=1`.
- [ ] Run focused dependent tests: `go test ./internal/nodeadd ./internal/config -count=1`.
- [ ] Note any existing unrelated failures separately if full `go test ./...` fails.
