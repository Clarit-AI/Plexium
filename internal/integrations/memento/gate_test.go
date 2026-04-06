package memento

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGate(t *testing.T) {
	gate := NewGate("/repo")

	assert.Equal(t, "/repo", gate.RepoRoot)
}

func TestGateCheck_ReturnsStructuredResult(t *testing.T) {
	// Create a temporary git repo for testing
	tmpDir := t.TempDir()

	// Initialize a git repo with a commit
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		require.NoError(t, cmd.Run(), "failed to run: %v", args)
	}

	// Create a file and commit it
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644))
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = tmpDir
	require.NoError(t, addCmd.Run())

	commitCmd := exec.Command("git", "commit", "-m", "initial commit")
	commitCmd.Dir = tmpDir
	require.NoError(t, commitCmd.Run())

	gate := NewGate(tmpDir)
	result, err := gate.Check()

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Without a memento note, gate should fail
	assert.False(t, result.Passes)
	assert.False(t, result.LastCommitHasNote)
	assert.Contains(t, result.Reason, "no memento session note")
}

func TestGateCheck_PassesWithNote(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		require.NoError(t, cmd.Run())
	}

	// Create a commit
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644))
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = tmpDir
	require.NoError(t, addCmd.Run())

	commitCmd := exec.Command("git", "commit", "-m", "initial commit")
	commitCmd.Dir = tmpDir
	require.NoError(t, commitCmd.Run())

	// Add a memento note
	noteCmd := exec.Command("git", "notes", "--ref=memento", "add", "-m", "session-abc123")
	noteCmd.Dir = tmpDir
	require.NoError(t, noteCmd.Run())

	gate := NewGate(tmpDir)
	result, err := gate.Check()

	require.NoError(t, err)
	assert.True(t, result.Passes)
	assert.True(t, result.LastCommitHasNote)
	assert.Contains(t, result.Reason, "memento session provenance")
}

func TestGateCheck_NotAGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	gate := NewGate(tmpDir)
	_, err := gate.Check()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}
