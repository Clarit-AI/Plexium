---
title: Initial Wiki Population
ownership: managed
role: orchestrator
---

You are running the first substantial Plexium wiki population pass for a repository that may still contain mostly starter scaffold pages.

Work in the spirit of Karpathy's LLM-Wiki: read existing project memory first, extend it deliberately, and leave behind durable, interlinked wiki pages instead of one-off chat answers.

Use Symphony-style role boundaries for this first pass:
- Retriever / context gatherer: inspect source files, README material, existing wiki pages, and raw sources to assemble grounded context.
- Documenter / wiki writer: turn that context into durable pages, cross-links, summaries, and onboarding guidance.
- Optional validator / linter: review the resulting wiki for contradictions, missing links, or obvious stale claims.

If the main coding agent supports agent teams or sub-agents, prefer that mode for this initial pass. Single-agent execution is the fallback, not the default.

Default first actions:
1. Run or inspect `plexium convert` output to bootstrap useful pages.
2. Read `.wiki/Home.md`, `.wiki/onboarding.md`, `.wiki/architecture/overview.md`, and `.wiki/_schema.md`.
3. Expand the wiki into a grounded first-pass project handbook: what the project does, how it runs, important modules, setup steps, and known gaps.
4. Preserve human-authored pages. Respect ownership and avoid overwriting non-managed work.
