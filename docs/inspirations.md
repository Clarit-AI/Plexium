# Inspirations

Plexium is influenced by four separate ideas, but it is not a copy of any one of them. This document explains the actual mapping: what Plexium took, where it diverged, and what new layers PageIndex and Memento contribute.

---

## What We Took From LLM Wiki

Karpathy's LLM Wiki idea provided the conceptual center of gravity:

- store project understanding inside the repository
- let agents read and update that knowledge layer
- make understanding compound across sessions instead of restarting from zero

That core idea is still visible in Plexium's `.wiki/` vault and in the expectation that agents should consult and maintain it as part of normal work.

## Where We Diverged From LLM Wiki

Plexium adds a lot more structure than the basic concept requires:

- a manifest-driven bridge between source files and wiki pages
- deterministic compile and lint behavior
- explicit ownership modes for pages
- agent-specific setup and instruction generation
- CLI, MCP, and marketplace/plugin retrieval surfaces
- hook, CI, and daemon-based enforcement

In other words, LLM Wiki is the conceptual seed. Plexium turns it into an operational system.

---

## What We Took From Symphony

From OpenAI Symphony, Plexium draws more from operating style than from file layout:

- agent role separation
- orchestration mindset
- tool-driven workflow boundaries
- the idea that different forms of work should be handled by different specialized loops

That influence shows up in Plexium's retrieval/documentation/maintenance role split and in the daemon and assistive-agent concepts.

## Where We Diverged From Symphony

Symphony is centered on multi-agent orchestration. Plexium is centered on persistent repo memory.

Plexium diverges by making the wiki and manifest the primary durable surface, then treating orchestration as one layer around that memory rather than the other way around. It is less about coordinating swarms for their own sake and more about making project understanding survive the swarm.

---

## What PageIndex Adds

PageIndex is the retrieval influence.

It helped shape Plexium's answer to a practical question: once the wiki exists, how do agents actually use it without rereading everything?

That influence shows up in:

- `plexium retrieve`
- `plexium pageindex serve`
- MCP-native search tools over the wiki
- the idea that the wiki should be queryable, not just browsable

PageIndex is what helps turn the wiki into an active memory surface instead of a passive archive.

---

## What Memento Adds

Memento contributes the provenance layer.

Without it, most systems can preserve final documentation but lose the session-level reasoning that led to the final result. With Memento, Plexium has a path to preserve and later ingest:

- rationale
- tradeoffs
- decision context
- unresolved questions surfaced during implementation

That is one of Plexium's most unusual differentiators: it can use session history as documentation input, not just as hidden operational residue.

---

## The Synthesis

The easiest way to summarize Plexium's lineage is:

- **LLM Wiki** gave it the memory model.
- **Symphony** gave it the orchestration mindset.
- **PageIndex** gave it the retrieval posture.
- **Memento** gave it the provenance and rationale layer.

Plexium's own contribution is weaving those together into a repo-native system with deterministic state, retrieval, enforcement, and optional autonomous maintenance.

---

## Related Docs

- [README](../README.md)
- [How Plexium Works](how-it-works.md)
- [Retrieval and MCP](retrieval-and-mcp.md)
- [Automation and Hooks](automation-and-hooks.md)
- [Memento Integration](memento-integration.md)
