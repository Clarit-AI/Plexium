package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeline_DryRun(t *testing.T) {
	dir := setupTestRepo(t)

	// Create .plexium dir for config (optional — pipeline handles missing config)
	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		Config:   nil,
		DryRun:   true,
		Depth:    "shallow",
	})

	result, err := pipeline.Run()
	require.NoError(t, err)

	// Should have pages
	assert.Greater(t, len(result.Pages), 0, "should generate pages")

	// Should have report
	assert.NotNil(t, result.Report)
	assert.Equal(t, "conversion", result.Report.Type)

	// .wiki/ should NOT be created in dry-run
	_, err = os.Stat(filepath.Join(dir, ".wiki"))
	assert.True(t, os.IsNotExist(err), ".wiki/ should not exist in dry-run")

	// Output should be in .plexium/output/
	outputDir := filepath.Join(dir, ".plexium", "output")
	_, err = os.Stat(outputDir)
	assert.NoError(t, err, ".plexium/output/ should exist")

	// Report files should exist
	assert.FileExists(t, result.ReportJSONPath)
	assert.FileExists(t, result.ReportPath)

	// JSON report should be valid
	data, err := os.ReadFile(result.ReportJSONPath)
	require.NoError(t, err)
	var report ConversionReport
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, "conversion", report.Type)
}

func TestPipeline_RealWrite(t *testing.T) {
	dir := setupTestRepo(t)

	// Create .wiki/ and .plexium/ dirs to simulate init
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".plexium"), 0755))

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		Config:   nil,
		DryRun:   false,
		Depth:    "shallow",
	})

	result, err := pipeline.Run()
	require.NoError(t, err)

	// Wiki pages should be written
	assert.Greater(t, len(result.FilesWritten), 0)

	// Home.md should exist in .wiki/
	homePath := filepath.Join(dir, ".wiki", "Home.md")
	assert.FileExists(t, homePath)

	homeContent, err := os.ReadFile(homePath)
	require.NoError(t, err)
	assert.Contains(t, string(homeContent), "My Project")

	// Manifest should be updated
	manifestPath := filepath.Join(dir, ".plexium", "manifest.json")
	assert.FileExists(t, manifestPath)
}

func TestPipeline_PrunesStaleConvertManagedManifestPages(t *testing.T) {
	dir := setupTestRepo(t)

	mgr, err := manifest.NewManager(manifest.DefaultPath(dir))
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/stale-convert-page.md",
				Title:     "Stale Convert Page",
				Ownership: "managed",
				UpdatedBy: "plexium-convert",
			},
			{
				WikiPath:  ".wiki/Home.md",
				Title:     "Bad Daemon Entry",
				Ownership: "managed",
				UpdatedBy: "plexium-daemon",
			},
			{
				WikiPath:  "notes/human-note.md",
				Title:     "Human Note",
				Ownership: "human-authored",
				UpdatedBy: "human",
			},
		},
	}))

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		DryRun:   false,
		Depth:    "shallow",
	})
	_, err = pipeline.Run()
	require.NoError(t, err)

	m, err := mgr.Load()
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, page := range m.Pages {
		paths[page.WikiPath] = true
	}

	assert.False(t, paths["modules/stale-convert-page.md"], "stale convert-managed pages should be pruned")
	assert.False(t, paths[".wiki/Home.md"], "managed entries that include the wiki root should be pruned")
	assert.True(t, paths["notes/human-note.md"], "human-authored manifest entries should be preserved")
	assert.True(t, paths["modules/auth.md"], "current generated pages should be retained")
}

func TestPipeline_DeepMode(t *testing.T) {
	dir := setupTestRepo(t)

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		Config:   nil,
		DryRun:   true,
		Depth:    "deep",
	})

	result, err := pipeline.Run()
	require.NoError(t, err)

	// Deep mode should produce pages (same or more than shallow)
	assert.Greater(t, len(result.Pages), 0)
}

func TestPipeline_ProducesModulePages(t *testing.T) {
	dir := setupTestRepo(t)

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		DryRun:   true,
		Depth:    "shallow",
	})

	result, err := pipeline.Run()
	require.NoError(t, err)

	// Should have module pages for src/auth and src/api
	wikiPaths := make(map[string]bool)
	for _, p := range result.Pages {
		wikiPaths[p.WikiPath] = true
	}

	assert.True(t, wikiPaths["modules/auth.md"], "should have auth module page")
	assert.True(t, wikiPaths["modules/api.md"], "should have api module page")
}

func TestPipeline_RespectsPlexiumIgnoreForDocsAndModules(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".plexiumignore", "docs/specs/**\nsrc/experimental/**\n")
	writeFile(t, dir, "docs/specs/provider-build-spec.md", "# Provider Build Spec\n\nPlanning material.")
	writeFile(t, dir, "src/experimental/prototype.go", "package experimental\n")

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		DryRun:   true,
		Depth:    "shallow",
	})
	result, err := pipeline.Run()
	require.NoError(t, err)

	wikiPaths := make(map[string]bool)
	for _, page := range result.Pages {
		wikiPaths[page.WikiPath] = true
		for _, source := range page.SourceFiles {
			assert.NotEqual(t, "docs/specs/provider-build-spec.md", source)
			assert.NotEqual(t, "src/experimental/prototype.go", source)
		}
	}
	assert.False(t, wikiPaths["guides/provider-build-spec.md"], "ignored planning spec should not become a guide page")
	assert.False(t, wikiPaths["modules/experimental.md"], "ignored source directory should not become a module stub")
}

func TestPipeline_GitignoreWithNegationFallsBackToDefaultBehavior(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".gitignore", "docs/specs/**\n!docs/specs/provider-build-spec.md\n")
	writeFile(t, dir, "docs/specs/provider-build-spec.md", "# Provider Build Spec\n\nPlanning material.")

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		DryRun:   true,
		Depth:    "shallow",
	})
	result, err := pipeline.Run()
	require.NoError(t, err)

	wikiPaths := make(map[string]bool)
	for _, page := range result.Pages {
		wikiPaths[page.WikiPath] = true
	}
	assert.True(t, wikiPaths["specs/specs/provider-build-spec.md"], "gitignore files with negations should not become partial excludes")
}

func TestPipeline_ProducesDecisionPages(t *testing.T) {
	dir := setupTestRepo(t)

	pipeline := NewPipeline(PipelineOptions{
		RepoRoot: dir,
		DryRun:   true,
		Depth:    "shallow",
	})

	result, err := pipeline.Run()
	require.NoError(t, err)

	wikiPaths := make(map[string]bool)
	for _, p := range result.Pages {
		wikiPaths[p.WikiPath] = true
	}

	assert.True(t, wikiPaths["decisions/001-use-go.md"], "should have ADR 1 page")
	assert.True(t, wikiPaths["decisions/002-use-postgres.md"], "should have ADR 2 page")
}

func TestPipeline_DefaultDepthIsShallow(t *testing.T) {
	p := NewPipeline(PipelineOptions{RepoRoot: "/tmp"})
	assert.Equal(t, "shallow", p.depth)
}
