package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
)

// TechStack represents the detected technology stack
type TechStack string

const (
	TechStackTypeScript TechStack = "typescript"
	TechStackJavaScript TechStack = "javascript"
	TechStackPython     TechStack = "python"
	TechStackRust       TechStack = "rust"
	TechStackGo         TechStack = "go"
	TechStackJava       TechStack = "java"
	TechStackGeneric    TechStack = "generic"
)

// SchemaGenerator generates _schema.md customized for the detected tech stack
type SchemaGenerator struct {
	repoRoot string
	stack    TechStack
}

// NewSchemaGenerator creates a new SchemaGenerator
func NewSchemaGenerator(repoRoot string) *SchemaGenerator {
	return &SchemaGenerator{
		repoRoot: repoRoot,
		stack:    DetectTechStack(repoRoot),
	}
}

// DetectTechStack detects the technology stack from project files
func DetectTechStack(repoRoot string) TechStack {
	// TypeScript: package.json + tsconfig.json
	if hasFile(repoRoot, "package.json") && hasFile(repoRoot, "tsconfig.json") {
		return TechStackTypeScript
	}

	// Python: requirements.txt or setup.py
	if hasFile(repoRoot, "requirements.txt") || hasFile(repoRoot, "setup.py") {
		return TechStackPython
	}

	// Rust: Cargo.toml
	if hasFile(repoRoot, "Cargo.toml") {
		return TechStackRust
	}

	// Go: go.mod
	if hasFile(repoRoot, "go.mod") {
		return TechStackGo
	}

	// Java: pom.xml
	if hasFile(repoRoot, "pom.xml") {
		return TechStackJava
	}

	// JavaScript: package.json only (no TypeScript)
	if hasFile(repoRoot, "package.json") {
		return TechStackJavaScript
	}

	return TechStackGeneric
}

func hasFile(repoRoot, filename string) bool {
	_, err := os.Stat(filepath.Join(repoRoot, filename))
	return err == nil
}

// Generate produces the schema content customized for the detected tech stack
func (g *SchemaGenerator) Generate() (string, error) {
	baseSchema := baseSchemaContent

	// Add stack-specific examples based on detected tech stack
	stackExamples := g.getStackExamples()

	return baseSchema + "\n\n## Tech Stack Examples\n\n" + stackExamples, nil
}

// GetStack returns the detected tech stack
func (g *SchemaGenerator) GetStack() TechStack {
	return g.stack
}

func (g *SchemaGenerator) getStackExamples() string {
	switch g.stack {
	case TechStackTypeScript:
		return "This project uses TypeScript. Key conventions:\n" +
			"- Files use .ts extension (.js for plain JavaScript)\n" +
			"- Types defined with interface or type keywords\n" +
			"- Run scripts with npm run <script>\n" +
			"- Test with npm test or jest"
	case TechStackJavaScript:
		return "This project uses JavaScript. Key conventions:\n" +
			"- Files use .js extension\n" +
			"- Run scripts with npm run <script>\n" +
			"- Test with npm test"
	case TechStackPython:
		return "This project uses Python. Key conventions:\n" +
			"- Files use .py extension\n" +
			"- Functions defined with def, classes with class\n" +
			"- Run with python <script>.py or pip install -e .\n" +
			"- Test with pytest or python -m unittest"
	case TechStackRust:
		return "This project uses Rust. Key conventions:\n" +
			"- Files use .rs extension\n" +
			"- Functions defined with fn, structs with struct, implementations with impl\n" +
			"- Run with cargo run, test with cargo test, build with cargo build"
	case TechStackGo:
		return "This project uses Go. Key conventions:\n" +
			"- Files use .go extension\n" +
			"- Functions defined with func, types with type, structs with struct\n" +
			"- Run with go run, test with go test, build with go build"
	case TechStackJava:
		return "This project uses Java. Key conventions:\n" +
			"- Files use .java extension\n" +
			"- Classes defined with class, methods with return types\n" +
			"- Build with mvn or gradle, test with mvn test"
	default:
		return "Generic project - no specific tech stack detected."
	}
}

