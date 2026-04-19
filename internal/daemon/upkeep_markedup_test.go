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
