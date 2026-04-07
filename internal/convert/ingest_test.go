package convert

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngest_ReadmeToHome(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{
		Readmes: []ReadmeDoc{
			{Path: "README.md", Title: "My Project", Content: "# My Project\n\nDescription.", Hierarchy: 0},
		},
	}
	filter := &FilterResult{Eligible: []scanner.File{}}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	require.Len(t, result.Pages, 1)
	assert.Equal(t, "Home.md", result.Pages[0].WikiPath)
	assert.Equal(t, "My Project", result.Pages[0].Title)
	assert.Equal(t, "Root", result.Pages[0].Section)
	assert.Equal(t, "high", result.Pages[0].Confidence)
}

func TestIngest_NestedReadmeToModule(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{
		Readmes: []ReadmeDoc{
			{Path: "src/auth/README.md", Title: "Auth Module", Content: "# Auth Module\n\nAuth stuff.", Hierarchy: 2},
		},
	}
	filter := &FilterResult{Eligible: []scanner.File{}}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	require.Len(t, result.Pages, 1)
	assert.Equal(t, "modules/auth.md", result.Pages[0].WikiPath)
	assert.Equal(t, "Modules", result.Pages[0].Section)
}

func TestIngest_ADRToDecision(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{
		ADRs: []ADRDoc{
			{Path: "adr/001-use-go.md", Number: 1, Title: "Use Go", Status: "Accepted", Content: "# Use Go\n\n## Context\n\nNeed lang."},
		},
	}
	filter := &FilterResult{Eligible: []scanner.File{}}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	require.Len(t, result.Pages, 1)
	assert.Equal(t, "decisions/001-use-go.md", result.Pages[0].WikiPath)
	assert.Equal(t, "Use Go", result.Pages[0].Title)
	assert.Equal(t, "Decisions", result.Pages[0].Section)
}

func TestIngest_SourceDirsToModules(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{}
	filter := &FilterResult{
		Eligible: []scanner.File{
			{Path: "src/auth/handler.go", Content: "package auth"},
			{Path: "src/auth/middleware.go", Content: "package auth"},
			{Path: "src/api/server.go", Content: "package api"},
		},
	}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(result.Pages), 2, "should create module pages for auth and api")

	wikiPaths := make(map[string]bool)
	for _, p := range result.Pages {
		wikiPaths[p.WikiPath] = true
	}
	assert.True(t, wikiPaths["modules/auth.md"], "should create auth module page")
	assert.True(t, wikiPaths["modules/api.md"], "should create api module page")
}

func TestIngest_ModuleStubsMarked(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{}
	filter := &FilterResult{
		Eligible: []scanner.File{
			{Path: "src/utils/helpers.go", Content: "package utils"},
		},
	}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	require.Len(t, result.Pages, 1)
	assert.True(t, result.Pages[0].IsStub, "module from source dirs should be stub")
	assert.Contains(t, result.Pages[0].Content, "<!-- STATUS: stub -->")
}

func TestIngest_ExistingDocsClassified(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{
		ExistingDocs: []ExistingDoc{
			{Path: "docs/architecture/overview.md", Type: "doc", Content: "# Architecture\n\nOverview."},
			{Path: "docs/concepts/auth.md", Type: "doc", Content: "# Authentication\n\nHow it works."},
		},
	}
	filter := &FilterResult{Eligible: []scanner.File{}}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	assert.Len(t, result.Pages, 2)

	sections := make(map[string]bool)
	for _, p := range result.Pages {
		sections[p.Section] = true
	}
	assert.True(t, sections["Architecture"], "architecture doc should be classified")
	assert.True(t, sections["Concepts"], "concept doc should be classified")
}

func TestIngest_SkipsAgentInstructions(t *testing.T) {
	ingestor := NewIngestor()

	findings := &ScourFindings{
		ExistingDocs: []ExistingDoc{
			{Path: "CLAUDE.md", Type: "claude", Content: "# Instructions"},
			{Path: "AGENTS.md", Type: "agents", Content: "# Instructions"},
		},
	}
	filter := &FilterResult{Eligible: []scanner.File{}}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	assert.Empty(t, result.Pages, "should not create pages for agent instructions")
}

func TestIngest_NoDuplicateWikiPaths(t *testing.T) {
	ingestor := NewIngestor()

	// Both a README and source files for the same module
	findings := &ScourFindings{
		Readmes: []ReadmeDoc{
			{Path: "src/auth/README.md", Title: "Auth", Content: "# Auth", Hierarchy: 2},
		},
	}
	filter := &FilterResult{
		Eligible: []scanner.File{
			{Path: "src/auth/handler.go", Content: "package auth"},
		},
	}

	result, err := ingestor.Ingest(findings, filter)
	require.NoError(t, err)

	// Should only have one modules/auth.md, not two
	authCount := 0
	for _, p := range result.Pages {
		if p.WikiPath == "modules/auth.md" {
			authCount++
		}
	}
	assert.Equal(t, 1, authCount, "should deduplicate wiki paths")
}
