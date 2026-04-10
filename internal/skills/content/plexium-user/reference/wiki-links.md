# Wiki Links

Plexium uses `[[wiki-link]]` syntax for cross-references between pages. Links are validated by `plexium lint` and tracked in the PageIndex for retrieval scoring.

## Syntax

```markdown
See [[authentication-flow]] for details.
```

With display text:

```markdown
See [[authentication-flow|the auth docs]] for details.
```

## Link Targets

Links resolve relative to the wiki root (`.wiki/`):

| Link | Resolves to |
|------|-------------|
| `[[auth]]` | `.wiki/auth.md` |
| `[[modules/auth]]` | `.wiki/modules/auth.md` |
| `[[decisions/use-jwt]]` | `.wiki/decisions/use-jwt.md` |

The `.md` extension is optional — `[[auth]]` and `[[auth.md]]` both work.

## When to Add Links

Add cross-references when:
- A page mentions a concept that has its own page
- Two pages describe related functionality
- A decision page references the module it affects
- A module page references the patterns it uses

Do not over-link. Link the first mention of a term in a section, not every occurrence.

## Broken Links

`plexium lint --deterministic` reports broken links (links to pages that don't exist). Fix by either:
1. Creating the missing target page
2. Removing the link
3. Correcting the target path

## Orphan Pages

Pages with no inbound links are reported as orphan pages. Fix by adding a `[[link]]` to the orphan from a related page, or by linking it from `_index.md`.
