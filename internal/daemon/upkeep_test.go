package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectWorkspaceChanges_PreservesLeadingStatusColumns(t *testing.T) {
	workdir := t.TempDir()

	require.NoError(t, exec.Command("git", "-C", workdir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "config", "user.email", "plexium@example.com").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "config", "user.name", "Plexium Tests").Run())

	filePath := filepath.Join(workdir, "tracked.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# initial\n"), 0o644))
	require.NoError(t, exec.Command("git", "-C", workdir, "add", "tracked.md").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "commit", "-m", "init").Run())

	require.NoError(t, os.WriteFile(filePath, []byte("# updated\n"), 0o644))

	changed, err := collectWorkspaceChanges(workdir)
	require.NoError(t, err)
	assert.Contains(t, changed, "tracked.md")
}

func TestDiscoverJobs_UsesConfiguredWikiRoot(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		RunnerName: "codex",
		Config: &config.Config{
			Wiki: config.Wiki{Root: "docs/wiki"},
		},
		Watches: WatchOpts{
			Ingest: WatchDef{Enabled: true, Action: "auto-ingest"},
		},
	})

	rawDir := filepath.Join(repoRoot, "docs", "wiki", "raw")
	require.NoError(t, os.MkdirAll(rawDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rawDir, "notes.md"), []byte("raw"), 0o644))

	jobs, actions := d.discoverJobs()
	require.Len(t, jobs, 2)
	assert.Equal(t, "docs/wiki/", jobs[0].Target)
	assert.Equal(t, "docs/wiki/raw/notes.md", jobs[1].Target)

	require.Len(t, actions, 2)
	assert.Equal(t, "docs/wiki/", actions[0].Target)
	assert.Equal(t, "notes.md", actions[1].Target)
}
