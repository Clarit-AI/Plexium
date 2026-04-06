package beads

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPage = `---
title: "Test Module"
ownership: managed
last-updated: 2026-01-01
beads-ids:
  - "plexium-m4-task-12"
  - "plexium-m4-task-15"
---

# Test Module

Content here.
`

const testPageNoBeads = `---
title: "Clean Page"
ownership: managed
last-updated: 2026-01-01
---

# Clean Page

No beads here.
`

const testPageSingleBead = `---
title: "Single Bead"
ownership: managed
beads-ids: "plexium-m1-task-1"
---

# Single Bead

One task linked.
`

func setupWiki(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wikiRoot := filepath.Join(dir, ".wiki")
	require.NoError(t, os.MkdirAll(filepath.Join(wikiRoot, "modules"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(wikiRoot, "decisions"), 0755))

	// Create test pages
	require.NoError(t, os.WriteFile(filepath.Join(wikiRoot, "modules", "auth.md"), []byte(testPage), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiRoot, "modules", "api.md"), []byte(testPageNoBeads), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiRoot, "decisions", "adr-001.md"), []byte(testPageSingleBead), 0644))

	// Create underscore files that should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(wikiRoot, "_schema.md"), []byte("---\ntitle: Schema\n---\n\nSchema content.\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiRoot, "_index.md"), []byte("---\ntitle: Index\n---\n\nIndex content.\n"), 0644))

	return wikiRoot
}

func TestNewLinker(t *testing.T) {
	l := NewLinker("/tmp/wiki")
	assert.Equal(t, "/tmp/wiki", l.WikiRoot)
	assert.Equal(t, "bd", l.BdPath, "BdPath should default to 'bd'")
}

func TestReadFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, os.WriteFile(path, []byte(testPage), 0644))

	fm, body, err := readFrontmatter(path)
	require.NoError(t, err)

	assert.Equal(t, "Test Module", fm["title"])
	assert.Equal(t, "managed", fm["ownership"])
	assert.Contains(t, body, "# Test Module")
	assert.Contains(t, body, "Content here.")
}

func TestReadFrontmatter_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, os.WriteFile(path, []byte("# No frontmatter\n"), 0644))

	_, _, err := readFrontmatter(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no frontmatter found")
}

func TestReadFrontmatter_UnclosedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, os.WriteFile(path, []byte("---\ntitle: Test\nbody without closing\n"), 0644))

	_, _, err := readFrontmatter(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed frontmatter")
}

func TestWriteFrontmatter_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, os.WriteFile(path, []byte(testPage), 0644))

	// Read original
	fm, body, err := readFrontmatter(path)
	require.NoError(t, err)

	// Write back
	require.NoError(t, writeFrontmatter(path, fm, body))

	// Read again
	fm2, body2, err := readFrontmatter(path)
	require.NoError(t, err)

	assert.Equal(t, fm["title"], fm2["title"])
	assert.Equal(t, fm["ownership"], fm2["ownership"])
	assert.Equal(t, body, body2, "body content should be preserved exactly")

	// Verify beads-ids survived round-trip
	ids := getBeadsIDs(fm2)
	assert.Equal(t, []string{"plexium-m4-task-12", "plexium-m4-task-15"}, ids)
}

