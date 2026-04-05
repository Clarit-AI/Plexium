> **Reference Document** — This is voluntary reading for context and rationale.
> The canonical specification is `plexium-spec.md` at project root.
> Agents may consult this but should NOT treat it as authoritative where it conflicts with the PRP.

# On-Device Assistive Agents for Plexium: Gemma 4 Viability Analysis

---

## Short Answer

**Highly viable, architecturally elegant, and a natural fit for Plexium's existing role separation model.** The Gemma 4 26B-A4B MoE variant is nearly purpose-built for this use case — 3.8B active parameters running at ~40 tok/s on a MacBook Pro, native function calling, structured JSON output, 256K context, and Apache 2.0 licensing. It can handle the majority of Plexium's "tidy work" at zero marginal token cost while the primary coding agent stays focused on expensive cognitive tasks.

This isn't a compromise. It's the correct architecture.

---

## Why This Is the Right Structural Move

Your PRP already describes the **Agent Role Separation (Swarm Pattern)** in §16:

| Role | Responsibility |
|------|---------------|
| **Coder** | Writes and modifies source code |
| **Explorer/Retriever** | Searches wiki + codebase via PageIndex |
| **Documenter** | Updates wiki pages, cross-references, runs lint |
| **Ingestor** | Processes raw sources into wiki pages |

Right now, these roles are conceptual — a single cloud agent session handles all four. The token cost concern I flagged earlier is specifically because **the Documenter and Explorer roles burn expensive cloud tokens on work that doesn't require frontier-model intelligence.** Updating frontmatter, appending to `_log.md`, regenerating `_Sidebar.md`, validating `[[wiki-links]]`, checking `source-files` paths — none of this requires Claude Opus or GPT-5. It requires reliable instruction-following, structured output, and function calling. That's exactly what Gemma 4 delivers.

The insight: **route tasks by cognitive difficulty, not by arrival order.**

