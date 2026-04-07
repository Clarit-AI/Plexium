package validation

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/lint"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// D1: Manifest ordering is deterministic (sorted by WikiPath)
// =============================================================================

func TestDeterminism_ManifestPagesSortedByWikiPath(t *testing.T) {
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	// Add pages in reverse order
	pages := []manifest.PageEntry{
		{WikiPath: "modules/zeta.md", Title: "Zeta", Ownership: "managed", Section: "Modules"},
		{WikiPath: "concepts/alpha.md", Title: "Alpha", Ownership: "managed", Section: "Concepts"},
		{WikiPath: "decisions/middle.md", Title: "Middle", Ownership: "managed", Section: "Decisions"},
		{WikiPath: "architecture/beta.md", Title: "Beta", Ownership: "managed", Section: "Architecture"},
	}

	m := manifest.NewEmptyManifest()
	m.Pages = pages
	err = mgr.Save(m)
	require.NoError(t, err)

	// Load and verify sorted
	loaded, err := mgr.Load()
	require.NoError(t, err)

	for i := 1; i < len(loaded.Pages); i++ {
		assert.True(t, loaded.Pages[i-1].WikiPath < loaded.Pages[i].WikiPath,
			"pages not sorted: %s should come before %s",
			loaded.Pages[i-1].WikiPath, loaded.Pages[i].WikiPath)
	}
}

func TestDeterminism_ManifestSaveIsIdempotent(t *testing.T) {
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "b.md", Title: "B", Ownership: "managed"},
			{WikiPath: "a.md", Title: "A", Ownership: "managed"},
			{WikiPath: "c.md", Title: "C", Ownership: "managed"},
		},
	}

	// Save multiple times and verify identical output each time
	var outputs []string
	for i := 0; i < 5; i++ {
		err := mgr.Save(m)
		require.NoError(t, err)
		data := f.ReadFile(".plexium/manifest.json")
		outputs = append(outputs, data)
	}

	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i],
			"manifest save produced different output on run %d vs run 0", i)
	}
}

// =============================================================================
// D2: Hash stability
// =============================================================================

func TestDeterminism_HashSameContentSameResult(t *testing.T) {
	f := NewFixture(t)
	f.WriteFile("test.go", "package main\n\nfunc main() {}\n")

	path := filepath.Join(f.Root, "test.go")

	var hashes []string
	for i := 0; i < 10; i++ {
		h, err := manifest.ComputeHash(path)
		require.NoError(t, err)
		hashes = append(hashes, h)
	}

	for i := 1; i < len(hashes); i++ {
		assert.Equal(t, hashes[0], hashes[i],
			"hash of same file differs on call %d", i)
	}
}

func TestDeterminism_HashDifferentContentDifferentResult(t *testing.T) {
	f := NewFixture(t)
	f.WriteFile("a.go", "package a\n")
	f.WriteFile("b.go", "package b\n")

	h1, err := manifest.ComputeHash(filepath.Join(f.Root, "a.go"))
	require.NoError(t, err)
	h2, err := manifest.ComputeHash(filepath.Join(f.Root, "b.go"))
	require.NoError(t, err)

	assert.NotEqual(t, h1, h2, "different files should produce different hashes")
}

// =============================================================================
// D3: Compile output stability
// =============================================================================

func TestDeterminism_CompileOutputStableAcrossRuns(t *testing.T) {
	f := PopulatedFixture(t)

	var indexOutputs []string
	var sidebarOutputs []string

	for i := 0; i < 5; i++ {
		c := compile.NewCompiler(f.Root, false)
		_, err := c.Compile()
		require.NoError(t, err)

		indexOutputs = append(indexOutputs, f.ReadFile(".wiki/_index.md"))
		sidebarOutputs = append(sidebarOutputs, f.ReadFile(".wiki/_Sidebar.md"))
	}

	for i := 1; i < len(indexOutputs); i++ {
		assert.Equal(t, indexOutputs[0], indexOutputs[i],
			"_index.md differs between compile run 0 and %d", i)
		assert.Equal(t, sidebarOutputs[0], sidebarOutputs[i],
			"_Sidebar.md differs between compile run 0 and %d", i)
	}
}

