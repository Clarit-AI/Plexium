package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// markedupEnrichTestSetup writes a manifest with the supplied page
// LastEnriched stamps and returns the daemon configured with a markedup
// plugin block. The caller chooses whether the plugin is enabled.
func markedupEnrichTestSetup(t *testing.T, enabled bool, refreshInterval string, pages []manifest.PageEntry) *Daemon {
	t.Helper()

	pluginBlock := map[string]any{
		"enabled":    enabled,
		"autoEnrich": true,
	}
	if refreshInterval != "" {
		pluginBlock["daemon"] = map[string]any{
			"refreshInterval": refreshInterval,
		}
	}

	cfg := &config.Config{
		Wiki: config.Wiki{Root: ".wiki"},
		Plugins: map[string]map[string]any{
			"markedup": pluginBlock,
		},
	}

	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Config: cfg,
	})

	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".plexium"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".wiki"), 0o755))

	mgr, err := manifest.NewManager(manifest.DefaultPath(repoRoot))
	require.NoError(t, err)
	m := manifest.NewEmptyManifest()
	m.Pages = pages
	require.NoError(t, mgr.Save(m))
	return d
}

func TestDetectMarkedupEnrichJob_QueuesWhenLastEnrichedMissing(t *testing.T) {
	d := markedupEnrichTestSetup(t, true, "24h", []manifest.PageEntry{
		{WikiPath: "Home.md", Title: "Home", Ownership: "managed"},
		{WikiPath: "guides/api.md", Title: "API", Ownership: "managed"},
	})

	job, action := d.detectMarkedupEnrichJob()
	require.NotNil(t, job, "expected a job when LastEnriched is empty for all pages")
	assert.Equal(t, jobTypeMarkedupEnrich, job.Type)
	assert.True(t, action.Success)
	assert.Equal(t, "queue", action.Action)

	payload := decodeMarkedupEnrichPayload(job.Payload)
	assert.ElementsMatch(t, []string{"Home.md", "guides/api.md"}, payload.StalePages)
}

func TestDetectMarkedupEnrichJob_NoJobWhenWithinTTL(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	d := markedupEnrichTestSetup(t, true, "24h", []manifest.PageEntry{
		{WikiPath: "Home.md", LastEnriched: now},
	})

	job, action := d.detectMarkedupEnrichJob()
	assert.Nil(t, job, "no job expected when LastEnriched is fresh")
	assert.Equal(t, TickAction{}, action)
}

func TestDetectMarkedupEnrichJob_QueuesWhenOlderThanTTL(t *testing.T) {
	stale := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	fresh := time.Now().UTC().Format(time.RFC3339)
	d := markedupEnrichTestSetup(t, true, "24h", []manifest.PageEntry{
		{WikiPath: "stale.md", LastEnriched: stale},
		{WikiPath: "fresh.md", LastEnriched: fresh},
	})

	job, action := d.detectMarkedupEnrichJob()
	require.NotNil(t, job)
	assert.True(t, action.Success)

	payload := decodeMarkedupEnrichPayload(job.Payload)
	assert.Equal(t, []string{"stale.md"}, payload.StalePages, "only the past-TTL page should be queued")
}

func TestDetectMarkedupEnrichJob_PluginDisabledNeverQueues(t *testing.T) {
	// Even if every page is wildly stale, a disabled plugin must never
	// produce an enrichment job — the user has not opted in.
	stale := time.Now().Add(-1000 * time.Hour).UTC().Format(time.RFC3339)
	d := markedupEnrichTestSetup(t, false, "24h", []manifest.PageEntry{
		{WikiPath: "stale.md", LastEnriched: stale},
	})

	job, action := d.detectMarkedupEnrichJob()
	assert.Nil(t, job)
	assert.Equal(t, TickAction{}, action, "disabled plugin must produce no tick action")
}

func TestDetectMarkedupEnrichJob_DefaultTTLIs24h(t *testing.T) {
	// 23h ago should NOT be stale at the default 24h TTL.
	twentyThreeHoursAgo := time.Now().Add(-23 * time.Hour).UTC().Format(time.RFC3339)
	d := markedupEnrichTestSetup(t, true, "", []manifest.PageEntry{
		{WikiPath: "borderline.md", LastEnriched: twentyThreeHoursAgo},
	})

	job, _ := d.detectMarkedupEnrichJob()
	assert.Nil(t, job, "23h-old entry must not be stale at the 24h default")

	// 25h ago SHOULD be stale.
	twentyFiveHoursAgo := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	d2 := markedupEnrichTestSetup(t, true, "", []manifest.PageEntry{
		{WikiPath: "borderline.md", LastEnriched: twentyFiveHoursAgo},
	})

	job2, _ := d2.detectMarkedupEnrichJob()
	require.NotNil(t, job2, "25h-old entry must be stale at the 24h default")
}

