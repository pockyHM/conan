# TUI Full-Width Input Design

## Goal

Make the chat input box span the terminal width in the TUI, similar to Codex, while keeping the existing single-line input behavior and keyboard handling unchanged.

## Current Behavior

`internal/tui/render.go` renders the input with `renderInputBox(input)`. The renderer does not receive the terminal width, so the rounded border sizes itself to the prompt and current input text.

`internal/tui/model.go` already tracks terminal width from `tea.WindowSizeMsg`, so the view layer has the information needed to size the input box.

## Design

Pass the current TUI width from `Model.View()` into the input renderer. When a positive width is available, render the input box with a fixed lipgloss width that fills the terminal row. When no width has been reported yet, preserve the current content-sized rendering.

The input remains single-line. This change does not add cursor movement, wrapping, textarea behavior, or new shortcuts.

## Testing

Update the existing input box view test to initialize the model with a window width and assert that the top border expands to that width. Keep the existing assertions that the prompt and input are still rendered inside the box.
