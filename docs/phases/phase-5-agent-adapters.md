# Phase 5: Agent Adapters

> **Model:** Mid-tier — Sonnet 4 (primary), GPT 4.1, Gemini 2.5 Flash acceptable
> **Execution:** Solo Agent
> **Status:** Complete  
> **bd Epic:** `plexium-m5`  
> **Prerequisites:** Phase 3 complete (independent of Phase 4)

## Objective

Implement the plugin architecture for agent adapter plugins, `_schema.md` generation customized per tech stack, and adapter scripts for Claude, Codex, Gemini, and Cursor. This enables Plexium to inject its behavioral schema into every coding agent's instruction files.

## Architecture Context

- [Universal Schema](../architecture/core-architecture.md#the-universal-schema) — Schema content and agent injection table
- [Invariants](../architecture/core-architecture.md#invariants--failure-modes) — Schema injection must be present in all configured agent instruction files

## Spec Sections Covered

- §4 The Universal Schema (schema injection table)
- §9 The CLI (`plexium plugin add <name>`, `plexium plugin list`)
- §16 Tool Integrations (agent adapter concept)

## Deliverables

1. **Plugin architecture** — `.plexium/plugins/` directory with adapter scripts
2. **Schema generator** — Generates `_schema.md` customized for detected tech stack
3. **Claude adapter** — Generates `CLAUDE.md`
4. **Codex adapter** — Generates `AGENTS.md`
5. **Gemini adapter** — Generates `.gemini/config.md`
6. **Cursor adapter** — Generates `.cursor/rules/plexium.mdc`
7. **`plexium plugin add <name>` command** — Adds a new adapter

## Tasks

### M5.1: Plugin Architecture

Define the plugin interface and directory structure.

**Plugin directory structure:**
```
.plexium/plugins/
├── README.md              # Plugin authoring guide
├── claude/
│   ├── plugin.sh          # Adapter script
│   └── manifest.json      # Plugin metadata
├── codex/
│   ├── plugin.sh
│   └── manifest.json
├── gemini/
│   ├── plugin.sh
│   └── manifest.json
└── cursor/
    ├── plugin.sh
    └── manifest.json
```

**Plugin manifest:**
```json
{
  "name": "claude",
  "version": 1,
  "description": "Claude Code adapter for Plexium",
  "instructionFile": "CLAUDE.md",
  "instructionFilePath": ".",  // relative to repo root
  "schemaInjection": true,
  "requires": ["_schema.md"]
}
```

**Adapter script interface:**
```bash
#!/bin/bash
# .plexium/plugins/claude/plugin.sh

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLEXIUM_DIR="$(cd "$PLUGIN_DIR/../.." && pwd)"
SCHEMA_PATH="$PLEXIUM_DIR/wiki/_schema.md"
OUTPUT_PATH="$PLEXIUM_DIR/CLAUDE.md"

# Generate CLAUDE.md by combining agent-specific preamble + schema content
# The schema content is injected by plexium init/upgrade
```

### M5.2: Schema Generator

Generate `_schema.md` customized for the detected tech stack.

**Implementation:**
```go
// internal/plugins/schema.go
type SchemaGenerator struct {
    detectedStack string  // Detected from package.json, Cargo.toml, etc.
}

func (g *SchemaGenerator) Generate() (string, error)

// Detect stack from files:
// - package.json + tsconfig.json → "typescript"
// - requirements.txt + setup.py → "python"
// - Cargo.toml → "rust"
// - go.mod → "go"
// - pom.xml → "java"
// - package.json (no tsconfig) → "javascript"
```

**Schema customization:**
- Base schema is universal (from `docs/architecture/core-architecture.md#the-universal-schema`)
- Tech stack detection adds stack-specific examples:
  - TypeScript: `.ts` extension, `interface` vs `type`, `npm run` scripts
  - Python: `.py` files, `def`/`class`, `pytest` conventions
  - Rust: `.rs` files, `fn`/`struct`/`impl`, `cargo test`
  - Go: `.go` files, `func`/`type`/`struct`, `go test`
  - etc.

### M5.3: Claude Adapter

Generate `CLAUDE.md` — the instruction file for Claude Code.

**Output location:** `CLAUDE.md` (repo root)

**Structure:**
```markdown
# Claude Code — Plexium Wiki Maintenance

You are working on a **Plexium** project. This repository uses an LLM-maintained
wiki (`.wiki/`) that you are responsible for keeping current.

## Your Responsibilities

1. **Before any code change**: Read `.wiki/_index.md` and relevant wiki pages
2. **After any code change**: Update affected wiki pages
3. **Never modify** pages with `ownership: human-authored`

## Plexium Schema

<!-- SCHEMA_INJECT_START -->
[Full _schema.md content from .wiki/_schema.md]
<!-- SCHEMA_INJECT_END -->

## Quick Reference

- Wiki: `.wiki/`
- Manifest: `.plexium/manifest.json`
- Report issues: `plexium lint --ci`

## Detected Stack

[typeScript/python/etc.]

## Commands

```bash
plexium sync      # Update wiki after changes
plexium lint      # Check wiki health
plexium retrieve  # Query the wiki
```
```

### M5.4: Codex Adapter

Generate `AGENTS.md` — the instruction file for OpenAI Codex.

**Output location:** `AGENTS.md` (repo root)

**Same structure as CLAUDE.md** but Codex-specific preamble and formatting.

### M5.5: Gemini Adapter

Generate `.gemini/config.md` — the instruction file for Gemini CLI.

**Output location:** `.gemini/config.md`

**Same schema content** but Gemini-specific preamble and formatting.

### M5.6: Cursor Adapter

Generate `.cursor/rules/plexium.mdc` — the instruction file for Cursor.

**Output location:** `.cursor/rules/plexium.mdc`

**MDC format** (Cursor's Markdown Commands format):
```markdown
---
description: Plexium wiki maintenance for Cursor
---

# Plexium Wiki Maintenance

[Schema content adapted for MDC format]

# Reference

- Wiki: `.wiki/`
- Manifest: `.plexium/manifest.json`
```

### M5.7: plexium plugin add Command

Add a new adapter plugin.

**Command:**
```bash
plexium plugin add <name> [--path /path/to/plugin]
```

**Implementation:**
```go
// cmd/plugin.go
func runPluginAdd(cmd *cobra.Command, args []string) error {
    name := args[0]
    pluginPath := flags.GetString("path")
    
    // If path provided, install from local path
    // If path not provided, look for official plugin (future: plugin registry)
    
    // 1. Validate plugin manifest
    // 2. Copy plugin to .plexium/plugins/<name>/
    // 3. Run plugin to generate instruction file
    // 4. Verify instruction file was created
    // 5. Report success
}
```

### M5.8: plexium init Integration

Update `plexium init` to run all detected adapters.

**In `plexium init` (Phase 3), after creating `.wiki/_schema.md`:**
```go
// After schema generation, run all detected agent adapters
adapters := detectAvailableAdapters()  // Check for claude, codex, gemini, cursor
for _, adapter := range adapters {
    pluginDir := filepath.Join(".plexium/plugins", adapter)
    if _, err := os.Stat(pluginDir); err == nil {
        runPlugin(pluginDir)  // Execute the adapter script
    }
}
```

### M5.9: plexium convert Integration

Wire agent adapters into the convert pipeline so `plexium convert --agent <name>` runs the selected adapter after conversion completes.

**Implementation:**
- Add `--agent` flag to `plexium convert` command
- After conversion pipeline finishes, run the selected adapter's plugin script
- If no `--agent` flag provided, skip adapter execution

## Interfaces

**Consumes from Phase 3:**
- Schema file location (`.wiki/_schema.md`)
- Config structure

**Consumes from Phase 1:**
- CLI command routing

**Provides to all subsequent phases:**
- Agent adapters ensure every coding agent follows Plexium's schema
- New agents onboarded via `plexium plugin add <name>`

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | `.plexium/plugins/claude/plugin.sh` exists and is executable |
| AC2 | `.plexium/plugins/codex/plugin.sh` exists and is executable |
| AC3 | `.plexium/plugins/gemini/plugin.sh` exists and is executable |
| AC4 | `.plexium/plugins/cursor/plugin.sh` exists and is executable |
| AC5 | Running plugin.sh generates valid instruction file |
| AC6 | `CLAUDE.md` contains full schema content |
| AC7 | `AGENTS.md` contains full schema content (Codex format) |
| AC8 | `.gemini/config.md` contains full schema content (Gemini format) |
| AC9 | `.cursor/rules/plexium.mdc` contains full schema content (MDC format) |
| AC10 | `plexium plugin add <name>` copies plugin to `.plexium/plugins/` |
| AC11 | `plexium init` runs all detected adapters |
| AC12 | Schema generator detects tech stack correctly |
| AC13 | `plexium convert --agent <name>` runs selected adapter after conversion; no adapter runs when flag is omitted |

## bd Task Mapping

```
plexium-m5
├── M5.1: Plugin architecture and directory structure
├── M5.2: Schema generator with tech stack detection
├── M5.3: Claude adapter (CLAUDE.md)
├── M5.4: Codex adapter (AGENTS.md)
├── M5.5: Gemini adapter (.gemini/config.md)
├── M5.6: Cursor adapter (.cursor/rules/plexium.mdc)
├── M5.7: plexium plugin add command
├── M5.8: plexium init integration (run adapters after schema gen)
└── M5.9: plexium convert integration (--agent flag wiring)
```