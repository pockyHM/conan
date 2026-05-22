# Tool Search BM25-Lite Design

## Goal

Improve `tool_search` ranking from substring-only matching to a lightweight lexical relevance score that works well for tool names, descriptions, and schemas.

## Behavior

`tool_search` continues to search the local `toolCache`; it does not call nodes during search. The output JSON shape remains unchanged.

The ranking algorithm uses BM25-style scoring over tokenized tool fields:

- `name` tokens have weight `4.0`.
- `description` tokens have weight `2.0`.
- `inputSchema` tokens have weight `0.75`.
- BM25 constants are `k1=1.2` and `b=0.75`.

Tokenization lowercases text and splits on non-letter/non-digit boundaries, so names like `docker/logs` become `docker` and `logs`.

Substring boosts preserve useful old behavior:

- full query contained in the name adds a strong boost.
- full query contained in the description adds a smaller boost.

Results with score `0` are excluded. Results sort by score descending, then name ascending. Duplicate tool names across nodes are merged into a single result with combined `available_on` nodes.

## Non-Goals

This does not add vector search, fuzzy matching, spelling correction, persistent indexes, or new dependencies.

## Testing

Add tests that verify:

- schema-only matches are returned.
- name matches rank above description-only matches.
- duplicate tool names across nodes merge `available_on`.
