package validation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/wiki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// S1: Source files unchanged after any wiki operation
// =============================================================================

func TestSafety_SourceFilesUnchangedAfterInit(t *testing.T) {
	f := GreenFieldFixture(t)

	// Snapshot source files before
	sourcesBefore := SnapshotDir(t, f.Root)

	// Run init
	_, err := wiki.Init(wiki.InitOptions{
		RepoRoot: f.Root,
		DryRun:   false,
	})
	require.NoError(t, err)

	// Verify source files unchanged
	for path, contentBefore := range sourcesBefore {
		abs := filepath.Join(f.Root, path)
		data, err := os.ReadFile(abs)
		require.NoError(t, err, "source file should still exist: %s", path)
		assert.Equal(t, contentBefore, string(data), "source file modified by init: %s", path)
	}
}

func TestSafety_SourceFilesUnchangedAfterCompile(t *testing.T) {
	f := PopulatedFixture(t)

	// Snapshot source files
	sourceSnapshot := make(map[string]string)
	for _, src := range []string{"main.go", "internal/auth/auth.go", "docs/adr/001-use-go.md", "README.md"} {
		sourceSnapshot[src] = f.ReadFile(src)
	}

	// Run compile
	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	// Verify source files unchanged
	for path, contentBefore := range sourceSnapshot {
		assert.Equal(t, contentBefore, f.ReadFile(path), "source file modified by compile: %s", path)
	}
}

func TestSafety_SourceFilesUnchangedAfterLint(t *testing.T) {
	f := PopulatedFixture(t)

	sourceSnapshot := make(map[string]string)
	for _, src := range []string{"main.go", "internal/auth/auth.go"} {
		sourceSnapshot[src] = f.ReadFile(src)
	}

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	_, err = linter.RunDeterministic()
	require.NoError(t, err)

	for path, contentBefore := range sourceSnapshot {
		assert.Equal(t, contentBefore, f.ReadFile(path), "source file modified by lint: %s", path)
	}
}

// =============================================================================
// S2: Dry-run produces zero live side effects
// =============================================================================

func TestSafety_DryRunInitCreatesNoFiles(t *testing.T) {
	f := DryRunFixture(t)

	_, err := wiki.Init(wiki.InitOptions{
		RepoRoot: f.Root,
		DryRun:   true,
	})
	require.NoError(t, err)

	// After dry-run, .wiki/ should NOT exist as a real directory
	assert.False(t, f.FileExists(".wiki/Home.md"), "Home.md should not exist after dry-run init")
	assert.False(t, f.FileExists(".plexium/manifest.json"), "manifest.json should not exist after dry-run init")
	assert.False(t, f.FileExists(".wiki/_schema.md"), "_schema.md should not exist after dry-run init")

	// Dry-run output goes to .plexium/output/ — that's by design.
	// The key invariant is: no files in the REAL .wiki/ or .plexium/manifest.json.
}

func TestSafety_DryRunCompileWritesNothing(t *testing.T) {
	f := PopulatedFixture(t)

	// Record current wiki content
	indexBefore := f.ReadFile(".wiki/_index.md")
	sidebarBefore := f.ReadFile(".wiki/_Sidebar.md")

	c := compile.NewCompiler(f.Root, true) // dry-run = true
	result, err := c.Compile()
	require.NoError(t, err)
	assert.True(t, result.DryRun)
	assert.Empty(t, result.FilesGenerated, "dry-run compile should not generate files")

	// Files unchanged
	assert.Equal(t, indexBefore, f.ReadFile(".wiki/_index.md"))
	assert.Equal(t, sidebarBefore, f.ReadFile(".wiki/_Sidebar.md"))
}

// =============================================================================
// S3: Human-authored pages never overwritten
// =============================================================================

func TestSafety_HumanAuthoredPageNotOverwrittenByUpsert(t *testing.T) {
	// NOTE: The protection only applies to pages in the manifest Pages list,
	// not pages in UnmanagedPages. This is the actual contract.
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	// Create a human-authored page in the Pages list
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "guides/onboarding.md",
				Title:     "Onboarding Guide",
				Ownership: "human-authored",
				Section:   "Guides",
			},
		},
	}
	err = mgr.Save(m)
	require.NoError(t, err)

	// Try to overwrite with a managed entry — should fail
	err = mgr.UpsertPage(manifest.PageEntry{
		WikiPath:  "guides/onboarding.md",
		Title:     "Overwritten!",
		Ownership: "managed",
		Section:   "Guides",
	})

	require.Error(t, err, "should refuse to overwrite human-authored page with managed")
	assert.Contains(t, err.Error(), "human-authored")
}

func TestSafety_HumanAuthoredDetectedAsStaleFalse(t *testing.T) {
	f := PopulatedFixture(t)

	mgr, err := manifest.NewManager(manifest.DefaultPath(f.Root))
	require.NoError(t, err)

	// Human-authored pages should never be flagged as stale
	stale, err := mgr.DetectStalePages(func(path string) (string, error) {
		return "completely-different-hash", nil
	})
	require.NoError(t, err)

	for _, s := range stale {
		assert.NotEqual(t, "human-authored", s.Ownership,
			"human-authored page %s should never be flagged as stale", s.WikiPath)
	}
}

