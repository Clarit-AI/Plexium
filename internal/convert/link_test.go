package convert

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinker_GenerateCrossReferences(t *testing.T) {
	linker := NewLinker()

	pages := []PageData{
		{
			WikiPath: "Home.md",
			Title:    "Home",
			Section:  "Root",
			Content:  "---\ntitle: Home\n---\n\n# Home\n\nThis project uses Authentication and API modules.",
		},
		{
			WikiPath: "modules/authentication.md",
			Title:    "Authentication",
			Section:  "Modules",
			Content:  "---\ntitle: Authentication\n---\n\n# Authentication\n\nHandles user login.",
		},
		{
			WikiPath: "modules/api.md",
			Title:    "API",
			Section:  "Modules",
			Content:  "---\ntitle: API\n---\n\n# API\n\nREST endpoints. Uses Authentication for tokens.",
		},
	}

	linker.AddPages(pages)
	linked := linker.GenerateCrossReferences(pages)

	// Home should now contain wiki-links to Authentication
	assert.Contains(t, linked[0].Content, "[[modules/authentication.md|Authentication]]",
		"Home should link to Authentication module")
}

func TestLinker_ComputeLinks(t *testing.T) {
	linker := NewLinker()

	pages := []PageData{
		{
			WikiPath: "Home.md",
			Content:  "See [[modules/auth.md|Auth]] and [[decisions/001-use-go.md|ADR 1]].",
		},
		{
			WikiPath: "modules/auth.md",
			Content:  "Auth module. See [[Home.md|Home]].",
		},
		{
			WikiPath: "decisions/001-use-go.md",
			Content:  "We chose Go.",
		},
	}

	linker.AddPages(pages)
	inbound, outbound := linker.ComputeLinks(pages)

	// Home links to auth and decision
	assert.Contains(t, outbound["Home.md"], "modules/auth.md")
	assert.Contains(t, outbound["Home.md"], "decisions/001-use-go.md")

	// Auth links back to Home
	assert.Contains(t, outbound["modules/auth.md"], "Home.md")

	// Auth has inbound from Home
	assert.Contains(t, inbound["modules/auth.md"], "Home.md")

	// Home has inbound from auth
	assert.Contains(t, inbound["Home.md"], "modules/auth.md")
}

func TestLinker_NoSelfLinks(t *testing.T) {
	linker := NewLinker()

	pages := []PageData{
		{
			WikiPath: "modules/auth.md",
			Title:    "Auth",
			Content:  "---\ntitle: Auth\n---\n\n# Auth\n\nThe Auth module handles authentication.",
		},
	}

	linker.AddPages(pages)
	linked := linker.GenerateCrossReferences(pages)

	// Should not contain a link to itself
	assert.NotContains(t, linked[0].Content, "[[modules/auth.md|Auth]]",
		"should not generate self-links")
}

func TestLinker_SkipsFrontmatter(t *testing.T) {
	linker := NewLinker()

	pages := []PageData{
		{
			WikiPath: "Home.md",
			Title:    "Home",
			Content:  "---\ntitle: Home\nrelated: Authentication\n---\n\n# Home\n\nBody text.",
		},
		{
			WikiPath: "modules/authentication.md",
			Title:    "Authentication",
			Content:  "---\ntitle: Authentication\n---\n\n# Authentication\n\nAuth module.",
		},
	}

	linker.AddPages(pages)
	linked := linker.GenerateCrossReferences(pages)

	// Frontmatter should not be modified
	assert.Contains(t, linked[0].Content, "related: Authentication",
		"frontmatter should not have wiki-links injected")
}

func TestFrontmatterEnd(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"---\ntitle: foo\n---\n\n# Hello", 3},
		{"# No frontmatter", 0},
		{"---\nonly opening", 0},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, frontmatterEnd(tt.content))
	}
}

func TestDedup(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	result := dedup(input)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}