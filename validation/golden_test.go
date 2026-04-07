package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/wiki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Golden tests compare generated output against stored expected files.
// Run with -update flag to regenerate golden files:
//   go test ./validation/ -run TestGolden -update
//
// Note: We use inline golden expectations rather than files for portability,
// and structural checks rather than byte-exact matches to avoid brittleness.

// =============================================================================
// G1: Init produces expected directory structure
// =============================================================================

func TestGolden_InitDirectoryStructure(t *testing.T) {
	f := GreenFieldFixture(t)

	_, err := wiki.Init(wiki.InitOptions{
		RepoRoot: f.Root,
	})
	require.NoError(t, err)

	// Expected directories
	expectedDirs := []string{
		".wiki",
		".wiki/architecture",
		".wiki/modules",
		".wiki/decisions",
		".wiki/patterns",
		".wiki/concepts",
		".wiki/raw",
		".wiki/raw/meeting-notes",
		".wiki/raw/ticket-exports",
		".wiki/raw/memento-transcripts",
		".wiki/raw/assets",
		".plexium",
		".plexium/plugins",
		".plexium/hooks",
		".plexium/templates",
		".plexium/prompts",
		".plexium/migrations",
	}

	for _, dir := range expectedDirs {
		abs := filepath.Join(f.Root, dir)
		info, err := os.Stat(abs)
		assert.NoError(t, err, "expected directory missing: %s", dir)
		if err == nil {
			assert.True(t, info.IsDir(), "%s should be a directory", dir)
		}
	}

	// Expected files
	expectedFiles := []string{
		".wiki/_schema.md",
		".wiki/_index.md",
		".wiki/_Sidebar.md",
		".wiki/_Footer.md",
		".wiki/_log.md",
		".wiki/Home.md",
		".wiki/architecture/overview.md",
		".wiki/onboarding.md",
		".wiki/contradictions.md",
		".wiki/open-questions.md",
		".plexium/config.yml",
		".plexium/manifest.json",
	}

	for _, file := range expectedFiles {
		assert.True(t, f.FileExists(file), "expected file missing: %s", file)
	}
}

// =============================================================================
// G2: Empty manifest JSON shape
// =============================================================================

func TestGolden_EmptyManifestShape(t *testing.T) {
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	err = mgr.Save(manifest.NewEmptyManifest())
	require.NoError(t, err)

	data := f.ReadFile(".plexium/manifest.json")

	var m manifest.Manifest
	err = json.Unmarshal([]byte(data), &m)
	require.NoError(t, err)

	assert.Equal(t, 1, m.Version)
	assert.NotNil(t, m.Pages)
	assert.Empty(t, m.Pages)
	assert.NotNil(t, m.UnmanagedPages)
	assert.Empty(t, m.UnmanagedPages)
	assert.Empty(t, m.LastProcessedCommit)
	assert.Empty(t, m.LastPublishTimestamp)
}

// =============================================================================
// G3: Populated manifest JSON roundtrip
// =============================================================================

func TestGolden_PopulatedManifestRoundtrip(t *testing.T) {
	f := PopulatedFixture(t)

	data := f.ReadFile(".plexium/manifest.json")

	var m manifest.Manifest
	err := json.Unmarshal([]byte(data), &m)
	require.NoError(t, err)

	assert.Equal(t, 1, m.Version)
	assert.True(t, len(m.Pages) >= 3, "should have at least 3 pages")
	assert.True(t, len(m.UnmanagedPages) >= 1, "should have at least 1 unmanaged page")

	// Verify pages are sorted by WikiPath
	for i := 1; i < len(m.Pages); i++ {
		assert.True(t, m.Pages[i-1].WikiPath <= m.Pages[i].WikiPath,
			"pages should be sorted by WikiPath")
	}

	// Verify page fields are populated
	for _, p := range m.Pages {
		assert.NotEmpty(t, p.WikiPath, "WikiPath required")
		assert.NotEmpty(t, p.Title, "Title required")
		assert.NotEmpty(t, p.Ownership, "Ownership required")
	}
}