func TestDeterminism_CompileSectionOrdering(t *testing.T) {
	f := InitializedFixture(t)

	// Create pages across multiple sections
	m := &manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{WikiPath: "modules/z-mod.md", Title: "Z Module", Ownership: "managed", Section: "Modules"},
			{WikiPath: "modules/a-mod.md", Title: "A Module", Ownership: "managed", Section: "Modules"},
			{WikiPath: "concepts/z-concept.md", Title: "Z Concept", Ownership: "managed", Section: "Concepts"},
			{WikiPath: "concepts/a-concept.md", Title: "A Concept", Ownership: "managed", Section: "Concepts"},
			{WikiPath: "decisions/mid.md", Title: "Mid Decision", Ownership: "managed", Section: "Decisions"},
			{WikiPath: "architecture/arch.md", Title: "Arch", Ownership: "managed", Section: "Architecture"},
		},
	}
	f.WriteManifest(m)

	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	index := f.ReadFile(".wiki/_index.md")

	// Sections should appear in alphabetical order
	assert.Contains(t, index, "## Architecture")
	assert.Contains(t, index, "## Concepts")
	assert.Contains(t, index, "## Decisions")
	assert.Contains(t, index, "## Modules")

	// Within sections, pages should be sorted by title
	assert.Contains(t, index, "[[a-concept]]")
	assert.Contains(t, index, "[[z-concept]]")
}

// =============================================================================
// D4: Lint result determinism
// =============================================================================

func TestDeterminism_LintResultsStableAcrossRuns(t *testing.T) {
	f := BrokenLinksFixture(t)

	cfg, err := config.Load(filepath.Join(f.Root, ".plexium", "config.yml"))
	require.NoError(t, err)

	var reports []string
	for i := 0; i < 3; i++ {
		linter := lint.NewLinter(f.Root, cfg)
		report, err := linter.RunDeterministic()
		require.NoError(t, err)

		// Zero out timestamp for comparison
		report.Timestamp = "FIXED"
		data, err := json.Marshal(report)
		require.NoError(t, err)
		reports = append(reports, string(data))
	}

	for i := 1; i < len(reports); i++ {
		assert.Equal(t, reports[0], reports[i],
			"lint report differs between run 0 and %d", i)
	}
}

// =============================================================================
// D5: Empty manifest produces stable empty outputs
// =============================================================================

func TestDeterminism_EmptyManifestCompileStable(t *testing.T) {
	f := InitializedFixture(t)

	// Empty manifest should produce stable (minimal) nav files
	c := compile.NewCompiler(f.Root, false)
	_, err := c.Compile()
	require.NoError(t, err)

	index := f.ReadFile(".wiki/_index.md")
	sidebar := f.ReadFile(".wiki/_Sidebar.md")

	// Should be minimal but valid
	assert.Contains(t, index, "# Wiki Index")
	assert.Contains(t, sidebar, "**[[Home]]**")

	// Run again — identical
	c2 := compile.NewCompiler(f.Root, false)
	_, err = c2.Compile()
	require.NoError(t, err)

	assert.Equal(t, index, f.ReadFile(".wiki/_index.md"))
	assert.Equal(t, sidebar, f.ReadFile(".wiki/_Sidebar.md"))
}

// =============================================================================
// D6: Manifest JSON shape is stable
// =============================================================================

func TestDeterminism_ManifestJSONShape(t *testing.T) {
	f := NewFixture(t)
	f.MkDir(".plexium")

	mgr, err := manifest.NewManager(filepath.Join(f.Root, ".plexium", "manifest.json"))
	require.NoError(t, err)

	m := manifest.NewEmptyManifest()
	err = mgr.Save(m)
	require.NoError(t, err)

	data := f.ReadFile(".plexium/manifest.json")

	// Verify JSON is well-formed and has expected top-level keys
	var raw map[string]json.RawMessage
	err = json.Unmarshal([]byte(data), &raw)
	require.NoError(t, err)

	expectedKeys := []string{"version", "lastProcessedCommit", "lastPublishTimestamp", "pages", "unmanagedPages"}
	for _, key := range expectedKeys {
		_, exists := raw[key]
		assert.True(t, exists, "manifest JSON missing key: %s", key)
	}
}
