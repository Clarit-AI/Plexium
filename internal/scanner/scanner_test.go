package scanner

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	s, err := New([]string{"**/*.go", "docs/**/*.md"}, []string{"**/node_modules/**"})
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNew_InvalidPattern(t *testing.T) {
	_, err := New([]string{"[invalid"}, nil)
	assert.Error(t, err)
}

func TestScanner_Scan(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, "src", "auth"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpDir, "docs", "guides"), 0755)
	require.NoError(t, err)

	// Create test files
	files := map[string]string{
		"src/main.go":        "package main",
		"src/auth/login.go":  "package auth",
		"docs/guide.md":      "# Guide",
		"docs/guides/api.md": "# API Guide",
		"README.md":          "# Readme",
	}
	for path, content := range files {
		err = os.WriteFile(filepath.Join(tmpDir, path), []byte(content), 0644)
		require.NoError(t, err)
	}

	s, err := New([]string{"**/*.go", "**/*.md"}, []string{})
	require.NoError(t, err)

	results, err := s.Scan(tmpDir)
	require.NoError(t, err)

	// Should find .go files, docs/*.md, AND README.md at root
	// (root-level files now match because **/*.md is augmented with *.md)
	assert.Len(t, results, 5)
	paths := make([]string, len(results))
	for i, f := range results {
		paths[i] = f.Path
	}
	assert.Contains(t, paths, "src/main.go")
	assert.Contains(t, paths, "src/auth/login.go")
	assert.Contains(t, paths, "docs/guide.md")
	assert.Contains(t, paths, "docs/guides/api.md")
	assert.Contains(t, paths, "README.md")
}

func TestScanner_Scan_RootLevelOnly(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Readme"), 0644)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpDir, "docs"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "docs", "guide.md"), []byte("# Guide"), 0644)
	require.NoError(t, err)

	s, err := New([]string{"**/*.md"}, nil)
	require.NoError(t, err)

	results, err := s.Scan(tmpDir)
	require.NoError(t, err)

	paths := filepaths(results)
	assert.Contains(t, paths, "README.md")
	assert.Contains(t, paths, "docs/guide.md")
}

func TestScanner_Scan_Exclude(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, "src", "node_modules"), 0755)
	require.NoError(t, err)

	files := map[string]string{
		"src/main.go":           "package main",
		"src/node_modules/a.go": "module a",
	}
	for path, content := range files {
		err = os.WriteFile(filepath.Join(tmpDir, path), []byte(content), 0644)
		require.NoError(t, err)
	}

	s, err := New([]string{"**/*.go"}, []string{"**/node_modules/**"})
	require.NoError(t, err)

	results, err := s.Scan(tmpDir)
	require.NoError(t, err)

	assert.Len(t, results, 1)
	assert.Equal(t, "src/main.go", results[0].Path)
}

func TestScanner_Scan_DeterministicOrder(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "src")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// Create files in non-alphabetical order within subdirectory
	files := []string{"z-file.go", "a-file.go", "m-file.go"}
	for _, f := range files {
		err := os.WriteFile(filepath.Join(subDir, f), []byte("package test"), 0644)
		require.NoError(t, err)
	}

	s, err := New([]string{"src/*.go"}, nil)
	require.NoError(t, err)

	results, err := s.Scan(tmpDir)
	require.NoError(t, err)

	// Should be sorted alphabetically
	assert.Equal(t, []string{"src/a-file.go", "src/m-file.go", "src/z-file.go"}, filepaths(results))
}

func TestScanner_Scan_SkipsUnixSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported on Windows")
	}

	tmpDir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tmpDir, ".beads"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Readme"), 0o644)
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	listener, err := net.Listen("unix", filepath.Join(".beads", "bd.sock"))
	require.NoError(t, err)
	defer listener.Close()

	s, err := New([]string{"**/*"}, nil)
	require.NoError(t, err)

	results, err := s.Scan(tmpDir)
	require.NoError(t, err)

	paths := filepaths(results)
	assert.Contains(t, paths, "README.md")
	assert.NotContains(t, paths, ".beads/bd.sock")
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	result := ExpandHome("~/test/path")
	assert.Equal(t, filepath.Join(home, "test/path"), result)
}

func filepaths(files []File) []string {
	p := make([]string, len(files))
	for i, f := range files {
		p[i] = f.Path
	}
	return p
}
