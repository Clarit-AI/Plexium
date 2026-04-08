package validation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// FixtureRepo creates a temporary directory with the specified structure.
// Returns the repo root path. Cleanup is handled by t.TempDir().
type FixtureRepo struct {
	Root string
	t    *testing.T
}

func NewFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	return &FixtureRepo{
		Root: t.TempDir(),
		t:    t,
	}
}

// WriteFile creates a file with the given content at relPath under the repo root.
func (f *FixtureRepo) WriteFile(relPath, content string) {
	f.t.Helper()
	abs := filepath.Join(f.Root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		f.t.Fatalf("fixture mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		f.t.Fatalf("fixture write %s: %v", abs, err)
	}
}

// MkDir creates a directory at relPath under the repo root.
func (f *FixtureRepo) MkDir(relPath string) {
	f.t.Helper()
	abs := filepath.Join(f.Root, relPath)
	if err := os.MkdirAll(abs, 0755); err != nil {
		f.t.Fatalf("fixture mkdir %s: %v", abs, err)
	}
}

// ReadFile reads a file from relPath under the repo root.
func (f *FixtureRepo) ReadFile(relPath string) string {
	f.t.Helper()
	abs := filepath.Join(f.Root, relPath)
	data, err := os.ReadFile(abs)
	if err != nil {
		f.t.Fatalf("fixture read %s: %v", abs, err)
	}
	return string(data)
}

// FileExists returns true if the file exists at relPath.
func (f *FixtureRepo) FileExists(relPath string) bool {
	abs := filepath.Join(f.Root, relPath)
	_, err := os.Stat(abs)
	return err == nil
}

// WriteManifest writes a manifest.json to .plexium/manifest.json using the
// Manager (which sorts pages by WikiPath for deterministic output).
func (f *FixtureRepo) WriteManifest(m *manifest.Manifest) {
	f.t.Helper()
	f.MkDir(".plexium")
	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	if err != nil {
		f.t.Fatalf("create manifest manager: %v", err)
	}
	if err := mgr.Save(m); err != nil {
		f.t.Fatalf("save manifest: %v", err)
	}
}

// WriteConfig writes a config.yml to .plexium/config.yml.
func (f *FixtureRepo) WriteConfig(content string) {
	f.t.Helper()
	f.WriteFile(".plexium/config.yml", content)
}

// WriteDefaultConfig writes a minimal valid config.
func (f *FixtureRepo) WriteDefaultConfig() {
	f.WriteConfig(`version: 1
repo:
  defaultBranch: main
  wikiEnabled: false
sources:
  include:
    - "**/*.go"
    - "**/*.md"
  exclude:
    - "vendor/**"
    - ".wiki/**"
    - ".plexium/**"
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
  autoClassify: true
publish:
  branch: main
  preserveUnmanagedPages: true
sync:
  mode: incremental
  idempotent: true
enforcement:
  strictness: moderate
githubWiki:
  enabled: false
sensitivity:
  maxFileSize: 1048576
  excludeExtensions:
    - ".env"
    - ".key"
`)
}

// --- Pre-built fixture scenarios ---

// GreenFieldFixture creates a bare repo with only source files, no .wiki/ or .plexium/.
func GreenFieldFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := NewFixture(t)
	f.WriteFile("README.md", "# Test Project\n\nA test project.\n")
	f.WriteFile("main.go", `package main

func main() {
	println("hello")
}
`)
	f.WriteFile("internal/auth/auth.go", `package auth

// Authenticate validates user credentials.
func Authenticate(user, pass string) bool {
	return user != "" && pass != ""
}
`)
	f.WriteFile("docs/adr/001-use-go.md", `# ADR-001: Use Go

## Status
Accepted

## Decision
Use Go for the CLI tool.
`)
	return f
}

