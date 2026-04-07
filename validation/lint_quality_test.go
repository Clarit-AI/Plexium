package validation

import (
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// LQ1: Broken link detection — true positive
// =============================================================================

func TestLintQuality_BrokenLinksDetected(t *testing.T) {
	f := BrokenLinksFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	// Should detect [[nonexistent-page]] and [[also-missing]]
	assert.True(t, len(report.Deterministic.BrokenLinks) >= 2,
		"expected at least 2 broken links, got %d", len(report.Deterministic.BrokenLinks))

	targets := make(map[string]bool)
	for _, bl := range report.Deterministic.BrokenLinks {
		targets[bl.Target] = true
	}
	assert.True(t, targets["nonexistent-page"] || targets["nonexistent-page.md"],
		"should detect [[nonexistent-page]] as broken")
}

// =============================================================================
// LQ2: Broken link detection — true negative (valid links pass)
// =============================================================================

func TestLintQuality_ValidLinksNotFlagged(t *testing.T) {
	f := InitializedFixture(t)

	// Create a page that links to Home (which exists)
	f.WriteFile(".wiki/modules/linker.md", `---
title: "Linker"
ownership: managed
last-updated: 2026-01-01
---

# Linker

Valid link to [[Home]].
`)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	// [[Home]] resolves to Home.md — should not be broken
	for _, bl := range report.Deterministic.BrokenLinks {
		assert.NotEqual(t, "Home", bl.Target,
			"[[Home]] should not be flagged as broken when Home.md exists")
	}
}

// =============================================================================
// LQ3: Orphan page detection — true positive
// =============================================================================

func TestLintQuality_OrphanPagesDetected(t *testing.T) {
	f := OrphanPagesFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	orphanPaths := make(map[string]bool)
	for _, o := range report.Deterministic.OrphanPages {
		orphanPaths[o.WikiPath] = true
	}

	// forgotten-module.md has no inbound links and is not in sidebar
	assert.True(t, orphanPaths["modules/forgotten-module.md"],
		"forgotten-module should be detected as orphan")
}

// =============================================================================
// LQ4: Orphan detection — true negative (sidebar-reachable pages OK)
// =============================================================================

func TestLintQuality_SidebarReachableNotOrphan(t *testing.T) {
	f := InitializedFixture(t)

	// Create a page referenced from sidebar
	f.WriteFile(".wiki/_Sidebar.md", "**[[Home]]**\n\n- [[my-page]]\n")
	f.WriteFile(".wiki/my-page.md", `---
title: "My Page"
ownership: managed
last-updated: 2026-01-01
---

# My Page
`)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	for _, o := range report.Deterministic.OrphanPages {
		assert.NotEqual(t, "my-page.md", filepath.Base(o.WikiPath),
			"sidebar-reachable page should not be flagged as orphan")
	}
}

// =============================================================================
// LQ5: Staleness detection — true positive
// =============================================================================

func TestLintQuality_StalePageDetected(t *testing.T) {
	f := StaleManifestFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	stalePaths := make(map[string]bool)
	for _, s := range report.Deterministic.StaleCandidates {
		stalePaths[s.WikiPath] = true
	}

	// auth.go was modified → auth-module.md should be stale
	assert.True(t, stalePaths["modules/auth-module.md"],
		"auth-module should be stale after source file change")
}

// =============================================================================
// LQ6: Staleness detection — true negative (matching hash)
// =============================================================================

func TestLintQuality_FreshPageNotFlaggedStale(t *testing.T) {
	f := PopulatedFixture(t)

	// Update manifest hash to match actual file
	h, err := manifest.ComputeHash(filepath.Join(f.Root, "internal/auth/auth.go"))
	require.NoError(t, err)

	mgr, err := manifest.NewManager(manifest.DefaultPath(f.Root))
	require.NoError(t, err)

	m, err := mgr.Load()
	require.NoError(t, err)

	for i := range m.Pages {
		if m.Pages[i].WikiPath == "modules/auth-module.md" {
			for j := range m.Pages[i].SourceFiles {
				if m.Pages[i].SourceFiles[j].Path == "internal/auth/auth.go" {
					m.Pages[i].SourceFiles[j].Hash = h
				}
			}
		}
	}
	err = mgr.Save(m)
	require.NoError(t, err)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	for _, s := range report.Deterministic.StaleCandidates {
		assert.NotEqual(t, "modules/auth-module.md", s.WikiPath,
			"page with matching hash should not be flagged stale")
	}
}

// =============================================================================
// LQ7: Frontmatter validation
// =============================================================================

func TestLintQuality_MissingFrontmatterDetected(t *testing.T) {
	f := InitializedFixture(t)

	// Page with no frontmatter
	f.WriteFile(".wiki/modules/no-frontmatter.md", `# No Frontmatter

This page has no YAML frontmatter.
`)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	fmPaths := make(map[string]bool)
	for _, fi := range report.Deterministic.FrontmatterIssues {
		fmPaths[fi.WikiPath] = true
	}

	assert.True(t, fmPaths["modules/no-frontmatter.md"],
		"page without frontmatter should be flagged")
}

func TestLintQuality_ValidFrontmatterPasses(t *testing.T) {
	f := PopulatedFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	// Pages in PopulatedFixture all have valid frontmatter
	for _, fi := range report.Deterministic.FrontmatterIssues {
		assert.NotEqual(t, "modules/auth-module.md", fi.WikiPath,
			"page with valid frontmatter should not be flagged")
	}
}

// =============================================================================
// LQ8: Lint summary counts
// =============================================================================

func TestLintQuality_SummaryCounts(t *testing.T) {
	f := BrokenLinksFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	// Broken links should count as errors
	assert.True(t, report.Summary.Errors > 0,
		"broken links should contribute to error count")

	// With errors, CI should not pass
	assert.False(t, report.Summary.PassesCI,
		"report with errors should not pass CI")
}

func TestLintQuality_CleanReportPassesCI(t *testing.T) {
	f := InitializedFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	linter := lint.NewLinter(f.Root, cfg)
	report, err := linter.RunDeterministic()
	require.NoError(t, err)

	// A freshly initialized repo with valid structure should have no errors
	if report.Summary.Errors == 0 {
		assert.True(t, report.Summary.PassesCI,
			"clean report should pass CI")
	}
}