// TestTick_MarkedupEnrichNotStarvedByRunnerGatedJobs is the regression
// test for the starvation bug fixed alongside this case: tick used to
// only consider jobs[0]. With debt > threshold (a runner-gated
// "auto-fix" job that sorts ahead of markedup-enrich) AND a stale
// markedup page, an unconfigured runner (canExecuteJobs() == false)
// caused the markedup-enrich job at jobs[1..] to be skipped every tick
// — even though it runs in-process and needs no runner.
//
// The fix scans the queue for markedup-enrich and runs it unconditionally
// before falling through to the runner-gated dispatch. This test pins
// that invariant: with no runner configured and both job types queued,
// markedup-enrich must still execute (via runMarkedupEnrichJob), while
// debt is left unrun.
func TestTick_MarkedupEnrichNotStarvedByRunnerGatedJobs(t *testing.T) {
	stale := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)

	cfg := &config.Config{
		Wiki: config.Wiki{Root: ".wiki"},
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled":    true,
				"autoEnrich": true,
				"daemon":     map[string]any{"refreshInterval": "24h"},
			},
		},
	}

	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Config: cfg,
		// No RunnerName -> canExecuteJobs() == false. This is exactly
		// the configuration where the bug manifested: a user enables
		// the markedup plugin without ever configuring a coding-agent
		// runner because enrichment doesn't need one.
		Watches: WatchOpts{
			Debt: WatchDef{Enabled: true, Action: "auto-fix", Threshold: "1"},
		},
	})

	// Create wiki + manifest so detectMarkedupEnrichJob fires.
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".plexium"), 0o755))
	wikiDir := filepath.Join(repoRoot, ".wiki")
	require.NoError(t, os.MkdirAll(wikiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte("# Home\n"), 0o644))

	mgr, err := manifest.NewManager(manifest.DefaultPath(repoRoot))
	require.NoError(t, err)
	m := manifest.NewEmptyManifest()
	m.Pages = []manifest.PageEntry{
		{WikiPath: "Home.md", Title: "Home", Ownership: "managed", LastEnriched: stale},
	}
	require.NoError(t, mgr.Save(m))

	// Create wiki log with WIKI-DEBT entries so detectDebtJob fires
	// above its threshold and emits a runner-gated job.
	require.NoError(t, os.WriteFile(
		filepath.Join(wikiDir, "_log.md"),
		[]byte("WIKI-DEBT: a\nWIKI-DEBT: b\nWIKI-DEBT: c\n"),
		0o644,
	))

	// Sanity-check the precondition the bug depends on: the runner is
	// not configured, so canExecuteJobs() must return false. Without
	// this the test would pass trivially via the gated-dispatch path.
	require.False(t, d.canExecuteJobs(), "test precondition: canExecuteJobs() must be false")

	actions := d.tick(context.Background())

	var sawMarkedupExecute, sawDebtExecute bool
	for _, a := range actions {
		if a.Watch == "markedup" && a.Action == "execute" {
			sawMarkedupExecute = true
		}
		if a.Watch == "debt" && (a.Action == "execute" || a.Action == "dispatch") {
			sawDebtExecute = true
		}
	}
	assert.True(t, sawMarkedupExecute,
		"markedup-enrich must execute even when a runner-gated job is queued ahead of it; got actions=%+v", actions)
	assert.False(t, sawDebtExecute,
		"runner-gated debt job must not execute when canExecuteJobs()=false")
}

