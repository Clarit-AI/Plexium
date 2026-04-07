package convert

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilter_IncludeExclude(t *testing.T) {
	f, err := NewFilter(nil, nil) // defaults
	require.NoError(t, err)

	files := []scanner.File{
		{Path: "README.md", Content: "# Hello"},
		{Path: "src/auth/handler.go", Content: "package auth"},
		{Path: "node_modules/foo/index.js", Content: "module.exports = {}"},
		{Path: "docs/guide.md", Content: "# Guide"},
	}

	result := f.Apply(files)

	// README.md and docs/guide.md should be eligible
	eligiblePaths := make(map[string]bool)
	for _, e := range result.Eligible {
		eligiblePaths[e.Path] = true
	}
	assert.True(t, eligiblePaths["README.md"], "README.md should be eligible")
	assert.True(t, eligiblePaths["docs/guide.md"], "docs/guide.md should be eligible")

	// node_modules should be excluded
	skippedPaths := make(map[string]bool)
	for _, s := range result.Skipped {
		skippedPaths[s.Path] = true
	}
	assert.True(t, skippedPaths["node_modules/foo/index.js"], "node_modules should be skipped")
	assert.Equal(t, "excluded by pattern", result.SkipReasons["node_modules/foo/index.js"])
}

func TestFilter_BinaryFiles(t *testing.T) {
	f, err := NewFilter([]string{"**/*"}, nil)
	require.NoError(t, err)

	files := []scanner.File{
		{Path: "image.png", Content: "binary data"},
		{Path: "app.wasm", Content: "binary data"},
		{Path: "readme.md", Content: "# Hello"},
	}

	result := f.Apply(files)

	assert.Len(t, result.Eligible, 1)
	assert.Equal(t, "readme.md", result.Eligible[0].Path)
	assert.Equal(t, "binary file", result.SkipReasons["image.png"])
	assert.Equal(t, "binary file", result.SkipReasons["app.wasm"])
}

func TestFilter_EmptyFiles(t *testing.T) {
	f, err := NewFilter([]string{"**/*"}, nil)
	require.NoError(t, err)

	files := []scanner.File{
		{Path: "empty.md", Content: ""},
		{Path: "whitespace.md", Content: "   \n\n  "},
		{Path: "real.md", Content: "# Content"},
	}

	result := f.Apply(files)

	assert.Len(t, result.Eligible, 1)
	assert.Equal(t, "real.md", result.Eligible[0].Path)
	assert.Equal(t, "empty file", result.SkipReasons["empty.md"])
	assert.Equal(t, "empty file", result.SkipReasons["whitespace.md"])
}

func TestFilter_LargeFiles(t *testing.T) {
	f, err := NewFilter([]string{"**/*"}, nil)
	require.NoError(t, err)

	// Create a string >1MB
	bigContent := make([]byte, 1024*1024+1)
	for i := range bigContent {
		bigContent[i] = 'a'
	}

	files := []scanner.File{
		{Path: "big.md", Content: string(bigContent)},
		{Path: "small.md", Content: "# Small"},
	}

	result := f.Apply(files)

	assert.Len(t, result.Eligible, 1)
	assert.Equal(t, "file too large (>1MB)", result.SkipReasons["big.md"])
}

func TestFilter_SkipsDirectories(t *testing.T) {
	f, err := NewFilter(nil, nil)
	require.NoError(t, err)

	files := []scanner.File{
		{Path: "src", IsDir: true},
		{Path: "src/main.go", Content: "package main"},
	}

	result := f.Apply(files)

	// Directories should not appear in either eligible or skipped
	for _, e := range result.Eligible {
		assert.False(t, e.IsDir, "directories should not be eligible")
	}
	for _, s := range result.Skipped {
		assert.False(t, s.IsDir, "directories should not be skipped")
	}
}

func TestFilter_LogsSkipReasons(t *testing.T) {
	f, err := NewFilter([]string{"**/*.md"}, []string{"**/vendor/**"})
	require.NoError(t, err)

	files := []scanner.File{
		{Path: "vendor/lib/readme.md", Content: "# Vendored"},
		{Path: "src/main.go", Content: "package main"},
		{Path: "docs/guide.md", Content: "# Guide"},
	}

	result := f.Apply(files)

	assert.Equal(t, "excluded by pattern", result.SkipReasons["vendor/lib/readme.md"])
	assert.Equal(t, "no matching include pattern", result.SkipReasons["src/main.go"])
	assert.NotContains(t, result.SkipReasons, "docs/guide.md")
}
