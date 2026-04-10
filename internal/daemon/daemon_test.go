package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestDaemon creates a Daemon backed by temp dirs with mocked workspace
// gitExec. Returns the daemon and repoRoot for filesystem setup.
func newTestDaemon(t *testing.T, opts DaemonOpts) (*Daemon, string) {
	t.Helper()
	tmpDir := t.TempDir()
	opts.RepoRoot = tmpDir

	mgr := NewWorkspaceMgr(tmpDir)
	mgr.gitExec = func(args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
			if len(args) >= 5 {
				_ = os.MkdirAll(args[4], 0o755)
			}
			return []byte("ok"), nil
		}
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
			if len(args) >= 4 {
				_ = os.RemoveAll(args[3])
			}
			return []byte("ok"), nil
		}
		return []byte("ok"), nil
	}

	tracker := &NoOpTracker{}
	runner := NewNoOpRunner()

	d := NewDaemon(opts, mgr, tracker, runner, nil, nil)
	return d, tmpDir
}

// setupWikiDir creates .wiki/ with the given markdown files and modification times.
func setupWikiDir(t *testing.T, repoRoot string, files map[string]time.Time) {
	t.Helper()
	wikiDir := filepath.Join(repoRoot, ".wiki")
	require.NoError(t, os.MkdirAll(wikiDir, 0o755))

	for name, modTime := range files {
		path := filepath.Join(wikiDir, name)
		require.NoError(t, os.WriteFile(path, []byte("# "+name), 0o644))
		require.NoError(t, os.Chtimes(path, modTime, modTime))
	}
}

// setupLogFile creates .wiki/_log.md with the given lines.
func setupLogFile(t *testing.T, repoRoot string, lines []string) {
	t.Helper()
	wikiDir := filepath.Join(repoRoot, ".wiki")
	require.NoError(t, os.MkdirAll(wikiDir, 0o755))

	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "_log.md"), []byte(content), 0o644))
}

// setupRawDir creates .wiki/raw/ with the given files.
func setupRawDir(t *testing.T, repoRoot string, files []string) {
	t.Helper()
	rawDir := filepath.Join(repoRoot, ".wiki", "raw")
	require.NoError(t, os.MkdirAll(rawDir, 0o755))

	for _, name := range files {
		require.NoError(t, os.WriteFile(filepath.Join(rawDir, name), []byte("data"), 0o644))
	}
}

// ---------------------------------------------------------------------------
// NewDaemon: defaults
// ---------------------------------------------------------------------------

func TestNewDaemon_Defaults(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{})
	assert.Equal(t, 5*time.Minute, d.pollInterval)
	assert.Equal(t, 2, d.maxConcurrent)
	assert.NotNil(t, d.stopCh)
}

func TestNewDaemon_CustomValues(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		PollInterval:  30 * time.Second,
		MaxConcurrent: 5,
	})
	assert.Equal(t, 30*time.Second, d.pollInterval)
	assert.Equal(t, 5, d.maxConcurrent)
}

// ---------------------------------------------------------------------------
// tick: no watches
// ---------------------------------------------------------------------------

func TestTick_NoWatchesEnabled(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{})

	actions := d.tick()
	assert.Empty(t, actions)
}

// ---------------------------------------------------------------------------
// tick: staleness watch
// ---------------------------------------------------------------------------

func TestTick_Staleness_DetectsOldFiles(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Staleness: WatchDef{Enabled: true, Action: "log-only", Threshold: "7d"},
		},
	})

	old := time.Now().Add(-14 * 24 * time.Hour)
	recent := time.Now()
	setupWikiDir(t, repoRoot, map[string]time.Time{
		"old-page.md":   old,
		"fresh-page.md": recent,
		"_schema.md":    old, // should be skipped (underscore prefix)
	})

	actions := d.tick()

	assert.Len(t, actions, 1)
	assert.Equal(t, "staleness", actions[0].Watch)
	assert.Equal(t, "old-page.md", actions[0].Target)
	assert.True(t, actions[0].Success)
}

