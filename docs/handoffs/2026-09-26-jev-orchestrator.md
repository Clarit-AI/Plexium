---
title: "Handoff to a new orchestrator session — Jev follow-on phases + Linear backlog"
kind: spec
status: 1
comments: none
---

# Handoff: Jev follow-on phases + Linear backlog

**Date:** 2026-09-26
**For:** the next orchestrator session the user opens to (a) implement the
Jev follow-on phases that have been waiting behind D3, and (b) drive the
MarkedUp / Plexium Linear backlog that was in progress when we paused for
the evaluation.

This is the durable record. The user's goal statement was:

> "Prepare a detailed handoff to start a new orchestrator session that drives not
> only the follow-up implementation phases for implementing Jev but also
> working it into the existing backlog of linear items and work that was
> currently in progress when we stopped to do this exploration."

There is also a session-scoped stop hook in effect with the same goal text;
this artifact is what that hook will reference.

---

## TL;DR

- **MarkedUp is adopted and on `main`.** PR #157 landed as squash commit
  `17282d7`. 903 tests green across 15 packages. `go test -race` clean on
  the three packages that touch production code paths (`enrich`, `index`,
  `internal/cli`).
- **Tier 2 has been verified end to end against a real local model.**
  Self-hosted `llama-server` + `gemma-4-E4B-it-Q4_K_M` produced real NER
  output, the resolver resolved every target through the alias table, and the
  graph is traversable on real documents. The plumbing is no longer
  theoretical — it ran against a 4-file KB at `/tmp/tier2run/kb/` during this
  session.
- **The previous decision was "Phase 3 does not run" (D3).** You are being
  asked to implement Jev follow-on phases anyway. That is a *deliberate
  reversal* by the user, not a continuation of the prior reasoning. Section
  "Navigating the reversal" below gives you the framing.
- **Linear state.** KHA-579 / 282 / 577 closed. KHA-276 left narrowed to
  Plexium-side read-only work. The full MarkedUp / Plexium Solo Developer v1
  backlog is open and yours to drive. KHA-574 and KHA-573 are in the
  *current cycle*.
- **Hard-won lessons to read first.** An adversarial review found seven
  defects in the merged work, four of which passed CI while being wrong. The
  durable artifact that captures them all is
  [markedup-final-decision](../markedup-final-decision/index.md), and the
  review's own write-up is at
  [b5-write-order-review](../b5-write-order-review/index.md).

---

## 1. State on disk

### 1.1 Repos

| Repo | Path | Working tree | Last commit on `main` |
| --- | --- | --- | --- |
| MarkedUp | `~/Documents/Development/LucidityLabs/markedup` | uncommitted docs edits (`.beads/README.md`, `AGENTS.md`, `README.md`, `docs/architecture.md`, `docs/cli-reference.md`, `docs/local-testing.md`); 10 gitnexus stashes | `17282d7` (the merge) |
| Plexium | `~/Documents/Development/Clarit.ai/Plexium` | uncommitted skill docs under `.claude/skills/`; one stash `feat: add agent setup with PKCE OAuth, skills directory, and credential hardening`; many gitnexus stashes | `26d4fa9` on `codex/backstage-assessment` |

> The uncommitted edits in both repos look like docs / agent-skill maintenance,
> not work that was in progress. Do not assume the new orchestrator inherits
> them — they were there before this evaluation session and were not touched
> by it. If in doubt, run `git status` after cloning the worktree fresh.

### 1.2 Idle MarkedUp worktrees (from earlier sessions)

