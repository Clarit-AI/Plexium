> **Reference Document** — This is voluntary reading for context and rationale.
> The canonical specification is `plexium-spec.md` at project root.
> Agents may consult this but should NOT treat it as authoritative where it conflicts with the PRP.

# Cloud Fallback for Assistive Agents via OpenRouter

---

## Short Answer

**Not just viable — it's arguably the better default for most users.** The economics are stunning. Gemma 4 26B-A4B on OpenRouter costs **$0.13/M input, $0.40/M output** ([openrouter.ai](https://openrouter.ai/google/gemma-4-26b-a4b-it)). But it gets better: OpenRouter currently offers **28+ free models with tool calling support**, including several that are more than capable of handling wiki maintenance tasks at literally **$0.00/M tokens** ([costgoat.com](https://costgoat.com/pricing/openrouter-free-models)). The user doesn't need to run anything locally. No 18GB of RAM consumed. No Ollama to manage. Just an API key and a config line.

This transforms the assistive agent from a "power user feature requiring beefy hardware" into a **zero-friction, zero-cost default** that works on any machine, including CI runners, cloud dev environments, and Chromebooks.

---

## The Three-Tier Provider Cascade

The right architecture isn't "local OR cloud." It's a **fallback cascade** — try the cheapest viable option first, escalate only when necessary:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Task Router                                   │
│    Classifies task complexity → selects provider tier            │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  TIER 1: Local Agent (if configured)                    │    │
│  │  Ollama / llama.cpp on localhost                        │    │
│  │  Model: gemma4:26b-a4b (or user's choice)              │    │
│  │  Cost: $0.00    Latency: ~25ms    Privacy: Total       │    │
│  │  Requires: 18-30GB RAM, Ollama running                  │    │
│  │                                                         │    │
│  │  ↓ Falls through if: not configured, not running,       │    │
│  │    model not loaded, or health check fails              │    │
│  └──────────────────────┬──────────────────────────────────┘    │
│                         ▼                                        │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  TIER 2: OpenRouter Assistive (default fallback)        │    │
│  │  API: https://openrouter.ai/api/v1                      │    │
│  │  Model: google/gemma-4-26b-a4b-it ($0.13/$0.40 per M)  │    │
│  │     OR: openrouter/free (auto-routes to free models)    │    │
│  │     OR: nvidia/nemotron-3-super-120b-a3b:free ($0.00)   │    │
│  │     OR: openai/gpt-oss-120b:free ($0.00)                │    │
│  │  Cost: $0.00–$0.40/M  Latency: ~200ms  Privacy: Varies │    │
│  │  Requires: OpenRouter API key only                      │    │
│  │                                                         │    │
│  │  ↓ Falls through if: rate limited, API down,            │    │
│  │    no API key configured                                │    │
│  └──────────────────────┬──────────────────────────────────┘    │
│                         ▼                                        │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  TIER 3: Primary Coding Agent (expensive, powerful)     │    │
│  │  Claude Opus/Sonnet, GPT-5, Gemini Pro, Codex           │    │
│  │  Cost: $2–$25/M   Latency: varies   Quality: Frontier  │    │
│  │  Used for: complex synthesis, contradiction detection,  │    │
│  │  ADR writing, architecture pages, deep code analysis    │    │
│  │                                                         │    │
│  │  Also the ONLY tier for HIGH complexity tasks —          │    │
│  │  these never route to Tier 1 or 2 regardless.           │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

The cascade logic is simple: **try the cheapest tier that's available and capable. Escalate on failure.** If the user has local configured and running, use it. If not, hit OpenRouter. If OpenRouter is down or unconfigured, fall through to the primary agent. For high-complexity tasks, skip directly to Tier 3.

---

## OpenRouter Model Options for Wiki Maintenance

Here's what's actually available today, ranked by fit for Plexium's assistive tasks:

### Free Tier ($0.00/M tokens)

| Model | Context | Tool Calling | Best For |
|-------|---------|-------------|----------|
| **NVIDIA Nemotron 3 Super** (`nvidia/nemotron-3-super-120b-a12b:free`) | 262K | ✅ | Strong general-purpose, 120B MoE with only 12B active. Excellent for structured wiki tasks. ([costgoat.com](https://costgoat.com/pricing/openrouter-free-models)) |
| **OpenAI GPT-OSS 120B** (`openai/gpt-oss-120b:free`) | 131K | ✅ | 117B MoE, 5.1B active. Native structured output. Apache 2.0. Purpose-built for tool use. ([teamday.ai](https://www.teamday.ai/blog/best-free-ai-models-openrouter-2026)) |
| **Qwen3.6 Plus** (`qwen/qwen3.6-plus:free`) | 1M | ✅ | Massive context, strong reasoning, free. Overkill for frontmatter but handles complex cross-referencing well. |
| **Step 3.5 Flash** (`stepfun/step-3.5-flash:free`) | 256K | ✅ | 196B MoE, only 11B active. Optimized for speed. |
| **NVIDIA Nemotron 3 Nano** (`nvidia/nemotron-3-nano-30b-a3b:free`) | 256K | ✅ | 30B MoE, 3B active. Closest to local Gemma 4 in profile. |
| **`openrouter/free`** (auto-router) | 200K | ✅ | Routes to whichever free model best handles your request's requirements. Zero config. ([openrouter.ai](https://openrouter.ai/openrouter/free)) |

### Budget Tier ($0.04–$0.40/M tokens)

| Model | Input $/M | Output $/M | Context | Why |
|-------|-----------|------------|---------|-----|
| **GPT-OSS 120B** (paid) | $0.039 | $0.19 | 131K | Higher rate limits than free tier. Nearly free. |
| **Gemma 4 26B-A4B** | $0.13 | $0.40 | 262K | The same model you'd run locally, served in the cloud. Native function calling + structured output. ([openrouter.ai](https://openrouter.ai/google/gemma-4-26b-a4b-it)) |
| **Gemma 4 31B** | $0.14 | $0.40 | 262K | Dense variant. Higher quality than MoE on complex tasks. |
| **Gemini 2.5 Flash Lite** | $0.10 | $0.40 | 1.05M | Google's own serving. Configurable thinking. |

### The Free Models Are Genuinely Good Enough

This isn't charity-tier quality. The free models on OpenRouter include 120B-parameter MoE models with native tool calling. For Plexium's assistive tasks — frontmatter updates, `_log.md` entries, `_Sidebar.md` regeneration, link validation, manifest updates — a model like **Nemotron 3 Super** (120B total, 12B active, 262K context, free) is wildly overqualified. The primary risk isn't quality — it's **rate limits** (20 req/min, 200 req/day on free tier) ([costgoat.com](https://costgoat.com/pricing/openrouter-free-models)).

---

## Revised Token Economics: Three Scenarios

For the same medium-sized project (~100K LOC, 50 wiki pages, active development):

### Scenario A: All-Cloud Primary Agent (Current PRP Baseline)

| | Monthly Tokens | Cost |
|---|---|---|
| Wiki reads (context loading) | 3,000,000 | ~$9.00 |
| Wiki writes (page updates) | 1,350,000 | ~$20.25 |
| Lint + ingest | 300,000 | ~$4.50 |
| **Total** | **4,650,000** | **~$33.75/mo** |

### Scenario B: Local Assistive Agent (Previous Proposal)

| | Agent | Monthly Tokens | Cost |
|---|---|---|---|
| Complex tasks (20%) | Cloud | 930,000 | ~$5.85 |
| Simple tasks (80%) | Local (Gemma 4) | 3,720,000 | $0.00 |
| **Total** | | **4,650,000** | **~$5.85/mo** |

Requires: 18-30GB RAM, Ollama running

### Scenario C: OpenRouter Assistive Agent (New Proposal)

| | Agent | Monthly Tokens | Cost |
|---|---|---|---|
| Complex tasks (20%) | Primary cloud agent | 930,000 | ~$5.85 |
| Simple tasks (80%) via free models | OpenRouter free tier | 3,720,000 | **$0.00** |
| **Total** | | **4,650,000** | **~$5.85/mo** |

Requires: OpenRouter API key only. Runs on any machine.

### Scenario D: OpenRouter Budget Tier (Higher Rate Limits)

| | Agent | Monthly Tokens | Cost |
|---|---|---|---|
| Complex tasks (20%) | Primary cloud agent | 930,000 | ~$5.85 |
| Simple tasks (80%) via Gemma 4 | OpenRouter paid ($0.13/$0.40) | 3,720,000 | **~$1.00** |
| **Total** | | **4,650,000** | **~$6.85/mo** |

Requires: OpenRouter API key + ~$1/mo credits. No rate limit anxiety.

**The punchline:** Scenarios B, C, and D all achieve roughly the same ~83% cost reduction. The difference is whether you pay that savings in **RAM and hardware management** (B) or in **essentially nothing** (C/D). For most teams, C or D is the obvious default.

---

## Rate Limit Management

The free tier's 200 requests/day limit is the main constraint. Let's see if it's actually a problem:

### Typical Daily Wiki Operations

| Operation | Requests | Notes |
|---|---|---|
| Post-commit frontmatter updates | 5-10 | Per active developer per day |
| `_log.md` entries | 5-10 | One per commit with wiki relevance |
| `_Sidebar.md` regeneration | 1-3 | Only when page structure changes |
| Link validation batches | 2-5 | Can batch many links per request |
| Cross-reference suggestions | 3-5 | Per coding session |
| Staleness checks (daemon) | 5-10 | Batch multiple pages per request |
| **Typical daily total** | **~25-50** | Well within 200/day |

For a solo developer or small team, the free tier handles this comfortably. **The rate limit only becomes a problem during bulk operations** — `plexium convert` on a large brownfield repo, or a full wiki regeneration. The solution:

```yaml
# .plexium/config.yml
assistiveAgent:
  provider: openrouter
  tier: free                           # free | budget | local
  
  # Rate limit management
  rateLimits:
    strategy: adaptive                 # adaptive | strict | ignore
    batchSize: 5                       # Max concurrent requests
    cooldownMs: 3100                   # Delay between batches (20 req/min = 3s each)
    dailyBudget: 180                   # Reserve 20 requests for ad-hoc tasks
    
  # Bulk operation fallback
  bulkOperations:
    tier: budget                       # Upgrade to paid tier for convert/bootstrap
    model: "google/gemma-4-26b-a4b-it" # $0.13/$0.40 per M
    maxSpend: 2.00                     # Hard cap for any single bulk operation
```

**Adaptive strategy:** Track daily request count. When approaching the limit, batch more aggressively (more pages per request), defer non-urgent tasks, or automatically escalate to the budget tier. When the budget tier's `maxSpend` is hit, defer remaining tasks to the next day or log them as queued work.

---

## The Configuration Surface

### Unified Provider Config

The previous response proposed `localAgent` config. Generalize it to a **provider-agnostic assistive agent config**:

```yaml
# .plexium/config.yml
assistiveAgent:
  enabled: true
  
  # Provider cascade (tried in order)
  providers:
    - name: local
      enabled: false                    # Opt-in for users with hardware
      type: ollama
      endpoint: "http://localhost:11434"
      model: "gemma4:26b-a4b"
      
    - name: openrouter-free
      enabled: true                     # Default on
      type: openai-compatible
      endpoint: "https://openrouter.ai/api/v1"
      model: "openrouter/free"          # Auto-routes to best free model
      apiKeyEnv: "OPENROUTER_API_KEY"   # Env var name, not the key itself
      rateLimits:
        requestsPerMinute: 20
        requestsPerDay: 200
      
    - name: openrouter-budget
      enabled: false                    # Opt-in when free tier is too constrained
      type: openai-compatible
      endpoint: "https://openrouter.ai/api/v1"
      model: "google/gemma-4-26b-a4b-it"
      apiKeyEnv: "OPENROUTER_API_KEY"
      maxMonthlySpend: 5.00             # Hard cap in USD
      
    - name: primary                     # Always last — the expensive fallback
      enabled: true
      type: inherit                     # Uses whatever the coding agent is
      # No model/endpoint config needed — inherits from the active agent session
  
  # Task routing (same as before — unchanged by provider choice)
  routing:
    assistive:                          # Route to Tier 1/2 providers
      - frontmatter-update
      - log-entry  
      - index-regeneration
      - sidebar-regeneration
      - link-validation
      - cross-reference-suggestion
      - manifest-update
      - page-state-transition
      - module-summary
      - staleness-check
      - wiki-debt-logging
    primary:                            # Always route to Tier 3
      - architecture-synthesis
      - contradiction-detection
      - adr-creation
      - complex-ingest
      - deep-code-analysis
```

### The Key Insight: `type: openai-compatible`

OpenRouter exposes an **OpenAI-compatible API**. So does Ollama. So does llama.cpp server. So does LM Studio, vLLM, Together AI, Groq, Fireworks, and every other inference provider that matters. By building the assistive agent interface against the OpenAI chat completions API shape, **Plexium's task router works with any provider, local or cloud, current or future, without code changes.** The config is the only thing that varies.

```bash
# Developer on a beefy workstation: local + OpenRouter fallback
plexium init --assistive-agent local,openrouter-free

# Developer on a laptop: OpenRouter only
plexium init --assistive-agent openrouter-free

# Team with budget: OpenRouter budget tier
plexium init --assistive-agent openrouter-budget

# Air-gapped environment: local only
plexium init --assistive-agent local

# Enterprise with self-hosted vLLM: custom endpoint
plexium config set assistiveAgent.providers[0].endpoint "http://internal-vllm:8000/v1"
```

---

## Privacy Considerations by Tier

This matters and should be documented clearly:

| Tier | Data Handling | Suitable For |
|---|---|---|
| **Local (Ollama)** | Never leaves the machine. Total privacy. | Proprietary codebases, regulated industries, air-gapped environments |
| **OpenRouter Free** | Some free models log prompts for training. Check model cards. Provider-dependent. ([teamday.ai](https://www.teamday.ai/blog/best-free-ai-models-openrouter-2026)) | Open-source projects, non-sensitive wiki content |
| **OpenRouter Paid** | Provider-dependent. OpenRouter's paid tier generally has better data policies. | Most commercial projects |
| **Primary Agent (Cloud)** | Depends on agent (Anthropic, OpenAI, Google policies) | Whatever the team already trusts for code |

**Default behavior for the sensitivity-conscious:**

```yaml
# .plexium/config.yml
assistiveAgent:
  privacyMode: strict                   # strict | standard
  # strict: only sends to providers marked 'no-training'
  # standard: uses any configured provider
  
  neverSend:                            # Content patterns never sent to assistive agent
    - "raw/internal/**"                 # Sensitive raw sources
    - "**/*CONFIDENTIAL*"              
    # Frontmatter with 'sensitivity: high' is also excluded
```

In `strict` privacy mode, Plexium only routes to providers that don't log prompts for training. For OpenRouter, that means filtering to open-weight models served by providers with explicit no-training guarantees (like self-hosted options or enterprise-tier providers). If no qualifying provider is available, tasks fall through to the primary agent or are deferred.

---

## CLI Changes

### Updated Commands

```bash
# Setup
plexium init --assistive-agent openrouter-free    # Default for most users
plexium init --assistive-agent local              # For power users
plexium init --assistive-agent local,openrouter-free  # Cascade

# Management
plexium agent status                              # Shows all tiers + health
plexium agent test                                # Sends a diagnostic task to each tier
plexium agent spend                               # Shows token usage by tier this month

# One-time config
plexium config set assistiveAgent.providers[1].apiKeyEnv OPENROUTER_API_KEY
```

### `plexium agent status` Output Example

```
Plexium Assistive Agent Status
───────────────────────────────────────────────────
  Tier 1 (Local):
    Status:    ⚫ Not configured
    
  Tier 2 (OpenRouter Free):
    Status:    🟢 Healthy
    Model:     openrouter/free (auto-routing)
    Last used: 2 minutes ago
    Today:     47/200 requests used (153 remaining)
    Latency:   ~180ms avg
    
  Tier 3 (Primary Agent):
    Status:    🟢 Available (Claude Opus 4.6 via CLAUDE.md)
    Reserved:  HIGH complexity tasks only
───────────────────────────────────────────────────
  This month:  142,000 tokens via assistive agents
  Cost saved:  ~$4.12 vs. routing all to primary agent
```

---

## Updated PRP Amendment: §16.5 Assistive Agent (Revised)

> **Assistive Agent — Provider-Agnostic Token-Efficient Wiki Maintenance**
>
> An optional lightweight LLM handles low-complexity wiki maintenance tasks, freeing the primary coding agent for cognitive work. The assistive agent operates through a **provider cascade**: local inference (Ollama), cloud API (OpenRouter), or any OpenAI-compatible endpoint.
>
> **Default provider:** OpenRouter free tier (`openrouter/free`). Requires only an API key — no local hardware, no model downloads, no GPU. Routes automatically to the best available free model with tool calling support.
>
> **Alternative providers:**
> - **Local** (Ollama / llama.cpp): $0.00, total privacy, requires 18-30GB RAM
> - **OpenRouter budget** (Gemma 4 26B-A4B): $0.13/$0.40 per M tokens, higher rate limits
> - **Self-hosted** (vLLM, TGI): custom endpoint, enterprise control
> - **Any OpenAI-compatible API**: generic adapter
>
> **Task routing** (unchanged by provider):
> - *Assistive agent handles:* frontmatter updates, `_log.md` entries, navigation regeneration, link validation, cross-reference suggestions, manifest updates, page state transitions, simple module summaries, staleness checks, WIKI-DEBT logging.
> - *Primary agent handles:* architecture synthesis, contradiction detection, ADR creation, complex ingestion, deep code analysis, new page creation.
> - *Deterministic pipeline handles (no LLM):* hash computation, path validation, orphan detection, manifest consistency.
>
> **Cascade behavior:** Try the cheapest configured tier first. On failure (unavailable, rate-limited, health check failed), fall through to the next tier. The primary agent is always the final fallback. High-complexity tasks skip directly to the primary agent regardless.
>
> **Rate limit management:** For free-tier providers, the CLI tracks daily request counts and adapts batching strategy. Bulk operations (`plexium convert`, `plexium bootstrap`) automatically escalate to the budget tier or queue work across multiple days.
>
> **Privacy:** `privacyMode: strict` restricts routing to providers that don't log prompts for training. Sensitive content patterns (configurable) are never sent to assistive agents.

---

## Bottom Line

| Dimension | Local Only (Previous) | OpenRouter Fallback (This Proposal) |
|---|---|---|
| **Setup friction** | Install Ollama, pull 18GB model, ensure it's running | Set one env var (`OPENROUTER_API_KEY`) |
| **Hardware requirement** | 18-30GB RAM dedicated | None |
| **Cost** | $0.00 | $0.00 (free tier) or ~$1/mo (budget) |
| **Privacy** | Total | Provider-dependent (configurable) |
| **Works in CI** | Needs GPU runner or skips | ✅ Works on any runner |
| **Works in cloud dev envs** | Usually no GPU | ✅ Works everywhere |
| **Rate limits** | None | 200/day free, unlimited paid |
| **Latency** | ~25ms | ~200ms |
| **Quality** | Gemma 4 26B-A4B | Nemotron 120B, GPT-OSS 120B, etc. (often *better*) |

**The recommendation:** Make OpenRouter the **default** assistive agent provider. Make local the **opt-in upgrade** for privacy-sensitive or high-volume users. The cascade means you never have to choose — configure both, and the system uses whichever is available.

For most developers, the assistive agent goes from "Phase 3 power-user feature requiring specific hardware" to **"check a box during `plexium init`, paste an API key, and forget about it."** That's the adoption curve difference between a feature 5% of users enable and one that 80% enable.