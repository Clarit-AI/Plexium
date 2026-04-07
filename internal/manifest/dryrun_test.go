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
	target := filepath.Join(dir, "real.txt")

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
