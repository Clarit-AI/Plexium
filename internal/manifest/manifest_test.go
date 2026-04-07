package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmptyManifest(t *testing.T) {
	m := NewEmptyManifest()
	assert.Equal(t, 1, m.Version)
	assert.NotNil(t, m.Pages)
	assert.Empty(t, m.Pages)
	assert.NotNil(t, m.UnmanagedPages)
	assert.Empty(t, m.UnmanagedPages)
}

func TestManager_Load_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	mgr, err := NewManager(path)
	require.NoError(t, err)

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, m.Version)
	assert.Empty(t, m.Pages)
}

func TestManager_Load_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// Write initial manifest
	initial := &Manifest{
		Version: 1,
		Pages: []PageEntry{
			{WikiPath: "modules/auth.md", Title: "Auth", Ownership: "managed"},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	require.NoError(t, os.WriteFile(path, data, 0644))

	mgr, err := NewManager(path)
	require.NoError(t, err)

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m.Pages, 1)
	assert.Equal(t, "Auth", m.Pages[0].Title)
}

func TestManager_Save_And_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "manifest.json")

	mgr, err := NewManager(path)
	require.NoError(t, err)

	m := &Manifest{
		Version: 1,
		Pages: []PageEntry{
			{WikiPath: "modules/auth.md", Title: "Auth", Ownership: "managed", Section: "Modules"},
		},
	}
	require.NoError(t, mgr.Save(m))

	// Verify file was created
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Reload
	loaded, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, m.Pages, loaded.Pages)
}

func TestManager_Save_Nil(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	err = mgr.Save(nil)
	assert.Error(t, err)
}

func TestManager_UpsertPage_Add(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	entry := PageEntry{
		WikiPath:  "modules/auth.md",
		Title:     "Auth",
		Ownership: "managed",
		Section:   "Modules",
	}
	require.NoError(t, mgr.UpsertPage(entry))

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m.Pages, 1)
	assert.Equal(t, "Auth", m.Pages[0].Title)
}

func TestManager_UpsertPage_Update(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	// Add initial
	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/auth.md", Title: "Auth", Ownership: "managed",
	}))

	// Update
	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/auth.md", Title: "Authentication", Ownership: "managed",
	}))

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m.Pages, 1)
	assert.Equal(t, "Authentication", m.Pages[0].Title)
}

func TestManager_UpsertPage_ProtectsHumanAuthored(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	// Add human-authored page
	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath:  "decisions/my-decision.md",
		Title:     "My Decision",
		Ownership: "human-authored",
	}))

	// Try to overwrite with managed
	err = mgr.UpsertPage(PageEntry{
		WikiPath:  "decisions/my-decision.md",
		Title:     "Updated",
		Ownership: "managed",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "human-authored")
}

func TestManager_RemovePage(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{WikiPath: "a.md", Ownership: "managed"}))
	require.NoError(t, mgr.UpsertPage(PageEntry{WikiPath: "b.md", Ownership: "managed"}))

	require.NoError(t, mgr.RemovePage("a.md"))

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m.Pages, 1)
	assert.Equal(t, "b.md", m.Pages[0].WikiPath)
}

func TestManager_PagesFromSource(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/auth.md",
		SourceFiles: []SourceFile{
			{Path: "src/auth/login.go", Hash: "abc123"},
			{Path: "src/auth/middleware.go", Hash: "def456"},
		},
	}))
	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/user.md",
		SourceFiles: []SourceFile{
			{Path: "src/user/model.go", Hash: "ghi789"},
		},
	}))

	pages, err := mgr.PagesFromSource("src/auth/login.go")
	require.NoError(t, err)
	assert.Len(t, pages, 1)
	assert.Equal(t, "modules/auth.md", pages[0].WikiPath)

	// Glob-style match
	pages, err = mgr.PagesFromSource("src/auth/**")
	require.NoError(t, err)
	assert.Len(t, pages, 1)
}

func TestManager_SourcesFromPage(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/auth.md",
		SourceFiles: []SourceFile{
			{Path: "src/auth/login.go", Hash: "abc123"},
		},
	}))

	sources, err := mgr.SourcesFromPage("modules/auth.md")
	require.NoError(t, err)
	assert.Len(t, sources, 1)
	assert.Equal(t, "src/auth/login.go", sources[0].Path)

	// Non-existent page
	sources, err = mgr.SourcesFromPage("nonexistent.md")
	require.NoError(t, err)
	assert.Nil(t, sources)
}

func TestManager_IsManaged(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{WikiPath: "modules/auth.md", Ownership: "managed"}))

	managed, err := mgr.IsManaged("modules/auth.md")
	require.NoError(t, err)
	assert.True(t, managed)

	managed, err = mgr.IsManaged("nonexistent.md")
	require.NoError(t, err)
	assert.False(t, managed)
}

func TestManager_GetPage(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath: "modules/auth.md",
		Title:    "Auth",
		Section:  "Modules",
	}))

	page, err := mgr.GetPage("modules/auth.md")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Auth", page.Title)

	page, err = mgr.GetPage("nonexistent.md")
	require.NoError(t, err)
	assert.Nil(t, page)
}

