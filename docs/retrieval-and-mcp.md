# Retrieval and MCP

Plexium is not only a wiki generator. It also exposes the wiki as a queryable memory layer. This document explains the retrieval side of the product: what PageIndex is doing, how the CLI and MCP surfaces relate, and how the Claude and Codex plugin flows fit in.

---

## The Retrieval Engine

Plexium indexes the wiki and lets you search it by meaningfully weighted text signals such as page title, section headings, summaries, content, and wiki-links. In practice, that means you can ask a question like:

```bash
plexium retrieve "how authentication works"
```

and get the most relevant wiki pages back without manually opening the entire vault.

This is the same retrieval capability whether you call it from the CLI, from an MCP client, or through the repo-shipped plugin surfaces.

---

## CLI Retrieval

The fastest path is the CLI:

```bash
plexium retrieve "database schema"
plexium retrieve "build pipeline" --format json
```

CLI retrieval is part of the core product. You do not need Ollama, OpenRouter, or an external MCP client to use it.

Use the CLI when:

- you want a quick answer in the terminal
- you are validating wiki coverage yourself
- you want scripts or tooling to query the wiki directly

---

## MCP Retrieval

When you want an agent to access the wiki natively inside its own session, Plexium exposes the same retrieval engine through PageIndex over MCP:

```bash
plexium pageindex serve
```

That makes the wiki available as MCP tools rather than plain terminal output. Supported agents can then query the wiki during a coding session without the user copy-pasting context manually.

Use MCP when:

- you want Claude or Codex to pull project memory during normal work
- you want retrieval to feel native inside the agent TUI
- you want setup, verification, and retrieval to stay inside one tool surface

---

## Setup Paths for Claude and Codex

There are two supported setup styles:

### Canonical repo setup

```bash
plexium setup claude
plexium setup codex
```

This is the preferred onboarding flow. It initializes Plexium if needed, compiles navigation, installs the appropriate agent adapter, prepares the PageIndex reference, and tells you whether any native MCP step is still outstanding.

Add `--write-config` if you want Plexium to run the native MCP command for you:

```bash
plexium setup claude --write-config
plexium setup codex --write-config
```

### MCP-only connection flow

If the repo is already set up and you only need the native MCP command:

```bash
plexium pageindex connect claude
plexium pageindex connect codex
```

---

## Marketplace and Plugin Surfaces

The Claude and Codex bundles do not invent new retrieval systems. They wrap the same CLI and MCP primitives:

- install or bootstrap the `plexium` binary
- run `plexium setup <agent>`
- run `plexium verify <agent>`
- expose retrieval through `plexium retrieve`
- show or apply the native MCP connection step

That matters because it keeps the TUI-native flows aligned with the CLI instead of creating separate logic paths that drift over time.

---

## Choosing Between CLI and MCP

Use CLI retrieval when the human operator wants answers directly in the terminal. Use MCP retrieval when the agent itself should be able to pull Plexium context mid-session.

They are complementary, not competing:

- CLI is the simplest human-facing interface.
- MCP is the simplest agent-facing interface.
- Plugins and marketplace bundles are guided wrappers over the same primitives.

---

## What Is Optional

- `plexium retrieve` is core and available as part of the basic setup.
- MCP wiring is optional, but strongly recommended if you use Claude or Codex heavily.
- Marketplace/plugin bundles are optional convenience surfaces.

If you skip MCP entirely, Plexium still works as a wiki and terminal retrieval system.

---

## Related Docs

- [How Plexium Works](how-it-works.md)
- [Automation and Hooks](automation-and-hooks.md)
- [Getting Started](getting-started.md)
- [User Guide](user-guide.md)