func TestTick_Staleness_NoStaleFiles(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Staleness: WatchDef{Enabled: true, Action: "log-only", Threshold: "7d"},
		},
	})

	setupWikiDir(t, repoRoot, map[string]time.Time{
		"fresh.md": time.Now(),
	})

	actions := d.tick()
	assert.Empty(t, actions)
}

func TestTick_Staleness_NoWikiDir(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Staleness: WatchDef{Enabled: true, Action: "log-only", Threshold: "7d"},
		},
	})

	actions := d.tick()
	assert.Len(t, actions, 1)
	assert.False(t, actions[0].Success)
	assert.Contains(t, actions[0].Error, "readdir")
}

// ---------------------------------------------------------------------------
// tick: debt watch
// ---------------------------------------------------------------------------

func TestTick_Debt_OverThreshold(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Debt: WatchDef{Enabled: true, Action: "log-only", Threshold: "3"},
		},
	})

	setupLogFile(t, repoRoot, []string{
		"- WIKI-DEBT: stale page modules/auth",
		"- WIKI-DEBT: orphan link decisions/old",
		"- WIKI-DEBT: missing page concepts/new",
		"- Normal log entry",
	})

	actions := d.tick()
	assert.Len(t, actions, 1)
	assert.Equal(t, "debt", actions[0].Watch)
	assert.Contains(t, actions[0].Target, "count=3")
	assert.True(t, actions[0].Success)
}

func TestTick_Debt_UnderThreshold(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Debt: WatchDef{Enabled: true, Action: "log-only", Threshold: "10"},
		},
	})

	setupLogFile(t, repoRoot, []string{
		"- WIKI-DEBT: one issue",
		"- Normal entry",
	})

	actions := d.tick()
	assert.Empty(t, actions)
}

func TestTick_Debt_NoLogFile(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Debt: WatchDef{Enabled: true, Action: "log-only", Threshold: "5"},
		},
	})

	actions := d.tick()
	assert.Empty(t, actions)
}

// ---------------------------------------------------------------------------
// tick: ingest watch
// ---------------------------------------------------------------------------

func TestTick_Ingest_DetectsRawFiles(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Ingest: WatchDef{Enabled: true, Action: "log-only"},
		},
	})

	setupRawDir(t, repoRoot, []string{"new-doc.md", "draft.md"})

	actions := d.tick()
	assert.Len(t, actions, 2)
	assert.Equal(t, "ingest", actions[0].Watch)
}

func TestTick_Ingest_NoRawDir(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Ingest: WatchDef{Enabled: true, Action: "log-only"},
		},
	})

	actions := d.tick()
	assert.Empty(t, actions)
}

// ---------------------------------------------------------------------------
// tick: create-issue action
// ---------------------------------------------------------------------------

func TestHandleAction_CreateIssue(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		Watches: WatchOpts{
			Staleness: WatchDef{Enabled: true, Action: "create-issue", Threshold: "1h"},
		},
	})

	// Track issues created.
	var issueTitles []string
	d.tracker = &mockTracker{
		createIssue: func(title, body string) (string, error) {
			issueTitles = append(issueTitles, title)
			return "42", nil
		},
	}

	old := time.Now().Add(-48 * time.Hour)
	setupWikiDir(t, repoRoot, map[string]time.Time{
		"stale.md": old,
	})

	actions := d.tick()
	require.Len(t, actions, 1)
	assert.True(t, actions[0].Success)
	assert.Contains(t, issueTitles[0], "staleness")
	assert.Contains(t, issueTitles[0], "stale.md")
}

// ---------------------------------------------------------------------------
// tick: auto-sync action respects maxConcurrent
// ---------------------------------------------------------------------------

func TestHandleAction_AutoSync_RespectsMaxConcurrent(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		MaxConcurrent: 1,
		Watches: WatchOpts{
			Staleness: WatchDef{Enabled: true, Action: "auto-sync", Threshold: "1h"},
		},
	})

	old := time.Now().Add(-48 * time.Hour)
	setupWikiDir(t, repoRoot, map[string]time.Time{
		"page1.md": old,
		"page2.md": old,
	})

	// Pre-create a running workspace to fill the concurrency slot.
	_, err := d.workspace.Create("existing")
	require.NoError(t, err)

	actions := d.tick()

	// Both pages are stale, but both should fail due to max concurrent.
	for _, a := range actions {
		assert.False(t, a.Success)
		assert.Contains(t, a.Error, "max concurrent")
	}
}