```
┌─────────────────────────────────────────────────────────────┐
│                    Task Router                               │
│  Classifies incoming work by cognitive complexity            │
│                                                              │
│  ┌──────────────────┐    ┌──────────────────────────────┐   │
│  │  HIGH complexity  │    │  LOW/MEDIUM complexity        │   │
│  │                   │    │                               │   │
│  │  • Architecture   │    │  • Frontmatter updates        │   │
│  │    synthesis      │    │  • _log.md entries            │   │
│  │  • Cross-module   │    │  • _index.md regeneration    │   │
│  │    contradiction  │    │  • _Sidebar.md generation    │   │
│  │    detection      │    │  • Cross-reference validation │   │
│  │  • ADR writing    │    │  • Staleness checks          │   │
│  │  • Deep code      │    │  • Orphan page detection     │   │
│  │    understanding  │    │  • Link resolution           │   │
│  │  • New page       │    │  • Manifest updates          │   │
│  │    synthesis from │    │  • Confidence tagging        │   │
│  │    raw sources    │    │  • WIKI-DEBT logging         │   │
│  │                   │    │  • Page state transitions    │   │
│  └────────┬─────────┘    └──────────────┬───────────────┘   │
│           │                              │                   │
│           ▼                              ▼                   │
│  ┌──────────────────┐    ┌──────────────────────────────┐   │
│  │  Primary Agent    │    │  Local Assistive Agent        │   │
│  │  (Cloud API)      │    │  (On-Device)                  │   │
│  │                   │    │                               │   │
│  │  Claude Opus      │    │  Gemma 4 26B-A4B             │   │
│  │  GPT-5            │    │  via Ollama / llama.cpp       │   │
│  │  Codex            │    │  ~40 tok/s on Apple Silicon   │   │
│  │  Gemini           │    │  $0.00 per token              │   │
│  │                   │    │  256K context                 │   │
│  │  $2-15/M tokens   │    │  Native function calling     │   │
│  └──────────────────┘    └──────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## Gemma 4 Model Assessment for Plexium Tasks

<details>
<summary><strong>Gemma 4 26B-A4B (MoE) — The Recommended Variant</strong></summary>

| Property | Value |
|----------|-------|
| Total Parameters | 25.2B |
| **Active Parameters** | **3.8B** (8 of 128 experts + 1 shared) |
| Context Window | **256K tokens** |
| Architecture | Mixture-of-Experts |
| Inference Speed (M4 Mac, 8-bit) | **~40-43 tok/s** ([youtube.com](https://www.youtube.com/watch?v=_lXgq-U49Aw)) |
| RAM (4-bit GGUF) | **16-18 GB** ([unsloth.ai](https://unsloth.ai/docs/models/gemma-4)) |
| RAM (8-bit GGUF) | **28-30 GB** |
| Function Calling | **Native** ([ai.google.dev](https://ai.google.dev/gemma/docs/core/model_card_4)) |
| Structured JSON Output | **Native** |
| System Prompt Support | **Native** (`system` role) |
| Thinking Mode | **Configurable** (enable/disable per request) |
| License | **Apache 2.0** (fully commercial) |
| MMLU Pro | 82.6% |
| LiveCodeBench v6 | 77.1% |

**Why this variant:** The MoE architecture is the killer feature. Only 3.8B parameters activate per token, meaning it runs at near-4B-model speeds while having 26B-model quality available through expert selection. For wiki maintenance tasks — which are structurally similar but draw on different domain knowledge (frontmatter schema, markdown syntax, cross-reference patterns, git concepts) — having 128 specialized experts is architecturally ideal. The model selects the right experts for the task without paying the full 26B inference cost.

</details>

<details>
<summary><strong>Gemma 4 31B (Dense) — The Quality Alternative</strong></summary>

| Property | Value |
|----------|-------|
| Total Parameters | 30.7B |
| Context Window | 256K tokens |
| Inference Speed | Slower than MoE (~10-15 tok/s on consumer hardware) |
| RAM (4-bit GGUF) | 17-20 GB ([unsloth.ai](https://unsloth.ai/docs/models/gemma-4)) |
| MMLU Pro | 85.2% |
| LiveCodeBench v6 | 80.0% |

**When to use:** If maximum accuracy on wiki synthesis tasks matters more than speed (e.g., batch lint runs, periodic full-wiki reconciliation). Not recommended as the default assistive agent due to slower inference in the interactive loop.

</details>

<details>
<summary><strong>Gemma 4 E4B (Edge) — The Minimal Footprint Option</strong></summary>

| Property | Value |
|----------|-------|
| Effective Parameters | 4.5B |
| Context Window | 128K tokens |
| RAM (4-bit) | 5.5-6 GB |
| MMLU Pro | 69.4% |

**When to use:** Developers on 16GB MacBooks who can't spare 18GB for the 26B model. Trades quality for accessibility. Could handle the simplest tier of tasks (frontmatter validation, `_log.md` appending, link checking) but would struggle with synthesis or cross-referencing.

</details>

### Capability Match to Plexium Tasks

| Plexium Task | Cognitive Demand | Gemma 4 26B-A4B Suitable? | Key Capability Required |
|---|---|---|---|
| Append entry to `_log.md` | Low | ✅ Excellent | Structured output, date formatting |
| Update frontmatter fields | Low | ✅ Excellent | YAML generation, schema compliance |
| Validate `[[wiki-links]]` resolve | Low | ✅ Excellent | String matching, file existence check |
| Regenerate `_Sidebar.md` from `_index.md` | Low | ✅ Excellent | Deterministic template following |
| Regenerate `_index.md` from page state | Medium | ✅ Good | Cataloging, consistent formatting |
| Check `source-files` paths exist | Low | ✅ Excellent (or deterministic — no LLM needed) | File system awareness |
| Detect orphan pages (graph traversal) | Low | ✅ Excellent (or deterministic) | Link parsing |
| Update manifest hashes | Low | ✅ Excellent | JSON manipulation, hash computation |
| Page state transitions (stale → regenerated) | Low | ✅ Excellent | State machine logic |
| Cross-reference suggestions | Medium | ✅ Good | Semantic similarity, wiki-link generation |
| Summarize a single module from source | Medium | ✅ Good | Code comprehension, concise writing |
| Synthesize architecture overview from multiple modules | High | ⚠️ Marginal — route to primary agent | Deep multi-document reasoning |
| Detect contradictions between pages | High | ⚠️ Marginal — route to primary agent | Nuanced semantic comparison |
| Write ADR from scratch | High | ❌ Route to primary agent | Deep architectural reasoning |
| Ingest complex raw source (meeting notes → wiki) | High | ⚠️ Depends on source complexity | Extract, synthesize, cross-reference |

**The 80/20 split is real:** roughly 80% of wiki maintenance operations fall in the Low/Medium band where Gemma 4 26B-A4B performs reliably. The remaining 20% — deep synthesis, contradiction detection, new page creation from complex sources — should stay with the primary cloud agent.

---

## Implementation Architecture

### The Local Agent Server

The assistive agent runs as a **persistent local server** via Ollama or llama.cpp, exposing an OpenAI-compatible API on localhost. The `plexium` CLI and the daemon communicate with it the same way they'd call any LLM API, but at `http://localhost:11434` instead of a cloud endpoint.

