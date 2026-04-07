---
name: plexium-user
description: Operate within a Plexium-powered repository — read wiki before work, update wiki after changes, follow ownership rules, use retrieval
---

# Working in a Plexium Repository

This repository uses Plexium to maintain a persistent wiki that compounds knowledge across agent sessions. You are expected to read from and contribute to the wiki as part of your workflow.

## The Loop

Every task follows four steps:

1. **READ** — Before starting work, gather context from the wiki
2. **EXECUTE** — Do the coding task
3. **DOCUMENT** — Update wiki pages to reflect what changed
4. **VALIDATE** — Check that your wiki updates are correct

## Step 1: READ

Before writing any code, retrieve relevant context:

```bash
# Search the wiki for relevant pages
plexium retrieve "<topic>"

# Or with JSON output for structured parsing
plexium retrieve "<topic>" --format json
```

Also read directly:
- `.wiki/_index.md` — full page listing
- `.wiki/_log.md` — recent changes and decisions
- `.wiki/_schema.md` — governance rules (read `reference/schema-guide.md` for details)

If the wiki has an MCP server configured, use `pageindex_search` instead of shelling out.

## Step 2: EXECUTE

Do the coding task. No special requirements here.

## Step 3: DOCUMENT

After changing source code, update the wiki:

**For module changes** — update or create the corresponding page in `.wiki/modules/`:
```yaml
---
title: Module Name
ownership: managed
source-files:
  - path/to/changed/file.go
last-updated: 2026-04-06
---
```

**For architectural decisions** — create a page in `.wiki/decisions/`:
```yaml
---
title: Decision Title
ownership: co-maintained
last-updated: 2026-04-06
---
# Decision Title
## Context
## Decision
## Consequences
```

**Always update `_log.md`** — append a timestamped entry:
```markdown
- **2026-04-06** — [module-name] Brief description of what changed and why
```

**Trivial change exception:** Single-file changes that don't affect architecture only need a `_log.md` entry.

## Step 4: VALIDATE

Before committing:

```bash
# Check for broken wiki-links, orphan pages, stale mappings
plexium lint --deterministic

# Recompile navigation if you added/removed pages
plexium compile
```

## Ownership Rules

Check page frontmatter before editing:

| `ownership` value | You can... |
|---|---|
| `managed` | Freely edit — this page is agent-maintained |
| `human-authored` | **Do not modify** — only humans edit this page |
| `co-maintained` | Append to existing sections, do not rewrite |

When in doubt, check `reference/ownership-model.md`.

## Quick Reference

| Task | Command |
|------|---------|
| Search wiki | `plexium retrieve "<query>"` |
| Check health | `plexium lint --deterministic` |
| Rebuild nav | `plexium compile` |
| Validate setup | `plexium doctor` |
| View manifest | Read `.plexium/manifest.json` |

## When to Read Reference Docs

- Unsure about wiki-link syntax or cross-references → `reference/wiki-links.md`
- Need to understand the page taxonomy → `reference/taxonomy.md`
- Questions about what the schema governs → `reference/schema-guide.md`
- Need the full ownership model details → `reference/ownership-model.md`
- Want to understand the retrieval system → `reference/retrieval.md`
