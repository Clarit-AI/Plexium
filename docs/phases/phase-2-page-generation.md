# Phase 2: Page Generation

> **Model:** Mid-tier — Sonnet 4 (primary), GPT 4.1, Gemini 2.5 Flash acceptable
> **Execution:** Solo Agent
> **Status:** Pending  
> **bd Epic:** `plexium-m2`  
> **Prerequisites:** Phase 1 complete

## Objective

Build the page generation pipeline: taxonomy classifier, generators for modules/decisions/concepts, slug deduplication, and navigation file generation (Home, Sidebar, Footer, Index). This phase transforms raw source files into structured wiki pages.

## Architecture Context

- [Page Generation Rules](../architecture/core-architecture.md#page-generation-rules) — Slug, title, content, navigation rules and taxonomy table
- [Vault Structure](../architecture/core-architecture.md#vault-structure) — Understand the directory layout and file purposes
- [Universal Schema](../architecture/core-architecture.md#the-universal-schema) — Frontmatter spec and content rules

## Spec Sections Covered

- §6 Page Generation Rules (all subsections)
- §4 Universal Schema (frontmatter spec, content rules, cross-reference rules)

## Deliverables

1. **Taxonomy classifier** — Maps source files to wiki sections
2. **Module page generator** — Generates `modules/{name}.md` from source analysis
3. **Decision page generator** — Generates `decisions/NNN-title.md` from ADR files
4. **Concept page generator** — Generates `concepts/{name}.md` from domain analysis
5. **Slug generator** — Deterministic deduplication for page names
6. **Navigation generators** — Home.md, _Sidebar.md, _Footer.md, _index.md

## Tasks

### M2.1: Taxonomy Classifier

Classify source files into wiki sections based on directory structure and content type.

**Implementation:**
```go
// internal/generate/classifier.go
type Taxonomy struct {
    Sections []string  // ["Architecture", "Modules", "Decisions", "Patterns", "Concepts", "Guides"]
}

type Classification struct {
    Section    string   // e.g., "modules", "decisions", "concepts"
    PageType   string   // e.g., "module", "decision", "concept", "pattern"
    Slug       string   // e.g., "auth", "001-chose-postgres", "rbac"
    Title      string   // Human-readable title
    SourcePath string   // Original source file path
}

func (t *Taxonomy) Classify(file scanner.File) (*Classification, error)
```

**Classification rules (from spec):**

| Source | Wiki Output | Section |
|--------|------------|---------|
| `README.md` | `Home.md` | Root |
| `src/{module}/` | `modules/{module}.md` | Modules |
| `docs/*.md` | Pages by content type | Architecture / Guides / Concepts |
| `docs/{folder}/` | Section index page + child pages | Varies |
| ADR files | `decisions/NNN-title.md` | Decisions |
| Named domain concepts | `concepts/{concept}.md` | Concepts |
| Recurring patterns | `patterns/{pattern}.md` | Patterns |

**Requirements:**
- Deterministic classification (same input always produces same output)
- Classify by file path first, then by content analysis if ambiguous
- ADR files detected by path (`adr/` directory or `docs/decisions/`) or filename pattern (`NNN-*.md`)

### M2.2: Module Page Generator

Generate a wiki page for a source module (a directory under `src/`).

**Implementation:**
```go
// internal/generate/module.go
type ModuleGenerator struct {
    scanner *scanner.Scanner
    engine  *template.Engine
}

type ModuleData struct {
    Title          string
    LastUpdated    string
    UpdatedBy      string
    RelatedModules []string
    SourceFiles    []string  // Glob patterns
    Confidence     string
    ReviewStatus   string
    Tags           []string
    Body           string    // Generated content
}

func (g *ModuleGenerator) Generate(modulePath string) (*markdown.Document, error)
```

**Content generation approach:**
1. Scan the module directory with the file scanner
2. Extract package name, exported functions/types from source files
3. Build a summary: what this module does, its key exports, dependencies
4. Use `[[wiki-links]]` to cross-reference related modules and decisions

**Requirements:**
- Never invent implementation details not present in source
- Preserve factual meaning from source code
- Summarize exports without listing every function signature
- Add at least 2 inbound cross-links from related pages

### M2.3: Decision Page Generator (ADR)

Generate a wiki page from an Architecture Decision Record.

**Implementation:**
```go
// internal/generate/decision.go
type DecisionGenerator struct {
    engine *template.Engine
}

type DecisionData struct {
    Number     int       // ADR number extracted from filename (001, 002, etc.)
    Title      string
    Status     string    // Proposed, Accepted, Deprecated, Superseded
    Date       string
    Context    string    // The situation that prompted the decision
    Decision   string    // What was decided
    Consequences string  // Positive and negative consequences
    RelatedDecisions []string
    RelatedModules   []string
    Tags        []string
}

func (g *DecisionGenerator) Generate(adrPath string) (*markdown.Document, error)
```

**ADR file format assumed:**
```markdown
# ADR-001: Chose PostgreSQL for Primary Database

**Status:** Accepted  
**Date:** 2024-01-15

## Context

We needed a primary database that supports...

## Decision

We chose PostgreSQL because...

## Consequences

- **Positive:** ACID compliance, rich indexing...
- **Negative:** Requires operational overhead...
```

### M2.4: Concept Page Generator

Generate a wiki page for a domain concept.

**Implementation:**
```go
// internal/generate/concept.go
type ConceptGenerator struct {
    scanner *scanner.Scanner
    engine  *template.Engine
}

type ConceptData struct {
    Title          string
    RelatedModules []string
    RelatedConcepts []string
    Tags           []string
    Body           string
}

func (g *ConceptGenerator) Generate(sourcePaths []string) (*markdown.Document, error)
```

**Concept sources:**
- `docs/concepts/*.md`
- Files named with concept-like titles
- Content tagged with `#concept` or similar markers

### M2.5: Slug Generator with Deterministic Deduplication

Generate filesystem-safe page names with collision handling.

**Implementation:**
```go
// internal/generate/slug.go

// ToSlug converts a title to a filesystem-safe slug
func ToSlug(title string) string {
    // lowercase, spaces→hyphens, remove special chars
}

// Deduplicate ensures no two pages have the same slug
func Deduplicate(slugs []string) map[string]string {
    // For duplicates, append parent dir or qualifier
    // auth/auth.go → auth
    // auth/middleware/auth.go → auth-middleware
}
```

**Deduplication rules:**
- `auth` (from `src/auth/`) and `auth-middleware` (from `src/auth-middleware/`) must not collide
- First occurrence gets the base slug; subsequent occurrences append qualifiers
- Qualifier is the parent directory name (e.g., `auth-middleware`)

### M2.6: Navigation File Generators

Generate Home.md, _Sidebar.md, _Footer.md, and _index.md.

**Home.md generator:**
```go
// internal/generate/home.go
type HomeGenerator struct {
    engine *template.Engine
}

func (g *HomeGenerator) Generate(repoName, description string, sections []SectionInfo) (*markdown.Document, error)
```

**Content:** Project overview from README, key sections, quick navigation links.

**_Sidebar.md generator:**
```go
// internal/generate/sidebar.go
type SidebarGenerator struct {
    engine *template.Engine
}

func (g *SidebarGenerator) Generate(pages []PageInfo) (*markdown.Document, error)
```

**Requirements:**
- Deterministic ordering (alphabetical within sections)
- All top-level sections exposed
- High-traffic pages (modules, architecture) given prominence

**_Footer.md generator:**
```go
// internal/generate/footer.go
type FooterGenerator struct {
    engine *template.Engine
}

func (g *FooterGenerator) Generate() (*markdown.Document, error)
```

**Content:** Last-updated timestamp, link back to Home.md, Plexium version.

**_index.md generator:**
```go
// internal/generate/index.go
type IndexGenerator struct {
    engine *template.Engine
}

type PageInfo struct {
    Path     string
    Title    string
    Section  string
    Summary  string  // One-line summary (first sentence or explicit field)
    Tags     []string
}

func (g *IndexGenerator) Generate(pages []PageInfo) (*markdown.Document, error)
```

**Requirements:**
- Machine-readable format (used by retrieval tools)
- Every known page listed
- Sorted deterministically

## Interfaces

**Consumes from Phase 1:**
- `internal/config.Config`
- `internal/scanner.Scanner`
- `internal/markdown.Document`
- `internal/template.Engine`

**Provides to Phase 3:**
- All generators ready to use
- Navigation files can be generated deterministically

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Classifier correctly maps `src/auth/` to `modules/auth.md` |
| AC2 | Classifier correctly maps `adr/001-foo.md` to `decisions/001-foo.md` |
| AC3 | Module generator produces valid frontmatter + body |
| AC4 | Decision generator extracts status, context, decision, consequences |
| AC5 | Slug deduplication handles `auth` vs `auth-middleware` |
| AC6 | Home.md contains project overview and section links |
| AC7 | _Sidebar.md exposes all top-level sections |
| AC8 | _Sidebar.md ordering is deterministic (alphabetical within sections) |
| AC9 | _Footer.md contains last-updated timestamp |
| AC10 | _index.md lists all pages with title, section, summary |
| AC11 | All generated pages have valid frontmatter |
| AC12 | No duplicate page slugs in output |

## bd Task Mapping

```
plexium-m2
├── M2.1: Taxonomy classifier
├── M2.2: Module page generator
├── M2.3: Decision page generator
├── M2.4: Concept page generator
├── M2.5: Slug generator with deduplication
└── M2.6: Navigation file generators (Home, Sidebar, Footer, Index)
```