// TestRunMarkedupEnrichJob_AdvancesLastEnrichedOnNoOp is the regression
// test for the infinite-requeue loop fixed alongside this case: with the
// Phase C "manifest save skipped on semantic-equal enrichment"
// optimization, a stale page whose graph content was unchanged would be
// re-detected as stale on every subsequent tick because LastEnriched
// never advanced. The fix flips the daemon's enricher into a "touch
// LastEnriched on no-op" mode via bootstrap.Options.DaemonMode.
//
// This test simulates exactly that loop end-to-end through the daemon's
// public surface: queue a stale page, run the enrichment job, then
// re-run detection and assert no job is queued (because LastEnriched
// has advanced past the stale cutoff).
func TestRunMarkedupEnrichJob_AdvancesLastEnrichedOnNoOp(t *testing.T) {
	stale := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)

	d := markedupEnrichTestSetup(t, true, "24h", []manifest.PageEntry{
		// Pre-seed graph fields that match what Tier 1 enrichment will
		// produce so the enricher's "semantic-equal" branch fires —
		// this is the exact precondition that triggered the loop.
		// We don't know the exact enrichment output, so instead we use
		// a body that produces minimal output and run twice: first run
		// populates graph fields, second run should be semantic-equal.
		{WikiPath: "Home.md", Title: "Home", Ownership: "managed", LastEnriched: stale},
	})

	// Seed the wiki file matching the manifest entry.
	wikiDir := filepath.Join(d.config.RepoRoot, ".wiki")
	require.NoError(t, os.WriteFile(
		filepath.Join(wikiDir, "Home.md"),
		[]byte("# Home\n\nThis is the home page about #welcome.\n"),
		0o644,
	))

	// First detection should queue the stale page.
	job1, action1 := d.detectMarkedupEnrichJob()
	require.NotNil(t, job1, "expected initial stale detection to queue a job")
	require.Equal(t, "queue", action1.Action)

	// Run the enricher. This populates graph fields AND advances
	// LastEnriched (real change → manifest save).
	runAction := d.runMarkedupEnrichJob(context.Background(), job1)
	require.True(t, runAction.Success, "first run should succeed: %+v", runAction)

	// Now manually rewind LastEnriched on the manifest to simulate the
	// page having been enriched once long ago, with the SAME graph
	// content the enricher would produce now. This is the exact state
	// the loop hits: stale timestamp + identical semantic content.
	mgr, err := manifest.NewManager(manifest.DefaultPath(d.config.RepoRoot))
	require.NoError(t, err)
	m, err := mgr.Load()
	require.NoError(t, err)
	require.NotEmpty(t, m.Pages[0].LastEnriched, "first run should have populated LastEnriched")
	m.Pages[0].LastEnriched = stale
	require.NoError(t, mgr.Save(m))

	// Second detection should re-queue (the page is stale again).
	job2, action2 := d.detectMarkedupEnrichJob()
	require.NotNil(t, job2, "expected stale detection to re-queue after timestamp rewind")
	require.Equal(t, "queue", action2.Action)

	// Second run: graph content is unchanged from the first run. With
	// the daemon-mode fix, LastEnriched MUST still advance.
	runAction2 := d.runMarkedupEnrichJob(context.Background(), job2)
	require.True(t, runAction2.Success, "second run should succeed: %+v", runAction2)

	// Third detection MUST NOT queue: the daemon-mode touch should
	// have bumped LastEnriched past the stale cutoff. Without the fix,
	// LastEnriched would still be `stale` and this would re-queue
	// forever.
	job3, action3 := d.detectMarkedupEnrichJob()
	assert.Nil(t, job3, "no job expected after daemon-mode touch advances LastEnriched")
	assert.Equal(t, TickAction{}, action3, "no tick action expected when no work is needed")

	// Cross-check: the manifest LastEnriched is now recent.
	m2, err := mgr.Load()
	require.NoError(t, err)
	parsed, perr := time.Parse(time.RFC3339, m2.Pages[0].LastEnriched)
	require.NoError(t, perr, "LastEnriched should be a valid RFC3339 stamp")
	assert.True(t, time.Since(parsed) < 5*time.Minute,
		"LastEnriched should be recent after the daemon-mode touch; got %q", m2.Pages[0].LastEnriched)
}

func TestDetectMarkedupEnrichJob_NoMarkedupConfigBlockNoJob(t *testing.T) {
	// A daemon whose config has no plugins.markedup block at all must
	// produce no enrichment job, even with stale pages on disk.
	cfg := &config.Config{
		Wiki: config.Wiki{Root: ".wiki"},
	}
	d, repoRoot := newTestDaemon(t, DaemonOpts{Config: cfg})

	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".plexium"), 0o755))
	mgr, err := manifest.NewManager(manifest.DefaultPath(repoRoot))
	require.NoError(t, err)
	m := manifest.NewEmptyManifest()
	m.Pages = []manifest.PageEntry{{WikiPath: "Home.md"}}
	require.NoError(t, mgr.Save(m))

	job, action := d.detectMarkedupEnrichJob()
	assert.Nil(t, job)
	assert.Equal(t, TickAction{}, action)
}