```
~/Documents/Development/LucidityLabs/markedup                                                     6944302 [codex/fix-keychain-test-prompts]
~/.traycer/.../traycer-markedup-brave-lemur-6458df31539b                                          4513879 [traycer/markedup-brave-lemur]
~/.traycer/.../traycer-markedup-chipper-penguin-f52d6be53160                                       4513879 [traycer/markedup-chipper-penguin]
~/.traycer/.../traycer-markedup-daring-swan-5f2f9893ce40                                          3660b72 [traycer/markedup-daring-swan]
~/.traycer/.../traycer-markedup-humble-walrus-a841d6d345c8                                       5f20615 [traycer/markedup-humble-walrus]
~/.traycer/.../traycer-markedup-silent-otter-fc7e9ef609be                                         4513879 [traycer/markedup-silent-otter]
~/.traycer/.../traycer-markedup-wild-yak-cbe9249953cc                                             3660b72 [traycer/markedup-wild-yak]
```

The five stale `traycer-markedup-{brave-lemur, chipper-penguin, silent-otter, daring-swan, wild-yak}` worktrees are pre-merge. The merged change is on `humble-walrus`, which is now at `5f20615` (the merge commit is `17282d7` on `main`).

### 1.3 Plexium worktrees — these are the active backlog

```
~/.traycer/.../bbrenner2217-kha-287-prevent-sync-from-declaring-unchanged-wiki-content-fresh    8b6dd85 [bbrenner2217/kha-287-prevent-sync-declaring-unchanged-wiki-content-fresh]
~/.traycer/.../bbrenner2217-kha-573-repair-scheduled-lint-and-validate-a-fresh-wiki-scaffold    a87b158 [bbrenner2217/kha-573-repair-scheduled-lint-and-validate-a-fresh-wiki-scaffold]
~/.traycer/.../bbrenner2217-kha-574-gate-markedup-dependency-bumps-with-integration-contract    02b661f [bbrenner2217/kha-574-gate-markedup-dependency-bumps-with-integration-contract]
~/.traycer/.../qa-pr28-kha-287                                                                  3c9867c [qa/pr28-kha-287]
~/.traycer/.../qa-pr29-kha-574                                                                 02b661f [qa/pr29-kha-574]
~/.traycer/.../traycer-plexium-chipper-turtle-7e711784986e                                        598c9bd [traycer/plexium-chipper-turtle]
~/.traycer/.../traycer-plexium-chipper-turtle-7e711784986e/.kilo/worktrees/twilight-turkey       eef0327 (detached HEAD)
~/.traycer/.../traycer-plexium-tidy-leopard-1168869c3077                                          26d4fa9 [traycer/plexium-tidy-leopard]
~/Documents/Development/Clarit.ai/Plexium/.claude/worktrees/agent-a1731f24                         52df09d [feat/kha-287-regen-package]          locked
~/Documents/Development/Clarit.ai/Plexium/.claude/worktrees/agent-a6826e15                         df6486e [worktree-agent-a6826e15]              locked
~/Documents/Development/Clarit.ai/Plexium/.claude/worktrees/agent-ae1637a4                         bf3abd2 [fix/kha-287-sync-ci-flag]            locked
```

The three Plexium branches are the backlog the user means by "currently in progress":
- **KHA-287** branch — sync freshness fix (the issue itself is now Done, but the branch is unmerged)
- **KHA-573** branch — scheduled lint and fresh-wiki scaffold
- **KHA-574** branch — gate MarkedUp dependency bumps with integration-contract tests

The other branches are leftovers.

---

## 2. What was just done, briefly

The end-to-end repair plan
([markedup-e2e-repair-plan](../markedup-e2e-repair-plan/index.md)) produced
seven blocker fixes (B1–B7), five of them with a number attached rather than
an assertion. The full before/after is in
[markedup-final-decision](../markedup-final-decision/index.md). Headline:

