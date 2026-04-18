package daemon

import (
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
