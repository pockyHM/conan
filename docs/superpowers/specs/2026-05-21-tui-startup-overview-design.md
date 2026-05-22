# TUI Startup Overview Design

## Goal

Replace the sparse first-launch empty state with a compact startup overview that makes the TUI feel populated before the first chat message.

## Behavior

When the chat has no messages and no streaming response, the body shows:

- A large ASCII `CONAN` wordmark.
- The current cluster.
- The current model.
- A node summary showing selected nodes, total nodes, and online nodes.
- Up to five node rows with name, host, online/offline status, and selected/unselected state.
- A short prompt: `Type a message or /help`.

If there are more than five nodes, show the first five in configured order and add a remaining count line. Once a message or streaming response exists, the normal conversation body replaces the overview.

## Non-Goals

This does not change the top header, input box, node selector, node selection behavior, or chat message rendering.

## Testing

Add a model view test that creates a model with mixed online/offline and selected/unselected nodes, renders the initial view, and asserts the overview contains the wordmark, cluster, model, node summary, node details, and startup prompt.
