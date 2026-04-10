package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureClaudeHooks_CreatesNew(t *testing.T) {
	repoRoot := t.TempDir()

	created, err := EnsureClaudeHooks(repoRoot)
	require.NoError(t, err)
	assert.True(t, created)

	// Verify the file was created with correct structure
	data, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "settings.json"))
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))

	hooks := settings["hooks"].(map[string]any)
	postToolUse := hooks["PostToolUse"].([]any)
	require.Len(t, postToolUse, 1)

	entry := postToolUse[0].(map[string]any)
	assert.Equal(t, "Write|Edit", entry["matcher"])
}

func TestEnsureClaudeHooks_PreservesExisting(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".claude"), 0o755))

	// Write existing settings with other content
	existing := map[string]any{
		"customKey": "customValue",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{}},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".claude", "settings.json"), data, 0o644))

	created, err := EnsureClaudeHooks(repoRoot)
	require.NoError(t, err)
	assert.True(t, created)

	// Verify existing content preserved
	newData, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "settings.json"))
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(newData, &settings))

	assert.Equal(t, "customValue", settings["customKey"])
	hooks := settings["hooks"].(map[string]any)
	assert.NotNil(t, hooks["PreToolUse"], "existing hooks should be preserved")
	assert.NotNil(t, hooks["PostToolUse"], "new hook should be added")
}

func TestEnsureClaudeHooks_Idempotent(t *testing.T) {
	repoRoot := t.TempDir()

	created1, err := EnsureClaudeHooks(repoRoot)
	require.NoError(t, err)
	assert.True(t, created1)

	created2, err := EnsureClaudeHooks(repoRoot)
	require.NoError(t, err)
	assert.False(t, created2, "second call should not create hook again")
}

func TestEnsureClaudeHooks_MalformedJSON(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".claude", "settings.json"), []byte(`{not valid json`), 0o644))

	created, err := EnsureClaudeHooks(repoRoot)
	require.NoError(t, err)
	assert.True(t, created, "should create hooks even when existing JSON is malformed")

	data, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "settings.json"))
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))

	hooks := settings["hooks"].(map[string]any)
	assert.NotNil(t, hooks["PostToolUse"])
}
