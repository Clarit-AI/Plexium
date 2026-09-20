package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock providers for regeneration tests
// ---------------------------------------------------------------------------

// mockProvider always returns the configured response.
type mockProvider struct {
	response string
}

func (m *mockProvider) Name() string          { return "mock" }
func (m *mockProvider) IsAvailable() bool     { return true }
func (m *mockProvider) HealthCheck() error    { return nil }
func (m *mockProvider) CostPerToken() float64 { return 0.001 }
func (m *mockProvider) Complete(_ context.Context, _ string) (*agent.CompletionResult, error) {
	return &agent.CompletionResult{Provider: "mock", Response: m.response, TokensUsed: 100}, nil
}

// failingProvider always errors at Complete().
type failingProvider struct{}

func (f *failingProvider) Name() string          { return "failing" }
func (f *failingProvider) IsAvailable() bool     { return true }
func (f *failingProvider) HealthCheck() error    { return nil }
func (f *failingProvider) CostPerToken() float64 { return 0.001 }
func (f *failingProvider) Complete(_ context.Context, _ string) (*agent.CompletionResult, error) {
	return nil, context.DeadlineExceeded
}

func testCascade(response string) *agent.ProviderCascade {
	rp := &retry.RetryPolicy{
		MaxAttempts:       1,
		InitialDelay:      time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxDelay:          time.Millisecond,
	}
	return agent.NewCascade([]agent.Provider{&mockProvider{response: response}}, rp)
}

func testFailingCascade() *agent.ProviderCascade {
	rp := &retry.RetryPolicy{
		MaxAttempts:       1,
		InitialDelay:      time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxDelay:          time.Millisecond,
	}
	return agent.NewCascade([]agent.Provider{&failingProvider{}}, rp)
}

// setupSyncFixture creates a v2-style Plexium scaffold where the tracked
// page already has ValidatedHash set (validated against the source hash).
// Use setupSyncFixtureV1 to emulate the pre-fix manifest shape.
func setupSyncFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create source file
	srcDir := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0644))

	// Create wiki dir
	wikiDir := filepath.Join(root, ".wiki", "modules")
	require.NoError(t, os.MkdirAll(wikiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".wiki", "Home.md"), []byte("# Home\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "auth-module.md"), []byte("# Auth Module\n"), 0644))

	// Create plexium dir and config
	plexDir := filepath.Join(root, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "config.yml"), []byte(`
version: 1
repo:
  name: test-repo
  language: go
sources:
  include: ["**/*.go"]
  exclude: ["vendor/**"]
wiki:
  root: .wiki
`), 0644))

	// Create manifest with hashes that match the original content.
	// ValidatedHash is set so the page is considered validated at baseline.
	hash, err := manifest.ComputeHash(filepath.Join(srcDir, "auth.go"))
	require.NoError(t, err)
	now := time.Now().UTC().Format(time.RFC3339)

	mgr, err := manifest.NewManager(filepath.Join(plexDir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&manifest.Manifest{
		Version: 2,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
				SourceFiles: []manifest.SourceFile{
					{
						Path:            "src/auth.go",
						Hash:            hash,
						ValidatedHash:   hash,
						LastValidatedAt: now,
					},
				},
				LastUpdated: now,
			},
		},
		UnmanagedPages: []manifest.UnmanagedEntry{},
	}))

	return root
}

// setupSyncFixtureV1 is the legacy shape — manifest version 1 with only
// the Hash field set and no ValidatedHash. Pages here must be detected as
// stale until validated (v1→v2 migration).
func setupSyncFixtureV1(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	srcDir := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0644))

	wikiDir := filepath.Join(root, ".wiki", "modules")
	require.NoError(t, os.MkdirAll(wikiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".wiki", "Home.md"), []byte("# Home\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "auth-module.md"), []byte("# Auth Module\n"), 0644))

	plexDir := filepath.Join(root, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "config.yml"), []byte(`
version: 1
repo:
  name: test-repo
  language: go
sources:
  include: ["**/*.go"]
  exclude: ["vendor/**"]
wiki:
  root: .wiki
`), 0644))

	hash, err := manifest.ComputeHash(filepath.Join(srcDir, "auth.go"))
	require.NoError(t, err)

	mgr, err := manifest.NewManager(filepath.Join(plexDir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
				SourceFiles: []manifest.SourceFile{
					{Path: "src/auth.go", Hash: hash},
				},
				LastUpdated: time.Now().UTC().Format(time.RFC3339),
			},
		},
		UnmanagedPages: []manifest.UnmanagedEntry{},
	}))

	return root
}