func TestGetBeadsIDs(t *testing.T) {
	tests := []struct {
		name     string
		fm       map[string]interface{}
		expected []string
	}{
		{
			name:     "interface slice",
			fm:       map[string]interface{}{"beads-ids": []interface{}{"task-1", "task-2"}},
			expected: []string{"task-1", "task-2"},
		},
		{
			name:     "string slice",
			fm:       map[string]interface{}{"beads-ids": []string{"task-1", "task-2"}},
			expected: []string{"task-1", "task-2"},
		},
		{
			name:     "single string",
			fm:       map[string]interface{}{"beads-ids": "task-1"},
			expected: []string{"task-1"},
		},
		{
			name:     "missing key",
			fm:       map[string]interface{}{"title": "Test"},
			expected: nil,
		},
		{
			name:     "empty slice",
			fm:       map[string]interface{}{"beads-ids": []interface{}{}},
			expected: []string{},
		},
		{
			name:     "nil map",
			fm:       map[string]interface{}{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBeadsIDs(tt.fm)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestLinkTaskToPage(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	// Link a new task to a page that has no beads-ids
	result, err := l.LinkTaskToPage("plexium-m5-task-1", "modules/api.md")
	require.NoError(t, err)
	assert.Equal(t, "added", result.Action)
	assert.Equal(t, "plexium-m5-task-1", result.TaskID)
	assert.Equal(t, "modules/api.md", result.WikiPath)

	// Verify it was actually written
	mapping, err := l.GetPageTasks("modules/api.md")
	require.NoError(t, err)
	assert.Contains(t, mapping.TaskIDs, "plexium-m5-task-1")
}

func TestLinkTaskToPage_Idempotent(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	// Task already exists in testPage
	result, err := l.LinkTaskToPage("plexium-m4-task-12", "modules/auth.md")
	require.NoError(t, err)
	assert.Equal(t, "already-linked", result.Action)

	// Verify no duplicate was added
	mapping, err := l.GetPageTasks("modules/auth.md")
	require.NoError(t, err)
	count := 0
	for _, id := range mapping.TaskIDs {
		if id == "plexium-m4-task-12" {
			count++
		}
	}
	assert.Equal(t, 1, count, "should not have duplicated the task ID")
}

func TestLinkTaskToPage_NonExistentPage(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	_, err := l.LinkTaskToPage("task-1", "modules/nonexistent.md")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestUnlinkTaskFromPage(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	// Remove one of two tasks
	result, err := l.UnlinkTaskFromPage("plexium-m4-task-12", "modules/auth.md")
	require.NoError(t, err)
	assert.Equal(t, "removed", result.Action)

	// Verify it was removed
	mapping, err := l.GetPageTasks("modules/auth.md")
	require.NoError(t, err)
	assert.NotContains(t, mapping.TaskIDs, "plexium-m4-task-12")
	assert.Contains(t, mapping.TaskIDs, "plexium-m4-task-15")
}

func TestUnlinkTaskFromPage_NotLinked(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	result, err := l.UnlinkTaskFromPage("nonexistent-task", "modules/auth.md")
	require.NoError(t, err)
	assert.Equal(t, "not-linked", result.Action)
}

func TestUnlinkTaskFromPage_RemovesLastID(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	// The single-bead page has only one task
	result, err := l.UnlinkTaskFromPage("plexium-m1-task-1", "decisions/adr-001.md")
	require.NoError(t, err)
	assert.Equal(t, "removed", result.Action)

	// Verify beads-ids key is removed entirely
	mapping, err := l.GetPageTasks("decisions/adr-001.md")
	require.NoError(t, err)
	assert.Empty(t, mapping.TaskIDs)
}

func TestGetTaskPages(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mapping, err := l.GetTaskPages("plexium-m4-task-12")
	require.NoError(t, err)
	assert.Equal(t, "plexium-m4-task-12", mapping.TaskID)
	assert.Equal(t, []string{"modules/auth.md"}, mapping.WikiPaths)
}

func TestGetTaskPages_NotFound(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mapping, err := l.GetTaskPages("nonexistent-task")
	require.NoError(t, err)
	assert.Empty(t, mapping.WikiPaths)
}

func TestGetPageTasks(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mapping, err := l.GetPageTasks("modules/auth.md")
	require.NoError(t, err)
	assert.Equal(t, "modules/auth.md", mapping.WikiPath)
	assert.Equal(t, []string{"plexium-m4-task-12", "plexium-m4-task-15"}, mapping.TaskIDs)
}

func TestGetPageTasks_NoBeads(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mapping, err := l.GetPageTasks("modules/api.md")
	require.NoError(t, err)
	assert.Empty(t, mapping.TaskIDs)
}

func TestScanAllLinks(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mappings, err := l.ScanAllLinks()
	require.NoError(t, err)

	// Should find 3 unique task IDs across the wiki
	taskIDs := make(map[string]bool)
	for _, m := range mappings {
		taskIDs[m.TaskID] = true
	}
	assert.Contains(t, taskIDs, "plexium-m4-task-12")
	assert.Contains(t, taskIDs, "plexium-m4-task-15")
	assert.Contains(t, taskIDs, "plexium-m1-task-1")
	assert.Len(t, taskIDs, 3)

	// Verify specific mapping
	for _, m := range mappings {
		if m.TaskID == "plexium-m4-task-12" {
			assert.Equal(t, []string{"modules/auth.md"}, m.WikiPaths)
		}
	}
}

func TestScanAllLinks_SkipsUnderscoreFiles(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	mappings, err := l.ScanAllLinks()
	require.NoError(t, err)

	// Ensure no paths contain underscore-prefixed files
	for _, m := range mappings {
		for _, p := range m.WikiPaths {
			base := filepath.Base(p)
			assert.False(t, strings.HasPrefix(base, "_"),
				"should skip underscore files, got: %s", p)
		}
	}
}

func TestScanAllLinks_EmptyWiki(t *testing.T) {
	dir := t.TempDir()
	wikiRoot := filepath.Join(dir, ".wiki")
	require.NoError(t, os.MkdirAll(wikiRoot, 0755))

	l := NewLinker(wikiRoot)
	mappings, err := l.ScanAllLinks()
	require.NoError(t, err)
	assert.Empty(t, mappings)
}

func TestLinkAndScan_Integration(t *testing.T) {
	wikiRoot := setupWiki(t)
	l := NewLinker(wikiRoot)

	// Link a new task to multiple pages
	_, err := l.LinkTaskToPage("plexium-m9-task-1", "modules/auth.md")
	require.NoError(t, err)
	_, err = l.LinkTaskToPage("plexium-m9-task-1", "modules/api.md")
	require.NoError(t, err)

	// Scan and verify
	mappings, err := l.ScanAllLinks()
	require.NoError(t, err)

	for _, m := range mappings {
		if m.TaskID == "plexium-m9-task-1" {
			sort.Strings(m.WikiPaths)
			assert.Equal(t, []string{"modules/api.md", "modules/auth.md"}, m.WikiPaths)
			return
		}
	}
	t.Fatal("plexium-m9-task-1 not found in scan results")
}

func TestWriteFrontmatter_PreservesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	body := "\n# Complex Body\n\nThis has **bold** and `code` and [[wiki-links]].\n\n```go\nfunc main() {}\n```\n"
	original := "---\ntitle: \"Test\"\n---" + body
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	fm, bodyRead, err := readFrontmatter(path)
	require.NoError(t, err)
	assert.Equal(t, body, bodyRead)

	// Add beads-ids and write back
	fm["beads-ids"] = []string{"task-1"}
	require.NoError(t, writeFrontmatter(path, fm, bodyRead))

	// Re-read and verify body preserved
	_, bodyAfter, err := readFrontmatter(path)
	require.NoError(t, err)
	assert.Equal(t, body, bodyAfter, "body must be preserved exactly after frontmatter modification")
}
