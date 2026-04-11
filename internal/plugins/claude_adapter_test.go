package plugins

import (
	"encoding/json"
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

	// Trim trailing newline to avoid off-by-one from Split's treatment of it
	content := strings.TrimRight(string(data), "\n")
	lines := strings.Split(content, "\n")
	assert.Less(t, len(lines), 50, "CLAUDE.md should be under 50 lines, got %d", len(lines))
	assert.Contains(t, string(data), "myproject")
	assert.Contains(t, string(data), "SCHEMA_INJECT_START")
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

	settingsPath := filepath.Join(repoRoot, ".claude", "settings.json")
	assert.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))

	hooks, ok := settings["hooks"].(map[string]any)
	require.True(t, ok, "settings.json should contain a hooks key")
	postToolUse, ok := hooks["PostToolUse"].([]any)
	require.True(t, ok, "hooks should contain PostToolUse array")

	found := false
	for _, entry := range postToolUse {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := m["matcher"].(string)
		// Support "Write", "Edit", or "Write|Edit" (pipe-separated)
		isExactMatch := matcher == "Write" || matcher == "Edit"
		isCombinedMatch := func() bool {
			for _, part := range strings.Split(matcher, "|") {
				if part == "Write" || part == "Edit" {
					return true
				}
			}
			return false
		}()
		if isExactMatch || isCombinedMatch {
			found = true
			hookSlice, ok := m["hooks"].([]any)
			require.True(t, ok, "hook entry should have hooks array")
			require.NotEmpty(t, hookSlice, "hooks array should not be empty")

			hasPostEditHook := false
			for _, h := range hookSlice {
				hook, ok := h.(map[string]any)
				if !ok {
					continue
				}
				if hook["command"] == "plexium hook post-edit" {
					hasPostEditHook = true
					break
				}
			}
			assert.True(t, hasPostEditHook, "PostToolUse Write|Edit hook should include plexium hook post-edit")
			break
		}
	}
	assert.True(t, found, "PostToolUse should contain a hook for Write or Edit")
}

func TestExtractSchemaDigest_SkipsFrontmatterComments(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".wiki"), 0o755))

	schema := "---\nschema-version: 1\n# this is a YAML comment\ntitle: Schema\n---\n# Wiki Schema\nThis is the schema.\n"
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".wiki", "_schema.md"), []byte(schema), 0o644))

	digest := extractSchemaDigest(repoRoot)
	assert.Contains(t, digest, "# Wiki Schema", "markdown heading should be included")
	assert.Contains(t, digest, "This is the schema.", "first paragraph should be included")
	assert.NotContains(t, digest, "this is a YAML comment", "YAML comment should not appear in digest")
	assert.NotContains(t, digest, "schema-version", "frontmatter fields should not appear in digest")
}

func TestExtractSchemaDigest_NoSchema(t *testing.T) {
	repoRoot := t.TempDir()
	digest := extractSchemaDigest(repoRoot)
	assert.Contains(t, digest, "Run `plexium init`")
}

func TestDetectProjectName_GoMod(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module github.com/org/myapp\n"), 0o644))

	assert.Equal(t, "myapp", detectProjectName(repoRoot))
}

func TestDetectProjectName_PackageJSON(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(`{"name":"my-npm-package","version":"1.0.0"}`), 0o644))

	assert.Equal(t, "my-npm-package", detectProjectName(repoRoot))
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
