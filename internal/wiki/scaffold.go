package wiki

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// InitOptions holds options for plexium init
type InitOptions struct {
	RepoRoot      string
	GitHubWiki    bool
	Obsidian      bool
	Strictness    string // strict | moderate | advisory
	DryRun        bool
	WithMemento   bool
	WithBeads     bool
	WithPageIndex bool
}

// InitResult holds the result of plexium init
type InitResult struct {
	WikiDir       string
	PlexiumDir    string
	FilesCreated  []string
	DirsCreated   []string
}

// Init scaffolds the .wiki/ and .plexium/ directories.
func Init(opts InitOptions) (*InitResult, error) {
	wikiDir := filepath.Join(opts.RepoRoot, ".wiki")
	plexiumDir := filepath.Join(opts.RepoRoot, ".plexium")

	dryRunOutputDir := filepath.Join(plexiumDir, "output")
	dr := manifest.NewDryRunner(opts.DryRun, dryRunOutputDir, nil)
	result := &InitResult{
		WikiDir:    wikiDir,
		PlexiumDir: plexiumDir,
	}

	// Create .wiki/ directory structure
	wikiDirs := []string{
		wikiDir,
		filepath.Join(wikiDir, "architecture"),
		filepath.Join(wikiDir, "modules"),
		filepath.Join(wikiDir, "decisions"),
		filepath.Join(wikiDir, "patterns"),
		filepath.Join(wikiDir, "concepts"),
		filepath.Join(wikiDir, "raw"),
		filepath.Join(wikiDir, "raw", "meeting-notes"),
		filepath.Join(wikiDir, "raw", "ticket-exports"),
		filepath.Join(wikiDir, "raw", "memento-transcripts"),
		filepath.Join(wikiDir, "raw", "assets"),
	}

	if opts.Obsidian {
		wikiDirs = append(wikiDirs, filepath.Join(wikiDir, ".obsidian"))
	}

	for _, dir := range wikiDirs {
		if err := dr.MkdirAll(dir); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", dir, err)
		}
		result.DirsCreated = append(result.DirsCreated, dir)
	}

	// Create .plexium/ directory structure
	plexiumDirs := []string{
		plexiumDir,
		filepath.Join(plexiumDir, "plugins"),
		filepath.Join(plexiumDir, "hooks"),
		filepath.Join(plexiumDir, "templates"),
		filepath.Join(plexiumDir, "prompts"),
		filepath.Join(plexiumDir, "migrations"),
	}

	for _, dir := range plexiumDirs {
		if err := dr.MkdirAll(dir); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", dir, err)
		}
		result.DirsCreated = append(result.DirsCreated, dir)
	}

	// Generate _schema.md using tech-stack-aware SchemaGenerator
	schemaGen := plugins.NewSchemaGenerator(opts.RepoRoot)
	schemaContent, err := schemaGen.Generate()
	if err != nil {
		return nil, fmt.Errorf("generating schema: %w", err)
	}
	if err := writeFile(dr, filepath.Join(wikiDir, "_schema.md"), schemaContent, result); err != nil {
		return nil, err
	}

	// Run all detected agent adapters (unless dry-run)
	if !opts.DryRun {
		adapters := plugins.GetAvailableAdapters(opts.RepoRoot)
		for _, adapter := range adapters {
			pluginDir := filepath.Join(plexiumDir, "plugins", adapter)
			scriptPath := filepath.Join(pluginDir, "plugin.sh")
			if _, err := os.Stat(scriptPath); err == nil {
				cmd := exec.Command("bash", scriptPath)
				cmd.Dir = opts.RepoRoot
				cmd.Env = append(os.Environ(), "PLEXIUM_DIR="+opts.RepoRoot)
				if err := cmd.Run(); err != nil {
					// Log but don't fail - adapter may not be critical
					fmt.Fprintf(os.Stderr, "Warning: adapter %q failed: %v\n", adapter, err)
				}
			}
		}
	}

	// Generate _index.md (empty)
	if err := writeFile(dr, filepath.Join(wikiDir, "_index.md"), "", result); err != nil {
		return nil, err
	}

	// Generate _log.md (empty)
	if err := writeFile(dr, filepath.Join(wikiDir, "_log.md"), "", result); err != nil {
		return nil, err
	}

	// Generate Home.md from README or template
	homeContent := generateHome(opts)
	if err := writeFile(dr, filepath.Join(wikiDir, "Home.md"), homeContent, result); err != nil {
		return nil, err
	}

	// Generate _Sidebar.md (empty)
	if err := writeFile(dr, filepath.Join(wikiDir, "_Sidebar.md"), "", result); err != nil {
		return nil, err
	}

	// Generate _Footer.md
	footerContent := generateFooter()
	if err := writeFile(dr, filepath.Join(wikiDir, "_Footer.md"), footerContent, result); err != nil {
		return nil, err
	}

	// Generate architecture/overview.md stub
	archOverview := generateArchStub()
	if err := writeFile(dr, filepath.Join(wikiDir, "architecture", "overview.md"), archOverview, result); err != nil {
		return nil, err
	}

	// Generate onboarding.md stub
	onboarding := generateOnboardingStub()
	if err := writeFile(dr, filepath.Join(wikiDir, "onboarding.md"), onboarding, result); err != nil {
		return nil, err
	}

	// Generate contradictions.md stub
	contradictions := generateEmptyStub("Contradictions", "Tracked contradictions between wiki pages.")
	if err := writeFile(dr, filepath.Join(wikiDir, "contradictions.md"), contradictions, result); err != nil {
		return nil, err
	}

	// Generate open-questions.md stub
	openQuestions := generateEmptyStub("Open Questions", "Unresolved questions about the codebase.")
	if err := writeFile(dr, filepath.Join(wikiDir, "open-questions.md"), openQuestions, result); err != nil {
		return nil, err
	}

	// Generate config.yml
	configContent := generateDefaultConfig(opts)
	if err := writeFile(dr, filepath.Join(plexiumDir, "config.yml"), configContent, result); err != nil {
		return nil, err
	}

	// Initialize manifest.json
	if !opts.DryRun {
		mgr, err := manifest.NewManager(manifest.DefaultPath(opts.RepoRoot))
		if err != nil {
			return nil, fmt.Errorf("creating manifest manager: %w", err)
		}
		if err := mgr.Save(manifest.NewEmptyManifest()); err != nil {
			return nil, fmt.Errorf("saving initial manifest: %w", err)
		}
		result.FilesCreated = append(result.FilesCreated, manifest.DefaultPath(opts.RepoRoot))
	} else {
		dr.Report("would create manifest.json")
	}

	// Generate Obsidian config if requested
	if opts.Obsidian {
		// Use the new Obsidian config generator
		obsidianCfg := NewObsidianConfig(opts.RepoRoot, opts.DryRun)
		if err := obsidianCfg.Ensure(); err != nil {
			return nil, err
		}
		for _, f := range obsidianCfg.FilesCreated() {
			result.FilesCreated = append(result.FilesCreated, f)
		}

		// Create templates directory with dataview queries
		if err := EnsureTemplates(opts.RepoRoot, opts.DryRun); err != nil {
			return nil, err
		}
		result.DirsCreated = append(result.DirsCreated, filepath.Join(wikiDir, "templates"))
		result.FilesCreated = append(result.FilesCreated, filepath.Join(wikiDir, "templates", "dataview-queries.md"))
	}

	// Set up optional integrations (--with-memento, --with-beads, --with-pageindex)
	if opts.WithMemento && !opts.DryRun {
		cmd := exec.Command("git", "memento", "init")
		cmd.Dir = opts.RepoRoot
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: git memento init failed: %v\n", err)
		}
	}

	if opts.WithBeads && !opts.DryRun {
		cmd := exec.Command("bd", "init")
		cmd.Dir = opts.RepoRoot
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: bd init failed: %v\n", err)
		}
	}

	if opts.WithPageIndex && !opts.DryRun {
		// Generate MCP config for PageIndex server
		mcpConfig := `{
  "server": "plexium-pageindex",
  "command": "plexium",
  "args": ["pageindex", "serve"],
  "transport": "stdio"
}
`
		mcpPath := filepath.Join(opts.RepoRoot, ".plexium", "pageindex-mcp.json")
		if err := os.WriteFile(mcpPath, []byte(mcpConfig), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write PageIndex MCP config: %v\n", err)
		} else {
			result.FilesCreated = append(result.FilesCreated, mcpPath)
		}
	}

	return result, nil
}