```yaml
# .plexium/config.yml additions
localAgent:
  enabled: false                        # Opt-in
  provider: ollama                      # ollama | llama-cpp | lm-studio
  endpoint: "http://localhost:11434"
  model: "gemma4:26b-a4b"
  thinkingMode: false                   # Disable for simple tasks, enable for synthesis
  contextBudget: 32768                  # Tokens per request (conservative default)
  
  # Task routing: which tasks go to local vs. cloud
  routing:
    local:                              # These tasks use the local agent
      - frontmatter-update
      - log-entry
      - index-regeneration
      - sidebar-regeneration
      - link-validation
      - cross-reference-suggestion
      - manifest-update
      - page-state-transition
      - module-summary                  # Simple single-module summaries
      - staleness-check
      - wiki-debt-logging
    cloud:                              # These tasks use the primary coding agent
      - architecture-synthesis
      - contradiction-detection
      - adr-creation
      - complex-ingest
      - deep-code-analysis
    auto:                               # Let the router decide based on complexity heuristics
      - page-regeneration               # Simple pages → local; complex → cloud
      - cross-reference-generation      # Few pages → local; many → cloud
```

### Serving Setup

```bash
# Option A: Ollama (simplest)
ollama pull gemma4:26b-a4b
ollama serve
# Model is now available at http://localhost:11434

# Option B: llama.cpp server (more control)
./llama-server \
    --model gemma-4-26B-A4B-it-Q4_K_M.gguf \
    --mmproj mmproj-BF16.gguf \
    --port 11434 \
    --alias "gemma4:26b-a4b" \
    --chat-template-kwargs '{"enable_thinking":false}' \
    --ctx-size 32768

# Option C: plexium manages it
plexium agent start                   # Starts Ollama with configured model
plexium agent status                  # Check if local agent is running
plexium agent stop                    # Stop the local agent
```

### The System Prompt for the Local Agent

The local assistive agent gets a stripped-down version of `_schema.md` focused purely on wiki maintenance tasks — no coding instructions, no architectural reasoning directives. This keeps the system prompt small (~500 tokens) and focused:

```markdown
# PLEXIUM LOCAL AGENT — WIKI MAINTENANCE DIRECTIVES

You are a wiki maintenance agent for the `.wiki/` vault. You handle 
structured updates, not creative synthesis.

## YOUR RESPONSIBILITIES
- Update YAML frontmatter (ownership, timestamps, confidence, tags)
- Append entries to _log.md in the standard parseable format
- Regenerate _index.md and _Sidebar.md from page state
- Validate and repair [[wiki-links]]
- Update manifest.json with new hashes and mappings
- Suggest cross-references between related pages
- Transition page states (stub → generated, stale → regenerated)
- Log WIKI-DEBT entries when bypasses are detected

## RULES
- NEVER modify page content beyond frontmatter and structural elements
- NEVER modify pages with ownership: human-authored
- NEVER invent information not present in source files or existing pages
- Always output valid YAML for frontmatter and valid JSON for manifest
- When uncertain, mark with <!-- CONFIDENCE: low -->
- Use structured output (JSON) when the caller requests it

## LOG FORMAT
## [YYYY-MM-DD] {task|maintenance|lint} | Brief description
- Changed: <list of modified wiki paths>
- Agent: local-gemma4
```

### The Interaction Pattern

The `plexium` CLI orchestrates the split. Here's the flow during a typical coding session:

