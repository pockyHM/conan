# /nodes Add Node Design

## Goal

Add a lightweight "Add new node" entry to the `/nodes` selector so users can create a node without leaving the node selection flow.

## Design

`/nodes` shows the existing node selector with an extra bottom row: `+ Add new node...`. The cursor can move to that row with the same up/down keys used for node rows. Space keeps its existing behavior for node selection. Enter confirms selected nodes when the cursor is on a node row, and opens the add-node form when the cursor is on the add row.

The add-node form collects:

- name
- host or IP
- agent port, default `9280`
- SSH username
- SSH password, masked on screen

Esc cancels the form and returns to the selector with previous node selections intact. Tab or Enter advances through fields. Enter on the password field submits.

Submission reuses the existing node add service path instead of duplicating deployment, config writing, credential storage, and health checks. On success the new node is added to the model, selected automatically, and the selector is refreshed. On failure the form stays open with the entered values and shows the error so the user can fix and retry.

## Testing

Tests should cover selector rendering/navigation, opening the form, field entry and masking, successful submission through an injected runner, empty-node `/nodes` behavior, and validation failures.