func writeFile(dr *manifest.DryRunner, path string, content string, result *InitResult) error {
	relPath, err := filepath.Rel(filepath.Dir(filepath.Dir(path)), path)
	if err != nil {
		relPath = path
	}

	// Skip existing files to keep init idempotent and non-destructive.
	// User-edited files are preserved on re-run.
	if _, statErr := os.Stat(path); statErr == nil {
		// File exists — skip writing but report it.
		result.FilesCreated = append(result.FilesCreated, relPath+" [skipped, existing]")
		return nil
	}

	if err := dr.WriteFile(path, []byte(content)); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	result.FilesCreated = append(result.FilesCreated, relPath)
	return nil
}

func generateDefaultSchema(opts InitOptions) string {
	strictness := opts.Strictness
	if strictness == "" {
		strictness = "moderate"
	}

	return fmt.Sprintf(`---
title: Schema
ownership: managed
last-updated: %s
---

# Plexium Wiki Schema

## Wiki Rules

### Page Structure
- Every page has YAML frontmatter with: title, ownership, last-updated
- Ownership types: managed, human-authored, co-maintained
- Managed pages are regenerated by Plexium; human-authored pages are never overwritten

### Sections
- architecture/ — System-level architectural documentation
- modules/ — Per-module documentation generated from source
- decisions/ — Architecture Decision Records (ADRs)
- patterns/ — Recurring code patterns
- concepts/ — Domain concepts and abstractions
- raw/ — Unprocessed source material

### Linking
- Use [[WikiLinks]] for cross-references within the wiki
- All links must be validated before commit

### Strictness: %s

## Naming Conventions
- File names: lowercase, hyphenated (e.g., module-name.md)
- Titles: Human-readable, title case
- Slugs: Derived from titles, lowercase, hyphenated
`, time.Now().Format("2006-01-02"), strictness)
}

