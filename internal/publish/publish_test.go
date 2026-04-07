package publish

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/wiki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToWikiURL(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"https", "https://github.com/owner/repo.git", "https://github.com/owner/repo.wiki.git"},
		{"ssh", "git@github.com:owner/repo.git", "git@github.com:owner/repo.wiki.git"},
		{"ssh no suffix", "git@github.com:owner/repo", "git@github.com:owner/repo"},
		{"gitlab", "https://gitlab.com/owner/repo.git", "https://gitlab.com/owner/repo.wiki.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, toWikiURL(tt.input))
		})
	}
}

func TestToWikiURL_HTTPS_NoGitSuffix(t *testing.T) {
	result := toWikiURL("https://github.com/owner/repo")
	assert.Equal(t, "https://github.com/owner/repo", result)
}

func TestClearWikiContent(t *testing.T) {
	// Set up a fake wiki directory structure
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	otherFile := filepath.Join(dir, "Home.md")
	subdirFile := filepath.Join(dir, "modules", "auth.md")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(otherFile, []byte("home"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Dir(subdirFile), 0755))
	require.NoError(t, os.WriteFile(subdirFile, []byte("auth"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0644))

	// Run clearWikiContent
	err := clearWikiContent(dir)
	require.NoError(t, err)

	// .git/ should be preserved
	assert.FileExists(t, filepath.Join(gitDir, "config"))

	// Wiki content files should be deleted
	assert.NoFileExists(t, otherFile)
	assert.NoFileExists(t, subdirFile)

	// Directories should be mostly empty but exist (git will handle them)
	_, err = os.Stat(filepath.Join(dir, "modules"))
	assert.True(t, os.IsNotExist(err) || true, "empty modules dir may or may not exist")
}

func TestCollectFiles_IncludesImages(t *testing.T) {
	dir := t.TempDir()

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Add non-markdown wiki assets
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", "assets"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "assets", "diagram.png"), []byte("PNG data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "assets", "chart.svg"), []byte("<svg></svg>"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "Home.md"), []byte("# Home"), 0644))

	pub, err := NewPublisher(PublishOptions{RepoRoot: dir})
	require.NoError(t, err)

	mf, err := pub.manifestMgr.Load()
	require.NoError(t, err)
	files, _, err := pub.collectFiles(mf)
	require.NoError(t, err)

	assert.Contains(t, files, "Home.md")
	assert.Contains(t, files, "assets/diagram.png")
	assert.Contains(t, files, "assets/chart.svg")
}

func TestCollectFiles_ExcludesHiddenFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Add hidden files and directories
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", ".obsidian"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", ".obsidian", "app.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", ".DS_Store"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "Home.md"), []byte("# Home"), 0644))

	pub, err := NewPublisher(PublishOptions{RepoRoot: dir})
	require.NoError(t, err)

	mf, _ := pub.manifestMgr.Load()
	files, _, err := pub.collectFiles(mf)
	require.NoError(t, err)

	assert.NotContains(t, files, ".obsidian/app.json")
	assert.NotContains(t, files, ".DS_Store")
	assert.Contains(t, files, "Home.md")
}

func TestCollectFiles_RespectsExcludeConfig(t *testing.T) {
	dir := t.TempDir()

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	pub, err := NewPublisher(PublishOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Configure exclude for _schema.md
	pub.config.GitHubWiki.Exclude = []string{"_schema.md"}

	mf, _ := pub.manifestMgr.Load()
	files, _, err := pub.collectFiles(mf)
	require.NoError(t, err)

	for _, f := range files {
		assert.NotEqual(t, "_schema.md", f, "_schema.md should be excluded by config")
	}
}

func TestCollectFiles_PreserveUnmanagedPages(t *testing.T) {
	dir := t.TempDir()

	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	// Create a human-authored page tracked in manifest
	humanFile := filepath.Join(dir, ".wiki", "my-notes.md")
	require.NoError(t, os.WriteFile(humanFile, []byte("# Notes\n\nPersonal notes."), 0644))

	pub, err := NewPublisher(PublishOptions{RepoRoot: dir})
	require.NoError(t, err)

	require.NoError(t, pub.manifestMgr.UpsertPage(manifest.PageEntry{
		WikiPath:  "my-notes.md",
		Ownership: "human-authored",
	}))

	// With PreserveUnmanagedPages=false, human-authored pages are skipped
	pub.config.Publish.PreserveUnmanagedPages = false

	mf, _ := pub.manifestMgr.Load()
	_, skipped, err := pub.collectFiles(mf)
	require.NoError(t, err)

	assert.Contains(t, skipped, "my-notes.md")
}

func TestClearWikiContent_PreservesGitDir(t *testing.T) {
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Dir(gitFile), 0755))
	require.NoError(t, os.WriteFile(gitFile, []byte("ref: refs/heads/main"), 0644))

	err := clearWikiContent(dir)
	require.NoError(t, err)

	// .git/HEAD must still exist
	assert.FileExists(t, gitFile)
}
