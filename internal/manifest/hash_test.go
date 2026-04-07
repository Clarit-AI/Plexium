package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeHash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.go")
	content := []byte("package test\n")
	require.NoError(t, os.WriteFile(file, content, 0644))

	hash1, err := ComputeHash(file)
	require.NoError(t, err)
	assert.Len(t, hash1, 64) // SHA256 hex = 64 chars

	// Same content → same hash
	hash2, err := ComputeHash(file)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)
}

func TestComputeHash_Deterministic(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")

	require.NoError(t, os.WriteFile(f1, []byte("hello world"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("hello world"), 0644))

	h1, err := ComputeHash(f1)
	require.NoError(t, err)
	h2, err := ComputeHash(f2)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "same content should produce same hash regardless of filename")
}

func TestComputeHash_Different(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")

	require.NoError(t, os.WriteFile(f1, []byte("hello"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("world"), 0644))

	h1, err := ComputeHash(f1)
	require.NoError(t, err)
	h2, err := ComputeHash(f2)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
}

func TestComputeHash_Nonexistent(t *testing.T) {
	_, err := ComputeHash("/nonexistent/file.txt")
	assert.Error(t, err)
}

func TestComputeHashString(t *testing.T) {
	h1 := ComputeHashString("hello")
	h2 := ComputeHashString("hello")
	h3 := ComputeHashString("world")

	assert.Equal(t, h1, h2)
	assert.NotEqual(t, h1, h3)
	assert.Len(t, h1, 64)
}

func TestComputeDirHash(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "c.go"), []byte("package c"), 0644))

	hash1, err := ComputeDirHash(dir, []string{"*.go", "sub/*.go"})
	require.NoError(t, err)
	assert.Len(t, hash1, 64)

	// Same content → same hash (deterministic)
	hash2, err := ComputeDirHash(dir, []string{"*.go", "sub/*.go"})
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2, "dir hash should be deterministic")
}

func TestComputeDirHash_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "z.go"), []byte("package z"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0644))

	hash1, err := ComputeDirHash(dir, []string{"*.go"})
	require.NoError(t, err)

	// Hash should be same regardless of file creation order
	hash2, err := ComputeDirHash(dir, []string{"*.go"})
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)
}

func TestComputeDirHash_Empty(t *testing.T) {
	dir := t.TempDir()
	hash, err := ComputeDirHash(dir, []string{"*.go"})
	require.NoError(t, err)
	assert.Len(t, hash, 64)
}

func TestHashAllSources(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "auth.go")
	f2 := filepath.Join(dir, "user.go")
	require.NoError(t, os.WriteFile(f1, []byte("package auth"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("package user"), 0644))

	entry := PageEntry{
		SourceFiles: []SourceFile{
			{Path: f1},
			{Path: f2},
		},
	}

	hashes, err := HashAllSources(entry)
	require.NoError(t, err)
	assert.Len(t, hashes, 2)

	h1, _ := ComputeHash(f1)
	h2, _ := ComputeHash(f2)
	assert.Equal(t, h1, hashes[f1])
	assert.Equal(t, h2, hashes[f2])
}

func TestHashAllSources_MissingFile(t *testing.T) {
	entry := PageEntry{
		SourceFiles: []SourceFile{
			{Path: "/nonexistent/file.go"},
		},
	}

	_, err := HashAllSources(entry)
	assert.Error(t, err)
}

func TestComputeDirHash_DoublestarRecursive(t *testing.T) {
	// Set up deeply nested source tree
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "auth", "middleware", "jwt"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "user"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "auth", "login.go"), []byte("package auth"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "auth", "middleware", "middleware.go"), []byte("package auth"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "auth", "middleware", "jwt", "token.go"), []byte("package jwt"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "user", "model.go"), []byte("package user"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "admin"), 0755))

	// Single ** recursive glob — should find all .go files at any depth
	hash, err := ComputeDirHash(dir, []string{"src/**/*.go"})
	require.NoError(t, err)
	assert.Len(t, hash, 64)

	// Same glob run again produces identical hash (deterministic)
	hash2, err := ComputeDirHash(dir, []string{"src/**/*.go"})
	require.NoError(t, err)
	assert.Equal(t, hash, hash2, "doublestar recursive glob should be deterministic")
}

func TestComputeDirHash_MixedPatterns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("# doc"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs", "api"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docs", "api", "ref.md"), []byte("# ref"), 0644))

	// Mix of recursive and shallow globs
	hash, err := ComputeDirHash(dir, []string{"**/*.go", "**/*.md"})
	require.NoError(t, err)
	assert.Len(t, hash, 64)

	hash2, err := ComputeDirHash(dir, []string{"**/*.go", "**/*.md"})
	require.NoError(t, err)
	assert.Equal(t, hash, hash2)
}