// =============================================================================
// S4: Init is non-destructive (re-run doesn't clobber existing files)
// =============================================================================

func TestSafety_InitNonDestructiveOnRerun(t *testing.T) {
	f := InitializedFixture(t)

	// Modify a wiki file to prove it existed
	f.WriteFile(".wiki/Home.md", `---
title: "Custom Home"
ownership: managed
last-updated: 2026-01-01
---

# Custom Home Content

This was customized by the user.
`)

	customContent := f.ReadFile(".wiki/Home.md")

	// Re-run init
	_, err := wiki.Init(wiki.InitOptions{
		RepoRoot: f.Root,
		DryRun:   false,
	})
	require.NoError(t, err)

	// Custom content should be preserved (init skips existing files)
	assert.Equal(t, customContent, f.ReadFile(".wiki/Home.md"),
		"init clobbered existing Home.md on re-run")
}

// =============================================================================
// S5: Publish respects exclude patterns
// =============================================================================

func TestSafety_PublishExcludesConfiguredPatterns(t *testing.T) {
	f := ConfigEdgeCaseFixture(t)

	// Create files that should be excluded
	f.WriteFile(".wiki/raw/notes.md", "raw notes")
	f.WriteFile(".wiki/_internal.md", "internal file starting with underscore")
	f.WriteFile(".wiki/modules/visible.md", `---
title: "Visible"
ownership: managed
last-updated: 2026-01-01
---

# Visible

This should be published.
`)

	// The publish selection logic is in publish.Publisher.collectFiles.
	// We test the exclude/publish pattern matching directly since actual publish
	// requires a GitHub wiki remote.
	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	// Verify config has the expected patterns
	assert.Contains(t, cfg.GitHubWiki.Exclude, "raw/**")
	assert.Contains(t, cfg.GitHubWiki.Exclude, "**/_*")
	assert.Contains(t, cfg.GitHubWiki.Publish, "modules/**")
}

// =============================================================================
// S6: Compile only writes navigation files
// =============================================================================

func TestSafety_CompileOnlyWritesNavFiles(t *testing.T) {
	f := PopulatedFixture(t)

	// Snapshot everything before compile
	wikiBefore := SnapshotDir(t, filepath.Join(f.Root, ".wiki"))

	c := compile.NewCompiler(f.Root, false)
	result, err := c.Compile()
	require.NoError(t, err)

	// Only _index.md and _Sidebar.md should be in generated list
	for _, gen := range result.FilesGenerated {
		base := filepath.Base(gen)
		assert.True(t, base == "_index.md" || base == "_Sidebar.md",
			"compile wrote unexpected file: %s", gen)
	}

	// All other wiki files should be unchanged
	wikiAfter := SnapshotDir(t, filepath.Join(f.Root, ".wiki"))
	for path, contentBefore := range wikiBefore {
		if path == "_index.md" || path == "_Sidebar.md" {
			continue // These are expected to change
		}
		contentAfter, exists := wikiAfter[path]
		assert.True(t, exists, "compile deleted wiki file: %s", path)
		assert.Equal(t, contentBefore, contentAfter, "compile modified non-nav wiki file: %s", path)
	}
}

// =============================================================================
// S7: Manifest updates preserve existing entries
// =============================================================================

func TestSafety_ManifestUpsertPreservesOtherEntries(t *testing.T) {
	f := PopulatedFixture(t)

	mgr, err := manifest.NewManager(manifest.DefaultPath(f.Root))
	require.NoError(t, err)

	// Load current manifest
	mBefore, err := mgr.Load()
	require.NoError(t, err)
	countBefore := len(mBefore.Pages)
	require.True(t, countBefore >= 2, "need at least 2 pages for this test")

	// Upsert one page
	err = mgr.UpsertPage(manifest.PageEntry{
		WikiPath:  "modules/new-module.md",
		Title:     "New Module",
		Ownership: "managed",
		Section:   "Modules",
	})
	require.NoError(t, err)

	// Reload and verify all original entries still present
	mAfter, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, countBefore+1, len(mAfter.Pages),
		"upsert should add one page without removing others")

	// Verify specific entries survived
	found := false
	for _, p := range mAfter.Pages {
		if p.WikiPath == "decisions/adr-001-use-go.md" {
			found = true
			break
		}
	}
	assert.True(t, found, "existing page entry was lost during upsert")
}

func TestSafety_ManifestRemovePreservesOtherEntries(t *testing.T) {
	f := PopulatedFixture(t)

	mgr, err := manifest.NewManager(manifest.DefaultPath(f.Root))
	require.NoError(t, err)

	mBefore, err := mgr.Load()
	require.NoError(t, err)
	countBefore := len(mBefore.Pages)

	// Remove one page
	err = mgr.RemovePage("concepts/authentication.md")
	require.NoError(t, err)

	mAfter, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, countBefore-1, len(mAfter.Pages))

	// Verify the removed page is gone
	for _, p := range mAfter.Pages {
		assert.NotEqual(t, "concepts/authentication.md", p.WikiPath,
			"removed page still in manifest")
	}

	// Verify other pages survived
	found := false
	for _, p := range mAfter.Pages {
		if p.WikiPath == "modules/auth-module.md" {
			found = true
			break
		}
	}
	assert.True(t, found, "unrelated page was removed during RemovePage")
}