func TestSync_NoChanges(t *testing.T) {
	root := setupSyncFixture(t)

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.SourceFilesChecked)
	assert.Equal(t, 0, result.StalePages, "no source changes → no stale pages")
	assert.Equal(t, 0, result.HashesUpdated)
	assert.False(t, result.NavRecompiled, "no stale pages → no recompile needed")
}

// TestSync_V1ManifestPageIsStale covers the v1→v2 migration: a manifest
// created by older plexium versions has no ValidatedHash, so its pages
// are treated as never-validated and must be flagged stale.
func TestSync_V1ManifestPageIsStale(t *testing.T) {
	root := setupSyncFixtureV1(t)

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StalePages, "v1 manifest with no ValidatedHash must be flagged stale until re-validated")
	assert.NotEmpty(t, result.NoProviderReason, "v1 stale page should produce an explanatory note")
}

// TestSync_PlainSyncDoesNotAdvanceValidated reproduces the F1 audit
// scenario. After a plain plexium sync with no provider and no
// --mark-reviewed, the validated state must NOT advance, so the next
// sync continues to flag the page as stale.
func TestSync_PlainSyncDoesNotAdvanceValidated(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	// First sync: detect stale, but do not advance ValidatedHash.
	r1, err := Run(Options{RepoRoot: root, Config: cfg, DryRun: false})
	require.NoError(t, err)
	assert.Equal(t, 1, r1.StalePages, "first sync after edit should flag stale")
	assert.Equal(t, 0, r1.ValidatedUpdated, "plain sync must not advance validated freshness")
	assert.Equal(t, 1, r1.HashesUpdated, "observed hash still gets recorded")

	// Second sync: stale flag must persist because validated hash did not advance.
	r2, err := Run(Options{RepoRoot: root, Config: cfg, DryRun: false})
	require.NoError(t, err)
	assert.Equal(t, 1, r2.StalePages, "second sync must continue to flag stale (the F1 'erased evidence' bug)")

	// Wiki text must remain byte-identical to the pre-sync baseline.
	wikiData, err := os.ReadFile(filepath.Join(root, ".wiki", "modules", "auth-module.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Auth Module\n", string(wikiData), "plain sync must not modify wiki content")
}

// TestSync_MarkReviewedAdvancesValidated ensures --mark-reviewed is the
// explicit debt mechanism: it advances ValidatedHash without invoking any
// LLM provider and without touching wiki content.
func TestSync_MarkReviewedAdvancesValidated(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	r1, err := Run(Options{
		RepoRoot:     root,
		Config:       cfg,
		DryRun:       false,
		MarkReviewed: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, r1.StalePages, "--mark-reviewed should clear stale")
	assert.Equal(t, 1, r1.PagesMarkedReviewed, "--mark-reviewed should count the marked page")
	assert.Equal(t, 1, r1.ValidatedUpdated, "--mark-reviewed should advance ValidatedHash")

	// Next sync finds nothing stale.
	r2, err := Run(Options{RepoRoot: root, Config: cfg, DryRun: false})
	require.NoError(t, err)
	assert.Equal(t, 0, r2.StalePages, "after --mark-reviewed the page should be fresh")

	// Wiki content was not touched.
	wikiData, err := os.ReadFile(filepath.Join(root, ".wiki", "modules", "auth-module.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Auth Module\n", string(wikiData), "--mark-reviewed must not modify wiki content")
}

// TestSync_FailedProviderDoesNotAdvanceValidated covers the audit's
// "failed/skipped/unavailable generation never advances validated"
// acceptance criterion.
func TestSync_FailedProviderDoesNotAdvanceValidated(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	// --regenerate with a cascade whose provider always fails.
	r1, err := Run(Options{
		RepoRoot:   root,
		Config:     cfg,
		DryRun:     false,
		Regenerate: true,
		Cascade:    testFailingCascade(),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, r1.StalePages, "failed regen: stale remains")
	assert.Equal(t, 0, r1.PagesRegenerated, "failed regen: nothing regenerated")
	assert.Equal(t, 0, r1.ValidatedUpdated, "failed regen: validated must NOT advance")
	assert.NotEmpty(t, r1.RegenerationErrors, "failed regen should report errors")
	assert.NotEmpty(t, r1.NoProviderReason, "failed regen should explain why stale persists")

	// Wiki content unchanged.
	wikiData, err := os.ReadFile(filepath.Join(root, ".wiki", "modules", "auth-module.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Auth Module\n", string(wikiData), "failed regen must not modify wiki content")
}

// TestSync_RegenerateSucceedsAdvancesValidated covers the successful
// recovery path: a working provider advances ValidatedHash for the
// regenerated page and the next sync finds nothing stale.
func TestSync_RegenerateSucceedsAdvancesValidated(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	cascade := testCascade(`{"content": "# Auth Module\nRegenerated content"}`)
	r1, err := Run(Options{
		RepoRoot:   root,
		Config:     cfg,
		DryRun:     false,
		Regenerate: true,
		Cascade:    cascade,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, r1.PagesRegenerated)
	assert.Equal(t, 1, r1.ValidatedUpdated, "successful regen should advance ValidatedHash")
	assert.Empty(t, r1.RegenerationErrors)
	assert.Empty(t, r1.NoProviderReason)

	// Second sync: no stale.
	r2, err := Run(Options{RepoRoot: root, Config: cfg, DryRun: false})
	require.NoError(t, err)
	assert.Equal(t, 0, r2.StalePages, "after successful regen, page is fresh")

	// Wiki content was rewritten by the regen.
	wikiData, err := os.ReadFile(filepath.Join(root, ".wiki", "modules", "auth-module.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Auth Module\nRegenerated content", string(wikiData))
}

// TestSync_DryRunDoesNotWrite asserts dry-run still previews staleness
// without writing to disk.
func TestSync_DryRunDoesNotWrite(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Changed() {}\n"),
		0644,
	))

	mgr, _ := manifest.NewManager(manifest.DefaultPath(root))
	before, _ := mgr.Load()
	oldValidated := before.Pages[0].SourceFiles[0].ValidatedHash

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StalePages)
	assert.True(t, result.DryRun)
	assert.Equal(t, 0, result.HashesUpdated, "dry-run should not update observed hashes")
	assert.Equal(t, 0, result.ValidatedUpdated, "dry-run should not advance validated hashes")
	assert.False(t, result.NavRecompiled, "dry-run should not recompile nav")

	after, _ := mgr.Load()
	assert.Equal(t, oldValidated, after.Pages[0].SourceFiles[0].ValidatedHash,
		"dry-run should not modify validated hash")
}

// TestSync_RegenerateAndMarkReviewedConflict guards against the
// nonsensical combination of attempting regeneration and skipping it at
// the same time.
func TestSync_RegenerateAndMarkReviewedConflict(t *testing.T) {
	root := setupSyncFixture(t)
	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	_, err = Run(Options{
		RepoRoot:     root,
		Config:       cfg,
		Regenerate:   true,
		MarkReviewed: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--regenerate with --mark-reviewed")
}

func TestSync_Regenerate_NoCascade_Errors(t *testing.T) {
	root := setupSyncFixture(t)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	_, err = Run(Options{
		RepoRoot:   root,
		Config:     cfg,
		DryRun:     false,
		Regenerate: true,
		Cascade:    nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM providers configured")
}

func TestSync_DryRunAndRegenerate_Errors(t *testing.T) {
	root := setupSyncFixture(t)

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	_, err = Run(Options{
		RepoRoot:   root,
		Config:     cfg,
		DryRun:     true,
		Regenerate: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot regenerate in dry-run mode")
}

func TestSyncResult_ExitCode_StalePages(t *testing.T) {
	result := &SyncResult{StalePages: 3}
	assert.Equal(t, 1, result.ExitCode(), "stale pages > 0 should return exit code 1")
}

func TestSyncResult_ExitCode_Clean(t *testing.T) {
	result := &SyncResult{StalePages: 0}
	assert.Equal(t, 0, result.ExitCode(), "no stale pages should return exit code 0")
}

// TestSync_PartialUpdateSuccess covers the "partial update" case: a page
// has two source files. One regen succeeds, the other fails. Only the
// regenerated source's validated state advances.
func TestSync_PartialUpdateSuccess(t *testing.T) {
	root := t.TempDir()

	// Two source files
	srcDir := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.go"), []byte("package a\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.go"), []byte("package b\n"), 0644))

	// Wiki dir
	wikiDir := filepath.Join(root, ".wiki", "modules")
	require.NoError(t, os.MkdirAll(wikiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".wiki", "Home.md"), []byte("# Home\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "combo.md"), []byte("# Combo\n"), 0644))

	// Plexium config
	plexDir := filepath.Join(root, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "config.yml"), []byte(`
version: 1
repo:
  name: test-repo
  language: go
sources:
  include: ["**/*.go"]
  exclude: ["vendor/**"]
wiki:
  root: .wiki
`), 0644))

	hashA, _ := manifest.ComputeHash(filepath.Join(srcDir, "a.go"))
	hashB, _ := manifest.ComputeHash(filepath.Join(srcDir, "b.go"))
	now := time.Now().UTC().Format(time.RFC3339)

	mgr, _ := manifest.NewManager(filepath.Join(plexDir, "manifest.json"))
	require.NoError(t, mgr.Save(&manifest.Manifest{
		Version: 2,
		Pages: []manifest.PageEntry{{
			WikiPath:  "modules/combo.md",
			Ownership: "managed",
			Section:   "Modules",
			SourceFiles: []manifest.SourceFile{
				{Path: "src/a.go", Hash: hashA, ValidatedHash: hashA, LastValidatedAt: now},
				{Path: "src/b.go", Hash: hashB, ValidatedHash: hashB, LastValidatedAt: now},
			},
			LastUpdated: now,
		}},
	}))

	// Edit BOTH source files so the page is stale twice.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.go"), []byte("package b\n\nfunc B() {}\n"), 0644))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	// Regen with the working cascade. The page regenerates as a unit;
	// both source files advance once regen succeeds.
	cascade := testCascade(`{"content": "# Combo\nRegenerated for both"}`)
	r, err := Run(Options{
		RepoRoot: root, Config: cfg, DryRun: false,
		Regenerate: true, Cascade: cascade,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, r.PagesRegenerated)
	assert.Equal(t, 2, r.ValidatedUpdated, "both source files should have their validated hash advanced")
	assert.Equal(t, 0, r.StalePages, "successful regen clears stale for the page")

	// Now mark the page stale by editing only B; A stays validated.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.go"), []byte("package b\n\nfunc B2() {}\n"), 0644))

	// A failing cascade regen leaves validated state untouched for B.
	r2, err := Run(Options{
		RepoRoot: root, Config: cfg, DryRun: false,
		Regenerate: true, Cascade: testFailingCascade(),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, r2.StalePages, "B edit should still flag the page stale after failed regen")
	assert.Equal(t, 0, r2.ValidatedUpdated, "failed regen must not advance validated for B")

	// Validate A's validated hash did not regress and B's is unchanged from before.
	m, err := mgr.Load()
	require.NoError(t, err)
	page := m.Pages[0]

	// Source ordering: SourceFiles[i] for i==0 is src/a.go and i==1 is src/b.go.
	// A's observed hash should reflect the most recent write (post-edit-A),
	// not the original baseline hashA.
	assert.NotEqual(t, hashA, page.SourceFiles[0].Hash,
		"A's observed hash should advance after the post-regen edit")
	// Validated for A should remain set to the post-regen hash from step 3.
	assert.NotEqual(t, "", page.SourceFiles[0].ValidatedHash,
		"A's validated hash should remain set after a failed regen")
	// B's observed hash advances to the latest edit; validated stays put.
	assert.NotEqual(t, "", page.SourceFiles[1].ValidatedHash,
		"B's validated hash should still be set from the successful first regen")
}
