package markedup

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Clarit-AI/markedup/enrich"
	"github.com/Clarit-AI/markedup/schema"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// fakeLLM is a deterministic LLMProvider for testing Tier 2 wiring. The
// callback decides what to return on each invocation; calls is incremented
// atomically so tests can assert how many pages got Tier 2 treatment.
type fakeLLM struct {
	calls int32
	fn    func(ctx context.Context, body string) (*enrich.ModelResult, string, error)
}

func (f *fakeLLM) ExtractGraph(ctx context.Context, body string) (*enrich.ModelResult, string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.fn(ctx, body)
}

// TestEnricherPlugin_ModelEnrichMergesLLMResult verifies the happy path:
// when ModelEnrich=true and an LLMProvider is attached, the model's
// extracted entities/relationships flow into the manifest on top of
// what Tier 1 produced.
func TestEnricherPlugin_ModelEnrichMergesLLMResult(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to [[b]].\nThis page is about authentication.\n",
	})

	llm := &fakeLLM{
		fn: func(ctx context.Context, body string) (*enrich.ModelResult, string, error) {
			return &enrich.ModelResult{
				EntityType: "concept",
				Entities: []schema.Entity{
					{Name: "OAuth2", Role: "protocol"},
					{Name: "JWT", Role: "token-format"},
				},
				Relationships: []schema.Relationship{
					{Target: "auth-server", Type: "depends-on", Strength: 0.9},
				},
				SemanticHints: []string{"auth", "identity"},
			}, "A short summary of the auth page.", nil
		},
	}

	p := NewEnricherWithLLM(Config{
		Enabled:     true,
		AutoEnrich:  true,
		ModelEnrich: true,
	}, llm)

	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := atomic.LoadInt32(&llm.calls); got != 1 {
		t.Errorf("expected LLM called once, got %d", got)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	entry := m.Pages[0]

	// LLM-extracted entities should appear in the manifest.
	foundOAuth, foundJWT := false, false
	for _, e := range entry.Entities {
		switch e.Name {
		case "OAuth2":
			foundOAuth = true
		case "JWT":
			foundJWT = true
		}
	}
	if !foundOAuth || !foundJWT {
		t.Errorf("expected OAuth2 and JWT entities from LLM; got %+v", entry.Entities)
	}
	// Tier 1 wikilink should be in entry.Relationships.
	foundB := false
	for _, r := range entry.Relationships {
		if r.Target == "b" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("expected Tier 1 wikilink to survive in Relationships; got %+v", entry.Relationships)
	}
	// Tier 2 NER edge should be in SemanticRelationships.
	foundAuthServer := false
	for _, r := range entry.SemanticRelationships {
		if r.Target == "auth-server" && r.Type == "depends-on" {
			foundAuthServer = true
		}
	}
	if !foundAuthServer {
		t.Errorf("expected Tier 2 NER edge to appear in SemanticRelationships; got %+v", entry.SemanticRelationships)
	}
	if entry.EnrichedBy != EnricherVersion {
		t.Errorf("expected EnrichedBy=%q, got %q", EnricherVersion, entry.EnrichedBy)
	}
}

// TestEnricherPlugin_ModelEnrichWithoutProviderWarnsAndFallsBack verifies
// that ModelEnrich=true with a nil LLMProvider does NOT error — it logs
// a warning and proceeds with Tier 1 only.
func TestEnricherPlugin_ModelEnrichWithoutProviderWarnsAndFallsBack(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to [[b]].\n",
	})

	// Capture log output to assert the warning surfaces.
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	p := NewEnricher(Config{
		Enabled:     true,
		AutoEnrich:  true,
		ModelEnrich: true,
	})
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}

	if !strings.Contains(buf.String(), "no LLM provider attached") {
		t.Errorf("expected warning about missing provider in log; got: %s", buf.String())
	}

	// Tier 1 should still have populated the manifest with the wikilink.
	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 2 {
		t.Errorf("expected Tier 1 to advance manifest to v2, got %d", m.Version)
	}
	foundB := false
	for _, r := range m.Pages[0].Relationships {
		if r.Target == "b" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("expected Tier 1 wikilink relationship to survive fallback")
	}
}

