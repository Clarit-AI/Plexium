# Phase 6: Deterministic Lint

> **Model:** Budget — Sonnet 4 mini (primary), GLM-5, Minimax acceptable
> **Execution:** Solo Agent
> **Status:** Complete  
> **bd Epic:** `plexium-m6`  
> **Prerequisites:** Phase 3 complete

## Objective

Build the deterministic lint pipeline: link crawler and resolver, orphan page detector, staleness detector (hash comparison), manifest consistency validator, sidebar link validator, and the `plexium doctor` command. These checks run without LLM calls and are fast, testable, and reproducible.

## Architecture Context

- [State Manifest & Mapping](../architecture/core-architecture.md#state-manifest--mapping) — Manifest schema needed for staleness and consistency checks
- [Page Generation Rules](../architecture/core-architecture.md#page-generation-rules) — Wiki-link syntax for link validation
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Never-violated rules checked by lint

## Spec Sections Covered

- §10 Workflows & Operations (Operation 4: Lint / Health Check — deterministic portion)
- §12 Deterministic vs. LLM Pipeline (deterministic column)

## Deliverables

1. **Link crawler** — Find and resolve all `[[wiki-links]]` in wiki pages
2. **Orphan page detector** — Find pages with no inbound links
3. **Staleness detector** — Hash comparison vs manifest for managed pages
4. **Manifest consistency validator** — Validate all paths, hashes, links in manifest
5. **Sidebar link validator** — Verify all sidebar links resolve
6. **Frontmatter validator** — Verify required frontmatter fields
7. **`plexium lint --deterministic` command** — Run all deterministic checks
8. **`plexium doctor` command** — Validate config, auth, wiki integrity

## Tasks

### M6.1: Link Crawler

Find all `[[wiki-links]]` in wiki pages and resolve them to file paths.

**Implementation:**
```go
// internal/lint/links.go
type LinkCrawler struct {
    wikiPath string
}

type WikiLink struct {
    PagePath  string  // Path to the page containing the link
    RawLink   string  // The raw [[link]] text
    Target    string  // Resolved target (e.g., "modules/auth.md")
    Resolved  bool    // Whether target exists
    LineNum   int    // Line number where link appears
}

func (c *LinkCrawler) Crawl() ([]WikiLink, error)

func (c *LinkCrawler) ResolveLink(linkText string) (targetPath string, exists bool)

// Wiki links can be:
// [[modules/auth]] → modules/auth.md
// [[auth]] → auth.md (in same directory)
// [[modules/auth#heading]] → anchor within page
// [[../decisions/001]] → relative path
```

**Requirements:**
- Crawl all `.md` files in `.wiki/`
- Extract all `[[...]]` patterns
- Resolve relative links to absolute wiki paths
- Detect broken links (target doesn't exist)
- Report link text, target path, source page, line number

### M6.2: Orphan Page Detector

Find pages that are not linked from any other page.

**Implementation:**
```go
// internal/lint/orphans.go
type OrphanDetector struct {
    crawler *LinkCrawler
    manifest *manifest.Manager
}

type OrphanResult struct {
    Orphans []OrphanPage
}

type OrphanPage struct {
    WikiPath    string
    Reason      string  // "no inbound links", "not in index", etc.
    Severity    string  // "error" or "warning"
}

func (d *OrphanDetector) Detect() (*OrphanResult, error)
```

**Detection logic:**
1. Build inbound-link graph from crawler results
2. Pages not in graph → orphan
3. Exception: `_index.md`, `_Sidebar.md`, `_Footer.md`, `Home.md` are navigation hubs and are allowed to have no inbound links
4. Exception: pages listed in `_Sidebar.md` are considered "reachable" even without inbound links

**Severity:**
- `error`: No inbound links and not reachable from sidebar
- `warning`: No inbound links but reachable from sidebar (possible dead-end page)

### M6.3: Staleness Detector

Compare source file hashes vs manifest to detect drift.

**Implementation:**
```go
// internal/lint/staleness.go
type StalenessDetector struct {
    manifestMgr *manifest.Manager
    wikiPath    string
}

type StalenessResult struct {
    StalePages []StalePage
}

type StalePage struct {
    WikiPath       string
    SourceFiles    []string  // Which sources changed
    LastUpdated    string
    DaysSinceUpdate int
    Severity       string  // "error" or "warning"
}

func (d *StalenessDetector) Detect() (*StalenessResult, error)

// Detection:
// 1. For each managed page in manifest
// 2. For each source file in page's SourceFiles
// 3. Recompute hash of current source file
// 4. Compare to stored hash in manifest
// 5. If different → page is stale
```

**Requirements:**
- Staleness threshold: 30 days since last update → warning
- Source file hash mismatch → error (source changed, wiki may be outdated)
- Use `DaysSinceUpdate` from manifest, not current date comparison alone

### M6.4: Manifest Consistency Validator

Validate manifest structure and references.

**Implementation:**
```go
// internal/lint/manifest.go
type ManifestValidator struct {
    manifestMgr *manifest.Manager
    wikiPath    string
}

type ManifestValidation struct {
    Valid         bool
    Errors        []ValidationError
    Warnings      []ValidationWarning
}

type ValidationError struct {
    Path    string  // manifest.json path or wiki path
    Field   string
    Message string
}

func (v *ManifestValidator) Validate() (*ManifestValidation, error)
```

**Validation checks:**
- All `wikiPath` values in manifest point to existing files
- All `sourceFiles` glob patterns resolve to existing files
- All `inboundLinks` and `outboundLinks` reference existing wiki pages
- Ownership values are valid (`managed`, `human-authored`, `co-maintained`)
- `version` field is present and correct
- `lastProcessedCommit` is a valid git SHA (if provided)

### M6.5: Sidebar Link Validator

Verify all links in `_Sidebar.md` resolve.

**Implementation:**
```go
// internal/lint/sidebar.go
type SidebarValidator struct {
    wikiPath  string
    crawler   *LinkCrawler
}

type SidebarValidation struct {
    Valid     bool
    BrokenLinks []BrokenSidebarLink
}

type BrokenSidebarLink struct {
    LinkText   string
    Target     string
    LineNum    int
}

func (v *SidebarValidator) Validate() (*SidebarValidation, error)
```

### M6.6: Frontmatter Validator

Verify all wiki pages have valid frontmatter.

**Implementation:**
```go
// internal/lint/frontmatter.go
type FrontmatterValidator struct {
    wikiPath string
}

type FrontmatterValidation struct {
    Valid       bool
    Errors      []FrontmatterError
}

type FrontmatterError struct {
    WikiPath    string
    Field       string
    Expected    string
    Actual      string
    Message     string
}

func (v *FrontmatterValidator) Validate() (*FrontmatterValidation, error)
```

**Required frontmatter fields:**
- `title` (string, non-empty)
- `ownership` (one of: managed, human-authored, co-maintained)
- `last-updated` (date format YYYY-MM-DD)
- `review-status` (one of: unreviewed, human-verified, stale)

**Optional fields:**
- `updated-by` (string)
- `related-modules` (array)
- `source-files` (array)
- `confidence` (one of: high, medium, low)
- `tags` (array)

### M6.7: plexium lint --deterministic Command

Run all deterministic checks and emit report.

**Command:**
```bash
plexium lint --deterministic [--ci] [--fail-on error]
```

**Output:**
```go
type LintReport struct {
    Type        string          `json:"type"`  // "lint"
    Timestamp   string          `json:"timestamp"`
    Deterministic DeterministicReport `json:"deterministic"`
    Summary     LintSummary     `json:"summary"`
}

type DeterministicReport struct {
    BrokenLinks     []BrokenLinkReport `json:"brokenLinks"`
    OrphanPages     []OrphanReport    `json:"orphanPages"`
    StaleCandidates []StaleReport     `json:"staleCandidates"`
    MissingSources  []string         `json:"missingSourceFiles"`
    ManifestDrift   []ManifestError  `json:"manifestDrift"`
    SidebarIssues   []SidebarIssue   `json:"sidebarIssues"`
    FrontmatterIssues []FrontmatterIssue `json:"frontmatterIssues"`
}

type LintSummary struct {
    Errors   int  `json:"errors"`
    Warnings int `json:"warnings"`
    Info     int `json:"info"`
    PassesCI bool `json:"passesCI"`
}
```

**Exit codes:**
- `0`: All checks pass
- `1`: Errors found (broken links, missing sources)
- `2`: Warnings found (orphans, staleness)
- `--ci --fail-on error`: Exit 1 on any error, not just broken links

### M6.8: plexium doctor Command

Validate config, auth, wiki integrity, and tool setup.

**Command:**
```bash
plexium doctor
```

**Checks:**
1. **Config validation**: `.plexium/config.yml` exists and is valid YAML
2. **Manifest validation**: `manifest.json` exists and is valid JSON
3. **Wiki structure**: Required wiki files exist (`_schema.md`, `_index.md`, `Home.md`, `_Sidebar.md`)
4. **Schema file**: `_schema.md` exists and has `schema-version`
5. **Plugin integrity**: All configured plugins exist and are executable
6. **Hook integrity**: Lefthook is installed if configured
7. **CI config**: GitHub Actions workflows exist if enabled
8. **Auth check**: GitHub token has write access to wiki repo (if publishing enabled)
9. **Git status**: Repo is a git repository
10. **memento**: `git memento check` passes if configured

**Output:**
```go
type DoctorReport struct {
    Checks []CheckResult
}

type CheckResult struct {
    Name     string
    Status   string  // "pass", "fail", "warning", "skip"
    Message  string
    Remediation string
}
```

## Interfaces

**Consumes from Phase 3:**
- Manifest manager
- Wiki path from config

**Provides to Phase 8:**
- Lint report format (used by CI workflows)
- Doctor command (used for pre-commit validation)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Link crawler finds all `[[wiki-links]]` in `.wiki/` |
| AC2 | Link crawler correctly resolves relative links |
| AC3 | Broken links reported with page path, line number, and target |
| AC4 | Orphan detector identifies pages with no inbound links |
| AC5 | Orphan detector excludes navigation hub pages from orphan list |
| AC6 | Staleness detector correctly identifies pages where source hashes differ |
| AC7 | Manifest validator identifies dangling wiki path references |
| AC8 | Manifest validator identifies dangling source file references |
| AC9 | Sidebar validator identifies broken links in `_Sidebar.md` |
| AC10 | Frontmatter validator identifies missing required fields |
| AC11 | `plexium lint --deterministic` emits valid JSON report |
| AC12 | `plexium lint --deterministic` exit code reflects severity |
| AC13 | `plexium doctor` reports pass/fail for all check categories |
| AC14 | `plexium doctor` provides remediation steps for failures |

## bd Task Mapping

```
plexium-m6
├── M6.1: Link crawler (find + resolve [[wiki-links]])
├── M6.2: Orphan page detector
├── M6.3: Staleness detector (hash comparison)
├── M6.4: Manifest consistency validator
├── M6.5: Sidebar link validator
├── M6.6: Frontmatter validator
├── M6.7: plexium lint --deterministic command
└── M6.8: plexium doctor command
```
