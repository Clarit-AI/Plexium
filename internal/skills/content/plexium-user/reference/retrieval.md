# Retrieval System

Plexium has a built-in search engine with two interfaces to the same underlying PageIndex.

## CLI: `plexium retrieve`

```bash
plexium retrieve "authentication flow"
plexium retrieve "database schema" --format json
```

Works immediately after `plexium init`. No additional setup.

## MCP: `plexium pageindex serve`

Same engine exposed over JSON-RPC 2.0 stdio. Three tools:

| Tool | Input | Returns |
|------|-------|---------|
| `pageindex_search` | `{"query": "..."}` | Ranked page hits with relevance scores |
| `pageindex_get_page` | `{"path": "modules/auth.md"}` | Full page content and metadata |
| `pageindex_list_pages` | `{}` | All indexed pages with metadata |

If MCP is configured for your agent, prefer `pageindex_search` over shelling out to `plexium retrieve`.

## Scoring

The PageIndex scores matches across five dimensions:

| Dimension | Weight | Example |
|-----------|--------|---------|
| Title match | 1.0 | Query term appears in page title |
| Section match | 0.8 | Query term matches the section name (e.g., "modules") |
| Summary match | 0.6 | Query term appears in the first paragraph |
| Content match | 0.4 | Query term appears anywhere in the body |
| Link match | 0.2 | Query term appears in an outbound `[[wiki-link]]` target |

Scores are normalized to 0.0-1.0. Top 20 results returned.

## Fallback Chain

If the PageIndex returns no results:

1. Parse `_index.md` for `[[wiki-link]]` entries and score by title/summary match
2. Walk all `.wiki/**/*.md` files and grep content directly
3. Merge, deduplicate, sort by relevance

This ensures queries always return something useful, even if the index is empty or stale.
