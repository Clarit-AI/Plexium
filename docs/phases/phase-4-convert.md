# Phase 4: Convert (Brownfield)

> **Model:** Frontier — Opus 4.6 (primary), GPT 5.4 acceptable
> **Execution:** Agent-Teams (claude-code) or sub-agents (codex)
> **Status:** Complete  
> **bd Epic:** `plexium-m4`  
> **Prerequisites:** Phase 3 complete

## Objective

Implement the `plexium convert` command: a multi-phase brownfield ingestion pipeline that bootstraps a wiki from an existing repository. Traverses the codebase, extracts documentation, generates wiki pages, and produces a conversion report.

## Architecture Context

- [Vault Structure](../architecture/core-architecture.md#vault-structure) — Understand the wiki directory layout
- [Page Generation Rules](../architecture/core-architecture.md#page-generation-rules) — Taxonomy and content rules
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Never modify source files

## Spec Sections Covered

- §9 The CLI (`plexium convert` command)
- §10 Workflows & Operations (Operation 1: Bootstrap, Operation 4: Lint)
- §19 Greenfield vs. Brownfield Workflows (brownfield workflow)

## Deliverables

1. **Scour phase** — Traverse directory tree, read README, docstrings, comments, configs
2. **Filter phase** — Classify eligible files, apply include/exclude globs
3. **Ingest phase** — Translate findings to wiki structure
4. **Link phase** — Generate cross-references, build index
5. **Lint phase** — Gap analysis, stub creation
6. **Report phase** — Generate conversion report
7. **`plexium convert` command** — End-to-end pipeline orchestration

## Tasks

### M4.1: Scour Phase

Traverse the repository and extract documentation from multiple sources.

**Implementation:**
```go
// internal/convert/scour.go
type Scourer struct {
    scanner *scanner.Scanner
}

type ScourFindings struct {
    Readmes      []ReadmeDoc
    SourceFiles  []SourceDoc
    Configs      []ConfigDoc
    ADRs         []ADRDoc
    ExistingDocs []ExistingDoc
    GitHistory   []GitMilestone
}

type ReadmeDoc struct {
    Path      string
    Title     string
    Content   string
    Hierarchy int  // nesting level (for docs/*.md)
}

type SourceDoc struct {
    Path        string
    PackageName string
    DocComments []string  // Collected // comments
    FunctionNames []string
    TypeNames   []string
}

type ConfigDoc struct {
    Path    string
    Type    string  // package.json, Cargo.toml, pyproject.toml, etc.
    Content map[string]any  // Parsed config
}

type ADRDoc struct {
    Path    string
    Number  int
    Title   string
    Status  string
    Content string
}

type GitMilestone struct {
    Commit    string
    Message   string
    Date      time.Time
    Tag       string
}
```

**Scouring sources:**
- README files (root and nested)
- JSDoc/docstrings from source files
- Inline comments (significant ones — not boilerplate)
- `package.json`, `Cargo.toml`, `pyproject.toml` (extract dependencies, scripts)
- CI configs (extract build/test commands)
- Existing ADRs in `adr/` or `docs/decisions/`
- OpenAPI specs (`docs/openapi.yaml`, etc.)
- `.env.example` (extract environment variables)
- Git history: scan for major milestones, significant commits
- Existing `CLAUDE.md`, `AGENTS.md`, `.cursorrules` (extract agent instructions)

**Requirements:**
- Do not load binary files
- Respect file size limits (skip files >1MB unless explicitly included)
- Handle encoding issues (prefer UTF-8)

### M4.2: Filter Phase

Classify eligible files and apply include/exclude patterns.

**Implementation:**
```go
// internal/convert/filter.go
type Filter struct {
    config *config.Sources  // include/exclude from config
}

type FilterResult struct {
    Eligible   []scanner.File  // Passed include, not excluded
    Skipped    []scanner.File  // Excluded or binary
    SkipReasons map[string]string  // path → reason
}

func (f *Filter) Apply(files []scanner.File) (*FilterResult, error)
```

**Requirements:**
- Include patterns: `["README.md", "docs/**/*.md", "adr/**/*.md", "src/**"]`
- Exclude patterns: `["**/node_modules/**", "**/.next/**", "**/dist/**", "**/vendor/**"]`
- Binary files automatically excluded
- Empty files logged with reason

### M4.3: Ingest Phase

Translate scour findings into wiki page data.

**Implementation:**
```go
// internal/convert/ingest.go
type Ingestor struct {
    classifiers map[string]*generate.Classifier
    generators  *GeneratorSet
}

type IngestResult struct {
    Pages []PageData
    Stubs []StubPage  // Pages created as stubs (needs deeper analysis)
}

type PageData struct {
    WikiPath    string
    Title       string
    Section     string
    Content     string
    SourceFiles []string
    Confidence  string
    IsStub      bool
}

func (i *Ingestor) Ingest(findings *ScourFindings, filter *FilterResult) (*IngestResult, error)
```

**Ingestion logic:**
- Module pages from `src/` directory structure
- Architecture overview from README + docs/
- Decision pages from ADR files
- Pattern pages from recurring code patterns (error handling conventions, etc.)
- Concept pages from domain-specific terminology found in docs
- Stub pages for undocumented modules (labeled `<!-- STATUS: stub -->`)

### M4.4: Link Phase

Generate cross-references between pages.

**Implementation:**
```go
// internal/convert/link.go
type Linker struct {
    pageIndex map[string]*PageData  // wikiPath → page
}

func (l *Linker) AddPages(pages []PageData) error

// ComputeInboundLinks finds all pages that reference a given page
func (l *Linker) ComputeInboundLinks(wikiPath string) []string

// ComputeOutboundLinks finds all pages referenced by a given page
func (l *Linker) ComputeOutboundLinks(wikiPath string) []string

// GenerateCrossReferences adds [[wiki-links]] to page content
func (l *Linker) GenerateCrossReferences() error

func (l *Linker) BuildIndex() ([]generate.PageInfo, error)
```

**Cross-reference rules:**
- When mentioning a concept that has its own page, use `[[wiki-links]]`
- When mentioning a module, link to `modules/{name}.md`
- When mentioning a decision, link to `decisions/NNN-title.md`
- Add inbound links from at least 2 related existing pages to each new page

### M4.5: Lint Phase

Gap analysis and stub creation.

**Implementation:**
```go
// internal/convert/lint.go
type ConverterLint struct {
    linker *Linker
    scanner *scanner.Scanner
}

type LintResult struct {
    UndocumentedModules []string  // src/ directories without wiki pages
    MissingCrossRefs    []CrossRefSuggestion
    Orphans             []string  // Pages with no inbound links
    GapScore            float64   // Percentage of source docs captured
}

type CrossRefSuggestion struct {
    FromPage string
    ToPage   string
    Reason   string
}

func (l *ConverterLint) Analyze() (*LintResult, error)
```

**Gap analysis:**
- Find `src/` directories without corresponding `modules/{name}.md` → create stubs
- Find concepts mentioned in multiple docs but without `concepts/{name}.md` → suggest creation
- Find pages with no inbound links → add cross-references or flag as orphan
- Compute gap score: (pages with content) / (total eligible source files)

### M4.6: Report Phase

Generate the conversion report.

**Implementation:**
```go
// internal/convert/report.go
type ReportGenerator struct{}

type ConversionReport struct {
    Type        string `json:"type"`  // "conversion"
    Timestamp   string `json:"timestamp"`
    Sources     SourcesSummary `json:"sources"`
    Pages       PagesSummary `json:"pages"`
    Navigation  NavSummary `json:"navigation"`
    Gaps        []GapEntry `json:"gaps"`
    Stubs       []StubEntry `json:"stubs"`
}

type SourcesSummary struct {
    Scanned   int            `json:"scanned"`
    Included  int            `json:"included"`
    Skipped   int            `json:"skipped"`
    SkipReasons map[string]int `json:"skipReasons"`
}

type PagesSummary struct {
    Generated  int   `json:"generated"`
    Stubs      int   `json:"stubs"`
    Collisions int   `json:"collisionsResolved"`
}

func (g *ReportGenerator) Generate(result *IngestResult, lint *LintResult, filter *FilterResult) (*ConversionReport, error)
```

**Report output:**
- Written to `.plexium/reports/conversion-{timestamp}.json`
- Also written to `.plexium/reports/conversion-{timestamp}.md` (human-readable)
- Summary printed to stdout

### M4.7: plexium convert Command

Orchestrate the full pipeline.

**Command:**
```bash
plexium convert [--depth shallow|deep] [--dry-run]
```

**Options:**
- `--depth shallow`: Only process directory structure and README files
- `--depth deep`: Full scour including docstrings, comments, git history
- `--dry-run`: Output to `.plexium/output/` without writing to `.wiki/`

> **Note:** The `--agent` flag (selecting which agent adapter to run post-conversion) is deferred to Phase 5 (M5.9) when agent adapters are built.

**Pipeline:**
```
convert --dry-run
    │
    ├─► scour
    │       └─► ScourFindings
    │
    ├─► filter
    │       └─► FilterResult
    │
    ├─► ingest
    │       └─► IngestResult
    │
    ├─► link
    │       └─► (updated IngestResult with cross-refs)
    │
    ├─► lint
    │       └─► LintResult
    │
    ├─► report
    │       └─► ConversionReport
    │
    └─► write (if not dry-run)
            └─► .plexium/manifest.json updated
                .wiki/* pages written
```

## Interfaces

**Consumes from Phase 3:**
- Manifest manager
- `plexium init` scaffolding
- All generators

**Provides to Phase 7:**
- Conversion report format
- Stub page generation logic (reused by reporting)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Scour extracts README content from root and nested dirs |
| AC2 | Scour extracts docstrings from source files |
| AC3 | Filter correctly applies include/exclude patterns |
| AC4 | Filter logs skip reasons for all excluded files |
| AC5 | Ingest creates module pages for all `src/` directories |
| AC6 | Ingest creates decision pages for ADR files |
| AC7 | Linker generates `[[wiki-links]]` in page content |
| AC8 | Linker populates `inboundLinks` and `outboundLinks` in manifest |
| AC9 | Lint detects undocumented modules and creates stubs |
| AC10 | Lint identifies orphan pages |
| AC11 | Report accurately lists captured, inferred, and missing items |
| AC12 | `plexium convert --dry-run` produces output without modifying `.wiki/` |
| AC13 | `plexium convert` updates `manifest.json` with new page entries |

## bd Task Mapping

```
plexium-m4
├── M4.1: Scour phase (directory traversal, content extraction)
├── M4.2: Filter phase (classification, glob filtering)
├── M4.3: Ingest phase (translate to wiki structure)
├── M4.4: Link phase (cross-reference generation)
├── M4.5: Lint phase (gap analysis, stub creation)
├── M4.6: Report phase (conversion report)
└── M4.7: plexium convert command orchestration
```