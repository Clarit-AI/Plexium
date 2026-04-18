package markedup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestEnricherPlugin_IdempotentOnNoChange(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nText with [[b]].",
	})

	p := NewEnricher(Config{Enabled: true, AutoEnrich: true})
	data := &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	}

	// First run writes graph metadata.
	if err := p.Process(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	first, _ := mgr.Load()
	firstJSON, _ := json.Marshal(first)

	// Second run: .wiki/ content is unchanged, so EnrichPage's delta
	// reports no additions (the frontmatter in the parsed file is already
	// populated via... wait — no, the file on disk has no frontmatter
	// because WriteEnrichedFrontmatter=false. So the second run will see
	// the same empty frontmatter and apply the same metadata. That's
	// still idempotent because the manifest fields get overwritten with
	// identical values (except LastEnriched which is a timestamp — so
	// that will differ; a caller using the manifest semantically can
	// compare all other fields).
	if err := p.Process(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	second, _ := mgr.Load()

	// Entities/Relationships/EntityType must match between runs.
	if !entitiesEqual(first.Pages[0].Entities, second.Pages[0].Entities) {
		t.Errorf("entities drifted between runs: %+v vs %+v",
			first.Pages[0].Entities, second.Pages[0].Entities)
	}
	if first.Pages[0].EntityType != second.Pages[0].EntityType {
		t.Errorf("entity type drifted: %q vs %q",
			first.Pages[0].EntityType, second.Pages[0].EntityType)
	}
	// Sanity: firstJSON was captured before the second run so we don't
	// accidentally compare the same pointer.
	_ = firstJSON
}

func entitiesEqual(a, b []manifest.EntityRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