| # | Blocker | Before | After |
| --- | --- | --- | --- |
| B1 | `SemanticRelationships` unread | 0 semantic edges ever traversed | folded into adjacency, reverse adjacency, counts |
| B1b | Dangling targets invisible | `LoadWarning` only | counted + named in `CompactGraphSummary` |
| B2 | Entity-type vocabulary broken | 6 outcomes in 200 runs; 4 of 5 types dropped | **1** outcome in 200 runs; **0** drops |
| B3 | Empty-ID collision | 3 files → 1 page indexed | 3 files → 3 pages |
| B4 | Wikilinks in code blocks | `wikilinks` target 4× | 1× — the genuine prose link |
| B5a | Crash leaves files half-enriched | Tier 1 written before Tier 2 outcome | write deferred until outcome known; `persistTier1` covers all five failure paths |
| B5b | Static `SchemaMode: !isLocal` | hard 400 on non-conforming endpoints | reactive, cached, no probe request |
| B6 | Untracked decision, no compat gate | doc hidden, test absent | doc tracked; compat test asserts the client surface |
| B7 | Keychain prompt blocked tests | — | already fixed at `41e4b0d`, this branch's base |

Plus two side findings worth filing:

1. **`llm/client.go` double-prepends `/v1`.** When the user passes `--endpoint http://host/v1`, the client appends `/v1/chat/completions` and the path becomes `/v1/v1/chat/completions`, which llama-server rejects with 404. One-line fix: strip a trailing `/v1` from the user-supplied endpoint before appending. (I worked around it in the live run by passing `--endpoint http://127.0.0.1:1235` without `/v1`.)
2. **`CompactGraphSummary` per-page output omits `semantic_relationships`.** The total `semantic_edges` count is correct; the per-page `relationships` array in the JSON output is wikilink-only. Either surface `semantic_relationships` on `SummaryNode` or rename the field to `wikilinks` for clarity.

These two are filed in the final-decision artifact but not in Linear as separate issues. Your call whether to file them or fold them into existing work.

---

## 3. Navigating the reversal (the most important paragraph here)

The previous decision, recorded in `artifacts/markedup-final-decision` and posted as the final-finding comment on **KHA-579**, was:

> "Phase 3 does not run, and that is the expected outcome, not a deferral."

The reasoning was: D3 gated Phase 3 on "a specific, named decision that deterministic code cannot resolve," and Phase 2 left none.

The user is now asking for Jev follow-on phases anyway. That is the user's prerogative — they decide. But it means **the prior decision is no longer the active constraint**. Do not treat "Phase 3 does not run" as a load-bearing premise; treat it as a prior conclusion that the user has overridden.

What changes, concretely, with the new Tier 2 evidence the previous session gathered:

- **The plumbing is now demonstrably correct.** That changes the cost of a Jev cascade tail from "build the whole Tier 2 plumbing first" to "plug Jev behind the existing Tier 2."
- **The `TECHNOLOGY` residual bucket is now real.** Decision D1 deliberately routes `TECHNOLOGY` to an unmapped residual because the whitelist has both `software` and `tool` and nothing in the repo defines the difference. A Jev cascade tail is now a defensible way to handle that bucket — provided it is gated on real, measured accuracy, not optimistic adoption.
- **The `Jev was 2/2 correct on genuinely-contradicted cases` correction still stands.** Jev is *not* unsafe per the KHA-579 evaluation; it was just unsupported by the n=6 study. A new evaluation with a labeled set of `TECHNOLOGY` cases would be the right evidence base.

Concretely, the orchestrator's first move on the Jev side is **rebuild the cascade-tail evaluation against a labeled set of `TECHNOLOGY` (and, separately, `claim-support`) cases**, not build a Jev integration yet. The decision to integrate is downstream of the measurement; the measurement is the work that was deferred from KHA-579's "insufficient evidence" verdict.

### Recommended ordering for Jev follow-on work

