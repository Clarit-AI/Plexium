package convert

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Root README
	writeFile(t, dir, "README.md", "# My Project\n\nA test project.\n")

	// Nested README
	writeFile(t, dir, "src/auth/README.md", "# Auth Module\n\nHandles authentication.\n")

	// Go source files
	writeFile(t, dir, "src/auth/handler.go", `package auth

// Handler handles auth requests.
func Handler() {}

// Validate checks credentials.
func Validate(token string) bool { return true }

type User struct {
	Name string
}
`)

	writeFile(t, dir, "src/api/server.go", `package api

// Server is the HTTP server.
type Server struct{}

func NewServer() *Server { return &Server{} }
`)

	// ADR files
	writeFile(t, dir, "adr/001-use-go.md", "# ADR 1: Use Go\n\n**Status:** Accepted\n\n## Context\n\nWe need a language.\n\n## Decision\n\nUse Go.\n")
	writeFile(t, dir, "adr/002-use-postgres.md", "# ADR 2: Use Postgres\n\n**Status:** Proposed\n\n## Context\n\nNeed a DB.\n")

	// Config files
	writeFile(t, dir, "go.mod", "module example.com/myproject\n\ngo 1.21\n")

	// Docs
	writeFile(t, dir, "docs/architecture/overview.md", "# Architecture Overview\n\nSystem design.\n")
	writeFile(t, dir, "docs/concepts/authentication.md", "# Authentication\n\nHow auth works.\n")

	// Agent instruction file
	writeFile(t, dir, "CLAUDE.md", "# Claude Instructions\n\nDo good things.\n")

	// .env.example
	writeFile(t, dir, ".env.example", "DATABASE_URL=postgres://localhost/mydb\nAPI_KEY=xxx\n")

	return dir
}

func writeFile(t *testing.T, base, path, content string) {
	t.Helper()
	full := filepath.Join(base, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
}

func TestScour_ExtractsReadmes(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(findings.Readmes), 2, "should find root and nested READMEs")

	// Root README
	var rootReadme *ReadmeDoc
	for i, r := range findings.Readmes {
		if r.Hierarchy == 0 {
			rootReadme = &findings.Readmes[i]
			break
		}
	}
	require.NotNil(t, rootReadme, "should find root README")
	assert.Equal(t, "My Project", rootReadme.Title)
	assert.Equal(t, 0, rootReadme.Hierarchy)
}

func TestScour_ExtractsADRs(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	assert.Len(t, findings.ADRs, 2)

	// Check first ADR
	var adr1 *ADRDoc
	for i, a := range findings.ADRs {
		if a.Number == 1 {
			adr1 = &findings.ADRs[i]
			break
		}
	}
	require.NotNil(t, adr1)
	assert.Equal(t, "Use Go", adr1.Title)
	assert.Equal(t, "Accepted", adr1.Status)
}

func TestScour_ExtractsConfigs(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(findings.Configs), 1, "should find go.mod")
	found := false
	for _, c := range findings.Configs {
		if c.Type == "go.mod" {
			found = true
			assert.Contains(t, c.Content["module"], "example.com/myproject")
		}
	}
	assert.True(t, found, "should extract go.mod")
}

func TestScour_DeepExtractsSourceFiles(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	// Shallow: no source files
	shallowFindings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)
	assert.Empty(t, shallowFindings.SourceFiles, "shallow should not extract source files")

	// Deep: should have source files
	deepFindings, err := scourer.Scour(ScourOptions{Depth: "deep"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(deepFindings.SourceFiles), 2, "deep should extract source files")

	// Check handler.go extraction
	var handlerDoc *SourceDoc
	for i, s := range deepFindings.SourceFiles {
		if filepath.Base(s.Path) == "handler.go" {
			handlerDoc = &deepFindings.SourceFiles[i]
			break
		}
	}
	require.NotNil(t, handlerDoc)
	assert.Equal(t, "auth", handlerDoc.PackageName)
	assert.Contains(t, handlerDoc.FunctionNames, "Handler")
	assert.Contains(t, handlerDoc.FunctionNames, "Validate")
	assert.Contains(t, handlerDoc.TypeNames, "User")
}

