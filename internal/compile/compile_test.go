package compile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testManifest returns a manifest with 6 pages across 3 sections.
func testManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "modules/auth-module.md", Title: "Auth Module", Section: "Modules"},
			{WikiPath: "decisions/adr-001.md", Title: "ADR-001: Language Choice", Section: "Decisions"},
			{WikiPath: "architecture/architecture-overview.md", Title: "Architecture Overview", Section: "Architecture"},
			{WikiPath: "architecture/data-model.md", Title: "Data Model", Section: "Architecture"},
			{WikiPath: "modules/api-gateway.md", Title: "API Gateway", Section: "Modules"},
			{WikiPath: "decisions/adr-002.md", Title: "ADR-002: Database Choice", Section: "Decisions"},
		},
	}
}

func writeManifest(t *testing.T, dir string, m *manifest.Manifest) {
	t.Helper()
	plexDir := filepath.Join(dir, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0755))
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "manifest.json"), data, 0644))
}

func TestCompile_GeneratesIndexAndSidebar(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, testManifest())

	c := NewCompiler(tmp, false)
	result, err := c.Compile()
	require.NoError(t, err)

	assert.False(t, result.DryRun)
	assert.Len(t, result.FilesGenerated, 2)
	assert.Empty(t, result.FilesSkipped)

	// Verify _index.md
	indexBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	index := string(indexBytes)

	expectedIndex := `# Wiki Index

## Architecture
- [[architecture-overview]] — Architecture Overview
- [[data-model]] — Data Model

## Decisions
- [[adr-001]] — ADR-001: Language Choice
- [[adr-002]] — ADR-002: Database Choice

## Modules
- [[api-gateway]] — API Gateway
- [[auth-module]] — Auth Module
`
	assert.Equal(t, expectedIndex, index)

	// Verify _Sidebar.md
	sidebarBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_Sidebar.md"))
	require.NoError(t, err)
	sidebar := string(sidebarBytes)

	expectedSidebar := `**[[Home]]**

**Architecture**
- [[architecture-overview]]
- [[data-model]]

**Decisions**
- [[adr-001]]
- [[adr-002]]

**Modules**
- [[api-gateway]]
- [[auth-module]]
`
	assert.Equal(t, expectedSidebar, sidebar)
}

func TestCompile_Determinism(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, testManifest())

	c := NewCompiler(tmp, false)

	r1, err := c.Compile()
	require.NoError(t, err)

	idx1, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	sb1, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_Sidebar.md"))
	require.NoError(t, err)

	r2, err := c.Compile()
	require.NoError(t, err)

	idx2, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	sb2, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_Sidebar.md"))
	require.NoError(t, err)

	assert.Equal(t, string(idx1), string(idx2), "_index.md should be identical across runs")
	assert.Equal(t, string(sb1), string(sb2), "_Sidebar.md should be identical across runs")
	assert.Equal(t, r1.FilesGenerated, r2.FilesGenerated)
}

func TestCompile_DryRun(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, testManifest())

	c := NewCompiler(tmp, true)
	result, err := c.Compile()
	require.NoError(t, err)

	assert.True(t, result.DryRun)
	assert.Empty(t, result.FilesGenerated)
	assert.Len(t, result.FilesSkipped, 2)

	// Files must NOT exist on disk.
	_, err = os.Stat(filepath.Join(tmp, ".wiki", "_index.md"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(tmp, ".wiki", "_Sidebar.md"))
	assert.True(t, os.IsNotExist(err))
}

func TestCompile_EmptyManifest(t *testing.T) {
	tmp := t.TempDir()
	writeManifest(t, tmp, manifest.NewEmptyManifest())

	c := NewCompiler(tmp, false)
	result, err := c.Compile()
	require.NoError(t, err)
	assert.Len(t, result.FilesGenerated, 2)

	indexBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Wiki Index\n", string(indexBytes))

	sidebarBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_Sidebar.md"))
	require.NoError(t, err)
	assert.Equal(t, "**[[Home]]**\n", string(sidebarBytes))
}

func TestCompile_AlphabeticalSorting(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "concepts/zebra.md", Title: "Zebra", Section: "Concepts"},
			{WikiPath: "concepts/alpha.md", Title: "Alpha", Section: "Concepts"},
			{WikiPath: "concepts/middle.md", Title: "Middle", Section: "Concepts"},
		},
	}
	tmp := t.TempDir()
	writeManifest(t, tmp, m)

	c := NewCompiler(tmp, false)
	_, err := c.Compile()
	require.NoError(t, err)

	indexBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	index := string(indexBytes)

	expectedIndex := `# Wiki Index

## Concepts
- [[alpha]] — Alpha
- [[middle]] — Middle
- [[zebra]] — Zebra
`
	assert.Equal(t, expectedIndex, index)
}

func TestCompile_NoManifestFile(t *testing.T) {
	tmp := t.TempDir()
	// No manifest file — should get empty manifest behavior.
	c := NewCompiler(tmp, false)
	result, err := c.Compile()
	require.NoError(t, err)
	assert.Len(t, result.FilesGenerated, 2)
}

func TestCompile_UncategorizedSection(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "orphan.md", Title: "Orphan Page", Section: ""},
		},
	}
	tmp := t.TempDir()
	writeManifest(t, tmp, m)

	c := NewCompiler(tmp, false)
	_, err := c.Compile()
	require.NoError(t, err)

	indexBytes, err := os.ReadFile(filepath.Join(tmp, ".wiki", "_index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(indexBytes), "## Uncategorized")
	assert.Contains(t, string(indexBytes), "[[orphan]] — Orphan Page")
}

func TestSlugFromPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"modules/auth-module.md", "auth-module"},
		{"architecture/data-model.md", "data-model"},
		{"top-level.md", "top-level"},
		{"deeply/nested/page.md", "page"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, slugFromPath(tt.input))
		})
	}
}
