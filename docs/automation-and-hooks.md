# Automation and Hooks

Plexium is useful as a passive wiki, but one of its real differentiators is that it can stay active while development is happening. This document explains the automation surfaces that keep the knowledge layer in sync with the repo.

---

## Passive vs Active Plexium

At minimum, Plexium can be used passively:

- humans and agents update the wiki when they remember
- retrieval helps later sessions recover that context

The active mode adds enforcement and maintenance:

- git hooks catch missing wiki updates
- CI checks wiki coverage at pull request time
- background automation can watch for drift and wiki debt
- agent-facing setup and retrieval surfaces keep the workflow easy to reuse

That active layer is a big part of what separates Plexium from "generate a wiki once and hope it stays current."

---

## The Built-In Enforcement Ladder

Plexium uses a layered enforcement model:

1. `_schema.md` gives agents the maintenance contract.
2. Git hooks enforce the contract during local commits.
3. CI enforces it at team and PR level.
4. Memento can add provenance gating when enabled.

This ladder matters because not every failure should be handled at the same level. The schema nudges, hooks catch early drift, and CI provides the hard stop when needed.

---

## Git Hooks

Plexium ships CLI entrypoints for the main git-hook workflow:

- `plexium hook pre-commit`
- `plexium hook post-commit`

With lefthook or another hook runner in place, the pre-commit hook checks whether relevant wiki updates accompany source changes. The post-commit hook can record wiki debt when a commit bypasses the normal path.

Git hooks are the primary built-in local automation surface today. They are the most direct way Plexium keeps the system honest during normal development.

---

## CI Automation

CI is the shared enforcement layer. It catches the cases local hooks miss, especially in team settings and pull requests.

Typical CI responsibilities include:

- checking whether source changes are covered by wiki updates
- running deterministic lint
- surfacing report summaries in pull requests

This is the last line of defense against a knowledge layer that quietly drifts away from the real repo.

---

## Agent-Facing Automation

Claude and Codex integrations are designed to keep users on one canonical Plexium path rather than forcing them to rediscover the commands every session.

The repo-shipped marketplace/plugin bundles focus on:

- bootstrapping `plexium`
- running `plexium setup <agent>`
- running `plexium verify <agent>`
- exposing retrieval through `plexium retrieve`
- helping with native MCP connection

In other words, the first-party agent integrations are workflow automation surfaces over the Plexium CLI. They are not separate products with separate state.

### About agent-native hooks

Git hooks, CI, and the daemon are the built-in automation backbone today. Claude and Codex can host richer hook-based workflows where the agent platform supports them, but Plexium's first-party bundles are intentionally thin wrappers over the CLI and MCP model so the product stays understandable and portable.

---

## Daemon and Assistive Maintenance

The daemon is the background automation layer:

- it can detect stale pages
- it can spot lint issues and wiki debt
- it can decide whether to log, create issues, or attempt auto-fix behavior in an isolated worktree

This is where provider-backed assistive maintenance comes into play. Ollama and OpenRouter are not required for the core system, but they let Plexium do more of the upkeep work automatically when you want that behavior.

---

## Why This Matters

Many LLM Wiki-style systems stop at "the wiki exists." Plexium pushes further by asking a harder question: how does that wiki stay alive once a team returns to normal coding behavior?

The answer is the automation stack:

- schema guidance
- git hooks
- CI checks
- daemon-driven maintenance
- agent-native setup and retrieval surfaces

Together, those make Plexium a living workflow instead of a static artifact.

---

## Related Docs

- [How Plexium Works](how-it-works.md)
- [Retrieval and MCP](retrieval-and-mcp.md)
- [Memento Integration](memento-integration.md)
- [Getting Started](getting-started.md)
