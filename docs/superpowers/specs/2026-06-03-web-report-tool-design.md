# Web Report Tool Design

## Goal

Add a node tool that lets the model publish a Markdown report to a local browser page and provide a direct Markdown download link. The tool belongs to the existing web/search tool group so it is discoverable through `tool_search`.

## Behavior

The new tool is named `web_report`.

Inputs:

- `title`: optional report title. Defaults to `Report`.
- `markdown`: required Markdown body.
- `filename`: optional download filename. Defaults to a sanitized title with `.md`.

Execution:

1. Validate that `markdown` is non-empty after trimming and below the configured hard limit.
2. Render Markdown to HTML using the existing Go Markdown dependency.
3. Sanitize rendered HTML with the existing HTML sanitizer dependency.
4. Listen on `127.0.0.1:0` so the OS assigns a free local port.
5. Serve `GET /` as a complete HTML page for viewing the rendered report.
6. Serve `GET /download` as the original Markdown with a download filename.
7. Return JSON containing `title`, `view_url`, `download_url`, and `port`.

The report server is process-local and ephemeral. It remains available while the agent process is alive. The tool does not write report content to disk.

## Architecture

`internal/tools/report.go` defines `webReportTool` and any small helpers for rendering, sanitizing, filename cleanup, and HTTP serving.

`internal/tools/web.go` includes `webReportTool` in `NewWebTools` alongside `web_fetch` and optional `web_search`. This keeps the feature in the web/search tool family requested by the user and makes it visible to the TUI through normal agent tool discovery.

`internal/tools/metadata.go` adds metadata for `web_report`:

- safety: `read-only`
- scope: `local`
- capability: `web`, `report`
- tags: `markdown`, `preview`, `download`, `html`

The tool schema describes that Markdown is rendered and made available through local URLs.

## Data Flow

The model calls `web_report` with Markdown. The agent validates and renders the content, starts a local HTTP server, and returns URLs. The user opens the view URL in a browser or uses the download URL to save the original Markdown report.

No remote network access is required beyond the existing MCP tool call to the agent. The browser endpoint binds only to loopback.

## Error Handling

The tool returns a tool error result when:

- `markdown` is empty.
- `markdown` exceeds the maximum accepted size.
- Markdown rendering fails.
- the local listener cannot be opened.

The HTML page uses a safe default title if the input title is empty. The download filename is sanitized to a conservative ASCII basename and falls back to `report.md` if needed.

## Security

The viewer must bind to `127.0.0.1`, not `0.0.0.0`.

Rendered HTML is sanitized before serving. The download endpoint returns the original Markdown as `text/markdown; charset=utf-8` with `Content-Disposition: attachment`.

Because the tool starts a local listener, it is metadata `read-only` and `local`, but it is not a node-mutating operation. It should not require the destructive shell path or command approval.

## Testing

Add focused Go tests that verify:

- `NewWebTools` always exposes `web_report`.
- `web_report` rejects empty Markdown.
- `web_report` returns loopback `view_url` and `download_url` on a random port.
- `GET /` returns sanitized HTML containing rendered Markdown.
- `GET /download` returns the original Markdown and an attachment filename.
- metadata covers `web_report`.
- agent registration includes `web_report` by default.

## Non-Goals

This does not add persistent report storage, report indexes, authentication for the local viewer, configurable fixed ports, editing, live updates, or PDF export.
