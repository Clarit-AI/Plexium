([Past chat][1])([Past chat][1])([Past chat][1])([Past chat][1])

Perfect. I’d update the concept into a **lighter, hype-friendly v1** instead of the bigger cross-marketplace platform.

## Updated direction

Treat this as **“GitHub Wiki Automation for AI-era repos”**, not a generic LLM knowledge system.

Karpathy’s gist gives the pattern: keep source material immutable, maintain a persistent synthesized wiki layer, and define behavior through an agent instruction/schema file. 

For a near-term build, I’d narrow it to 3 workflows:

### 1. Bootstrap existing docs into GitHub Wiki

This is your brownfield entry point.

Input:

* `README.md`
* `/docs/**`
* ADRs
* design notes
* selected issue/PR summaries

Output:

* `Home.md`
* `_Sidebar.md`
* section pages
* topic/entity/concept pages
* optional “Open Questions” / “Recent Changes” pages

That works cleanly because GitHub Wikis are separate Git repositories, editable locally and publishable via normal Git workflows, and GitHub supports `_Sidebar` and `_Footer` files for navigation. ([GitHub Docs][2])

### 2. Incremental sync after merge

Once the wiki exists, keep it warm.

Trigger ideas:

* `workflow_dispatch`
* merge to `main`
* label like `wiki-sync`
* docs-related PR merge only

Behavior:

* inspect changed files
* decide whether this is docs-only, code+docs, or architecture-impacting
* update affected wiki pages
* append a change summary
* refresh sidebar/home if needed

### 3. Optional agent enforcement

Do not make the LLM the hard gate at first.

Use deterministic checks first:

* “changed API surface requires docs note”
* “new feature folder requires page or changelog entry”
* “ADR change requires wiki refresh”

Then let the agent do the fuzzy part:

* summarize changes
* detect stale pages
* propose updated wording
* create missing pages

Claude Code is a strong early fit here because its hooks can run project scripts on events like prompt submit, tool use, stop, and session start, and subagents let you isolate specialized wiki-maintainer behavior in a separate context window. ([Claude API Docs][3])

## What I’d actually build first

Not the full “Claude Marketplace / Gemini / Codex combo” yet.

First build this:

**`repo -> action -> wiki repo`**

Meaning:

1. main repo contains source docs and policy file
2. GitHub Action runs the conversion/sync logic
3. output gets committed to `REPO.wiki.git`

That gives you the dedicated Wiki tab immediately, without overcommitting to plugin ecosystems.

## Best v1 shape

I’d define the product as two modes:

### Mode A — Convert Existing Documentation to Wiki

One-shot or manually triggered.

Good for:

* brownfield repos
* launch week demos
* “turn your messy docs into a usable wiki”

### Mode B — Keep Wiki Current

Incremental ongoing sync.

Good for:

* teams that already merged the bootstrap
* hype-friendly “AI maintains your repo wiki” demos

## Suggested scope cut

To move fast later, skip these in v1:

* multi-model orchestration
* marketplace packaging
* deep PR semantic enforcement
* issue ingestion
* Slack/Linear ingestion
* heavy RAG/search infra

Keep:

* markdown ingestion
* page clustering
* wiki publish
* sidebar/home generation
* incremental refresh from changed files

## The pitch angle

If you want to ride the post’s momentum, the framing should be:

**“Give it your repo docs, and it builds and maintains your GitHub Wiki.”**

That is easier to understand than:

* persistent knowledge substrate
* agentic documentation compiler
* repo memory graph

Those are true, but they are phase-two language.

## My updated recommendation

Build later as:

**Phase 1**

* bootstrap docs to GitHub Wiki
* manual sync trigger
* decent page taxonomy
* one agent profile/instruction file

**Phase 2**

* auto-sync on merge
* PR summaries into wiki updates
* stale page detection / lint mode

**Phase 3**

* Claude/Gemini/Codex packaging
* policy enforcement hooks
* broader knowledge ingestion

If you build this soon, the smartest move is to market **the transformation step** first, then reveal the maintenance loop second. That’s the part people instantly “get.”

When you circle back to it, the next artifact should be a repo one-pager with triggers, inputs, outputs, and folder/layout conventions.

[1]: https://chatgpt.com/c/69d1cc9e-8ecc-83e8-998b-e804af20e781 "Reusable Automation Paths"
[2]: https://docs.github.com/articles/adding-and-editing-wiki-pages-locally?utm_source=chatgpt.com "Adding or editing wiki pages - GitHub Docs"
[3]: https://docs.anthropic.com/en/docs/claude-code/hooks?utm_source=chatgpt.com "Hooks reference - Anthropic"