// InitializedFixture creates a repo that has been through `plexium init`.
func InitializedFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := GreenFieldFixture(t)
	f.WriteDefaultConfig()
	f.WriteManifest(manifest.NewEmptyManifest())

	// Scaffold wiki structure
	f.WriteFile(".wiki/_schema.md", `---
title: Schema
ownership: managed
last-updated: 2026-01-01
---

# Plexium Wiki Schema
`)
	f.WriteFile(".wiki/_index.md", "# Wiki Index\n\n_Run `plexium compile` to regenerate this file._\n")
	f.WriteFile(".wiki/_Sidebar.md", `**[[Home]]**

**Start Here**
- [[architecture/overview|Architecture Overview]]
- [[onboarding|Onboarding Guide]]
- [[contradictions|Contradictions]]
- [[open-questions|Open Questions]]
- [[_log|Activity Log]]
`)
	f.WriteFile(".wiki/_Footer.md", "Powered by Plexium\n")
	f.WriteFile(".wiki/_log.md", `---
title: "Activity Log"
ownership: co-maintained
last-updated: 2026-01-01
---

# Activity Log

Use this page to capture notable wiki maintenance, validation runs, and follow-up work.
`)
	f.WriteFile(".wiki/Home.md", `---
title: "Test Project"
ownership: managed
last-updated: 2026-01-01
---

# Test Project

Wiki for Test Project.

## Start Here

- [[architecture/overview|Architecture Overview]]
- [[onboarding|Onboarding Guide]]
- [[contradictions|Contradictions]]
- [[open-questions|Open Questions]]
- [[_log|Activity Log]]
`)
	f.WriteFile(".wiki/architecture/overview.md", `---
title: "Architecture Overview"
ownership: managed
last-updated: pending
---

# Architecture Overview

> Stub.
`)
	f.WriteFile(".wiki/onboarding.md", `---
title: "Onboarding"
ownership: co-maintained
last-updated: 2026-01-01
---

# Onboarding Guide

Quick start information for new contributors.
`)
	f.WriteFile(".wiki/contradictions.md", `---
title: "Contradictions"
ownership: managed
last-updated: 2026-01-01
---

# Contradictions

Tracked contradictions between wiki pages.
`)
	f.WriteFile(".wiki/open-questions.md", `---
title: "Open Questions"
ownership: managed
last-updated: 2026-01-01
---

# Open Questions

Unresolved questions about the codebase.
`)
	return f
}

// PopulatedFixture creates a repo with pages in the manifest and wiki.
func PopulatedFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := InitializedFixture(t)

	// Add source-backed wiki pages
	f.WriteFile(".wiki/modules/auth-module.md", `---
title: "Auth Module"
ownership: managed
last-updated: 2026-01-01
---

# Auth Module

Handles authentication. See [[architecture-overview]].
`)
	f.WriteFile(".wiki/decisions/adr-001-use-go.md", `---
title: "ADR-001: Use Go"
ownership: managed
last-updated: 2026-01-01
---

# ADR-001: Use Go

Decided to use Go. See [[auth-module]].
`)
	f.WriteFile(".wiki/concepts/authentication.md", `---
title: "Authentication"
ownership: managed
last-updated: 2026-01-01
---

# Authentication

Core concept. See [[auth-module]] and [[adr-001-use-go]].
`)

	// Add a human-authored page
	f.WriteFile(".wiki/guides/onboarding.md", `---
title: "Onboarding Guide"
ownership: human-authored
last-updated: 2026-01-01
---

# Onboarding Guide

Written by humans. Do not overwrite.
`)

	// Set up manifest with these pages
	m := &manifest.Manifest{
		Version:             1,
		LastProcessedCommit: "abc123",
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
				SourceFiles: []manifest.SourceFile{
					{Path: "internal/auth/auth.go", Hash: "deadbeef"},
				},
				LastUpdated:   "2026-01-01T00:00:00Z",
				OutboundLinks: []string{"architecture/overview.md"},
			},
			{
				WikiPath:  "decisions/adr-001-use-go.md",
				Title:     "ADR-001: Use Go",
				Ownership: "managed",
				Section:   "Decisions",
				SourceFiles: []manifest.SourceFile{
					{Path: "docs/adr/001-use-go.md", Hash: "cafebabe"},
				},
				LastUpdated:   "2026-01-01T00:00:00Z",
				InboundLinks:  []string{"concepts/authentication.md"},
				OutboundLinks: []string{"modules/auth-module.md"},
			},
			{
				WikiPath:    "concepts/authentication.md",
				Title:       "Authentication",
				Ownership:   "managed",
				Section:     "Concepts",
				LastUpdated: "2026-01-01T00:00:00Z",
				OutboundLinks: []string{
					"modules/auth-module.md",
					"decisions/adr-001-use-go.md",
				},
			},
		},
		UnmanagedPages: []manifest.UnmanagedEntry{
			{
				WikiPath:  "guides/onboarding.md",
				FirstSeen: "2026-01-01T00:00:00Z",
				Ownership: "human-authored",
			},
		},
	}
	f.WriteManifest(m)
	return f
}