// baseSchemaContent is the universal Plexium schema
const baseSchemaContent = `---
schema-version: "1.0"
---

# PLEXIUM SCHEMA v1 — MANDATORY AGENT DIRECTIVES

You are the custodian of the .wiki/ vault in this repository. Your memory
does not persist between sessions, but this vault does. It is the compiled,
persistent knowledge of this entire codebase.

## MANDATORY WORKFLOW — EVERY TASK

### 1. READ (before any code change)
- Read .wiki/_index.md to orient yourself.
- Fetch relevant module, architecture, and decision pages for your work area.
- If a retrieval tool is available (PageIndex MCP, plexium retrieve),
  use it instead of scanning files manually.
- Check .wiki/_log.md (last 10 entries) for recent context.
- Check page ownership frontmatter before modifying any wiki page.

### 2. EXECUTE
- Perform the coding task requested by the user.

### 3. DOCUMENT (FORBIDDEN to end your task without this step)
- Update every .wiki/modules/*.md page affected by your changes.
- If you made an architectural decision, create or update a .wiki/decisions/*.md ADR.
- If you discovered a contradiction, add it to .wiki/contradictions.md.
- Add an entry to .wiki/_log.md (see LOG FORMAT below).
- Update .wiki/_index.md if you created or removed pages.
- Update cross-references ([[wiki-links]]) on pages whose relationships changed.
- NEVER modify pages with ownership: human-authored unless explicitly instructed.
- For ownership: co-maintained pages, append only — do not rewrite existing sections
  unless the user specifically requests it.

### 4. VALIDATE
- Confirm wiki updates are consistent with the code you actually wrote.
- Mark uncertain claims with <!-- CONFIDENCE: low — needs human review -->.
- Verify all [[wiki-links]] you created resolve to existing pages.
- Verify source-files frontmatter references existing paths.

## TRIVIAL CHANGE EXCEPTION
For changes affecting only a single file with no architectural impact
(typo fixes, version bumps, formatting): a brief _log.md entry suffices.
Full wiki update not required.

## LOG FORMAT
Each entry in _log.md must use this parseable format:

  ## [YYYY-MM-DD] {task|ingest|lint|query|convert} | Brief description
  - Changed: modules/auth.md, architecture/overview.md
  - Decision: decisions/015-jwt-rotation.md (new)
  - Contradictions: None found
  - Files touched: src/auth/middleware.ts, src/auth/jwt.ts

## PAGE GENERATION RULES

### Slug rules
- Page names must be filesystem-safe (no spaces — use hyphens).
- Duplicate titles must be deduplicated predictably (append qualifier).
- Heading-derived slugs must remain stable across regenerations.

### Navigation rules
- Every generated page must be reachable from _index.md directly or indirectly.
- _Sidebar.md must expose top-level sections and key pages.
- Navigation ordering must be deterministic (alphabetical within sections).

### Content rules
- Preserve factual meaning from source docs and code.
- NEVER invent implementation details not present in sources.
- Summarize when needed but do not silently discard major sections.
- Prefer cross-links ([[wiki-links]]) over duplicated paragraphs.
- Every page must begin with YAML frontmatter (see FRONTMATTER SPEC).

### Cross-reference rules
- When mentioning a concept, module, or decision that has its own page, use [[wiki-links]].
- Never remove existing cross-references without logging the removal in _log.md.
- When creating a new page, add inbound links from at least 2 related existing pages.

## FRONTMATTER SPEC
Every wiki page must begin with:

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

## LINT PROTOCOL
When asked to lint, check for:
- Pages not updated in >30 days that reference frequently-changed code
- Orphan pages (no inbound [[links]])
- Concepts mentioned in 3+ pages without their own page
- Contradictions between module pages and architecture overview
- source-files in frontmatter referencing paths that no longer exist
- Missing cross-references between related modules
- Pages with confidence: low that need investigation
- Managed pages whose source file hashes differ from the state manifest

## INGEST PROTOCOL
When a new raw source is added (meeting note, ticket export, memento transcript):
1. Read it fully
2. Discuss key takeaways with the user (unless batch mode)
3. Write a summary page or update existing pages
4. Update _index.md, _log.md, _Sidebar.md
5. Cross-reference with existing module/decision pages
6. Flag contradictions with existing wiki content
7. Update the state manifest with new source mappings`

// GetAvailableAdapters returns a list of available agent adapters
func GetAvailableAdapters(repoRoot string) []string {
	pluginsDir := filepath.Join(repoRoot, ".plexium", "plugins")
	var adapters []string

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return adapters
	}

	for _, entry := range entries {
		if entry.IsDir() {
			pluginDir := filepath.Join(pluginsDir, entry.Name())
			scriptPath := filepath.Join(pluginDir, "plugin.sh")
			if _, err := os.Stat(scriptPath); err == nil {
				adapters = append(adapters, entry.Name())
			}
		}
	}

	return adapters
}

// RunAllAdapters runs all available adapter plugins
func RunAllAdapters(repoRoot string) error {
	adapters := GetAvailableAdapters(repoRoot)
	for _, adapter := range adapters {
		pluginDir := filepath.Join(repoRoot, ".plexium", "plugins", adapter)
		scriptPath := filepath.Join(pluginDir, "plugin.sh")

		// Execute via bash
		cmd := exec.Command("bash", scriptPath)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "PLEXIUM_DIR="+repoRoot)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