1. **Re-evaluate `TECHNOLOGY` against a labeled set.** MarkedUp's `TECHNOLOGY` cases from the live Tier 2 run are in `/tmp/tier2run/kb/concepts/knowledge-graph.md` and `/tmp/tier2run/kb/notes/ingestion.md` (model emitted `Neo4j TECHNOLOGY`, `RDF CONCEPT` — the assignment is right for `RDF` but wrong for `Neo4j` if your taxonomy is `software` vs `tool`). That is one example already; build a set of 20–30 `TECHNOLOGY` cases with hand-adjudicated gold, run both a deterministic-table cascade and a Jev cascade, measure. Document the threshold where the Jev cascade beats the table.
2. **Re-evaluate `claim-support` for Plexium** at n≥30 with unsupported-claim acceptance as the primary metric. KHA-579's original study was n=6 per task and was rejected for that reason. The previous verdict was "INSUFFICIENT EVIDENCE, leans Jev for claim-support."
3. **If 1 and 2 produce a defended keep decision for any component:** design and build the integration. Otherwise, file the new evidence and stop.

---

## 4. The Plexium / MarkedUp backlog to drive

These are the items that were "in progress when we stopped to do this evaluation." I have not started any of them; this is the queue handed to you.

### 4.1 Solo Developer v1 — MarkedUp / Plexium

Sorted roughly by urgency / signal:

| ID | Title | Status | Notes |
| --- | --- | --- | --- |
| **KHA-574** | Gate MarkedUp dependency bumps with integration-contract tests | Todo, in current cycle | Owns the dependency-bump workflow. The compat test added in B6 lives under `enrich/compatibility_test.go` and is the seed for the contract. |
| **KHA-573** | Repair scheduled lint and validate a fresh wiki scaffold | Todo, in current cycle | Plexium-side. |
| KHA-578 | Establish reproducible MarkedUp test and release contracts | Backlog | The .baseline/ artifacts from this session are an existence proof of reproducible measurements; a follow-up to formalize them is natural. |
| KHA-575 | Prove the solo install, update and recovery workflow | Backlog | Not addressed. |
| KHA-576 | Graph identity stable, retrieval read-only | Backlog, narrowed | MarkedUp-side is done by the merge. The Plexium-side read-only-by-default behavior is the remaining piece. Worth filing as a separate Plexium issue if not yet tracked. |
| KHA-298 | Measure Plexium's solo workflow against a coding-agent baseline | Backlog | Independent measurement on `llm-dev-council`. Not addressed. |

### 4.2 Already closed in this session (do not re-open)

- **KHA-579** — Jev evaluation: Done. Final-finding comment posted with the corrected conclusion.
- **KHA-282** — Real-model Tier 2 validation: closed with live evidence from the `gemma-4-E4B` run.
- **KHA-577** — Failure-safe enrichment: closed; every acceptance criterion met by the merge.

### 4.3 Other open Linear work — not in this orchestrator's scope

The Linear backlog also includes a large pile of AnimeTrackPro, Legilimens, and Engram issues (KHA-520 Epic and child issues; KHA-526 typed relations; KHA-546 ingest-anilist writer; KHA-580 MAL proxy security; KHA-543 cron 500s; etc.). Those are independent projects. **Do not pull them into the MarkedUp/Plexium/Jev stream** unless the user re-scopes. If in doubt, ask.

---

## 5. Existing in-progress work that the user likely wants resumed

The three Plexium branches with commits are the most concrete "in progress" signal. Their commits and status as of this handoff:

| Branch | HEAD | Linked worktree | Notes |
| --- | --- | --- | --- |
| `bbrenner2217/kha-287-prevent-sync-from-declaring-unchanged-wiki-content-fresh` | `8b6dd85` | `bbrenner2217-kha-287-...` | Issue is Done but branch may not be merged. Inspect the branch tip to see whether work is already on `main`. |
| `bbrenner2217/kha-573-repair-scheduled-lint-and-validate-a-fresh-wiki-scaffold` | `a87b158` | `bbrenner2217-kha-573-...` | Todo, in current cycle. |
| `bbrenner2217/kha-574-gate-markedup-dependency-bumps-with-integration-contract` | `02b661f` | `bbrenner2217-kha-574-...` | Todo, in current cycle. |
| `qa/pr28-kha-287` | `3c9867c` | `qa-pr28-kha-287` | QA branch. |
| `qa/pr29-kha-574` | `02b661f` | `qa-pr29-kha-574` | QA branch. |

