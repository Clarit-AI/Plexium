package markedup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// setupWikiFixture creates a temp repo with a .wiki/ directory, seeds the
// given markdown pages, and seeds a manifest with PageEntry stubs so
// ApplyGraphMetadata has somewhere to land. Returns repoRoot, wikiRoot.
func setupWikiFixture(t *testing.T, pages map[string]string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	wikiRoot := filepath.Join(repoRoot, ".wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed an empty manifest with page entries for every fixture page.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m := manifest.NewEmptyManifest()
	for wikiPath, body := range pages {
		fullPath := filepath.Join(wikiRoot, wikiPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m.Pages = append(m.Pages, manifest.PageEntry{
			WikiPath:  wikiPath,
			Title:     strings.TrimSuffix(filepath.Base(wikiPath), ".md"),
			Ownership: "managed",
		})
	}
	if err := mgr.Save(m); err != nil {
		t.Fatal(err)
	}

	return repoRoot, wikiRoot
}

func TestEnricherPlugin_DisabledIsNoOp(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n#tag1 [[b]]",
	})

	p := NewEnricher(Config{Enabled: false, AutoEnrich: true})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "a.md", Title: "A"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	// Manifest should have Version=1 still (no graph metadata written).
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 1 {
		t.Errorf("expected Version=1 when plugin disabled, got %d", m.Version)
	}
	if m.Pages[0].EntityType != "" {
		t.Errorf("expected no graph metadata when disabled, got EntityType=%q", m.Pages[0].EntityType)
	}
}

func TestEnricherPlugin_AutoEnrichPopulatesManifest(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to #authentication and [[b]].",
	})

	p := NewEnricher(Config{
		Enabled:                  true,
		AutoEnrich:               true,
		WriteEnrichedFrontmatter: false, // manifest-only (the default mode)
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "a.md", Title: "A"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// Manifest should be v2 with graph metadata on the page.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 2 {
		t.Errorf("expected Version=2 after enrichment, got %d", m.Version)
	}
	entry := m.Pages[0]
	if entry.EntityType == "" {
		t.Error("expected EntityType populated")
	}
	if entry.Confidence <= 0 {
		t.Errorf("expected Confidence>0, got %v", entry.Confidence)
	}
	if entry.EnrichedBy != EnricherVersion {
		t.Errorf("expected EnrichedBy=%q, got %q", EnricherVersion, entry.EnrichedBy)
	}
	// Wikilink [[b]] should show up as a relationship.
	foundRel := false
	for _, r := range entry.Relationships {
		if r.Target == "b" {
			foundRel = true
			break
		}
	}
	if !foundRel {
		t.Errorf("expected relationship to [b], got %+v", entry.Relationships)
	}

	// Default manifest-only mode should NOT mutate .wiki/ files.
	raw, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))
	if strings.HasPrefix(string(raw), "---") {
		t.Errorf("default mode should not write frontmatter; got:\n%s", raw)
	}
}

func TestEnricherPlugin_WriteFrontmatterMutatesFile(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to [[b]].",
	})

	p := NewEnricher(Config{
		Enabled:                  true,
		AutoEnrich:               true,
		WriteEnrichedFrontmatter: true,
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))
	if !strings.HasPrefix(string(raw), "---") {
		t.Fatalf("expected YAML frontmatter delimiter at top of file; got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "entity-type") {
		t.Errorf("expected entity-type in frontmatter; got:\n%s", raw)
	}
}

func TestEnricherPlugin_SkipsMissingWikiFiles(t *testing.T) {
	// PipelineData references a page that doesn't exist on disk (e.g.
	// dry-run mode). Should not fail — just skip.
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{})

	p := NewEnricher(Config{Enabled: true, AutoEnrich: true})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "nonexistent.md", Title: "None"}},
	})
	if err != nil {
		t.Fatalf("expected missing file to be skipped, got %v", err)
	}
}

// Regression for CodeRabbit review pass 2: with WriteEnrichedFrontmatter=true,
// frontmatter writes must NOT be flushed to disk until after the manifest
// save succeeds. Otherwise a mid-pipeline failure could leave the .wiki/
// files carrying enriched frontmatter with no manifest record, drifting
// the two sources of truth apart. We verify the ordering by checking
// that both sides land together (manifest v2 + file rewritten) on the
// success path — and, separately, that when the enricher runs with
// WriteEnrichedFrontmatter=false, files stay untouched even though the
// manifest advances.
func TestEnricherPlugin_WriteFrontmatterDeferredUntilManifestSave(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n[[b]]\n",
		"b.md": "# B\n\nContent.\n",
	})

	beforeA, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))
	beforeB, _ := os.ReadFile(filepath.Join(wikiRoot, "b.md"))

	p := NewEnricher(Config{
		Enabled:                  true,
		AutoEnrich:               true,
		WriteEnrichedFrontmatter: true,
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages: []plugins.PipelinePage{
			{WikiPath: "a.md", Title: "A"},
			{WikiPath: "b.md", Title: "B"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// Both files should now carry frontmatter (the deferred writes
	// fired because the manifest save succeeded).
	afterA, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))
	afterB, _ := os.ReadFile(filepath.Join(wikiRoot, "b.md"))
	if string(afterA) == string(beforeA) {
		t.Error("a.md should have been rewritten with frontmatter")
	}
	if string(afterB) == string(beforeB) {
		t.Error("b.md should have been rewritten with frontmatter")
	}
	if !strings.HasPrefix(string(afterA), "---") {
		t.Errorf("a.md missing frontmatter delimiter: %s", afterA)
	}

	// Manifest must have advanced too.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 2 {
		t.Errorf("expected Version=2 after successful enrichment, got %d", m.Version)
	}
}

