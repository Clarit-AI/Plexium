# Schema Guide

The file `.wiki/_schema.md` is the constitution governing how agents maintain the wiki. It is the first file an agent should read when entering a Plexium repository.

## What the Schema Governs

- **Page structure**: required frontmatter fields, heading conventions, content expectations
- **The agent loop**: read-execute-document-validate cycle
- **Ownership rules**: which pages agents can edit, which are locked
- **Cross-referencing**: when and how to add `[[wiki-links]]`
- **Logging**: format and expectations for `_log.md` entries
- **Conflict resolution**: what to do when wiki content contradicts source code

## Strictness Levels

The schema respects the `enforcement.strictness` setting in `.plexium/config.yml`:

| Level | Behavior |
|-------|----------|
| `strict` | Pre-commit hook blocks commits without wiki updates. All source changes require corresponding wiki changes. |
| `moderate` | Pre-commit hook blocks commits, but explains bypass and wiki-debt handling. Debt is tracked in `_log.md`. |
| `advisory` | No enforcement. Schema serves as guidance only. |

## Schema Version

The schema has a `schema-version` field in `.wiki/_schema.md`. When Plexium upgrades, `plexium migrate` updates it to the latest version.

## Reading the Schema

The schema is written in Markdown with structured sections. Key sections to look for:

- **Agent Contract** — the loop you must follow
- **Page Types** — what goes in each wiki section
- **Frontmatter Spec** — required and optional fields
- **Linking Policy** — when cross-references are required
- **Update Policy** — rules for what constitutes a "wiki update"
