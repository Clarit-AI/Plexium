package convert

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/stretchr/testify/assert"
)

func TestLint_FindsUndocumentedModules(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	pages := []PageData{
		{WikiPath: "modules/auth.md", Title: "Auth", Section: "Modules"},
		// No page for "api" module
	}

	eligible := []scanner.File{
		{Path: "src/auth/handler.go", Content: "package auth"},
		{Path: "src/api/server.go", Content: "package api"},
	}

	linker.AddPages(pages)
	result := linter.Analyze(pages, eligible)

	assert.Contains(t, result.UndocumentedModules, "api", "should detect undocumented api module")
	assert.NotContains(t, result.UndocumentedModules, "auth", "auth is documented")
}

func TestLint_CreatesStubsForUndocumented(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	pages := []PageData{}
	eligible := []scanner.File{
		{Path: "src/utils/helpers.go", Content: "package utils"},
	}

	linker.AddPages(pages)
	result := linter.Analyze(pages, eligible)

	assert.Len(t, result.StubPages, 1)
	assert.Equal(t, "modules/utils.md", result.StubPages[0].WikiPath)
	assert.True(t, result.StubPages[0].IsStub)
	assert.Contains(t, result.StubPages[0].Content, "<!-- STATUS: stub -->")
}

func TestLint_DetectsOrphans(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	pages := []PageData{
		{WikiPath: "Home.md", Title: "Home", Section: "Root", Content: "# Home"},
		{WikiPath: "modules/auth.md", Title: "Auth", Section: "Modules", Content: "# Auth"},
		{WikiPath: "modules/api.md", Title: "API", Section: "Modules", Content: "# API\n\nSee [[modules/auth.md]]."},
	}

	linker.AddPages(pages)
	result := linter.Analyze(pages, []scanner.File{})

	// api.md links to auth.md, so auth has inbound. api.md has no inbound links.
	// Home is exempt from orphan detection.
	assert.Contains(t, result.Orphans, "modules/api.md", "api should be orphan (no inbound links)")
	assert.NotContains(t, result.Orphans, "modules/auth.md", "auth has inbound from api, not orphan")
}

func TestLint_GapScore(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	// 2 source dirs, 1 documented (non-stub)
	pages := []PageData{
		{WikiPath: "modules/auth.md", Title: "Auth", Section: "Modules", IsStub: false},
	}
	eligible := []scanner.File{
		{Path: "src/auth/handler.go", Content: "package auth"},
		{Path: "src/api/server.go", Content: "package api"},
	}

	linker.AddPages(pages)
	result := linter.Analyze(pages, eligible)

	assert.Equal(t, 0.5, result.GapScore, "1/2 documented = 50%")
}

func TestLint_GapScoreNoSourceDirs(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	result := linter.Analyze([]PageData{}, []scanner.File{})

	assert.Equal(t, 1.0, result.GapScore, "no source dirs = 100% coverage")
}

func TestLint_SuggestsCrossRefsForOrphans(t *testing.T) {
	linker := NewLinker()
	linter := NewConvertLinter(linker)

	pages := []PageData{
		{WikiPath: "Home.md", Title: "Home", Section: "Root", Content: "# Home"},
		{WikiPath: "modules/lonely.md", Title: "Lonely", Section: "Modules", Content: "# Lonely Module"},
	}

	linker.AddPages(pages)
	result := linter.Analyze(pages, []scanner.File{})

	// Lonely has no inbound links → suggest linking from Home
	foundSuggestion := false
	for _, ref := range result.MissingCrossRefs {
		if ref.ToPage == "modules/lonely.md" && ref.FromPage == "Home.md" {
			foundSuggestion = true
		}
	}
	assert.True(t, foundSuggestion, "should suggest Home→lonely cross-reference")
}