package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectWorkspaceChanges_PreservesLeadingStatusColumns(t *testing.T) {
	workdir := t.TempDir()

	initWorkspaceGit(t, workdir)

	filePath := filepath.Join(workdir, "tracked.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# initial\n"), 0o644))
	require.NoError(t, exec.Command("git", "-C", workdir, "add", "tracked.md").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "commit", "-m", "init").Run())

	require.NoError(t, os.WriteFile(filePath, []byte("# updated\n"), 0o644))

	changed, err := collectWorkspaceChanges(workdir)
	require.NoError(t, err)
	assert.Contains(t, changed, "tracked.md")
}

func TestCollectWorkspaceChanges_IgnoresMetaAndExpandsUntrackedDirectories(t *testing.T) {
	workdir := t.TempDir()
	initWorkspaceGit(t, workdir)

	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".wiki"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "_index.md"), []byte("# Index\n"), 0o644))
	require.NoError(t, exec.Command("git", "-C", workdir, "add", ".wiki/_index.md").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "commit", "-m", "init").Run())

	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".wiki", "reference"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "reference", "plexium-user-reference.md"), []byte("# Reference\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "reference", "skill.md"), []byte("# Skill\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, "meta.json"), []byte("{}\n"), 0o644))

	changed, err := collectWorkspaceChanges(workdir)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		".wiki/reference/plexium-user-reference.md",
		".wiki/reference/skill.md",
	}, changed)
	assert.NotContains(t, changed, "meta.json")
	assert.NotContains(t, changed, ".wiki/reference/")
}

func TestApplyWorkspaceChanges_AppliesAllowedFilesAndReportsUnsupported(t *testing.T) {
	repoRoot := t.TempDir()
	workdir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".wiki"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".plexium"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "_log.md"), []byte("# Log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".plexium", "manifest.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, "meta.json"), []byte("{}\n"), 0o644))

	applied, outcome, attentionNeeded, err := applyWorkspaceChanges(repoRoot, workdir, ".wiki", []string{
		".plexium/manifest.json",
		".wiki/_log.md",
		"meta.json",
	})
	require.NoError(t, err)
	assert.True(t, attentionNeeded)
	assert.ElementsMatch(t, []string{".plexium/manifest.json", ".wiki/_log.md"}, applied)
	assert.Contains(t, outcome, "applied 2 file(s)")
	assert.Contains(t, outcome, "left 1 unsupported file(s)")
	assert.Contains(t, outcome, "meta.json")
	assert.FileExists(t, filepath.Join(repoRoot, ".wiki", "_log.md"))
	assert.FileExists(t, filepath.Join(repoRoot, ".plexium", "manifest.json"))
}

func TestApplyWorkspaceChanges_SkipsDirectoryPaths(t *testing.T) {
	repoRoot := t.TempDir()
	workdir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".wiki", "reference"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "reference", "skill.md"), []byte("# Skill\n"), 0o644))

	applied, outcome, attentionNeeded, err := applyWorkspaceChanges(repoRoot, workdir, ".wiki", []string{
		".wiki/reference/",
		".wiki/reference/skill.md",
	})
	require.NoError(t, err)
	assert.False(t, attentionNeeded)
	assert.Equal(t, []string{".wiki/reference/skill.md"}, applied)
	assert.Contains(t, outcome, "applied 1 file(s)")
	assert.FileExists(t, filepath.Join(repoRoot, ".wiki", "reference", "skill.md"))
}

func TestUpdateManifestForWorkspace_StoresWikiRelativePaths(t *testing.T) {
	workdir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".wiki", "guides"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workdir, ".plexium"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "Home.md"), []byte("---\ntitle: \"Home\"\n---\n\n# Home\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".wiki", "guides", "api.md"), []byte("# API\n"), 0o644))

	mgr, err := manifest.NewManager(manifest.DefaultPath(workdir))
	require.NoError(t, err)
	require.NoError(t, mgr.Save(manifest.NewEmptyManifest()))

	d, _ := newTestDaemon(t, DaemonOpts{
		Config: &config.Config{Wiki: config.Wiki{Root: ".wiki"}},
	})
	d.config.RepoRoot = workdir

	err = d.updateManifestForWorkspace(&upkeepJob{Type: jobTypeRepoDrift}, workdir, []string{
		".wiki/Home.md",
		".wiki/guides/api.md",
	})
	require.NoError(t, err)

	m, err := mgr.Load()
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, page := range m.Pages {
		paths[page.WikiPath] = true
	}

	assert.True(t, paths["Home.md"])
	assert.True(t, paths["guides/api.md"])
	assert.False(t, paths[".wiki/Home.md"])
	assert.False(t, paths[".wiki/guides/api.md"])
}

func TestDiscoverJobs_UsesConfiguredWikiRoot(t *testing.T) {
	d, repoRoot := newTestDaemon(t, DaemonOpts{
		RunnerName: "codex",
		Config: &config.Config{
			Wiki: config.Wiki{Root: "docs/wiki"},
		},
		Watches: WatchOpts{
			Ingest: WatchDef{Enabled: true, Action: "auto-ingest"},
		},
	})

	rawDir := filepath.Join(repoRoot, "docs", "wiki", "raw")
	require.NoError(t, os.MkdirAll(rawDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rawDir, "notes.md"), []byte("raw"), 0o644))

	jobs, actions := d.discoverJobs()
	require.Len(t, jobs, 2)
	assert.Equal(t, "docs/wiki/", jobs[0].Target)
	assert.Equal(t, "docs/wiki/raw/notes.md", jobs[1].Target)

	require.Len(t, actions, 2)
	assert.Equal(t, "docs/wiki/", actions[0].Target)
	assert.Equal(t, "notes.md", actions[1].Target)
}

func initWorkspaceGit(t *testing.T, workdir string) {
	t.Helper()
	require.NoError(t, exec.Command("git", "-C", workdir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "config", "user.email", "plexium@example.com").Run())
	require.NoError(t, exec.Command("git", "-C", workdir, "config", "user.name", "Plexium Tests").Run())
}