The .locked worktrees under `~/Documents/Development/Clarit.ai/Plexium/.claude/worktrees/agent-*` belong to other paused agents; leave them alone.

> The Plexium main is on `codex/backstage-assessment` (commit `26d4fa9`). The
> branch the merge would target depends on which Plexium base the user wants
> active; that decision has not been made here.

---

## 6. Hard-won lessons (read these first)

These come from the merged work and its adversarial review. Each is a memory entry in `~/.claude/projects/-Users-bbrenner-Documents-Development-Clarit-ai-Plexium/memory/`; the index is `MEMORY.md`.

1. **[project_markedup_final_decision.md]** — the final state. The headline is that B1's "the graph is traversable" claim is verified by unit tests against a synthetic corpus *and* by a real Tier 2 run on a 4-file KB (no longer just synthetic).
2. **[feedback_evaluate_by_building.md]** — fix the thing and run it on real input rather than proposing another measurement phase. Applies to Jev too: build a Jev cascade tail with a labeled set, don't argue whether to.
3. **[feedback_review_repairs_adversarially.md]** — every repair PR needs an independent adversarial review on a different model family, and **the new tests must be proven to fail without the fix**. Seven defects in this work reached green-suite state; the review caught four, two of which would each have caused silent data loss in a user's documents.

Other memory entries to read first if you haven't in this session:

- **[project_phase0_baseline.md]** — what the baseline artifacts measure.
- **[project_kha579_correction.md]** — the corrected verdict on Jev's safety (2/2 correct on genuinely-contradicted cases; the metric was an abstention-failure rate, not a false-acceptance rate).

---

## 7. Recommended first moves

In order, with rough cost:

1. **Read the four artifacts above** (≤10 min). They are the durable record of the state you are inheriting.
2. **Resume KHA-574 and KHA-573** (the in-cycle work). Check the branches, see what is staged but not pushed, decide whether to finish and open PRs. This is the lowest-risk first move.
3. **File the two side findings as Linear issues** (≤15 min each): the `/v1` double-prefix and the missing `semantic_relationships` in `CompactGraphSummary`. Both are real, both have reproduction, both are one-line fixes. Tag them as low priority.
4. **Build a labeled `TECHNOLOGY` evaluation set** (1–2 hours). Reuse the live Tier 2 evidence pattern — small KB, model runs, hand-adjudicate types. Aim for 20–30 cases spanning `software` vs `tool` vs `document` for tech-named entities. This is the foundation of the Jev cascade-tail decision.
5. **Decide on the Jev integration shape** only after step 4. The right next move is *measurement*, not a new integration path. A cascade tail that ships without measurement will reproduce KHA-579's "insufficient evidence" pattern, and we just spent five days learning that lesson.

What you should **not** do first:

- **Do not start a Jev cascade tail without a measurement gate.** The whole point of the prior session was that measurement without a build is wasted tokens; building without measurement is the symmetric failure.
- **Do not touch the AnimeTrackPro / Legilimens / Engram backlog** unless the user re-scopes.
- **Do not amend or force-push the merged MarkedUp commit history.** It is on `main` and the merge commit `17282d7` is the durable record.

---

## 8. Pointers — durable records in this epic

- `markedup-e2e-repair-plan/index.md` — the original 5-phase plan with B1–B7, D1–D3.
- `markedup-final-decision/index.md` — the final state and decision, with the before/after table and the defect list.
- `b5-write-order-review/index.md` — the adversarial reviewer's write-up; the seven defects in one place.
- `markedup-pipeline-stall-history/index.md` — the recovery context for how we got here (Sept 19 audit onward).

These four are the durable record. The handoff artifact you are reading is meta: it tells you what to do with them.