func generateHome(opts InitOptions) string {
	// Try to read README.md
	readmePath := filepath.Join(opts.RepoRoot, "README.md")
	readmeBody := ""
	if data, err := os.ReadFile(readmePath); err == nil {
		readmeBody = strings.TrimSpace(string(data))
		// Remove existing frontmatter from README
		if strings.HasPrefix(readmeBody, "---\n") {
			if idx := strings.Index(readmeBody[4:], "\n---\n"); idx != -1 {
				readmeBody = readmeBody[4+idx+5:]
			}
		}
	}

	repoName := filepath.Base(opts.RepoRoot)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("---\ntitle: %q\nownership: managed\nlast-updated: %s\n---\n\n",
		repoName, time.Now().Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("# %s\n\n", repoName))
	b.WriteString(fmt.Sprintf("Wiki for %s, maintained by Plexium.\n\n", repoName))

	if readmeBody != "" {
		b.WriteString("---\n\n")
		b.WriteString(readmeBody)
		b.WriteString("\n")
	}

	return b.String()
}

func generateFooter() string {
	return fmt.Sprintf(`---
---

*Last updated: %s*

Powered by [Plexium](https://github.com/Clarit-AI/Plexium)

[[Home.md|Back to Home]]
`, time.Now().Format("2006-01-02"))
}

func generateArchStub() string {
	return `---
title: "Architecture Overview"
ownership: managed
last-updated: pending
---

# Architecture Overview

> This page is a stub. It will be filled in by agents as they analyze the codebase.
`
}

func generateOnboardingStub() string {
	return `---
title: "Onboarding"
ownership: co-maintained
last-updated: pending
---

# Onboarding Guide

> This page provides a guide for new developers joining the project.
> It will be co-maintained by both agents and humans.

## Quick Start

<!-- TODO: Add quick start instructions -->

## Key Concepts

<!-- TODO: Add key concepts -->
`
}

func generateEmptyStub(title, description string) string {
	return fmt.Sprintf(`---
title: %q
ownership: managed
last-updated: pending
---

# %s

%s
`, title, title, description)
}

func generateDefaultConfig(opts InitOptions) string {
	strictness := opts.Strictness
	if strictness == "" {
		strictness = "moderate"
	}

	return fmt.Sprintf(`version: 1

repo:
  defaultBranch: main
  wikiEnabled: %t

sources:
  include:
    - "**/*.go"
    - "**/*.md"
    - "**/*.yml"
    - "**/*.yaml"
  exclude:
    - "vendor/**"
    - ".wiki/**"
    - ".plexium/**"

agents:
  adapters: []
  strictness: %s

wiki:
  root: .wiki
  home: Home.md
  sidebar: _Sidebar.md
  footer: _Footer.md
  log: _log.md
  index: _index.md
  schema: _schema.md

taxonomy:
  sections:
    - Architecture
    - Modules
    - Decisions
    - Patterns
    - Concepts
    - Guides
  autoClassify: true

publish:
  branch: main
  message: "docs: update wiki"
  autoPush: false
  preserveUnmanagedPages: true
  managedMarkerComment: true

sync:
  mode: incremental
  autoSync: false
  onCommit: false
  onPush: false
  rewriteHomeOnSync: false
  rewriteSidebarOnSync: false
  idempotent: true
  exclude: []

enforcement:
  preCommitHook: false
  ciCheck: false
  mementoGate: %t
  strictness: %s
  blockOnDebt: false
  debtThreshold: 0

integrations:
  llmProvider: ""
  memento: %t
  beads: %t
  pageindex: %t
  obsidian: %t

reports:
  bootstrap:
    - markdown
  sync:
    - markdown
  lint:
    - markdown
  format: markdown
  outputDir: .plexium/output

githubWiki:
  enabled: %t
  submodule: false
  publish: []
  exclude: []

sensitivity:
  rules: ""
  neverPublish: []
  maxFileSize: 1048576
  excludeExtensions:
    - ".env"
    - ".key"
    - ".pem"
    - ".secret"
`, opts.GitHubWiki, strictness, opts.WithMemento, strictness, opts.WithMemento, opts.WithBeads, opts.WithPageIndex, opts.Obsidian, opts.GitHubWiki)
}