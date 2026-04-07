package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSyncFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create source file
	srcDir := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0644))

	// Create wiki dir
	wikiDir := filepath.Join(root, ".wiki", "modules")
	require.NoError(t, os.MkdirAll(wikiDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".wiki", "Home.md"), []byte("# Home\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wikiDir, "auth-module.md"), []byte("# Auth Module\n"), 0644))

	// Create plexium dir and config
	plexDir := filepath.Join(root, ".plexium")
	require.NoError(t, os.MkdirAll(plexDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(plexDir, "config.yml"), []byte(`
version: 1
repo:
  name: test-repo
  language: go
sources:
  include: ["**/*.go"]
  exclude: ["vendor/**"]
wiki:
  root: .wiki
`), 0644))

	// Create manifest with a hash that matches original content
	hash, err := manifest.ComputeHash(filepath.Join(srcDir, "auth.go"))
	require.NoError(t, err)

	mgr, err := manifest.NewManager(filepath.Join(plexDir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&manifest.Manifest{
		Version: 1,
		Pages: []manifest.PageEntry{
			{
				WikiPath:  "modules/auth-module.md",
				Title:     "Auth Module",
				Ownership: "managed",
				Section:   "Modules",
				SourceFiles: []manifest.SourceFile{
					{Path: "src/auth.go", Hash: hash},
				},
				LastUpdated: time.Now().UTC().Format(time.RFC3339),
			},
		},
		UnmanagedPages: []manifest.UnmanagedEntry{},
	}))

	return root
}

func TestSync_NoChanges(t *testing.T) {
	root := setupSyncFixture(t)

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.SourceFilesChecked)
	assert.Equal(t, 0, result.StalePages, "no source changes → no stale pages")
	assert.Equal(t, 0, result.HashesUpdated)
	assert.False(t, result.NavRecompiled, "no stale pages → no recompile needed")
}

func TestSync_DetectsStaleAndUpdates(t *testing.T) {
	root := setupSyncFixture(t)

	// Modify source file to make it stale
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Login() {}\n"),
		0644,
	))

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StalePages, "modified source → 1 stale page")
	assert.Equal(t, 1, result.HashesUpdated, "hash should be updated")
	assert.True(t, result.NavRecompiled, "nav should be recompiled after updates")
	assert.Contains(t, result.PagesAffected, "modules/auth-module.md")

	// Running sync again should find no stale pages (hash was updated)
	result2, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   false,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result2.StalePages, "second sync should find nothing stale")
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	root := setupSyncFixture(t)

	// Modify source
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "src", "auth.go"),
		[]byte("package auth\n\nfunc Changed() {}\n"),
		0644,
	))

	// Read manifest before sync
	mgr, _ := manifest.NewManager(manifest.DefaultPath(root))
	before, _ := mgr.Load()
	oldHash := before.Pages[0].SourceFiles[0].Hash

	cfg, err := config.LoadFromDir(root)
	require.NoError(t, err)

	result, err := Run(Options{
		RepoRoot: root,
		Config:   cfg,
		DryRun:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StalePages)
	assert.True(t, result.DryRun)
	assert.Equal(t, 0, result.HashesUpdated, "dry-run should not update hashes")
	assert.False(t, result.NavRecompiled, "dry-run should not recompile nav")

	// Verify manifest was NOT changed
	after, _ := mgr.Load()
	assert.Equal(t, oldHash, after.Pages[0].SourceFiles[0].Hash,
		"dry-run should not modify manifest hashes")
}