```
Developer + Primary Agent (Claude Code)
    │
    │  1. Developer asks Claude to refactor auth module
    │  2. Claude reads .wiki/modules/auth.md (context)
    │  3. Claude refactors code
    │  4. Claude writes updated auth.md content
    │  5. Claude signals: "wiki pages updated"
    │
    ▼
plexium post-commit hook fires
    │
    │  Deterministic checks first (zero LLM):
    │  - Which source files changed?
    │  - Which wiki pages map to them?
    │  - Are frontmatter timestamps current?
    │  - Do all [[links]] resolve?
    │
    │  If wiki was updated by primary agent: ✅ pass
    │  If wiki was NOT updated:
    │
    ▼
Task Router
    │
    ├── Simple tasks → Local Agent (Gemma 4)
    │   │
    │   │  "Update frontmatter last-updated for modules/auth.md"
    │   │  "Append sync entry to _log.md"  
    │   │  "Regenerate _Sidebar.md"
    │   │  "Check cross-references from auth.md"
    │   │
    │   └── Done. ~2-5 seconds. $0.00.
    │
    └── Complex tasks → Queue for primary agent or flag as WIKI-DEBT
        │
        │  "Architecture overview may need updating"
        │  "Potential contradiction between auth.md and overview.md"
        │
        └── Logged. Human or next cloud session handles it.
```

### During Daemon Mode (Symphony Integration)

The local agent becomes even more valuable in the daemon mode proposed in the Symphony analysis:

```
plexium daemon (running continuously)
    │
    │  Poll: any stale pages? any WIKI-DEBT?
    │
    ├── Stale page detected: modules/api-gateway.md
    │   │
    │   │  Complexity assessment:
    │   │  - Source files changed: 2 files, +30 lines
    │   │  - Change type: new error handling pattern
    │   │  - Mapped wiki page: 1 module page
    │   │
    │   │  Verdict: MEDIUM → Local Agent can handle
    │   │
    │   ▼ Local Agent (Gemma 4):
    │   │  1. Read source diff
    │   │  2. Read current modules/api-gateway.md
    │   │  3. Update relevant section
    │   │  4. Update frontmatter
    │   │  5. Append _log.md entry
    │   │  6. Mark confidence: medium
    │   │
    │   └── Done. ~15 seconds. $0.00.
    │
    └── Complex stale page: architecture/overview.md
        │
        │  Verdict: HIGH → Queue for next cloud session or create issue
        │
        └── Logged as WIKI-DEBT with remediation suggestion
```

---

## Token Economics: The Case in Numbers

Let's quantify the savings for a medium-sized project (~100K LOC, 50 wiki pages, active development team).

### Current Model (All Cloud)

| Operation | Frequency | Tokens per Op | Monthly Tokens | Cost @ $3/M input, $15/M output |
|---|---|---|---|---|
| Agent reads `_index.md` + relevant pages | 20/day | ~5,000 | 3,000,000 | ~$9 input |
| Agent updates wiki pages after coding | 15/day | ~3,000 output | 1,350,000 | ~$20 output |
| Weekly lint (LLM-augmented) | 4/month | ~50,000 | 200,000 | ~$3 |
| Ingest operations | 5/month | ~20,000 | 100,000 | ~$1.50 |
| **Total wiki-related token cost** | | | **~4,650,000** | **~$33.50/mo** |

This is on top of the coding agent's primary token usage. At frontier model pricing, wiki overhead adds 15-25% to total agent costs.

### Proposed Model (Split Routing)

| Operation | Agent | Tokens | Cost |
|---|---|---|---|
| Complex reads (architecture, synthesis) | Cloud (20%) | 600,000 | ~$1.80 |
| Complex writes (ADRs, architecture pages) | Cloud (20%) | 270,000 | ~$4.05 |
| Simple reads (frontmatter, links, index) | **Local** (80%) | 2,400,000 | **$0.00** |
| Simple writes (log, sidebar, frontmatter) | **Local** (80%) | 1,080,000 | **$0.00** |
| Weekly lint (deterministic + local LLM) | **Local** (100%) | 200,000 | **$0.00** |
| **Total wiki-related token cost** | | | **~$5.85/mo** |

**Savings: ~83% reduction in wiki-related API costs.** For a team running multiple repos, this compounds significantly.

The local agent also has **zero latency to the API** — no network round-trip. For the daemon mode polling every 5 minutes and running dozens of small checks, this eliminates thousands of API calls per day that would otherwise add up.

---

## Hardware Requirements and Developer Experience

### Minimum Viable Setup