func TestScour_ExtractsAgentInstructions(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	// CLAUDE.md should be in ExistingDocs
	found := false
	for _, doc := range findings.ExistingDocs {
		if doc.Type == "claude" {
			found = true
			assert.Contains(t, doc.Content, "Claude Instructions")
		}
	}
	assert.True(t, found, "should find CLAUDE.md as agent instruction")
}

func TestScour_ExtractsExistingDocs(t *testing.T) {
	dir := setupTestRepo(t)
	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	// docs/architecture/overview.md and docs/concepts/authentication.md
	docCount := 0
	for _, doc := range findings.ExistingDocs {
		if doc.Type == "doc" {
			docCount++
		}
	}
	assert.GreaterOrEqual(t, docCount, 2, "should find docs/*.md files")
}

func TestScour_ExcludesAgentInstallSurfaces(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".claude/skills/plexium-user/skill.md", "# Plexium User")
	writeFile(t, dir, ".claude/agents/kfc/spec-design.md", "# Spec Design")
	writeFile(t, dir, ".codex/skills/plexium-user/SKILL.md", "# Plexium User")
	writeFile(t, dir, ".cursor/rules/project.mdc", "# Cursor Rules")

	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	for _, doc := range findings.ExistingDocs {
		assert.NotContains(t, doc.Path, ".claude/")
		assert.NotContains(t, doc.Path, ".codex/")
		assert.NotContains(t, doc.Path, ".cursor/")
	}
}

func TestScour_RespectsPlexiumIgnore(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".plexiumignore", "docs/specs/**\n")
	writeFile(t, dir, "docs/specs/build-plan.md", "# Build Plan\n\nThis is planning material.")

	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	for _, doc := range findings.ExistingDocs {
		assert.NotEqual(t, "docs/specs/build-plan.md", doc.Path)
	}
}

func TestScour_RespectsGitignore(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".gitignore", "scratch-docs/\n")
	writeFile(t, dir, "scratch-docs/generated.md", "# Generated\n\nThis should not become wiki source.")

	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	for _, doc := range findings.ExistingDocs {
		assert.NotEqual(t, "scratch-docs/generated.md", doc.Path)
	}
}

func TestScour_GitignoreWithNegationFallsBackToDefaultBehavior(t *testing.T) {
	dir := setupTestRepo(t)
	writeFile(t, dir, ".gitignore", "scratch-docs/\n!scratch-docs/keep.md\n")
	writeFile(t, dir, "scratch-docs/generated.md", "# Generated\n\nVisible under fallback behavior.")
	writeFile(t, dir, "scratch-docs/keep.md", "# Keep\n\nImportant.")

	scourer, err := NewScourer(dir)
	require.NoError(t, err)

	findings, err := scourer.Scour(ScourOptions{Depth: "shallow"})
	require.NoError(t, err)

	foundGenerated := false
	foundKeep := false
	for _, doc := range findings.ExistingDocs {
		if doc.Path == "scratch-docs/generated.md" {
			foundGenerated = true
		}
		if doc.Path == "scratch-docs/keep.md" {
			foundKeep = true
		}
	}
	assert.True(t, foundGenerated, "gitignore files with negations should not be translated into partial excludes")
	assert.True(t, foundKeep, "negated docs should remain visible when falling back to default behavior")
}

func TestIsADRPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"adr/001-use-go.md", true},
		{"decisions/002-db.md", true},
		{"docs/decisions/003-api.md", true},
		{"src/auth/handler.go", false},
		{"README.md", false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, isADRPath(tt.path), "isADRPath(%q)", tt.path)
	}
}

func TestExtractFirstHeading(t *testing.T) {
	tests := []struct {
		content string
		want    string
	}{
		{"# Hello World\n\nBody", "Hello World"},
		{"No heading here", ""},
		{"## Second Level\n\nBody", ""},
		{"---\ntitle: foo\n---\n\n# Real Title\n", "Real Title"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, extractFirstHeading(tt.content))
	}
}