// TestEnricherPlugin_ModelEnrichLLMErrorIsNonFatal verifies that a
// transient LLM failure does not abort the pipeline — Tier 1 results
// are still applied to the manifest.
func TestEnricherPlugin_ModelEnrichLLMErrorIsNonFatal(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to [[b]].\n",
	})

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	llm := &fakeLLM{
		fn: func(ctx context.Context, body string) (*enrich.ModelResult, string, error) {
			return nil, "", errors.New("model timeout")
		},
	}

	p := NewEnricherWithLLM(Config{
		Enabled:     true,
		AutoEnrich:  true,
		ModelEnrich: true,
	}, llm)
	err := p.Process(context.Background(), &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("Tier 2 failure should be non-fatal, got: %v", err)
	}

	if !strings.Contains(buf.String(), "Tier 2 extraction failed") {
		t.Errorf("expected Tier 2 failure log; got: %s", buf.String())
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	if m.Version != 2 {
		t.Errorf("expected Tier 1 to advance manifest to v2 despite Tier 2 error, got %d", m.Version)
	}
}

// TestEnricherPlugin_ModelEnrichNilResultIsNoOp verifies that when
// AutoEnrich=false and Tier 2 returns nil model (e.g. model declined to
// extract), the enricher does NOT bump the manifest. This guards against
// re-stamping on every pipeline pass.
func TestEnricherPlugin_ModelEnrichNilResultIsNoOp(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n[[b]]",
	})

	llm := &fakeLLM{
		fn: func(ctx context.Context, body string) (*enrich.ModelResult, string, error) {
			return nil, "", nil // model declined
		},
	}

	p := NewEnricherWithLLM(Config{
		Enabled:     true,
		AutoEnrich:  false,
		ModelEnrich: true,
	}, llm)
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
		t.Errorf("nil model result with no Tier 1 should leave manifest at v1, got %d", m.Version)
	}
}

// TestEnricherPlugin_ModelEnrichMissingLLMWarnsOnlyOnce is the regression
// test for the pass-3 finding: the missing-LLM warning lived inside the
// per-page loop, so a daemon refresh on a 1000-page repo would emit 1000
// identical warnings every TTL cycle. The fix moved the warning behind a
// sync.Once guard on EnricherPlugin. We exercise three pages across two
// Process calls (six potential log sites) and assert exactly one warning
// is observed.
func TestEnricherPlugin_ModelEnrichMissingLLMWarnsOnlyOnce(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\n[[b]]\n",
		"b.md": "# B\n\n[[c]]\n",
		"c.md": "# C\n\n[[a]]\n",
	})

	var buf bytes.Buffer
	// Inject a logger so this test does not race with other tests'
	// log.SetOutput swaps and so the assertion is hermetic.
	p := NewEnricher(Config{
		Enabled:     true,
		AutoEnrich:  true,
		ModelEnrich: true,
	})
	p.warnLogger = log.New(&buf, "", 0)

	pages := []plugins.PipelinePage{
		{WikiPath: "a.md", Title: "A"},
		{WikiPath: "b.md", Title: "B"},
		{WikiPath: "c.md", Title: "C"},
	}

	for i := 0; i < 2; i++ {
		if err := p.Process(context.Background(), &plugins.PipelineData{
			RepoRoot: repoRoot,
			WikiRoot: wikiRoot,
			Pages:    pages,
		}); err != nil {
			t.Fatalf("process pass %d: %v", i, err)
		}
	}

	count := strings.Count(buf.String(), "no LLM provider attached")
	if count != 1 {
		t.Errorf("expected missing-LLM warning to fire exactly once across 2 Process calls over 3 pages; got %d.\nlog:\n%s", count, buf.String())
	}
}