| Hardware | Model | Performance | Experience |
|---|---|---|---|
| MacBook Pro M2/M3/M4 with 32GB | Gemma 4 26B-A4B (4-bit) | ~30-40 tok/s | Smooth. Background agent barely noticeable alongside primary coding agent. |
| MacBook Pro M-series with 16GB | Gemma 4 E4B (4-bit) | ~40+ tok/s | Workable but limited quality. Best for frontmatter-only tasks. |
| Linux workstation with RTX 4070+ (12GB) | Gemma 4 26B-A4B (4-bit) | ~60-90 tok/s | Excellent. GPU handles local agent while CPU runs coding tools. |
| Linux workstation with RTX 4090 (24GB) | Gemma 4 26B-A4B (8-bit) | ~40-60 tok/s | Premium. Higher quantization = better quality for synthesis tasks. |

### The Ollama Background Pattern

On macOS or Linux, Ollama runs as a persistent background service. It loads the model into memory once and keeps it warm (configurable `OLLAMA_KEEP_ALIVE`). The `plexium` CLI makes HTTP calls to `localhost:11434` — this adds ~50-100ms of overhead versus direct library calls, which is negligible for wiki maintenance operations.

```bash
# Developer's terminal 1: coding with Claude Code
cd my-project
claude

# Developer's terminal 2 (or background): plexium daemon
plexium daemon --local-agent

# Behind the scenes:
# - Ollama is running with gemma4:26b-a4b loaded
# - plexium daemon polls for changes
# - Simple wiki tasks → localhost:11434 (Gemma 4)
# - Complex tasks → flagged for next cloud session
```

The developer never interacts with the local agent directly. It's infrastructure. They see its effects: wiki pages stay current, `_log.md` entries appear, frontmatter timestamps are fresh, and `plexium lint` reports fewer issues.

---

## What the Local Agent Should NOT Do

Clarity on boundaries prevents scope creep and quality degradation:

| Task | Why Not Local |
|---|---|
| **Write new wiki pages from scratch** | Requires deep codebase understanding and synthesis that benefits from frontier model quality |
| **Resolve contradictions** | Requires nuanced semantic comparison across multiple documents — too error-prone at local model quality |
| **Modify `human-authored` pages** | Not a model capability issue — this is an ownership invariant that applies to ALL agents |
| **Make architectural decisions** | Wiki documentation of architecture should reflect frontier-quality reasoning |
| **Ingest complex raw sources** (meeting notes with implicit decisions) | Extraction of implicit knowledge from unstructured sources needs frontier comprehension |
| **Override primary agent's wiki updates** | The primary agent's edits take precedence; local agent only fills gaps |

**The principle: the local agent is a maintainer, not an author.** It keeps existing wiki pages healthy. It doesn't create knowledge — it preserves and organizes it.

---

## Recommended PRP Amendments

### New Section: §16.5 — Local Assistive Agent

> **On-Device Assistive Agent — Token-Efficient Wiki Maintenance**
>
> An optional on-device LLM (default: Gemma 4 26B-A4B via Ollama) handles low-complexity wiki maintenance tasks at zero API cost, freeing the primary coding agent for cognitive work.
>
> **Task routing:**
> - **Local agent handles:** frontmatter updates, `_log.md` entries, `_index.md`/`_Sidebar.md` regeneration, link validation, cross-reference suggestions, manifest updates, page state transitions, simple module summaries, WIKI-DEBT logging.
> - **Primary agent handles:** architecture synthesis, contradiction detection, ADR creation, complex ingestion, deep code analysis, new page creation.
> - **Deterministic pipeline handles (no LLM):** hash computation, path validation, orphan detection by graph traversal, manifest consistency checks.
>
> **Serving:** Ollama (`ollama serve` + `ollama pull gemma4:26b-a4b`) or llama.cpp server on localhost. The `plexium` CLI communicates via OpenAI-compatible REST API.
>
> **System prompt:** Stripped-down `_schema.md` focused on structural maintenance. ~500 tokens. No coding or architectural reasoning directives.
>
> **Thinking mode:** Disabled by default for simple tasks (faster inference). Enabled selectively for cross-reference suggestion and module summary tasks.
>
> **Fallback:** If the local agent is unavailable (`plexium doctor` detects this), all tasks route to the primary agent. The system degrades gracefully to the all-cloud model.

### Add to Config (§8)

```yaml
localAgent:
  enabled: false
  provider: ollama
  endpoint: "http://localhost:11434"
  model: "gemma4:26b-a4b"
  fallbackModel: "gemma4:e4b"           # For lower-memory systems
  thinkingMode: false
  contextBudget: 32768
  keepAlive: 30m                         # How long to keep model in memory
  routing:
    local: [frontmatter-update, log-entry, index-regeneration, 
            sidebar-regeneration, link-validation, cross-reference-suggestion,
            manifest-update, page-state-transition, module-summary,
            staleness-check, wiki-debt-logging]
    cloud: [architecture-synthesis, contradiction-detection, 
            adr-creation, complex-ingest, deep-code-analysis]
```

