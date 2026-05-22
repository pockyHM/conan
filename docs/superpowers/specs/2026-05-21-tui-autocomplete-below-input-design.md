# TUI Autocomplete Below Input Design

## Goal

Move slash-command autocomplete below the input box, matching the interaction pattern in Codex and Claude Code.

## Behavior

When autocomplete is visible, the footer renders in this order:

1. Status line.
2. Full-width input box.
3. Full-width autocomplete panel.

The autocomplete content and selection behavior stay unchanged. The panel should use the current terminal width when available so it aligns visually with the input box. When terminal width is unavailable, it keeps the existing content-sized rendering.

## Non-Goals

This does not change filtering, keyboard navigation, Tab completion, command descriptions, or the confirm/security footer.

## Testing

Add a view test that types a slash-command prefix, renders the model with a window width, and verifies the input box appears before the autocomplete panel. The test also verifies the autocomplete panel top border spans the same terminal width.
