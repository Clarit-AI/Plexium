# How Plexium Works

Plexium gives a repository a durable knowledge layer that agents can read, update, retrieve, and validate over time. The easiest way to think about it is:

- your source code stays the ground truth
- Plexium builds and maintains a wiki beside it
- a manifest keeps those two worlds connected

For the product-level overview, start with the [README](../README.md). This document focuses on the core system model.

---

## The Three Core Layers

Plexium operates on three layers:

```text
Source Layer (immutable)
  src/**, docs/**, README, ADRs
        |
State Manifest (.plexium/manifest.json)
  Bidirectional mapping, hashes, ownership, staleness
        |
Wiki Layer (.wiki/)
  Pages, navigation, log, raw material, agent guidance
```

### Source Layer

This is your code, docs, READMEs, ADRs, and other primary project artifacts. Plexium reads from this layer, but it does not treat it as the place where accumulated agent understanding lives.

### State Manifest

`.plexium/manifest.json` is the bridge. It tracks which source files relate to which wiki pages, stores hashes for staleness detection, and records ownership metadata so automation can tell when a page is safe to regenerate and when it must stay human-controlled.

### Wiki Layer

`.wiki/` is the durable knowledge surface. This is where architecture summaries, module pages, ADRs, concepts, guides, contradictions, change logs, and raw source material live. Agents are expected to read from it before they work and update it after they work.

---

## What Plexium Creates

On a fresh repo, `plexium init` creates two trees:

- `.wiki/`
  The wiki vault, navigation files, schema, log, and starter pages.
- `.plexium/`
  Config, manifest, templates, migrations, reports, agent adapters, and integration references.

The current preferred path is `plexium setup claude` or `plexium setup codex`, which wraps init, compile, adapter installation, PageIndex wiring, and verification into one repo-onboarding flow.

---

## The Day-to-Day Loop

The intended daily loop is:

1. Retrieve context from the wiki.
2. Make code changes in the repo.
3. Update or validate the affected wiki pages.
4. Compile navigation and run checks.

Git hooks and CI are there to keep that loop honest. They do not replace the wiki; they keep the wiki from drifting away from the codebase.

---

## Ownership Modes

Every wiki page has an ownership mode:

| Mode | Meaning |
|------|---------|
| `managed` | Plexium or an agent may regenerate this page from source. |
| `human-authored` | Automation should not overwrite this page. |
| `co-maintained` | Humans and agents both contribute, with automation avoiding destructive rewrites. |

This is one of the places Plexium diverges from simpler "generate docs and hope" flows. It needs a durable answer to the question: who is allowed to change what?

---

## What Is Core vs Optional

### Core

These are part of the basic Plexium model:

- `.wiki/` and `.plexium/`
- manifest-driven state tracking
- compile/lint/doctor flows
- agent instruction generation
- CLI retrieval with `plexium retrieve`

### Optional

These add leverage, but are not required to use Plexium well:

- MCP setup for agent-native retrieval
- marketplace/plugin install surfaces for Claude and Codex
- git-hook enforcement with lefthook
- daemon-driven maintenance
- Ollama or OpenRouter provider configuration
- Memento transcript ingestion

That distinction matters because Plexium should still feel useful if you only want the core wiki and retrieval loop.

---

## Related Docs

- [Retrieval and MCP](retrieval-and-mcp.md)
- [Automation and Hooks](automation-and-hooks.md)
- [Memento Integration](memento-integration.md)
- [Inspirations](inspirations.md)
- [User Guide](user-guide.md)