// =============================================================================
// G4: Compiled _index.md structure
// =============================================================================

func TestGolden_CompiledIndexStructure(t *testing.T) {
	f := PopulatedFixture(t)

	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	index := f.ReadFile(".wiki/_index.md")

	// Must start with title
	assert.True(t, strings.HasPrefix(index, "# Wiki Index"),
		"_index.md should start with '# Wiki Index'")

	// Must contain section headers for populated sections
	assert.Contains(t, index, "## Concepts")
	assert.Contains(t, index, "## Decisions")
	assert.Contains(t, index, "## Modules")

	// Must contain wiki-link format entries
	assert.Contains(t, index, "[[")
	assert.Contains(t, index, "]]")

	// Must contain page titles
	assert.Contains(t, index, "Auth Module")
}

// =============================================================================
// G5: Compiled _Sidebar.md structure
// =============================================================================

func TestGolden_CompiledSidebarStructure(t *testing.T) {
	f := PopulatedFixture(t)

	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	sidebar := f.ReadFile(".wiki/_Sidebar.md")

	// Must start with Home link
	assert.True(t, strings.HasPrefix(sidebar, "**[[Home]]**"),
		"_Sidebar.md should start with Home link")

	// Must contain section headers
	assert.Contains(t, sidebar, "**Modules**")

	// Must contain wiki-links
	assert.Contains(t, sidebar, "[[auth-module]]")
}

// =============================================================================
// G6: Default config.yml structure
// =============================================================================

func TestGolden_DefaultConfigStructure(t *testing.T) {
	f := GreenFieldFixture(t)

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: f.Root})
	require.NoError(t, err)

	configContent := f.ReadFile(".plexium/config.yml")

	// Must contain core sections
	requiredSections := []string{
		"version:", "repo:", "sources:", "wiki:", "taxonomy:",
		"publish:", "sync:", "enforcement:", "sensitivity:",
	}
	for _, section := range requiredSections {
		assert.Contains(t, configContent, section,
			"default config should contain section: %s", section)
	}

	// Must contain source include patterns
	assert.Contains(t, configContent, "**/*.go")
	assert.Contains(t, configContent, "**/*.md")
}

// =============================================================================
// G7: Schema file structure
// =============================================================================

func TestGolden_SchemaFileStructure(t *testing.T) {
	f := GreenFieldFixture(t)

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: f.Root})
	require.NoError(t, err)

	schema := f.ReadFile(".wiki/_schema.md")

	// Schema is generated by the SchemaGenerator — may or may not have frontmatter
	// depending on tech-stack detection. The key contract is it's non-empty and
	// contains agent directives.
	assert.NotEmpty(t, schema, "_schema.md should not be empty")
	assert.Contains(t, schema, "PLEXIUM SCHEMA",
		"_schema.md should contain PLEXIUM SCHEMA heading")
}

// =============================================================================
// G8: Home.md incorporates README content
// =============================================================================

func TestGolden_HomeIncorporatesREADME(t *testing.T) {
	f := GreenFieldFixture(t)

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: f.Root})
	require.NoError(t, err)

	home := f.ReadFile(".wiki/Home.md")

	// Should contain frontmatter
	assert.Contains(t, home, "---")
	assert.Contains(t, home, "ownership: managed")

	// Should incorporate README content
	assert.Contains(t, home, "Test Project",
		"Home.md should incorporate README content")
}

// =============================================================================
// G9: Init with --obsidian creates obsidian config
// =============================================================================

func TestGolden_ObsidianInitStructure(t *testing.T) {
	f := GreenFieldFixture(t)

	_, err := wiki.Init(wiki.InitOptions{
		RepoRoot: f.Root,
		Obsidian: true,
	})
	require.NoError(t, err)

	assert.True(t, f.FileExists(".wiki/.obsidian"),
		".obsidian/ should exist when --obsidian is used")
}
