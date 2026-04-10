package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