// BrokenLinksFixture creates a wiki with broken wiki-links.
func BrokenLinksFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := InitializedFixture(t)

	f.WriteFile(".wiki/modules/auth-module.md", `---
title: "Auth Module"
ownership: managed
last-updated: 2026-01-01
---

# Auth Module

See [[nonexistent-page]] and [[also-missing]].
Links to real page: [[Home]].
`)

	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
			},
		},
	}
	f.WriteManifest(m)
	return f
}

// OrphanPagesFixture creates a wiki with orphan pages (no inbound links).
func OrphanPagesFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := InitializedFixture(t)

	// This page exists in wiki but nothing links to it
	f.WriteFile(".wiki/modules/forgotten-module.md", `---
title: "Forgotten Module"
ownership: managed
last-updated: 2026-01-01
---

# Forgotten Module

Nobody links here.
`)

	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/forgotten-module.md",
				Title:     "Forgotten Module",
				Ownership: "managed",
				Section:   "Modules",
			},
		},
	}
	f.WriteManifest(m)
	return f
}

// StaleManifestFixture creates a wiki where source hashes don't match manifest.
func StaleManifestFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := PopulatedFixture(t)

	// Modify the source file so its hash no longer matches manifest
	f.WriteFile("internal/auth/auth.go", `package auth

// Authenticate validates user credentials.
// MODIFIED: added logging
func Authenticate(user, pass string) bool {
	println("auth attempt:", user)
	return user != "" && pass != ""
}
`)
	return f
}

// MixedOwnershipFixture creates a wiki with all three ownership types.
func MixedOwnershipFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := PopulatedFixture(t)

	f.WriteFile(".wiki/architecture/co-maintained.md", `---
title: "Architecture Notes"
ownership: co-maintained
last-updated: 2026-01-01
---

# Architecture Notes

Co-maintained by humans and agents.
`)

	// Update manifest
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
			},
			{
				WikiPath:  "architecture/co-maintained.md",
				Title:     "Architecture Notes",
				Ownership: "co-maintained",
				Section:   "Architecture",
			},
		},
		UnmanagedPages: []manifest.UnmanagedEntry{
			{
				WikiPath:  "guides/onboarding.md",
				FirstSeen: "2026-01-01T00:00:00Z",
				Ownership: "human-authored",
			},
		},
	}
	f.WriteManifest(m)
	return f
}

// DryRunFixture creates a repo for testing dry-run isolation.
func DryRunFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := GreenFieldFixture(t)
	// Deliberately no .wiki/ or .plexium/ — init with dry-run should NOT create them
	return f
}

// ConfigEdgeCaseFixture creates configs with edge cases.
func ConfigEdgeCaseFixture(t *testing.T) *FixtureRepo {
	t.Helper()
	f := InitializedFixture(t)

	// Override with edge-case config: deep glob nesting, exclude overlap
	f.WriteConfig(`version: 1
repo:
  defaultBranch: main
sources:
  include:
    - "**/*.go"
    - "src/**/*.ts"
    - "deeply/nested/**/path/**/*.md"
  exclude:
    - "vendor/**"
    - "node_modules/**"
    - ".wiki/**"
    - ".plexium/**"
    - "**/*_test.go"
    - "**/*.generated.go"
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
  autoClassify: true
publish:
  preserveUnmanagedPages: true
githubWiki:
  enabled: true
  publish:
    - "modules/**"
    - "decisions/**"
  exclude:
    - "raw/**"
    - "**/_*"
sensitivity:
  maxFileSize: 1048576
  excludeExtensions:
    - ".env"
    - ".key"
    - ".pem"
`)
	return f
}

// SnapshotDir captures all file paths and contents under a directory.
func SnapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return snapshot
}
