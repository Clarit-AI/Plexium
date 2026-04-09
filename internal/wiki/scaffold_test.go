package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_Basic(t *testing.T) {
	dir := t.TempDir()

	result, err := Init(InitOptions{
		RepoRoot: dir,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify key directories exist
	assert.DirExists(t, filepath.Join(dir, ".wiki"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "architecture"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "modules"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "decisions"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "patterns"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "concepts"))
	assert.DirExists(t, filepath.Join(dir, ".wiki", "raw"))

	assert.DirExists(t, filepath.Join(dir, ".plexium"))
	assert.DirExists(t, filepath.Join(dir, ".plexium", "plugins"))
	assert.DirExists(t, filepath.Join(dir, ".plexium", "templates"))
}

func TestInit_CreatesFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Verify key files exist
	files := []string{
		".wiki/_schema.md",
		".wiki/Home.md",
		".wiki/_Footer.md",
		".wiki/_Sidebar.md",
		".wiki/_index.md",
		".wiki/_log.md",
		".wiki/onboarding.md",
		".wiki/contradictions.md",
		".wiki/open-questions.md",
		".wiki/architecture/overview.md",
		".plexium/config.yml",
		".plexium/manifest.json",
	}

	for _, f := range files {
		path := filepath.Join(dir, f)
		_, err := os.Stat(path)
		assert.NoError(t, err, "expected file to exist: %s", f)
	}
}

func TestInit_ManifestIsValid(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "manifest.json"))
	require.NoError(t, err)

	// Should be valid JSON
	assert.Contains(t, string(data), `"version": 1`)
	assert.Contains(t, string(data), `"pages": []`)
}

func TestInit_ConfigIsValid(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir, Strictness: "strict"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "config.yml"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "version: 1")
	assert.Contains(t, content, "strictness: strict")
	assert.Contains(t, content, "wiki:")
	assert.Contains(t, content, "sources:")
}

func TestInit_DryRun(t *testing.T) {
	dir := t.TempDir()

	result, err := Init(InitOptions{
		RepoRoot: dir,
		DryRun:   true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// In dry-run mode, .wiki/ is not created
	_, err = os.Stat(filepath.Join(dir, ".wiki"))
	assert.True(t, os.IsNotExist(err), ".wiki should not exist in dry-run mode")

	// .plexium/manifest.json is NOT created (manifest writes are dry-run isolated).
	// .plexium/ may exist as parent of .plexium/output/ which is the dry-run output.
	_, err = os.Stat(filepath.Join(dir, ".plexium", "manifest.json"))
	assert.True(t, os.IsNotExist(err), "manifest.json should not exist in dry-run mode")
}

func TestInit_Obsidian(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{
		RepoRoot: dir,
		Obsidian: true,
	})
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(dir, ".wiki", ".obsidian"))
	_, err = os.Stat(filepath.Join(dir, ".wiki", ".obsidian", "app.json"))
	assert.NoError(t, err)
}

func TestInit_HomeFromREADME(t *testing.T) {
	dir := t.TempDir()

	// Create a README
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My Project\n\nThis is my project."), 0644))

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".wiki", "Home.md"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "My Project")
	assert.Contains(t, content, "This is my project")
	assert.Contains(t, content, "[[architecture/overview|Architecture Overview]]")
	assert.Contains(t, content, "[[_log|Activity Log]]")
}

func TestInit_StarterScaffoldIsLintFriendly(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	logContent, err := os.ReadFile(filepath.Join(dir, ".wiki", "_log.md"))
	require.NoError(t, err)
	assert.Contains(t, string(logContent), `title: "Activity Log"`)

	sidebarContent, err := os.ReadFile(filepath.Join(dir, ".wiki", "_Sidebar.md"))
	require.NoError(t, err)
	assert.Contains(t, string(sidebarContent), "[[onboarding|Onboarding Guide]]")
}

func TestInit_WithPageIndex_CreatesStableReference(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir, WithPageIndex: true})
	require.NoError(t, err)

	path := filepath.Join(dir, ".plexium", "pageindex-mcp.json")
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	assert.Contains(t, string(first), `"server": "plexium-pageindex"`)

	_, err = Init(InitOptions{RepoRoot: dir, WithPageIndex: true})
	require.NoError(t, err)

	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, second)
	assert.Equal(t, string(first), string(second))
}

func TestInit_MaterializesPromptPack(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".plexium", "prompts", "assistive", "initial-wiki-population.md"))
	assert.FileExists(t, filepath.Join(dir, ".plexium", "prompts", "assistive", "documenter.md"))
	assert.FileExists(t, filepath.Join(dir, ".plexium", "prompts", "profiles", "balanced.md"))
}

func TestInit_SchemaContent(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{
		RepoRoot:   dir,
		Strictness: "strict",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".wiki", "_schema.md"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "PLEXIUM SCHEMA v1")
	assert.Contains(t, content, "MANDATORY AGENT DIRECTIVES")
	assert.Contains(t, content, "managed")
	assert.Contains(t, content, "human-authored")
}

func TestInit_GitHubWikiConfig(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{
		RepoRoot:   dir,
		GitHubWiki: true,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".plexium", "config.yml"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "wikiEnabled: true")
	assert.Contains(t, content, "enabled: true")
}

func TestInit_RawSubdirs(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	subdirs := []string{"meeting-notes", "ticket-exports", "memento-transcripts", "assets"}
	for _, sub := range subdirs {
		assert.DirExists(t, filepath.Join(dir, ".wiki", "raw", sub))
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// First init
	_, err := Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Read the original Home.md
	homePath := filepath.Join(dir, ".wiki", "Home.md")
	_, err = os.ReadFile(homePath)
	require.NoError(t, err)

	// Modify Home.md with user content
	userContent := "# User Custom Title\n\nThis is my custom content.\n"
	require.NoError(t, os.WriteFile(homePath, []byte(userContent), 0644))

	// Second init — should NOT overwrite the user-modified file
	_, err = Init(InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	newData, err := os.ReadFile(homePath)
	require.NoError(t, err)
	assert.Equal(t, userContent, string(newData), "user-modified file should not be overwritten by init re-run")
}

func TestInit_DryRunManifestIsolation(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(InitOptions{
		RepoRoot: dir,
		DryRun:   true,
	})
	require.NoError(t, err)

	// manifest.json should NOT be created in dry-run mode
	manifestPath := filepath.Join(dir, ".plexium", "manifest.json")
	_, err = os.Stat(manifestPath)
	assert.True(t, os.IsNotExist(err), "manifest.json should not exist after dry-run init")
}