### Add CLI Commands (§9)

| Command | Description |
|---------|-------------|
| `plexium agent start` | Start the local assistive agent (launches Ollama if needed) |
| `plexium agent stop` | Stop the local agent |
| `plexium agent status` | Check local agent health, model loaded, memory usage |
| `plexium agent benchmark` | Run a diagnostic task set to verify local agent quality |

### Add to Phase 3 Scope (§22)

```
- Local assistive agent integration (Ollama / llama.cpp)
- Task router with complexity classification
- System prompt generation for local agent
- `plexium agent` CLI commands (start, stop, status, benchmark)
- Automatic fallback to cloud when local agent unavailable
- Token cost tracking (local vs. cloud) in reports
```

### Add Open Design Question #12

```
12. **Local agent model selection:** Should Plexium bundle a recommended model 
    (Gemma 4 26B-A4B) and auto-pull it during `plexium init --local-agent`, 
    or should it remain model-agnostic and let users configure any Ollama-served 
    model? Bundling simplifies onboarding but couples to a specific model family.
    Model-agnostic preserves flexibility but requires users to benchmark quality.
```

### Add Acceptance Criteria (§24)

```
### Local Agent
- [ ] `plexium agent start` launches Ollama and loads configured model
- [ ] `plexium agent status` reports model name, memory usage, and health
- [ ] Tasks routed to local agent produce valid YAML frontmatter
- [ ] Tasks routed to local agent produce valid JSON manifest updates
- [ ] `_log.md` entries from local agent follow the standard parseable format
- [ ] Local agent never modifies pages with ownership: human-authored
- [ ] Fallback to cloud agent works seamlessly when local is unavailable
- [ ] Token cost report distinguishes local vs. cloud token usage
- [ ] `plexium agent benchmark` passes quality threshold on reference task set
```

---

## Broader Model Ecosystem Note

While Gemma 4 26B-A4B is the recommended default today, the architecture should be **model-agnostic.** The local agent interface is just an OpenAI-compatible REST API on localhost. Any model served by Ollama, llama.cpp, vLLM, or LM Studio works. This future-proofs against:

- **Qwen 3.5 35B-A3B** — another strong MoE option (35B total, 3B active, 256K context), currently leading local coding benchmarks ([insiderllm.com](https://insiderllm.com/guides/pi-agent-local-models-ollama/))
- **Future Gemma releases** — Google's cadence suggests Gemma 5 within a year
- **Fine-tuned variants** — someone *will* fine-tune Gemma 4 on wiki maintenance tasks, and the quality will jump
- **Smaller models** as hardware evolves — what requires 18GB today may fit in 8GB in two generations

The `localAgent.model` config key and Ollama's model management handle this transparently. Swap models with `ollama pull newmodel && plexium config set localAgent.model newmodel`.

---

## Bottom Line

| Dimension | Assessment |
|---|---|
| **Technical viability** | ✅ High — Gemma 4 26B-A4B has native function calling, structured output, 256K context, and runs at 40+ tok/s on commodity hardware |
| **Quality for target tasks** | ✅ High — 82.6% MMLU Pro is far more than needed for frontmatter updates and link validation |
| **Cost impact** | ✅ ~83% reduction in wiki-related API costs |
| **Developer experience** | ✅ Invisible — runs as background infrastructure via Ollama |
| **Architectural fit** | ✅ Perfect — implements the PRP's existing role separation model |
| **Risk** | ⚠️ Low — graceful fallback to cloud means local agent failure is a performance regression, not a system failure |
| **Timing** | Phase 3 feature, design the interface now |

The local assistive agent transforms Plexium's token economics from "wiki maintenance is an ongoing API cost center" to "wiki maintenance is a one-time hardware cost that runs forever." For teams running Plexium across multiple repos with the daemon mode, this is the difference between a system that's expensive to operate and one that's effectively free after hardware.

**Build the router interface in Phase 2 (abstract "send this task to an LLM" behind a provider-agnostic function). Wire in the local agent in Phase 3. Default to cloud-only. Let `--local-agent` be the opt-in flag. The people who care about token costs will find it immediately.**