// ---------------------------------------------------------------------------
// Stop causes Run to exit
// ---------------------------------------------------------------------------

func TestDaemon_Stop_ExitsRun(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		PollInterval: 1 * time.Hour, // long interval so we don't tick again
	})

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Give Run a moment to start, then stop.
	time.Sleep(50 * time.Millisecond)
	d.Stop()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after Stop()")
	}
}

func TestDaemon_ContextCancel_ExitsRun(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		PollInterval: 1 * time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}

// ---------------------------------------------------------------------------
// parseDuration / parseIntThreshold
// ---------------------------------------------------------------------------

func TestParseDuration_Days(t *testing.T) {
	assert.Equal(t, 7*24*time.Hour, parseDuration("7d", time.Hour))
}

func TestParseDuration_Hours(t *testing.T) {
	assert.Equal(t, 2*time.Hour, parseDuration("2h", time.Hour))
}

func TestParseDuration_Default(t *testing.T) {
	assert.Equal(t, 5*time.Minute, parseDuration("", 5*time.Minute))
	assert.Equal(t, 5*time.Minute, parseDuration("invalid", 5*time.Minute))
}

func TestParseIntThreshold(t *testing.T) {
	assert.Equal(t, 10, parseIntThreshold("10", 5))
	assert.Equal(t, 5, parseIntThreshold("", 5))
	assert.Equal(t, 5, parseIntThreshold("abc", 5))
}

// ---------------------------------------------------------------------------
// Mock tracker for tests
// ---------------------------------------------------------------------------

type mockTracker struct {
	createIssue func(title, body string) (string, error)
}

func (m *mockTracker) CreateIssue(title, body string) (string, error) {
	if m.createIssue != nil {
		return m.createIssue(title, body)
	}
	return "", nil
}

func (m *mockTracker) CloseIssue(_ string) error  { return nil }
func (m *mockTracker) AddLabel(_, _ string) error { return nil }
func (m *mockTracker) Comment(_, _ string) error  { return nil }

// ---------------------------------------------------------------------------
// countDebtEntries
// ---------------------------------------------------------------------------

func TestCountDebtEntries(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "_log.md")

	content := "- WIKI-DEBT: issue1\n- Normal line\n- WIKI-DEBT: issue2\n"
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o644))

	count, err := countDebtEntries(logPath)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCountDebtEntries_MissingFile(t *testing.T) {
	_, err := countDebtEntries("/tmp/nonexistent-plexium-log.md")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// handleAction: unknown action
// ---------------------------------------------------------------------------

func TestHandleAction_UnknownAction(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{})
	a := d.handleAction("test", "unknown-action", "target")
	assert.False(t, a.Success)
	assert.Contains(t, a.Error, "unknown action")
}

// ---------------------------------------------------------------------------
// handleAction: auto-sync creates and cleans workspace
// ---------------------------------------------------------------------------

func TestHandleAction_AutoSync_Success(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		MaxConcurrent: 5,
	})

	a := d.handleAction("staleness", "auto-sync", "page.md")
	assert.True(t, a.Success)
	assert.Equal(t, "auto-sync", a.Action)

	// Workspace should be cleaned up.
	count, err := d.workspace.ActiveCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestHandleAction_AutoSync_RunnerFails(t *testing.T) {
	d, _ := newTestDaemon(t, DaemonOpts{
		MaxConcurrent: 5,
	})

	// Replace runner with one that fails.
	d.runner = &failingRunner{}

	a := d.handleAction("staleness", "auto-sync", "page.md")
	assert.False(t, a.Success)
	assert.Contains(t, a.Error, "runner failed")
}

type failingRunner struct{}

func (f *failingRunner) Run(_ context.Context, _, _ string, _ []string, _ string) (*RunResult, error) {
	return nil, fmt.Errorf("runner failed")
}
