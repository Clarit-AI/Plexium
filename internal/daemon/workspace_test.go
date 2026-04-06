package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestMgr creates a WorkspaceMgr backed by a temp dir with a mock gitExec
// that simply creates the target directory (simulating git worktree add).
func newTestMgr(t *testing.T) *WorkspaceMgr {
	t.Helper()
	tmpDir := t.TempDir()
	mgr := NewWorkspaceMgr(tmpDir)

	// Mock gitExec: on "worktree add", just create the directory.
	// On "worktree remove", remove it. Otherwise no-op.
	mgr.gitExec = func(args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
			// args: worktree add -b <branch> <path>
			if len(args) >= 5 {
				if err := os.MkdirAll(args[4], 0o755); err != nil {
					return nil, err
				}
			}
			return []byte("ok"), nil
		}
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
			// args: worktree remove --force <path>
			if len(args) >= 4 {
				_ = os.RemoveAll(args[3])
			}
			return []byte("ok"), nil
		}
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "prune" {
			return []byte("ok"), nil
		}
		return []byte("ok"), nil
	}

	return mgr
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_Create(t *testing.T) {
	mgr := newTestMgr(t)

	wt, err := mgr.Create("ISSUE-42")
	require.NoError(t, err)

	assert.Equal(t, "wt-ISSUE-42", wt.ID)
	assert.Equal(t, "ISSUE-42", wt.IssueID)
	assert.Equal(t, "plexium/wt-ISSUE-42", wt.Branch)
	assert.Equal(t, "running", wt.Status)
	assert.Contains(t, wt.Path, "wt-ISSUE-42")
	assert.False(t, wt.StartedAt.IsZero())
}

func TestWorkspaceMgr_Create_WritesMetaJSON(t *testing.T) {
	mgr := newTestMgr(t)

	wt, err := mgr.Create("123")
	require.NoError(t, err)

	metaFile := filepath.Join(mgr.basePath, wt.ID, "meta.json")
	data, err := os.ReadFile(metaFile)
	require.NoError(t, err)

	var loaded Worktree
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, wt.ID, loaded.ID)
	assert.Equal(t, wt.Branch, loaded.Branch)
	assert.Equal(t, "running", loaded.Status)
}

func TestWorkspaceMgr_Create_GitFailure(t *testing.T) {
	mgr := newTestMgr(t)
	mgr.gitExec = func(args ...string) ([]byte, error) {
		return []byte("fatal: branch already exists"), fmt.Errorf("exit status 128")
	}

	_, err := mgr.Create("FAIL")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "git worktree add")
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_Get(t *testing.T) {
	mgr := newTestMgr(t)

	created, err := mgr.Create("GET-1")
	require.NoError(t, err)

	got, err := mgr.Get(created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.IssueID, got.IssueID)
	assert.Equal(t, created.Branch, got.Branch)
}

func TestWorkspaceMgr_Get_NotFound(t *testing.T) {
	mgr := newTestMgr(t)

	_, err := mgr.Get("wt-nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read meta")
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_List(t *testing.T) {
	mgr := newTestMgr(t)

	_, err := mgr.Create("A")
	require.NoError(t, err)
	_, err = mgr.Create("B")
	require.NoError(t, err)

	list, err := mgr.List()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	ids := []string{list[0].ID, list[1].ID}
	assert.Contains(t, ids, "wt-A")
	assert.Contains(t, ids, "wt-B")
}

func TestWorkspaceMgr_List_EmptyDir(t *testing.T) {
	mgr := newTestMgr(t)

	list, err := mgr.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestWorkspaceMgr_List_NoDir(t *testing.T) {
	mgr := NewWorkspaceMgr("/tmp/nonexistent-plexium-test-dir")

	list, err := mgr.List()
	require.NoError(t, err)
	assert.Nil(t, list)
}

// ---------------------------------------------------------------------------
// UpdateStatus
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_UpdateStatus(t *testing.T) {
	mgr := newTestMgr(t)

	wt, err := mgr.Create("STATUS-1")
	require.NoError(t, err)
	assert.Equal(t, "running", wt.Status)

	err = mgr.UpdateStatus(wt.ID, "completed")
	require.NoError(t, err)

	got, err := mgr.Get(wt.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
}

func TestWorkspaceMgr_UpdateStatus_Failed(t *testing.T) {
	mgr := newTestMgr(t)

	wt, err := mgr.Create("STATUS-2")
	require.NoError(t, err)

	err = mgr.UpdateStatus(wt.ID, "failed")
	require.NoError(t, err)

	got, err := mgr.Get(wt.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.Status)
}

func TestWorkspaceMgr_UpdateStatus_NotFound(t *testing.T) {
	mgr := newTestMgr(t)

	err := mgr.UpdateStatus("wt-ghost", "completed")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// ActiveCount
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_ActiveCount(t *testing.T) {
	mgr := newTestMgr(t)

	_, err := mgr.Create("AC-1")
	require.NoError(t, err)
	_, err = mgr.Create("AC-2")
	require.NoError(t, err)
	_, err = mgr.Create("AC-3")
	require.NoError(t, err)

	count, err := mgr.ActiveCount()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Mark one completed.
	err = mgr.UpdateStatus("wt-AC-2", "completed")
	require.NoError(t, err)

	count, err = mgr.ActiveCount()
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestWorkspaceMgr_ActiveCount_Empty(t *testing.T) {
	mgr := newTestMgr(t)

	count, err := mgr.ActiveCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestWorkspaceMgr_Cleanup(t *testing.T) {
	mgr := newTestMgr(t)

	wt, err := mgr.Create("CLEAN-1")
	require.NoError(t, err)

	// Directory should exist.
	_, err = os.Stat(filepath.Join(mgr.basePath, wt.ID))
	require.NoError(t, err)

	err = mgr.Cleanup(wt.ID)
	require.NoError(t, err)

	// Directory should be gone.
	_, err = os.Stat(filepath.Join(mgr.basePath, wt.ID))
	assert.True(t, os.IsNotExist(err))
}

func TestWorkspaceMgr_CleanupAll(t *testing.T) {
	mgr := newTestMgr(t)

	_, err := mgr.Create("CA-1")
	require.NoError(t, err)
	_, err = mgr.Create("CA-2")
	require.NoError(t, err)

	list, err := mgr.List()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	err = mgr.CleanupAll()
	require.NoError(t, err)

	list, err = mgr.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

// ---------------------------------------------------------------------------
// Meta.json round-trip (serde)
// ---------------------------------------------------------------------------

func TestWorktree_MetaJSON_RoundTrip(t *testing.T) {
	mgr := newTestMgr(t)

	original, err := mgr.Create("SERDE-1")
	require.NoError(t, err)

	loaded, err := mgr.Get(original.ID)
	require.NoError(t, err)

	assert.Equal(t, original.ID, loaded.ID)
	assert.Equal(t, original.Path, loaded.Path)
	assert.Equal(t, original.IssueID, loaded.IssueID)
	assert.Equal(t, original.Branch, loaded.Branch)
	assert.Equal(t, original.Status, loaded.Status)
	assert.Equal(t, original.StartedAt.Unix(), loaded.StartedAt.Unix())
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewWorkspaceMgr(t *testing.T) {
	mgr := NewWorkspaceMgr("/repo")
	assert.Equal(t, "/repo/.plexium/workspaces", mgr.basePath)
	assert.Equal(t, "/repo", mgr.repoRoot)
	assert.NotNil(t, mgr.gitExec)
}
