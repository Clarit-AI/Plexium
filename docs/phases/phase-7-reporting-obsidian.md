# Phase 7: Reporting & Obsidian

> **Model:** Budget — Sonnet 4 mini (primary), GLM-5, Minimax acceptable
> **Execution:** Solo Agent
> **Status:** Pending  
> **bd Epic:** `plexium-m7`  
> **Prerequisites:** Phase 4 and Phase 5 complete

## Objective

Implement structured report generation (bootstrap, sync, lint reports in Markdown and JSON formats), Obsidian vault configuration, and the `plexium gh-wiki-sync` command with publish/exclude filtering.

## Architecture Context

- [Vault Structure](../architecture/core-architecture.md#vault-structure) — Understand .obsidian/ plugin structure
- [Configuration](../architecture/core-architecture.md#configuration) — `reports` and `githubWiki` config sections

## Spec Sections Covered

- §13 Reporting (all report types and formats)
- §14 GitHub Wiki Integration (submodule setup, selective publishing, auth)
- §15 Obsidian Integration (plugins, Dataview queries, workflow)

## Deliverables

1. **Bootstrap report** — Generated after `plexium init` or `plexium convert`
2. **Sync report** — Generated after `plexium sync`
3. **Lint report** — Generated after `plexium lint`
4. **`.obsidian/` config** — Obsidian vault configuration with recommended plugins
5. **Dataview query templates** — Pre-built queries for wiki health
6. **`plexium gh-wiki-sync` command** — Selective sync to GitHub Wiki with filtering

## Tasks

### M7.1: Bootstrap Report Generator

Generate a report after `plexium init` or `plexium convert`.

**Report schema (from spec):**
```json
{
  "type": "bootstrap",
  "timestamp": "2026-04-05T14:30:00Z",
  "sources": {
    "scanned": 47,
    "included": 38,
    "skipped": 9,
    "skipReasons": {"excluded_by_glob": 6, "binary_file": 2, "empty": 1}
  },
  "pages": {
    "generated": 24,
    "stubs": 7,
    "collisionsResolved": 1
  },
  "navigation": {
    "homeGenerated": true,
    "sidebarGenerated": true,
    "allPagesReachable": true
  },
  "publish": {
    "status": "success",
    "pagesPublished": 24,
    "commit": "abc123f"
  }
}
```

**Implementation:**
```go
// internal/reports/bootstrap.go
type BootstrapReport struct {
    Type        string `json:"type"`
    Timestamp   string `json:"timestamp"`
    Sources     SourcesSummary `json:"sources"`
    Pages       PagesSummary `json:"pages"`
    Navigation  NavSummary `json:"navigation"`
    Publish     PublishSummary `json:"publish"`
}

type SourcesSummary struct {
    Scanned     int            `json:"scanned"`
    Included    int            `json:"included"`
    Skipped     int            `json:"skipped"`
    SkipReasons map[string]int `json:"skipReasons"`
}

type PagesSummary struct {
    Generated        int `json:"generated"`
    Stubs            int `json:"stubs"`
    CollisionsResolved int `json:"collisionsResolved"`
}
```

**Output:**
- JSON: `.plexium/reports/bootstrap-{timestamp}.json`
- Markdown: `.plexium/reports/bootstrap-{timestamp}.md`

### M7.2: Sync Report Generator

Generate a report after `plexium sync`.

**Report schema (from spec):**
```json
{
  "type": "sync",
  "timestamp": "2026-04-05T15:00:00Z",
  "trigger": "push_to_main",
  "commit": "def456a",
  "changes": {
    "sourceFilesChanged": 5,
    "wikiRelevant": 3,
    "pagesImpacted": 4,
    "pagesRewritten": 3,
    "pagesSkipped": 1,
    "skipReason": "no_mapped_wiki_page"
  },
  "navigation": {
    "sidebarUpdated": false,
    "homeUpdated": false
  },
  "idempotent": true,
  "publish": {
    "status": "success"
  }
}
```

**Implementation:**
```go
// internal/reports/sync.go
type SyncReport struct {
    Type        string `json:"type"`
    Timestamp   string `json:"timestamp"`
    Trigger     string `json:"trigger"`
    Commit      string `json:"commit"`
    Changes     ChangesSummary `json:"changes"`
    Navigation  NavSummary `json:"navigation"`
    Idempotent  bool `json:"idempotent"`
    Publish     PublishSummary `json:"publish"`
}

type ChangesSummary struct {
    SourceFilesChanged int `json:"sourceFilesChanged"`
    WikiRelevant       int `json:"wikiRelevant"`
    PagesImpacted      int `json:"pagesImpacted"`
    PagesRewritten     int `json:"pagesRewritten"`
    PagesSkipped       int `json:"pagesSkipped"`
    SkipReason         string `json:"skipReason,omitempty"`
}
```

### M7.3: Lint Report Enhancement

Enhance the lint report from Phase 6 with full structure.

**Report schema (from spec):**
```json
{
  "type": "lint",
  "timestamp": "2026-04-05T16:00:00Z",
  "deterministic": {
    "brokenLinks": [{"page": "modules/auth.md", "link": "[[nonexistent]]", "severity": "error"}],
    "orphanPages": [{"page": "concepts/old-pattern.md", "severity": "warning"}],
    "staleCandidates": [{"page": "modules/api.md", "daysSinceUpdate": 45, "severity": "warning"}],
    "missingSourceFiles": [],
    "manifestDrift": []
  },
  "llmAugmented": {
    "contradictions": [{"pages": ["modules/auth.md", "architecture/overview.md"], "description": "..."}],
    "suggestedPages": ["concepts/rate-limiting"],
    "missingCrossRefs": [{"from": "modules/auth.md", "shouldLinkTo": "patterns/error-handling.md"}]
  },
  "summary": {
    "errors": 1,
    "warnings": 2,
    "info": 3,
    "passesCI": false
  }
}
```

**Note:** The `llmAugmented` section is populated by Phase 9's LLM-augmented lint. In Phase 7, this section is empty but the structure is defined for forward compatibility.

### M7.4: Obsidian Configuration

Generate `.obsidian/` vault configuration.

**Files to create in `.wiki/.obsidian/`:**

**app.json (Obsidian vault core config):**
```json
{
  "pluginEnabled": {
    "global-search": true,
    "file-explorer": true,
    "backlink": true,
    "graph": true,
    "daily-notes": false,
    "tag-pane": true,
    "page-preview": true,
    "word-count": true
  },
  "attachmentFolderPath": "raw/assets",
  "newFileFolderPath": ".",
  "showInlineTitle": true,
  "userInputPrefix": "",
  "userInputSuffix": ""
}
```

**community-plugins.json:**
```json
{
  "enabledPlugins": [
    "obsidian-dataview",
    "obsidian-marp",
    "obsidian-templater-obsidian"
  ]
}
```

**plugins/dataview/** — Dataview plugin config with pre-built queries

### M7.5: Dataview Query Templates

Create template pages with Dataview queries for wiki health.

**`.wiki/templates/dataview-queries.md`:**
```markdown
## Recently Updated Modules

```dataview
TABLE last-updated, updated-by, confidence
FROM "modules"
SORT last-updated DESC
LIMIT 10
```

## Unreviewed Pages

```dataview
LIST
FROM ""
WHERE review-status = "unreviewed"
SORT last-updated ASC
```

## Wiki Debt

```dataview
LIST
FROM "_log"
WHERE contains(file.content, "WIKI-DEBT")
```

## Stale Pages (>30 days)

```dataview
LIST
FROM ""
WHERE review-status = "stale"
SORT last-updated ASC
```

## Human-Authored Pages

```dataview
LIST
FROM ""
WHERE ownership = "human-authored"
SORT file.ctime DESC
```
```

### M7.6: plexium gh-wiki-sync Command

Selective sync to GitHub Wiki with publish/exclude filtering.

**Command:**
```bash
plexium gh-wiki-sync [--push] [--dry-run]
```

**Implementation:**
```go
// cmd/gh-wiki-sync.go
type WikiSyncer struct {
    wikiPath    string
    config      *config.Config
    manifestMgr *manifest.Manager
}

type SyncResult struct {
    PagesIncluded []string
    PagesExcluded []Exclusion
    Commit       string
    Pushed       bool
}

type Exclusion struct {
    Path    string
    Reason  string  // "excluded_by_pattern" or "unmanaged_page"
}

func (s *WikiSyncer) Sync(dryRun bool, push bool) (*SyncResult, error)
```

**Filtering logic:**
1. Load all pages from `.wiki/`
2. For each page, check:
   - Does it match any pattern in `githubWiki.publish`? If yes, include
   - Does it match any pattern in `githubWiki.exclude`? If yes, exclude
   - Is it in `unmanagedPages` in manifest? If yes, exclude
3. Include page only if it matches publish patterns AND doesn't match exclude patterns
4. If neither publish nor exclude is specified, include all managed pages

**Default behavior (when no config):**
- Publishes: `architecture/**`, `modules/**`, `decisions/**`, `patterns/**`, `concepts/**`, `onboarding.md`, `Home.md`, `_Sidebar.md`, `_Footer.md`
- Excludes: `raw/**`, `reports/**`, `.obsidian/**`

### M7.7: Report Formatting Utilities

Shared utilities for all report types.

```go
// internal/reports/format.go
type ReportFormatter struct {
    outputDir string
}

func (f *ReportFormatter) EmitJSON(report any, filename string) error

func (f *ReportFormatter) EmitMarkdown(report any, template string, filename string) error

func (f *ReportFormatter) EmitBoth(report any, reportType string) error
```

**Output locations:**
- `.plexium/reports/{type}-{timestamp}.json`
- `.plexium/reports/{type}-{timestamp}.md`

## Interfaces

**Consumes from Phase 4:**
- Conversion report structure

**Consumes from Phase 6:**
- Lint report structure

**Provides to Phase 8:**
- Report generation utilities
- gh-wiki-sync command (used by CI)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Bootstrap report has all required fields per schema |
| AC2 | Sync report has all required fields per schema |
| AC3 | Lint report has all required fields per schema (deterministic section) |
| AC4 | Reports emit valid JSON to `.plexium/reports/` |
| AC5 | Reports emit human-readable Markdown to `.plexium/reports/` |
| AC6 | `.obsidian/app.json` contains valid Obsidian vault config |
| AC7 | Community plugins list includes dataview, marp, templater |
| AC8 | Dataview queries return expected results |
| AC9 | `plexium gh-wiki-sync --dry-run` shows correct pages to sync |
| AC10 | `plexium gh-wiki-sync` respects `githubWiki.publish` patterns |
| AC11 | `plexium gh-wiki-sync` respects `githubWiki.exclude` patterns |
| AC12 | `plexium gh-wiki-sync` excludes unmanaged pages |

## bd Task Mapping

```
plexium-m7
├── M7.1: Bootstrap report generator
├── M7.2: Sync report generator
├── M7.3: Lint report enhancement (structure for llmAugmented)
├── M7.4: Obsidian configuration (.obsidian/)
├── M7.5: Dataview query templates
├── M7.6: plexium gh-wiki-sync command
└── M7.7: Report formatting utilities
```
