package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunner_Disabled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")

	dr := NewDryRunner(false, "", nil)
	assert.False(t, dr.Enabled())

	require.NoError(t, dr.WriteFile(target, []byte("hello")))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestDryRunner_Enabled(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "dryrun-output")
	target := filepath.Join("docs", "real.txt")

	var buf bytes.Buffer
	dr := NewDryRunner(true, outputDir, &buf)
	assert.True(t, dr.Enabled())

	require.NoError(t, dr.WriteFile(target, []byte("hello")))

	// Real path should NOT exist
	_, err := os.Stat(target)
	assert.True(t, os.IsNotExist(err), "real path should not exist in dry-run mode")

	// Output path should exist
	outputPath := filepath.Join(outputDir, target)
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	assert.Contains(t, buf.String(), "[dry-run]")
}

func TestDryRunner_EnabledNormalizesRepoAbsolutePaths(t *testing.T) {
	repoRoot := t.TempDir()
	outputDir := filepath.Join(repoRoot, ".plexium", "output")
	target := filepath.Join(repoRoot, ".wiki", "guides", "index.md")

	var buf bytes.Buffer
	dr := NewDryRunner(true, outputDir, &buf)

	require.NoError(t, dr.WriteFile(target, []byte("# Index\n")))

	assert.NoFileExists(t, target, "real path should not exist in dry-run mode")
	outputPath := filepath.Join(outputDir, ".wiki", "guides", "index.md")
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, "# Index\n", string(data))
	assert.Contains(t, buf.String(), filepath.Join(".wiki", "guides", "index.md"))
	assert.NoDirExists(t, filepath.Join(outputDir, "Users"))
}

func TestDryRunner_EnabledNormalizesOutputAbsolutePaths(t *testing.T) {
	repoRoot := t.TempDir()
	outputDir := filepath.Join(repoRoot, ".plexium", "output")
	target := filepath.Join(outputDir, "guides", "index.md")

	var buf bytes.Buffer
	dr := NewDryRunner(true, outputDir, &buf)

	require.NoError(t, dr.WriteFile(target, []byte("# Index\n")))

	data, err := os.ReadFile(filepath.Join(outputDir, "guides", "index.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Index\n", string(data))
	assert.Contains(t, buf.String(), filepath.Join("guides", "index.md"))
	assert.NoDirExists(t, filepath.Join(outputDir, ".plexium", "output"))
}

func TestDryRunner_EnabledSanitizesExternalAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, ".plexium", "output")
	target := filepath.Join(string(filepath.Separator), "outside", "repo", "file.md")

	var buf bytes.Buffer
	dr := NewDryRunner(true, outputDir, &buf)

	require.NoError(t, dr.WriteFile(target, []byte("# External\n")))

	data, err := os.ReadFile(filepath.Join(outputDir, "outside", "repo", "file.md"))
	require.NoError(t, err)
	assert.Equal(t, "# External\n", string(data))
	assert.Contains(t, buf.String(), filepath.Join("outside", "repo", "file.md"))
}

func TestDryRunner_MkdirAll_Disabled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "dir")

	dr := NewDryRunner(false, "", nil)
	require.NoError(t, dr.MkdirAll(target))
	assert.DirExists(t, target)
}

func TestDryRunner_MkdirAll_Enabled(t *testing.T) {
	var buf bytes.Buffer
	dr := NewDryRunner(true, "/tmp/output", &buf)

	require.NoError(t, dr.MkdirAll("/fake/path"))
	assert.Contains(t, buf.String(), "[dry-run]")
}

func TestDryRunner_Report(t *testing.T) {
	var buf bytes.Buffer
	dr := NewDryRunner(true, "/tmp/output", &buf)

	dr.Report("would publish 5 files")
	assert.Contains(t, buf.String(), "[dry-run] would publish 5 files")
}
