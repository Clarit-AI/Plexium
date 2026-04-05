# Auto-Wiki: GitHub Wiki Automation for AI-Era Repos

## Executive Summary

Auto-Wiki transforms repository documentation into a maintained GitHub Wiki. Given a repo's docs (README, `/docs/`, ADRs, design notes), it bootstraps a navigable wiki and keeps it current as the repo evolves. Inspired by Karpathy's LLM Wiki pattern: keep raw sources immutable, maintain a persistent synthesized wiki layer, govern behavior through a schema/config file.

**The pitch:** "Give it your repo docs, and it builds and maintains your GitHub Wiki."

---

## The Problem

Repos accumulate docs but wikis go stale. Manual wiki maintenance is tedious and consistently deprioritized. Meanwhile:
- GitHub Wikis are themselves Git repos (`REPO.wiki.git`) — fully scriptable
- LLMs can synthesize, interlink, and refresh markdown pages
- Claude Code hooks and GitHub Actions provide enforcement points

---

## What We're Building

Three workflows, shipped in order:

### Workflow 1: Bootstrap Existing Docs into GitHub Wiki

**Trigger:** Manual (`workflow_dispatch`) or first-run setup.

**Input:**
- `README.md`
- `/docs/**`
- ADRs
- Design notes
- Selected issue/PR summaries

**Output:**
- `Home.md`
- `_Sidebar.md`
- Section pages (by folder/topic)
- Topic/entity/concept pages
- Optional "Open Questions" / "Recent Changes" pages

GitHub Wikis support `_Sidebar.md` and `_Footer.md` for navigation. Only default-branch pushes render live.

### Workflow 2: Incremental Sync After Merge

**Trigger:**
- `workflow_dispatch` (manual)
- Merge to `main`
- Label like `wiki-sync`
- Docs-related PR merge only

**Behavior:**
1. Inspect changed files
2. Classify: docs-only, code+docs, or architecture-impacting
3. Update affected wiki pages
4. Append change summary to log
5. Refresh sidebar/home if needed

### Workflow 3: Policy Enforcement (Phase 3)

Deterministic checks first, LLM as soft辅助:

**Deterministic rules (always enforced):**
- "Changed API surface requires docs note"
- "New feature folder requires page or changelog entry"
- "ADR change requires wiki refresh"

**LLM handles (advisory, not hard gate):**
- Summarize changes
- Detect stale pages
- Propose updated wording
- Create missing pages

---

## Phase 1: Bootstrap + Manual Sync

**Goal:** Ship the transformation step people instantly understand.

**Delivery:**
1. `repo -> action -> wiki repo` pipeline
2. Main repo contains source docs + policy file (`AGENTS.md`)
3. GitHub Action runs conversion logic
4. Output committed to `REPO.wiki.git`

**Policy file** (`AGENTS.md` in repo root):
- Defines wiki structure conventions
- Names page taxonomy (what = section page, what = topic page)
- Lists ignored paths
- Sets trigger rules

**Scope:**
- Markdown ingestion
- Page clustering by folder/filename heuristics
- Wiki publish (Home, Sidebar, pages)
- Incremental refresh from changed files
- One agent instruction file

---

## Phase 2: Auto-Sync on Merge

**Goal:** Make "AI maintains your repo wiki" a reality.

**Delivery:**
- Auto-trigger on merge to `main` (or label `wiki-sync`)
- Diff-aware page updates (only touch affected pages)
- Change log appended to `log.md` in wiki
- Stale page detection / lint mode
- PR summaries flowing into wiki updates

---

## Phase 3: Multi-Model Packaging

**Goal:** Reusable across Claude Code, Gemini CLI, and Codex.

**Delivery:**
- `skillpack/` repo with portable `SKILL.md` folders
- Vendor-specific packaging:
  - `.claude-plugin/marketplace.json` for Claude Code
  - `.agents/plugins/marketplace.json` + `.codex-plugin/plugin.json` for Codex
  - `.github/plugin/marketplace.json` for Copilot CLI
- Multi-model routing (Claude for doc workflows, Gemini for freshness, Codex for diff-aware patches)
- PR bot for comments + autofix branches

**Scope cuts maintained through all phases:**

Skip until actually needed:
- Multi-model orchestration (phase 3 only)
- Marketplace packaging (phase 3 only)
- Deep PR semantic enforcement
- Issue/Slack/Linear ingestion
- Heavy RAG/search infra

---

## Technical Design

### GitHub Wiki Mechanics

- Wiki is a separate Git repo: `https://github.com/OWNER/REPO.wiki.git`
- Clone, commit, push — only default branch renders live
- Supports `_Sidebar.md` and `_Footer.md` for navigation

### Page Taxonomy

| Source | Wiki Output |
|---|---|
| `README.md` | `Home.md` |
| `/docs/*.md` | Pages by section (`docs/intro.md` → `docs/intro`) |
| `/docs/folder/` | Section index page |
| `ADR/*.md` | `Architecture/adr-001-title` |
| Named concepts | `Topics/concept-name` |

### Agent Instruction File (`AGENTS.md`)

```markdown
---
name: auto-wiki
trigger:
  - workflow_dispatch
  - push to main (if wiki-sync label)
  - pull_request (docs files only)

wiki:
  root: wiki/
  home: Home.md
  sidebar: _Sidebar.md
  log: log.md

taxonomy:
  docs/: section
  ADR/: architecture
  concept-*.md: topics

ignore:
  - "**/CHANGELOG.md"
  - "**/node_modules/**"

synthesis:
  max_page_length: 2000
  link_topics: true
---
```

### GitHub Action Skeleton

```yaml
name: auto-wiki

on:
  workflow_dispatch:
  push:
    branches: [main]
  pull_request:
    paths:
      - '**.md'
      - 'docs/**'
      - 'ADR/**'

jobs:
  bootstrap-or-sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          sparse-checkout: |
            README.md
            docs/**
            ADR/**
            AGENTS.md
          sparse-checkout-cone-mode: false

      - name: Run auto-wiki
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          ./scripts/auto-wiki.sh

      - name: Push to wiki
        if: github.event_name == 'workflow_dispatch' || github.ref == 'refs/heads/main'
        run: |
          git clone "https://github.com/${{ github.repository }}.wiki.git" wiki_out
          cp -r wiki_output/* wiki_out/
          cd wiki_out
          git add -A
          git commit -m "Wiki update from ${{ github.sha }}"
          git push
```

---

## Triggers Summary

| Trigger | When it fires | Use case |
|---|---|---|
| `workflow_dispatch` | Manual button click | One-off bootstrap, testing |
| Push to `main` | Any merge to main | Keep wiki current continuously |
| Label `wiki-sync` | PR merged with label | Selective sync |
| PR merge (docs only) | Only docs files changed | Avoid unnecessary runs |

---

## Why This Order

**Phase 1** proves the transformation works — the "wow, it made a wiki from my docs" moment.

**Phase 2** reveals the maintenance loop — the real value proposition.

**Phase 3** packages for reuse — only when there's actual demand across repos or vendors.

---

## Next Steps

1. Bootstrap a test repo's docs into its GitHub Wiki
2. Wire up the GitHub Action
3. Add the `AGENTS.md` schema
4. Verify page taxonomy looks right
5. Add incremental sync logic