func TestManager_AddUnmanaged(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.AddUnmanaged(UnmanagedEntry{
		WikiPath:  "notes.md",
		FirstSeen: "2026-04-05",
		Ownership: "human-authored",
	}))

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m.UnmanagedPages, 1)
	assert.Equal(t, "notes.md", m.UnmanagedPages[0].WikiPath)

	// Adding duplicate should be no-op
	require.NoError(t, mgr.AddUnmanaged(UnmanagedEntry{
		WikiPath:  "notes.md",
		FirstSeen: "2026-04-05",
	}))

	m, _ = mgr.Load()
	assert.Len(t, m.UnmanagedPages, 1)
}

func TestManager_RemoveUnmanaged(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.AddUnmanaged(UnmanagedEntry{WikiPath: "a.md"}))
	require.NoError(t, mgr.AddUnmanaged(UnmanagedEntry{WikiPath: "b.md"}))
	require.NoError(t, mgr.RemoveUnmanaged("a.md"))

	m, _ := mgr.Load()
	assert.Len(t, m.UnmanagedPages, 1)
	assert.Equal(t, "b.md", m.UnmanagedPages[0].WikiPath)
}

func TestManager_DetectStalePages(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	// Create a temp file with known content
	file1 := filepath.Join(dir, "src", "auth.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(file1), 0755))
	require.NoError(t, os.WriteFile(file1, []byte("package auth"), 0644))

	hash1, err := ComputeHash(file1)
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath:  "modules/auth.md",
		Ownership: "managed",
		SourceFiles: []SourceFile{
			{Path: file1, Hash: hash1},
		},
	}))

	// No changes — no stale pages
	stale, err := mgr.DetectStalePages(ComputeHash)
	require.NoError(t, err)
	assert.Empty(t, stale)

	// Modify the file
	require.NoError(t, os.WriteFile(file1, []byte("package auth // modified"), 0644))

	stale, err = mgr.DetectStalePages(ComputeHash)
	require.NoError(t, err)
	assert.Len(t, stale, 1)
	assert.Equal(t, "modules/auth.md", stale[0].WikiPath)
}

func TestManager_DetectStalePages_SkipsHumanAuthored(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpsertPage(PageEntry{
		WikiPath:  "decisions/adr.md",
		Ownership: "human-authored",
		SourceFiles: []SourceFile{
			{Path: "/nonexistent/file.go", Hash: "old-hash"},
		},
	}))

	// Human-authored pages should never be flagged
	stale, err := mgr.DetectStalePages(ComputeHash)
	require.NoError(t, err)
	assert.Empty(t, stale)
}

func TestManager_UpdatePublishTimestamp(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpdatePublishTimestamp())

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.NotEmpty(t, m.LastPublishTimestamp)
}

func TestManager_UpdateProcessedCommit(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, mgr.UpdateProcessedCommit("abc123def"))

	m, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, "abc123def", m.LastProcessedCommit)
}

func TestManager_EmptyPath(t *testing.T) {
	_, err := NewManager("")
	assert.Error(t, err)
}

func TestDefaultPath(t *testing.T) {
	assert.Equal(t, filepath.Join("repo", ".plexium", "manifest.json"), DefaultPath("repo"))
}

func TestMatchGlobPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		// doublestar patterns
		{"src/auth/**", "src/auth/login.go", true},
		{"src/auth/**", "src/auth/middleware/token.go", true},
		{"src/auth/**", "src/user/model.go", false},
		{"**/login.go", "src/auth/login.go", true},
		{"**/login.go", "pkg/login.go", true},
		{"docs/**/*.md", "docs/api/reference.md", true},
		{"docs/**/*.md", "docs/readme.md", true},
		{"docs/**/*.md", "readme.md", false},
		// single star
		{"src/*.go", "src/auth.go", true},
		{"src/*.go", "src/auth/login.go", false},
		// exact match
		{"exact/path.go", "exact/path.go", true},
		{"exact/path.go", "other/path.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.match, matchGlob(tt.pattern, tt.path))
		})
	}
}

func TestSave_DeterministicOrdering(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	// Insert pages in non-sorted order
	pages := []PageEntry{
		{WikiPath: "zebra.md", Title: "Zebra"},
		{WikiPath: "alpha.md", Title: "Alpha"},
		{WikiPath: "modules/b.md", Title: "B"},
		{WikiPath: "modules/a.md", Title: "A"},
		{WikiPath: "beta.md", Title: "Beta"},
	}
	for _, p := range pages {
		require.NoError(t, mgr.UpsertPage(p))
	}

	// Reload and verify sorted order
	m, err := mgr.Load()
	require.NoError(t, err)

	paths := make([]string, len(m.Pages))
	for i, p := range m.Pages {
		paths[i] = p.WikiPath
	}

	// Verify they are sorted
	for i := 1; i < len(paths); i++ {
		assert.True(t, paths[i-1] < paths[i], "pages should be sorted alphabetically")
	}
}

func TestSave_ManifestIsolation(t *testing.T) {
	// Verify that calling Save on a manager does not affect the original
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mgr, err := NewManager(path)
	require.NoError(t, err)

	m1 := NewEmptyManifest()
	m1.Pages = []PageEntry{{WikiPath: "a.md"}}
	require.NoError(t, mgr.Save(m1))

	// Modify the returned manifest and save again
	m1.Pages[0].Title = "Modified"
	require.NoError(t, mgr.Save(m1))

	// Reload - should have the modified title, not a duplicate
	m2, err := mgr.Load()
	require.NoError(t, err)
	assert.Len(t, m2.Pages, 1)
	assert.Equal(t, "Modified", m2.Pages[0].Title)
}
