# Phase 1: CLI Foundation

> **Model:** Budget — Sonnet 4 mini (primary), GLM-5, Minimax acceptable
> **Execution:** Solo Agent
> **Status:** Complete  
> **bd Epic:** `plexium-m1`  
> **Prerequisites:** Phase 0 complete

## Objective

Build the `plexium` CLI binary skeleton with command routing, configuration loading, file scanning with glob support, markdown normalization, and a template engine. This is the foundation every subsequent phase depends on.

## Architecture Context

- [Configuration](../architecture/core-architecture.md#configuration) — Understand `config.yml` schema before building the loader
- [Vault Structure](../architecture/core-architecture.md#vault-structure) — Understand what directories and files the CLI will eventually manage
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Config errors must fail fast with actionable messages

## Spec Sections Covered

- §9 The CLI (`plexium` command reference table)
- §8 Configuration (full config.yml schema)

## Deliverables

1. **CLI binary** at `cmd/plexium/main.go` with command routing
2. **Config loader** in `internal/config/`
3. **File scanner** in `internal/scanner/` with glob support
4. **Markdown normalizer** in `internal/markdown/` (frontmatter extraction/injection)
5. **Template engine** in `internal/template/`

## Tasks

### M1.1: CLI Command Routing

Implement a Cobra-based command tree. The CLI is the user-facing interface for all operations.

**Command structure:**
```
plexium
├── init [--github-wiki] [--obsidian] [--strictness] [--dry-run]
├── convert [--depth shallow|deep] [--dry-run]
├── sync [--dry-run] [--ci]
├── lint [--deterministic|--full] [--ci] [--fail-on error]
├── bootstrap
├── retrieve "<query>"
├── publish
├── gh-wiki-sync [--push]
├── doctor
├── migrate
├── plugin add <name>
├── hook pre-commit|post-commit
├── ci check --base SHA --head SHA
├── daemon [options]
├── compile
├── agent <subcommand>
└── orchestrate --issue <ID>
```

**Implementation:**
```go
// cmd/plexium/main.go
var rootCmd = &cobra.Command{
    Use:   "plexium",
    Short: "Self-documenting repositories via LLM Wiki pattern",
    Long:  `Plexium transforms repositories into self-documenting systems...`,
}

rootCmd.AddCommand(initCmd, convertCmd, syncCmd, lintCmd, ...)
```

**Requirements:**
- All commands return non-zero exit code on failure
- All commands emit structured JSON to stdout on `--output json` (when supported)
- `--help` works for all commands
- `--version` flag on root command

### M1.2: Config Loader

Load and validate `.plexium/config.yml`. See [Configuration](../architecture/core-architecture.md#configuration) for the full schema.

**Implementation:**
```go
// internal/config/config.go
type Config struct {
    Version     int    `yaml:"version"`
    Repo        Repo   `yaml:"repo"`
    Sources     Sources `yaml:"sources"`
    Agents      Agents  `yaml:"agents"`
    Wiki        Wiki    `yaml:"wiki"`
    Taxonomy    Taxonomy `yaml:"taxonomy"`
    Publish     Publish  `yaml:"publish"`
    Sync        Sync     `yaml:"sync"`
    Enforcement Enforcement `yaml:"enforcement"`
    Integrations Integrations `yaml:"integrations"`
    Reports     Reports   `yaml:"reports"`
    GitHubWiki  GitHubWiki `yaml:"githubWiki"`
    Sensitivity Sensitivity `yaml:"sensitivity"`
}
```

**Requirements:**
- Load from `.plexium/config.yml` in repo root
- Validate required fields; fail fast with actionable error if missing
- Support environment variable overrides (e.g., `PLEXIUM_WIKI_ROOT`)
- Return a typed `Config` struct (no unstructured map[string]any)

### M1.3: File Scanner

Traverse the repository with include/exclude glob patterns. Used by convert, sync, and lint operations.

**Implementation:**
```go
// internal/scanner/scanner.go
type Scanner struct {
    include []string  // e.g., ["src/**", "docs/**/*.md"]
    exclude []string  // e.g., ["**/node_modules/**", "**/.next/**"]
}

func (s *Scanner) Scan(root string) ([]File, error)
type File struct {
    Path     string    // Relative path from repo root
    AbsPath  string    // Absolute path
    Content  string    // File content
    IsDir    bool
    Mode     os.FileMode
    ModTime  time.Time
}
```

**Requirements:**
- Include/exclude patterns follow glob syntax (e.g., `**/*.md`, `src/{auth,api}/**`)
- Exclude patterns take precedence over include
- Return files in deterministic order (alphabetical by path)
- Handle symlinks (do not follow; record as symlink)
- Support `~` in paths (expand to home directory)

### M1.4: Markdown Normalizer

Handle YAML frontmatter extraction, injection, and normalization. Every wiki page has frontmatter.

**Implementation:**
```go
// internal/markdown/markdown.go
type Document struct {
    Frontmatter map[string]any  // Parsed YAML frontmatter
    Body        string          // Content after frontmatter
    Raw        string          // Original full content
}

// Extract frontmatter from raw markdown
func Parse(raw string) (*Document, error)

// Remove frontmatter from document (for re-processing)
func StripFrontmatter(doc *Document) string

// Inject frontmatter into markdown body
func InjectFrontmatter(doc *Document) (string, error)

// Normalize heading levels (convert.md may need to shift H1→H2, etc.)
func NormalizeHeadings(doc *Document, baseLevel int) string

// Extract all [[wiki-links]] from body
func ExtractWikiLinks(body string) []string

// Validate frontmatter schema
func ValidateFrontmatter(doc *Document) error
```

**Frontmatter spec (from schema):**
```yaml
---
title: <Human-readable title>
ownership: managed              # managed | human-authored | co-maintained
last-updated: YYYY-MM-DD
updated-by: <agent-name>
related-modules: [<list>]
source-files: [<glob patterns>]
confidence: high                # high | medium | low
review-status: unreviewed       # unreviewed | human-verified | stale
tags: [<list>]
---
```

### M1.5: Template Engine

Generate wiki pages from templates. Used by page generators in Phase 2.

**Implementation:**
```go
// internal/template/engine.go
type Engine struct {
    templateDir string  // .plexium/templates/
}

func (e *Engine) Render(name string, data interface{}) (string, error)

func (e *Engine) Register(name string, template string) error
```

**Template files (to be created in `.plexium/templates/`):**
```
.plexium/templates/
├── module.md.tpl       # Module page template
├── decision.md.tpl     # ADR template
├── concept.md.tpl      # Concept page template
├── architecture.md.tpl # Architecture page template
└── _index_entry.md.tpl # Index entry template
```

**Module template example:**
```markdown
---
title: {{.Title}}
ownership: managed
last-updated: {{.LastUpdated}}
updated-by: {{.UpdatedBy}}
related-modules: [{{range $i, $m := .RelatedModules}}{{if $i}}, {{end}}{{$m}}{{end}}]
source-files: [{{range $i, $f := .SourceFiles}}{{if $i}}, {{end}}"{{$f}}"{{end}}]
confidence: {{.Confidence}}
review-status: {{.ReviewStatus}}
tags: [{{range $i, $t := .Tags}}{{if $i}}, {{end}}{{$t}}{{end}}]
---

# {{.Title}}

{{.Body}}
```

## Interfaces

**Consumes from Phase 0:**
- CLI binary skeleton
- Project structure (cmd/, internal/)

**Provides to Phase 2:**
- Config loader (`internal/config.Config`)
- File scanner (`internal/scanner.Scanner`)
- Markdown parser (`internal/markdown.Document`)
- Template engine (`internal/template.Engine`)

**Provides to Phase 3:**
- CLI command routing (all commands stubbed, implemented later)

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | `plexium --help` outputs valid help text |
| AC2 | `plexium --version` outputs version |
| AC3 | Config loader parses valid `.plexium/config.yml` without error |
| AC4 | Config loader fails fast with actionable message on missing required fields |
| AC5 | File scanner includes files matching glob patterns |
| AC6 | File scanner excludes files matching exclude patterns |
| AC7 | File scanner returns deterministic ordering |
| AC8 | Markdown parser extracts frontmatter correctly |
| AC9 | Markdown parser injects frontmatter without corrupting body |
| AC10 | Template engine renders templates with data |
| AC11 | All commands route to stub handlers |
| AC12 | All commands return non-zero on failure |

## bd Task Mapping

```
plexium-m1
├── M1.1: CLI command routing
├── M1.2: Config loader
├── M1.3: File scanner with glob support
├── M1.4: Markdown normalizer (frontmatter)
└── M1.5: Template engine
```
