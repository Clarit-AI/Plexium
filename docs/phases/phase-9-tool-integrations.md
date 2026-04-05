# Phase 9: Tool Integrations

> **Model:** Split — Memento ingestion + LLM-augmented lint require Frontier (Opus 4.6 / GPT 5.4); PageIndex MCP + Beads are Mid-tier (Sonnet 4 / GPT 4.1)
> **Execution:** Agent-Teams (claude-code) or sub-agents (codex) — parallel integrations benefit from isolation
> **Status:** Pending  
> **bd Epic:** `plexium-m9`  
> **Prerequisites:** Phase 8 complete

## Objective

Implement integrations with Memento (session provenance), Beads (task graphs), and PageIndex (retrieval agent). This phase makes Plexium's wiki a living knowledge layer by connecting it to the development workflow's session history, task tracking, and semantic search capabilities.

## Architecture Context

- [Security & Trust](../architecture/core-architecture.md#security--trust) — Privacy by provider tier
- [Scaling Considerations](../architecture/core-architecture.md#scaling-considerations) — Token cost management

## Spec Sections Covered

- §16 Tool Integrations (Memento, Beads, PageIndex, retrieval agent, LLM-augmented lint)
- §10 Workflows & Operations (Operation 3: Query, Operation 5: Ingest)

## Deliverables

1. **Memento integration** — Session transcript ingestion pipeline
2. **Beads integration** — `bd` task ID linking to wiki pages
3. **PageIndex MCP server** — Hierarchical retrieval for agents
4. **`plexium retrieve` command** — CLI wrapper for retrieval
5. **LLM-augmented lint** — Semantic contradiction detection, cross-ref suggestions
6. **Agent role separation** — Retriever, Coder, Documenter, Ingestor roles

## Tasks

### M9.1: Memento Integration

Integrate session transcript capture and ingestion pipeline.

**Memento config in `config.yml`:**
```yaml
integrations:
  memento: true

enforcement:
  mementoGate: true  # Enable CI gate
```

**Session transcript ingestion:**
1. `git memento init` is run during `plexium init --with-memento`
2. Session transcripts are stored as git notes AND copied to `.wiki/raw/memento-transcripts/`
3. CI gate (`git memento check --gate`) fails builds without proper session provenance

**Transcript ingestion pipeline:**
```go
// internal/integrations/memento/ingest.go
type MementoIngestor struct {
    rawPath    string
    generator  *generate.ConceptGenerator
    linker     *convert.Linker
}

func (i *MementoIngestor) IngestNewTranscripts() (*IngestResult, error) {
    // 1. Scan .wiki/raw/memento-transcripts/ for new transcripts
    // 2. For each new transcript:
    //    - Read full content
    //    - Extract decisions, rationale, tradeoffs
    //    - Create or update ADR pages
    //    - Add to relevant module pages
    //    - Flag contradictions with existing wiki content
    // 3. Update manifest with new source mappings
}
```

**Decision extraction from transcripts:**
- Look for patterns like "we decided to...", "the tradeoff is...", "because of X, we chose Y"
- Create `decisions/` pages from extracted decisions
- Link to relevant module pages

### M9.2: Beads Integration

Link `bd` task IDs to wiki pages for bidirectional traceability.

**Beads config in `config.yml`:**
```yaml
integrations:
  beads: true
```

**Frontmatter link:**
```yaml
beads-ids: ["plexium-m4-task-12", "plexium-m4-task-15"]
```

**Implementation:**
```go
// internal/integrations/beads/beads.go
type BeadsLinker struct {
    bdPath string  // path to bd executable
}

func (l *BeadsLinker) GetTaskPageIDs(taskID string) ([]string, error)

func (l *BeadsLinker) GetPageTasks(wikiPath string) ([]string, error)

func (l *BeadsLinker) LinkTaskToPage(taskID, wikiPath string) error

// Reads bd graph to find which tasks touched which wiki pages
// Reads wiki frontmatter to find beads-ids field
// Bidirectional: task → pages and page → tasks
```

**Beads graph → wiki synthesis:**
- When beads graph resolves (task completed), agent synthesizes outcomes into wiki decision pages
- Wiki page frontmatter includes `beads-ids` for traceability
- The beads graph captures *process*; the wiki captures *knowledge*

### M9.3: PageIndex MCP Server

Set up PageIndex as an MCP server for retrieval.

**PageIndex config:**
```yaml
integrations:
  pageindex: true
```

**MCP server implementation:**
```go
// internal/integrations/pageindex/server.go
type PageIndexServer struct {
    wikiPath string
    port     int
}

func (s *PageIndexServer) Start() error {
    // Start PageIndex MCP server on specified port
    // Exposes tools:
    // - pageindex_search(query: string) → PageSearchResult
    // - pageindex_get_page(path: string) → PageContent
    // - pageindex_list_pages() → []PageInfo
}
```

**Hierarchical search approach:**
- Uses tree-based navigation (like using a book's TOC + index) rather than vector similarity
- Better for structured documentation than embedding-based RAG
- Navigate by section → subsection → page

### M9.4: plexium retrieve Command

CLI wrapper for retrieval, usable by any agent via shell-out.

**Command:**
```bash
plexium retrieve "<query>" [--format json|markdown]
```

**Implementation:**
```go
// cmd/retrieve.go
type Retriever struct {
    config   *config.Config
    pageIdx  *pageindex.Client
    fallback bool  // Use _index.md scan if PageIndex unavailable
}

type RetrieveResult struct {
    Query      string     `json:"query"`
    Pages      []PageHit `json:"pages"`
    Answer     string    `json:"answer,omitempty"`  // Synthesized if LLM available
}

type PageHit struct {
    Path    string `json:"path"`
    Title   string `json:"title"`
    Summary string `json:"summary"`
    Relevance float64 `json:"relevance"`
}
```

**Retrieval chain:**
1. Try PageIndex MCP server (if configured)
2. Try `plexium retrieve` CLI (if PageIndex unavailable)
3. Fallback: read `_index.md`, grep for relevant pages

### M9.5: LLM-Augmented Lint

Add semantic checks that require LLM analysis.

**New lint checks (Phase 9 scope):**

| Check | Description |
|-------|-------------|
| Contradiction detection | Compare module pages vs architecture overview; flag conflicts |
| Concept extraction | Find concepts mentioned in 3+ pages without their own page |
| Cross-ref suggestions | Identify related concepts that should link but don't |
| Semantic staleness | Content present but outdated in meaning, not just hash |

**Implementation:**
```go
// internal/lint/llm.go
type LLMAnalyzer struct {
    llmClient LLMClient  // Primary coding agent's LLM
    config    *config.Config
}

type LLMAnalysisResult struct {
    Contradictions     []Contradiction
    SuggestedPages     []SuggestedPage
    MissingCrossRefs  []CrossRefSuggestion
    SemanticStaleness []SemanticStalePage
}

type Contradiction struct {
    Pages     []string
    Description string
    Severity   string
}

func (a *LLMAnalyzer) Analyze(pages []string) (*LLMAnalysisResult, error)
```

**Note:** LLM-augmented lint is expensive. It should be:
- Opt-in via `plexium lint --full`
- Rate-limited (don't analyze more than N pages per run)
- Results cached until pages change

### M9.6: Agent Role Separation

Support explicit role assignment for complex operations.

**Roles:**

| Role | Responsibility |
|------|---------------|
| **Coder** | Writes and modifies source code |
| **Explorer/Retriever** | Searches wiki + codebase via PageIndex, compiles context |
| **Documenter** | Updates wiki pages, maintains cross-references, runs lint |
| **Ingestor** | Processes raw sources into wiki pages |

**Implementation:**
For Phase 9, this is a documented pattern. Actual multi-agent orchestration is Phase 10.

```go
// internal/integrations/roles/roles.go
type Role string

const (
    RoleCoder      Role = "coder"
    RoleRetriever  Role = "retriever"
    RoleDocumenter Role = "documenter"
    RoleIngestor   Role = "ingestor"
)

type RoleContext struct {
    Role           Role
    TaskDescription string
    WikiPath       string
    SourceFiles    []string
}
```

**Usage:** Document the role pattern. The daemon in Phase 10 implements actual role sequencing.

### M9.7: plexium init with Integrations

Update `plexium init` to optionally set up integrations.

**New flags:**
```bash
plexium init [--with-memento] [--with-beads] [--with-pageindex]
```

**Implementation:**
```go
func runInit(cmd *cobra.Command, args []string) error {
    withMemento := flags.GetBool("with-memento")
    withBeads := flags.GetBool("with-beads")
    withPageIndex := flags.GetBool("with-pageindex")
    
    // Existing init logic...
    
    if withMemento {
        run("git memento init")
        updateConfig("integrations.memento", true)
        updateConfig("enforcement.mementoGate", true)
    }
    
    if withBeads {
        run("bd init")
        updateConfig("integrations.beads", true)
    }
    
    if withPageIndex {
        setupPageIndexMCP()
        updateConfig("integrations.pageindex", true)
    }
}
```

## Interfaces

**Consumes from Phase 8:**
- Hook infrastructure (memento gate integrates here)
- CI workflows (memento gate added to lint workflow)

**Consumes from Phase 6:**
- Lint infrastructure (LLM-augmented lint extends)

**Provides to Phase 10:**
- Retrieval infrastructure (daemon uses PageIndex)
- Task tracking (beads links to wiki pages)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | `plexium init --with-memento` runs `git memento init` |
| AC2 | Transcripts appear in `.wiki/raw/memento-transcripts/` |
| AC3 | Memento CI gate fails builds without session provenance |
| AC4 | `plexium init --with-beads` runs `bd init` |
| AC5 | Wiki pages can have `beads-ids` frontmatter |
| AC6 | `bd task` shows linked wiki pages |
| AC7 | `plexium init --with-pageindex` sets up PageIndex MCP |
| AC8 | PageIndex MCP server starts and serves queries |
| AC9 | `plexium retrieve` returns relevant pages |
| AC10 | `plexium retrieve` falls back to index scan if PageIndex unavailable |
| AC11 | LLM-augmented lint detects contradictions |
| AC12 | LLM-augmented lint suggests missing concept pages |
| AC13 | LLM-augmented lint suggests missing cross-references |

## bd Task Mapping

```
plexium-m9
├── M9.1: Memento integration (session transcript ingestion)
├── M9.2: Beads integration (task ID linking)
├── M9.3: PageIndex MCP server
├── M9.4: plexium retrieve command
├── M9.5: LLM-augmented lint
├── M9.6: Agent role separation (pattern documentation)
└── M9.7: plexium init with integration flags
```
