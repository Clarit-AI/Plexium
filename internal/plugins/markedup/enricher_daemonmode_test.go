package markedup

import (
	"context"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// TestEnricherPlugin_DaemonMode_TouchesLastEnrichedOnNoOp pins the fix
// for the infinite-requeue loop: when SetDaemonMode(true) is set, a
// second Process call on a page whose graph content is unchanged must
// still advance LastEnriched. Without the daemon-mode flag, the second
// call leaves LastEnriched untouched (Phase C idempotency).
func TestEnricherPlugin_DaemonMode_TouchesLastEnrichedOnNoOp(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to #authentication and [[b]].",
	})

	p := NewEnricher(Config{
		Enabled:    true,
		AutoEnrich: true,
	})
	p.SetDaemonMode(true)

	data := &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	}

	if err := p.Process(context.Background(), data); err != nil {
		t.Fatalf("first process: %v", err)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	firstStamp := m.Pages[0].LastEnriched
	if firstStamp == "" {
		t.Fatalf("expected LastEnriched populated after first call, got empty")
	}

	// Sleep a hair so the second timestamp differs at RFC3339 second
	// resolution. The fix must produce a strictly newer stamp.
	time.Sleep(1100 * time.Millisecond)

	if err := p.Process(context.Background(), data); err != nil {
		t.Fatalf("second process: %v", err)
	}
	m2, _ := mgr.Load()
	secondStamp := m2.Pages[0].LastEnriched
	if secondStamp == firstStamp {
		t.Errorf("daemon mode: expected LastEnriched to advance on no-op enrichment; first=%q second=%q",
			firstStamp, secondStamp)
	}

	// Sanity: the semantic graph fields should match — the only
	// difference between the two stored entries should be the timestamp.
	if m.Pages[0].EntityType != m2.Pages[0].EntityType {
		t.Errorf("EntityType drifted between calls: %q -> %q", m.Pages[0].EntityType, m2.Pages[0].EntityType)
	}
	if len(m.Pages[0].Relationships) != len(m2.Pages[0].Relationships) {
		t.Errorf("Relationships count drifted: %d -> %d",
			len(m.Pages[0].Relationships), len(m2.Pages[0].Relationships))
	}
}

// TestEnricherPlugin_NoDaemonMode_LeavesLastEnrichedFixedOnNoOp pins the
// inverse: with daemon mode OFF (the default for convert-time callers),
// a second Process on identical content must NOT bump LastEnriched —
// that's the Phase C idempotency optimization the fix preserves.
func TestEnricherPlugin_NoDaemonMode_LeavesLastEnrichedFixedOnNoOp(t *testing.T) {
	repoRoot, wikiRoot := setupWikiFixture(t, map[string]string{
		"a.md": "# A\n\nRelates to #authentication and [[b]].",
	})

	p := NewEnricher(Config{
		Enabled:    true,
		AutoEnrich: true,
	})
	// Note: no SetDaemonMode(true) — convert-time default.

	data := &plugins.PipelineData{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		Pages:    []plugins.PipelinePage{{WikiPath: "a.md", Title: "A"}},
	}

	if err := p.Process(context.Background(), data); err != nil {
		t.Fatalf("first process: %v", err)
	}

	mgr, _ := manifest.NewManager(manifest.DefaultPath(repoRoot))
	m, _ := mgr.Load()
	firstStamp := m.Pages[0].LastEnriched
	if firstStamp == "" {
		t.Fatalf("expected LastEnriched populated after first call, got empty")
	}

	time.Sleep(1100 * time.Millisecond)

	if err := p.Process(context.Background(), data); err != nil {
		t.Fatalf("second process: %v", err)
	}
	m2, _ := mgr.Load()
	secondStamp := m2.Pages[0].LastEnriched
	if secondStamp != firstStamp {
		t.Errorf("non-daemon mode: expected LastEnriched unchanged on no-op (idempotency); first=%q second=%q",
			firstStamp, secondStamp)
	}
}
