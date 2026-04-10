package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureSkills_Creates(t *testing.T) {
	repoRoot := t.TempDir()

	created, err := EnsureSkills(repoRoot)
	require.NoError(t, err)
	assert.NotEmpty(t, created)

	// Verify key files exist
	assert.FileExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-user", "skill.md"))
	assert.FileExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-dev", "skill.md"))
	assert.DirExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-user", "reference"))
	assert.DirExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-dev", "reference"))
}

func TestEnsureSkills_Idempotent(t *testing.T) {
	repoRoot := t.TempDir()

	created1, err := EnsureSkills(repoRoot)
	require.NoError(t, err)
	assert.NotEmpty(t, created1)

	created2, err := EnsureSkills(repoRoot)
	require.NoError(t, err)
	assert.Empty(t, created2, "second call should create no files")
}

func TestEnsureSkills_PreservesExisting(t *testing.T) {
	repoRoot := t.TempDir()

	// Write a custom file where a skill file would go
	skillDir := filepath.Join(repoRoot, ".claude", "skills", "plexium-user")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	customContent := []byte("# My custom skill content")
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "skill.md"), customContent, 0o644))

	_, err := EnsureSkills(repoRoot)
	require.NoError(t, err)

	// Verify custom content was preserved
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.md"))
	require.NoError(t, err)
	assert.Equal(t, customContent, data)
}
