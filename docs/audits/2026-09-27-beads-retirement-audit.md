---
kind: spec
title: "Beads retirement audit — hooks and remaining work"
comments: none
---

# Beads retirement audit

**Checked:** 2026-09-27. This is a read-only comparison of the local Plexium Beads database, current Linear issues, GitHub issues/PRs, and active Claude/Codex/Git hook configuration. No tracker items were created, closed, or migrated.

## Development instructions and hooks

- `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, and `docs/phases/OVERVIEW.md` no longer require `bd` for development. Remaining Beads references in `CLAUDE.md` are historical phase descriptions or the optional `plexium beads` product integration.
- `bd setup claude --check` and `bd setup codex --check` report no project integration. Their `--global --check` variants also report none. Active Claude settings, enabled plugins, hook scripts, and Codex config contain no Beads command or plugin reference. The repo has no harness-local Claude/Codex settings that install Beads hooks.
- `bd hooks list` reports `pre-push` as installed because an executable hook exists. Inspection of the hook shows only Clarit-AI GitHub-account enforcement; it does not call Beads. Other Beads Git hooks are absent.
- Plexium's optional product feature remains: `plexium init --with-beads` and the `plexium beads` task-ID/frontmatter commands. No product code was changed in this audit.

## Open Beads inventory and destination

`bd stats` reports **21 open, 0 in progress, 0 blocked, 2 closed**. GitHub has **0 open issues** for `Clarit-AI/Plexium`. Linear is the existing home for the substantive follow-up work.

| Beads record | Finding | Recommended disposition |
| --- | --- | --- |
| 11 phase epics (`plexium-p0` through `plexium-m10`) | Historical build plan. The phase overview marks nine complete and M5/M7 pending; `docs/status.md` describes partial agent-role and Obsidian behavior. | Do not bulk-copy the epics. Reassess specific M5/M7 gaps against current product priorities before opening focused Linear issues. |
| Five April M1 tasks: CLI routing, config loader, scanner, normalizer, template engine | Phase 1 is marked complete; implementations exist in the repository. Their open Beads status is stale planning state. | No new issue; reconcile/close the old Beads records only after an owner review. |
| `Plexium-a7v` — freshness semantics | [KHA-287](https://linear.app/khaentertainment/issue/KHA-287/prevent-sync-from-declaring-unchanged-wiki-content-fresh) is Done; [PR #28](https://github.com/Clarit-AI/Plexium/pull/28) is merged. | Already represented; no migration. |
| `Plexium-jk2` — safe transcript ingestion | [KHA-266](https://linear.app/khaentertainment/issue/KHA-266/wire-memento-transcript-ingestion-into-wiki-workflow-with-secret) is Backlog. | Already represented; no migration. |
| `Plexium-1g3` — scheduled lint and MCP notification | [KHA-573](https://linear.app/khaentertainment/issue/KHA-573/repair-scheduled-lint-and-validate-a-fresh-wiki-scaffold) is Todo; [KHA-572](https://linear.app/khaentertainment/issue/KHA-572/fix-mcp-notification-handling-and-verify-client-interoperability) is Backlog. | Already represented as two focused issues; no migration. |
| `Plexium-5o0` — retrieval refresh, graph identity, raw exclusion | [KHA-570](https://linear.app/khaentertainment/issue/KHA-570/keep-retrieval-fresh-and-consistent-across-cli-and-mcp), [KHA-571](https://linear.app/khaentertainment/issue/KHA-571/apply-one-raw-content-exclusion-policy-to-every-retrieval-provider), and [KHA-576](https://linear.app/khaentertainment/issue/KHA-576/make-enriched-graph-identity-stable-and-retrieval-read-only) are Backlog. | Already represented; no migration. |
| `Plexium-rbd` — audit recovery, PR review, release contracts | Release-contract work is represented by [KHA-574](https://linear.app/khaentertainment/issue/KHA-574/gate-markedup-dependency-bumps-with-integration-contract-tests) (Todo) and [KHA-578](https://linear.app/khaentertainment/issue/KHA-578/establish-reproducible-markedup-test-and-release-contracts) (Backlog). [PR #27](https://github.com/Clarit-AI/Plexium/pull/27) remains open for its V3 planning document. The separate audit-recovery work remains: the primary clone still has untracked `docs/decisions/KHA-299-markedup-release-strategy.md` and `docs/plans/pageindex-plugin-migration-roadmap.md`, and `docs/status.md` needs reconciliation against current behavior. | **One focused Linear issue is warranted** for preserving the two May documents and reconciling current product status, if this work remains in scope. Do not copy the whole mixed Beads task. |

## Recommendation

Use Linear for the one distinct recovery/status item. Keep the old Beads database as historical evidence until each record has been reconciled; do not bulk-migrate stale epics or duplicate issues already in Linear. The optional Plexium product adapter can remain available independently of the development workflow.
