package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()

	// Create minimum scaffolding that RunClaudeAdapter expects
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".wiki"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".plexium"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".wiki", "_schema.md"), []byte("---\nschema-version: 1\n---\n# Wiki Schema\nThis is the schema."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module github.com/example/myproject\n\ngo 1.22\n"), 0o644))

	return repoRoot
}

func TestRunClaudeAdapter_LeanOutput(t *testing.T) {
	repoRoot := setupTestRepo(t)

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	require.NoError(t, err)

	lines := strings.Split(string(data), "\n")
	assert.Less(t, len(lines), 50, "CLAUDE.md should be under 50 lines, got %d", len(lines))
	assert.Contains(t, string(data), "myproject")
	assert.Contains(t, string(data), "SCHEMA_INJECT_START")
	assert.Contains(t, string(data), "plexium-user")
}

func TestRunClaudeAdapter_PreservesUserContent(t *testing.T) {
	repoRoot := setupTestRepo(t)

	// Write an existing CLAUDE.md with custom user content and markers
	existing := `# My Custom Header

Some user-written context about the project.

<!-- SCHEMA_INJECT_START -->
old schema content
<!-- SCHEMA_INJECT_END -->

## My Custom Section
Do not delete this.
`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"), []byte(existing), 0o644))

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "My Custom Header", "user content before markers should be preserved")
	assert.Contains(t, content, "My Custom Section", "user content after markers should be preserved")
	assert.NotContains(t, content, "old schema content", "old schema should be replaced")
	assert.Contains(t, content, "Wiki Schema", "new schema digest should be injected")
}

func TestRunClaudeAdapter_GeneratesLefthook(t *testing.T) {
	repoRoot := setupTestRepo(t)

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(repoRoot, "lefthook.yml"))
	data, err := os.ReadFile(filepath.Join(repoRoot, "lefthook.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "pre-commit")
	assert.Contains(t, string(data), "plexium hook")
}

func TestRunClaudeAdapter_SkipsExistingLefthook(t *testing.T) {
	repoRoot := setupTestRepo(t)

	customLefthook := "# My custom lefthook config\n"
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "lefthook.yml"), []byte(customLefthook), 0o644))

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(repoRoot, "lefthook.yml"))
	require.NoError(t, err)
	assert.Equal(t, customLefthook, string(data), "existing lefthook.yml should not be overwritten")
}

func TestRunClaudeAdapter_InstallsSkills(t *testing.T) {
	repoRoot := setupTestRepo(t)

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-user", "skill.md"))
	assert.FileExists(t, filepath.Join(repoRoot, ".claude", "skills", "plexium-dev", "skill.md"))
}

func TestRunClaudeAdapter_CreatesClaudeHooks(t *testing.T) {
	repoRoot := setupTestRepo(t)

	err := RunClaudeAdapter(repoRoot)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(repoRoot, ".claude", "settings.json"))
}

func TestDetectProjectName_GoMod(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module github.com/org/myapp\n"), 0o644))

	assert.Equal(t, "myapp", detectProjectName(repoRoot))
}

func TestDetectProjectName_FallbackToDir(t *testing.T) {
	repoRoot := t.TempDir()
	assert.Equal(t, filepath.Base(repoRoot), detectProjectName(repoRoot))
}

func TestMergeWithExisting_NoMarkers(t *testing.T) {
	existing := "# Some file without markers\nContent here."
	generated := "# New content\n<!-- SCHEMA_INJECT_START -->\nnew schema\n<!-- SCHEMA_INJECT_END -->"
	assert.Equal(t, generated, mergeWithExisting(existing, generated))
}

func TestMergeWithExisting_WithMarkers(t *testing.T) {
	existing := "# Header\n<!-- SCHEMA_INJECT_START -->\nold\n<!-- SCHEMA_INJECT_END -->\n# Footer"
	generated := "# Ignored\n<!-- SCHEMA_INJECT_START -->\nnew schema\n<!-- SCHEMA_INJECT_END -->\n# Also ignored"

	result := mergeWithExisting(existing, generated)
	assert.Contains(t, result, "# Header")
	assert.Contains(t, result, "new schema")
	assert.Contains(t, result, "# Footer")
	assert.NotContains(t, result, "old")
}
