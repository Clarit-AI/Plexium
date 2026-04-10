# Ownership Model

Every wiki page has an ownership mode that determines who can modify it. Ownership is tracked in two places:

1. **Page frontmatter** — the `ownership` field in the YAML header
2. **Manifest** — `Pages[].ownership` in `.plexium/manifest.json`

The manifest value is authoritative for enforcement. If they disagree, the manifest wins.

## Modes

### `managed`

Agent-maintained. Plexium regenerates this page from source files during sync and convert operations. Human edits are overwritten on the next sync.

**You can:** freely edit, rewrite, restructure.

**Common for:** module docs, auto-generated architecture pages, index files.

### `human-authored`

Locked from automated changes. Agents must not overwrite, rewrite, or modify this page in any way. The page is also excluded from staleness detection — it will never be flagged as stale.

**You must:** leave this page untouched. If you believe it needs updating, note the suggestion in `_log.md` instead.

**Common for:** hand-written guides, onboarding docs, compliance docs, curated decision records.

### `co-maintained`

Both agents and humans edit. Agents may append to existing sections but must not rewrite human-written content. New sections can be added at the end of the page.

**You can:** add new sections, append to existing lists, update timestamps.
**You must not:** rewrite, reorganize, or delete existing content.

**Common for:** design pattern catalogs, concept glossaries, meeting notes pages.

## Changing Ownership

To change a page's ownership:

1. Edit the frontmatter:
```yaml
---
title: Page Title
ownership: human-authored
last-updated: 2026-04-06
---
```

2. Update the manifest entry to match. The `ownership` field in `.plexium/manifest.json` under the page's entry must agree.

## Edge Cases

- **New pages you create** default to `managed` unless you set otherwise.
- **Pages without frontmatter** are treated as `managed`.
- **Conflicting ownership** (frontmatter says `managed`, manifest says `human-authored`) — manifest wins for enforcement, but this state should be fixed.
