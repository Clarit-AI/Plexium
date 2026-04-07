# Phase 3: State & Publishing

> **Model:** Mid-tier — Sonnet 4 (primary), GPT 4.1, Gemini 2.5 Flash acceptable
> **Execution:** Solo Agent
> **Status:** Complete  
> **bd Epic:** `plexium-m3`  
> **Prerequisites:** Phase 2 complete

## Objective

Implement the state manifest (`manifest.json`), bidirectional source↔wiki mapping, hash computation for staleness detection, the `plexium publish` command, full `plexium init` scaffolding, and dry-run mode for all write operations.

## Architecture Context

- [State Manifest & Mapping](../architecture/core-architecture.md#state-manifest--mapping) — Complete manifest schema and update rules
- [Configuration](../architecture/core-architecture.md#configuration) — Config fields for wiki paths, publish settings
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Never delete unmanaged pages, never modify source files

## Spec Sections Covered

- §7 State Manifest & Mapping (full schema, update rules)
- §9 The CLI (`plexium init`, `plexium publish`)
- §10 Workflows & Operations (Operation 1: Bootstrap)

## Deliverables

1. **Manifest CRUD** — Create, read, update manifest.json
2. **Bidirectional mapping** — Source→wiki and wiki→source lookups
3. **Hash computation** — SHA256 hashes for staleness detection
4. **Wiki publisher** — Push wiki to GitHub Wiki repo
5. **`plexium init` command** — Full scaffolding of `.wiki/` and `.plexium/`
6. **Dry-run mode** — All write operations can run without side effects

## Tasks

### M3.1: Manifest Data Structures

Define the manifest schema and Go/Rust structs.

**Implementation:**
```go
// internal/manifest/manifest.go
type Manifest struct {
    Version              int           `json:"version"`
    LastProcessedCommit  string        `json:"lastProcessedCommit"`
    LastPublishTimestamp string        `json:"lastPublishTimestamp"`
    Pages                []PageEntry   `json:"pages"`
    UnmanagedPages       []UnmanagedEntry `json:"unmanagedPages"`
}

type PageEntry struct {
    WikiPath       string       `json:"wikiPath"`
    Title          string       `json:"title"`
    Ownership      string       `json:"ownership"`  // managed | human-authored | co-maintained
    Section        string       `json:"section"`
    SourceFiles    []SourceFile `json:"sourceFiles"`
    GeneratedFrom  []string     `json:"generatedFrom"`
    LastUpdated    string       `json:"lastUpdated"`
    UpdatedBy      string       `json:"updatedBy"`
    InboundLinks    []string     `json:"inboundLinks"`
    OutboundLinks  []string     `json:"outboundLinks"`
}

type SourceFile struct {
    Path               string `json:"path"`
    Hash               string `json:"hash"`
    LastProcessedCommit string `json:"lastProcessedCommit"`
}

type UnmanagedEntry struct {
    WikiPath    string `json:"wikiPath"`
    FirstSeen   string `json:"firstSeen"`
    Ownership   string `json:"ownership"`
}
```

### M3.2: Manifest Operations

Implement CRUD operations on the manifest.

**Implementation:**
```go
// internal/manifest/crud.go
type Manager struct {
    path string  // .plexium/manifest.json
}

func NewManager(path string) (*Manager, error)

func (m *Manager) Load() (*Manifest, error)

func (m *Manager) Save(manifest *Manifest) error

// Forward lookup: which wiki pages were generated from a source path?
func (m *Manager) PagesFromSource(sourcePath string) []PageEntry

// Reverse lookup: which source files feed into a wiki page?
func (m *Manager) SourcesFromPage(wikiPath string) []SourceFile

// Check if a page is managed
func (m *Manager) IsManaged(wikiPath string) bool

// Add or update a page entry
func (m *Manager) UpsertPage(entry PageEntry) error

// Add an unmanaged page entry
func (m *Manager) AddUnmanaged(entry UnmanagedEntry) error

// Compute staleness: has any source file changed since last processed?
func (m *Manager) DetectStalePages() ([]PageEntry, error)
```

### M3.3: Hash Computation

Compute SHA256 hashes for source files to detect changes.

**Implementation:**
```go
// internal/manifest/hash.go

// ComputeHash returns SHA256 hash of file content
func ComputeHash(filePath string) (string, error)

// ComputeDirHash returns combined hash of all files matching globs in a directory
func ComputeDirHash(dirPath string, globs []string) (string, error)

// HashAllSources computes hashes for all source files in a PageEntry's SourceFiles
func HashAllSources(entry PageEntry) (map[string]string, error)
```

**Requirements:**
- Hash is of file content, not file path/metadata
- Recomputing hash for identical content produces identical hash (idempotent)
- Directory hash combines individual file hashes deterministically (sorted by path)

### M3.4: plexium init Command

Fully scaffold `.wiki/` and `.plexium/` directories.

**Command:**
```bash
plexium init [--github-wiki] [--obsidian] [--strictness strict|moderate|advisory] [--dry-run]
```

**What init creates:**

`.wiki/` structure:
```
.wiki/
├── .obsidian/                  # (if --obsidian)
├── _schema.md                  # Generated from template, customized for detected stack
├── _index.md                   # Empty (generated by first sync)
├── _log.md                     # Empty (generated by first sync)
├── Home.md                     # From README or template
├── _Sidebar.md                 # Empty (generated by first sync)
├── _Footer.md                  # Generated
├── architecture/
│   └── overview.md             # Stub (filled by agents)
├── modules/                    # Empty (filled by sync/convert)
├── decisions/                  # Empty (filled by convert or agents)
├── patterns/                   # Empty
├── concepts/                   # Empty
├── onboarding.md               # Generated stub
├── contradictions.md           # Generated empty stub
├── open-questions.md            # Generated empty stub
└── raw/                        # Empty, with subdirs
    ├── meeting-notes/
    ├── ticket-exports/
    ├── memento-transcripts/
    └── assets/
```

`.plexium/` structure:
```
.plexium/
├── config.yml                  # Generated with defaults
├── manifest.json               # Empty manifest (version: 1, empty pages array)
├── plugins/                    # (populated by phase 5)
├── hooks/                      # (populated by phase 8)
├── templates/                  # (from phase 1)
├── prompts/                    # (populated as needed)
└── migrations/                 # (populated by phase 8)
```

**Implementation:**
```go
// cmd/init.go
func runInit(cmd *cobra.Command, args []string) error {
    // 1. Load config (or create default)
    // 2. Create .wiki/ directory structure
    // 3. Generate _schema.md (detect tech stack from package.json, Cargo.toml, etc.)
    // 4. Generate Home.md from README.md (or template if no README)
    // 5. Create empty navigation files
    // 6. Create .plexium/ structure
    // 7. Initialize manifest.json
    // 8. Install hooks (via lefthook, phase 8 scope but init should call the installer)
    // 9. If --github-wiki: init git submodule
    // 10. Emit bootstrap report
}
```

### M3.5: plexium publish Command

Push wiki changes to the GitHub Wiki repository.

**Command:**
```bash
plexium publish [--dry-run]
```

**Implementation:**
```go
// cmd/publish.go
type Publisher struct {
    wikiPath    string
    repoPath    string
    config      *config.Config
    manifestMgr *manifest.Manager
}

func (p *Publisher) Publish(dryRun bool) (*PublishResult, error)

type PublishResult struct {
    PagesPublished int
    PagesSkipped   int
    Commit         string
    Timestamp      string
}
```

**Behavior:**
1. Read manifest to get list of managed pages
2. Filter pages per `githubWiki.publish` and `githubWiki.exclude` config
3. If `--dry-run`: output what would be published without writing
4. Otherwise: commit and push to `{repo}.wiki.git`
5. Update `lastPublishTimestamp` in manifest

**Auth note:** Wiki push requires write access to `{repo}.wiki.git`, which has separate permissions from the main repo. See `docs/reference/plexium-spec-full.md` §17.

### M3.6: Dry-Run Mode

All write operations must support `--dry-run` which outputs to `.plexium/output/` without modifying `.wiki/` or `.plexium/`.

**Implementation pattern:**
```go
type DryRunner struct {
    enabled bool
    outputDir string
}

func (d *DryRunner) Write(path string, content string) error {
    if d.enabled {
        fullPath := filepath.Join(d.outputDir, path)
        return os.MkdirAll(filepath.Dir(fullPath), 0755), os.WriteFile(fullPath, ...)
    }
    // actually write
}
```

**Requirements:**
- Dry-run output directory is `.plexium/output/`
- Directory structure mirrors what would be written to `.wiki/`
- No modifications to `.wiki/` or `.plexium/` when dry-run is active
- Clear output indicating which files would be created/modified

## Interfaces

**Consumes from Phase 2:**
- All generators ready
- Navigation files can be generated

**Provides to Phase 4:**
- Manifest manager ready for conversion pipeline
- `plexium init` scaffolding available
- `plexium publish` available

**Provides to Phase 6:**
- Manifest for staleness detection
- Hash computation for source file tracking

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Manifest loads from `.plexium/manifest.json` without error |
| AC2 | Manifest saves with correct schema |
| AC3 | Forward lookup: `PagesFromSource("src/auth/**")` returns correct pages |
| AC4 | Reverse lookup: `SourcesFromPage("modules/auth.md")` returns correct sources |
| AC5 | `IsManaged("modules/auth.md")` returns correct ownership |
| AC6 | Hash computation is deterministic (same content = same hash) |
| AC7 | `plexium init` creates complete directory structure |
| AC8 | `plexium init --dry-run` creates no files |
| AC9 | `plexium publish --dry-run` shows correct output |
| AC10 | `plexium publish` commits to wiki repo with correct files |
| AC11 | Publish respects `githubWiki.publish` and `githubWiki.exclude` config |
| AC12 | Managed pages never overwrite `human-authored` pages |

## bd Task Mapping

```
plexium-m3
├── M3.1: Manifest data structures
├── M3.2: Manifest CRUD operations
├── M3.3: Hash computation
├── M3.4: plexium init command
├── M3.5: plexium publish command
└── M3.6: Dry-run mode
```
