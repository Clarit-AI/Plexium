---
name: plexium-dev
description: Develop and contribute to the Plexium codebase — package structure, build commands, test patterns, architecture layers
---

# Contributing to Plexium

Plexium is a Go CLI tool that gives repositories a persistent, agent-maintained wiki. This skill covers the codebase structure, build process, and contribution patterns.

## Build and Test

```bash
# Build
go build ./...

# Run all tests (540+ across 25 packages)
go test ./...

# Run a specific package
go test ./internal/agent/...

# Build the binary
go build -o plexium ./cmd/plexium

# Run the binary
./plexium --help
```

## Package Structure

```
cmd/plexium/          # CLI entry point — cobra commands across multiple *.go files
internal/
  agent/              # Provider cascade, task router, rate limiter, HTTP transport, setup
  ci/                 # CI check command (diff-aware wiki validation)
  compile/            # Navigation file regeneration (_index.md, _Sidebar.md)
  config/             # Config loading (viper), validation, env overrides
  convert/            # Brownfield ingestion pipeline (scour/filter/ingest/link/lint)
  daemon/             # Autonomous maintenance loop, workspace manager, runners, trackers
  generate/           # Page generators for each taxonomy section
  hook/               # Git hook handlers (pre-commit, post-commit)
  integrations/
    beads/            # Task tracking integration
    memento/          # Session provenance (ingest transcripts, CI gate)
    pageindex/        # PageIndex search engine, MCP server, retrieval
    roles/            # Agent role definitions (coder, retriever, documenter, ingestor)
  lint/               # Deterministic lint + LLM-augmented analysis, doctor
  manifest/           # Manifest manager (bidirectional source-wiki mapping)
  markdown/           # Markdown parser (frontmatter extraction, wiki-link parsing)
  migrate/            # Schema version migrations
  plugins/            # Plugin architecture (manifest, loader, schema generation)
  publish/            # GitHub Wiki publishing
  reports/            # Report generation (bootstrap, sync, lint)
  retry/              # Exponential backoff with jitter
  scanner/            # Source file scanner with glob matching
  template/           # Page templates
  wiki/               # Wiki scaffolding (init), page operations
```

## Architecture Layers

```
Source Layer (immutable)     — src/**, docs/**
State Manifest              — .plexium/manifest.json
Wiki Layer                  — .wiki/**
Control Layer               — _schema.md, config.yml, lefthook.yml
Enforcement                 — Schema (soft) → Git hooks (medium) → CI (hard)
```

**Invariants** (these must never break):
- Wiki operations never modify source code
- Pages with `ownership: human-authored` are never overwritten
- Partial failures roll back — no half-updated wikis
- All `[[wiki-links]]` validated before commit
- Sync is idempotent (same commit → identical output)

## Key Patterns

- **Injectable dependencies**: HTTP functions, runners, trackers use injectable function signatures for testing. See `cascade.go` providers and `daemon/runner.go`.
- **Structural interfaces**: `lint.LLMClient`, `daemon.RunnerAdapter`, `daemon.TrackerAdapter` use Go's structural typing — implementations don't import the interface package.
- **Config-driven behavior**: Everything reads from `.plexium/config.yml` via viper. Zero-value defaults preserve backward compatibility.
- **Testify for assertions**: All tests use `github.com/stretchr/testify/assert` and `require`.

## When to Read Reference Docs

- Need to understand a specific package → `reference/packages.md`
- Working on the provider cascade or setup → `reference/agent-system.md`
- Working on the daemon or workspace manager → `reference/daemon-system.md`
- Need to understand the test patterns → `reference/testing.md`