// Regression: if the pipeline delivers a page that isn't tracked in the
// manifest, the enricher must not save a spurious v1→v2 bump or invent
// a manifest entry. Addresses CodeRabbit review finding on enricher.go:131.
func TestEnricherPlugin_UntrackedPageDoesNotBumpManifest(t *testing.T) {
	// Set up a wiki with a real page, but seed the manifest so that
	// the page is NOT listed — simulating pipeline/manifest drift.
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{})

	// Now create a .wiki file without a corresponding manifest entry.
	body := "# Untracked\n\n[[other]]"
	if err := os.WriteFile(filepath.Join(wikiRoot, "untracked.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewEnricher(Config{Enabled: true, AutoEnrich: true})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "untracked.md", Title: "Untracked"}},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	// Manifest should remain at v1 with no pages — the enricher must
	// not have triggered a save.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 1 {
		t.Errorf("expected Version=1 when no tracked pages matched, got %d", m.Version)
	}
}

// Regression for CodeRabbit pass 3: a second convert pass over an
// unchanged wiki must NOT re-stamp LastEnriched or trigger a manifest
// save. The first pass writes graph metadata; the second pass should
// find the manifest already reflects Tier 1's output and skip silently.
//
// Without this behavior, the manifest is rewritten on every pipeline
// pass even when nothing has changed, producing noisy git diffs and
// LastEnriched churn.
func TestEnricherPlugin_IdempotentAcrossRunsAtManifestLevel(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n#topic [[b]]",
	})

	p := NewEnricher(Config{Enabled: true, AutoEnrich: true})
	data := &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	}

	// First pass populates the manifest.
	if err := p.Process(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	manifestPath := manifest.DefaultPath(repoRoot)
	firstBytes, _ := os.ReadFile(manifestPath)

	// Pin the manifest mtime to a known old timestamp. If the second
	// Process call rewrites the file, os.Stat will report a fresher
	// mtime. Using os.Chtimes is deterministic — no race with wall
	// clock resolution or filesystem mtime granularity.
	pinned := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(manifestPath, pinned, pinned); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Second pass on unchanged inputs.
	if err := p.Process(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	secondStat, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, _ := os.ReadFile(manifestPath)

	if !bytes.Equal(firstBytes, secondBytes) {
		t.Errorf("manifest content drifted on idempotent re-run:\nfirst:\n%s\n\nsecond:\n%s",
			firstBytes, secondBytes)
	}
	if !secondStat.ModTime().Equal(pinned) {
		t.Errorf("manifest was rewritten despite unchanged input; expected mtime %v, got %v",
			pinned, secondStat.ModTime())
	}
}

// Regression for CodeRabbit pass 4: the deferred-write test must prove
// ordering, not just success. Force a manifest save failure (by making
// the manifest file read-only) and assert the .wiki/ files are still
// untouched — if writes happened before Save, this test would fail.
func TestEnricherPlugin_WriteFrontmatterDeferredUntilManifestSave_FailSave(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n[[b]]\n",
	})

	// Snapshot the page bytes before Process runs.
	beforeA, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))

	// Make the manifest file read-only so mgr.Save() fails with
	// "permission denied". mgr.Load() still succeeds (read access).
	manifestPath := manifest.DefaultPath(repoRoot)
	if err := os.Chmod(manifestPath, 0o400); err != nil {
		t.Fatalf("chmod manifest: %v", err)
	}
	// Restore perms after the test so t.TempDir() cleanup works cleanly.
	t.Cleanup(func() { _ = os.Chmod(manifestPath, 0o600) })

	p := NewEnricher(Config{
		Enabled:                  true,
		AutoEnrich:               true,
		WriteEnrichedFrontmatter: true,
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err == nil {
		t.Fatal("expected Process to fail when manifest save is blocked")
	}

	// Critical assertion: the wiki file must be IDENTICAL to its
	// pre-Process state. If ReplaceFrontmatter had been flushed
	// before the failed Save, a.md would carry new YAML frontmatter.
	afterA, _ := os.ReadFile(filepath.Join(wikiRoot, "a.md"))
	if !bytes.Equal(beforeA, afterA) {
		t.Errorf("wiki file was rewritten despite manifest save failure:\nbefore:\n%s\nafter:\n%s",
			beforeA, afterA)
	}
}

// Regression: with AutoEnrich=false and ModelEnrich=true, the enricher
// has nothing to apply (Tier 2 is not yet wired). It must not write to
// the manifest on every pipeline pass. Addresses CodeRabbit review
// finding on enricher.go:113.
func TestEnricherPlugin_ModelEnrichOnlyIsNoOp(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n[[b]]",
	})

	p := NewEnricher(Config{
		Enabled:     true,
		AutoEnrich:  false,
		ModelEnrich: true,
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 1 {
		t.Errorf("ModelEnrich-only run should not bump manifest; got Version=%d", m.Version)
	}
	if m.Pages[0].EntityType != "" {
		t.Error("ModelEnrich-only run should not populate graph metadata")
	}
}

// NOTE: An earlier version of this file had a second idempotency test
// (TestEnricherPlugin_IdempotentOnNoChange) that only compared
// Entities and EntityType and would miss Relationships/Confidence/
// EnrichedBy/LastEnriched drift. It's superseded by
// TestEnricherPlugin_IdempotentAcrossRunsAtManifestLevel above, which
// asserts byte-for-byte manifest stability and mtime stability.
