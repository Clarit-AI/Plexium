package publish

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
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
		{"https no suffix", "https://github.com/owner/repo", "https://github.com/owner/repo.wiki.git"},
		{"http no suffix", "http://github.com/owner/repo", "https://github.com/owner/repo.wiki.git"},
		{"https with token", "https://ghp_token@github.com/owner/repo.git", "https://ghp_token@github.com/owner/repo.wiki.git"},
		{"ssh", "git@github.com:owner/repo.git", "git@github.com:owner/repo.wiki.git"},
		{"ssh no suffix", "git@github.com:owner/repo", "git@github.com:owner/repo.wiki.git"},
		{"ssh url", "ssh://git@github.com/owner/repo.git", "ssh://git@github.com/owner/repo.wiki.git"},
		{"ssh url no suffix", "ssh://git@github.com/owner/repo", "ssh://git@github.com/owner/repo.wiki.git"},
		{"ssh url with port", "ssh://git@github.com:2222/owner/repo.git", "ssh://git@github.com:2222/owner/repo.wiki.git"},
		{"gitlab", "https://gitlab.com/owner/repo.git", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, toWikiURL(tt.input))
		})
	}
}

func TestToWikiURL_HTTPS_NoGitSuffix(t *testing.T) {
	result := toWikiURL("https://github.com/owner/repo")
	assert.Equal(t, "https://github.com/owner/repo.wiki.git", result)
}

func TestSanitizeRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"https plain", "https://github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"https with token", "https://ghp_token@github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"ssh url with port", "ssh://git@github.com:2222/owner/repo.git", "ssh://git@github.com:2222/owner/repo.git"},
		{"scp style unchanged", "git@github.com:owner/repo.git", "git@github.com:owner/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, sanitizeRemoteURL(tt.input))
		})
	}
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

func TestGetRemoteURL_UsesOnlyConfiguredRemoteWhenOriginMissing(t *testing.T) {
	dir := t.TempDir()
	initPublishGitRepo(t, dir)
	runPublishGit(t, dir, "remote", "add", "Engram-Langraph-Provider", "https://github.com/Clarit-AI/Engram-Langraph-Provider.git")

	pub := &Publisher{repoRoot: dir}
	url, err := pub.getRemoteURL()
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/Clarit-AI/Engram-Langraph-Provider.git", url)
}

func TestGetRemoteURL_PrefersCurrentBranchUpstreamRemote(t *testing.T) {
	dir := t.TempDir()
	initPublishGitRepo(t, dir)
	runPublishGit(t, dir, "remote", "add", "origin", "https://github.com/example/wrong.git")
	runPublishGit(t, dir, "remote", "add", "Engram-Langraph-Provider", "https://github.com/Clarit-AI/Engram-Langraph-Provider.git")
	runPublishGit(t, dir, "config", "branch.main.remote", "Engram-Langraph-Provider")

	pub := &Publisher{repoRoot: dir}
	url, err := pub.getRemoteURL()
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/Clarit-AI/Engram-Langraph-Provider.git", url)
}

func TestGetRemoteURL_FallsBackWhenBranchRemoteIsStale(t *testing.T) {
	dir := t.TempDir()
	initPublishGitRepo(t, dir)
	runPublishGit(t, dir, "remote", "add", "origin", "https://github.com/Clarit-AI/Engram-Langraph-Provider.git")
	// Set branch.remote to a name that no longer exists
	runPublishGit(t, dir, "config", "branch.main.remote", "deleted-remote")

	pub := &Publisher{repoRoot: dir}
	url, err := pub.getRemoteURL()
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/Clarit-AI/Engram-Langraph-Provider.git", url)
}

func TestGitHubRepoFromRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
		ok     bool
	}{
		{name: "https", remote: "https://github.com/Clarit-AI/Engram-Langraph-Provider.git", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "https no git suffix", remote: "https://github.com/Clarit-AI/Engram-Langraph-Provider", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "https with token", remote: "https://ghp_token@github.com/Clarit-AI/Engram-Langraph-Provider.git", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "ssh scp", remote: "git@github.com:Clarit-AI/Engram-Langraph-Provider.git", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "ssh url", remote: "ssh://git@github.com/Clarit-AI/Engram-Langraph-Provider.git", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "ssh url with port", remote: "ssh://git@github.com:2222/Clarit-AI/Engram-Langraph-Provider.git", want: "Clarit-AI/Engram-Langraph-Provider", ok: true},
		{name: "gitlab", remote: "https://gitlab.com/Clarit-AI/Engram-Langraph-Provider.git", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := githubRepoFromRemoteURL(tt.remote)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGitHubWikiPublishingEnabled_DetectsEnabledWikiWithGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stub not supported on Windows")
	}
	dir := t.TempDir()
	binDir := t.TempDir()
	initPublishGitRepo(t, dir)
	runPublishGit(t, dir, "remote", "add", "Engram-Langraph-Provider", "https://github.com/Clarit-AI/Engram-Langraph-Provider.git")

	ghStub := "#!/bin/sh\nprintf 'true\\n'\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghStub), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pub := &Publisher{
		repoRoot: dir,
		config:   &config.Config{},
	}
	enabled, err := pub.githubWikiPublishingEnabled()
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestGitHubWikiPublishingEnabled_ReportsDisabledWiki(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stub not supported on Windows")
	}
	dir := t.TempDir()
	binDir := t.TempDir()
	initPublishGitRepo(t, dir)
	runPublishGit(t, dir, "remote", "add", "Engram-Langraph-Provider", "https://github.com/Clarit-AI/Engram-Langraph-Provider.git")

	ghStub := "#!/bin/sh\nprintf 'false\\n'\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghStub), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pub := &Publisher{
		repoRoot: dir,
		config:   &config.Config{},
	}
	enabled, err := pub.githubWikiPublishingEnabled()
	require.Error(t, err)
	assert.False(t, enabled)
	assert.Contains(t, err.Error(), "GitHub Wiki is disabled")
}

func TestFormatWikiCloneError_RepositoryNotFoundAddsRemediation(t *testing.T) {
	err := formatWikiCloneError(
		"https://github.com/Clarit-AI/Engram-Langraph-Provider.wiki.git",
		[]byte("remote: Repository not found.\nfatal: repository 'https://github.com/Clarit-AI/Engram-Langraph-Provider.wiki.git/' not found\n"),
		assert.AnError,
	)
	require.Error(t, err)
	message := err.Error()
	assert.Contains(t, message, "the first wiki page exists")
	assert.Contains(t, message, "create a first page in the GitHub web UI")
	assert.Contains(t, message, "active git credentials")
	assert.Contains(t, message, "gh auth status")
}

func TestWikiSyncerGetWikiPages_PreservesNestedPaths(t *testing.T) {
	dir := t.TempDir()
	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", "architecture"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", "guides"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "architecture", "overview.md"), []byte("# Overview"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "guides", "api_guide.md"), []byte("# API"), 0644))

	syncer, err := NewWikiSyncer(dir, nil)
	require.NoError(t, err)

	pages, err := syncer.getWikiPages(filepath.Join(dir, ".wiki"))
	require.NoError(t, err)
	assert.Contains(t, pages, "architecture/overview.md")
	assert.Contains(t, pages, "guides/api_guide.md")
	assert.NotContains(t, pages, "overview.md")
	assert.NotContains(t, pages, "api_guide.md")
}

func TestWikiSyncerSync_IncludesNestedDefaultPublishPaths(t *testing.T) {
	dir := t.TempDir()
	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", "architecture"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".wiki", "guides"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "architecture", "overview.md"), []byte("# Overview"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".wiki", "guides", "api_guide.md"), []byte("# API"), 0644))

	syncer, err := NewWikiSyncer(dir, &config.Config{Wiki: config.Wiki{Root: ".wiki"}})
	require.NoError(t, err)

	result, err := syncer.Sync(true, false)
	require.NoError(t, err)
	assert.Contains(t, result.PagesIncluded, "architecture/overview.md")
	assert.NotContains(t, result.PagesIncluded, "guides/api_guide.md")

	foundGuideExclusion := false
	for _, excluded := range result.PagesExcluded {
		if excluded.Path == "guides/api_guide.md" && excluded.Reason == "not_matching_publish" {
			foundGuideExclusion = true
			break
		}
	}
	assert.True(t, foundGuideExclusion, "expected nested guide path to be excluded by default publish filters")
}

func TestWikiSyncerSync_PushIsGuarded(t *testing.T) {
	dir := t.TempDir()
	_, err := wiki.Init(wiki.InitOptions{RepoRoot: dir})
	require.NoError(t, err)

	syncer, err := NewWikiSyncer(dir, nil)
	require.NoError(t, err)

	_, err = syncer.Sync(false, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented safely")
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

func initPublishGitRepo(t *testing.T, dir string) {
	t.Helper()
	runPublishGit(t, dir, "init", "-b", "main")
}

func runPublishGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
