package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	doc, err := Parse(`---
title: Test Page
ownership: managed
---

# Hello World

This is content.
`)
	require.NoError(t, err)
	assert.Equal(t, "Test Page", doc.Frontmatter["title"])
	assert.Equal(t, "managed", doc.Frontmatter["ownership"])
	assert.Contains(t, doc.Body, "# Hello World")
}

func TestParse_NoFrontmatter(t *testing.T) {
	doc, err := Parse("Just regular content")
	require.NoError(t, err)
	assert.Empty(t, doc.Frontmatter)
	assert.Equal(t, "Just regular content", doc.Body)
}

func TestStripFrontmatter(t *testing.T) {
	doc, err := Parse("---\ntitle: Test\n---\n# Content\n")
	require.NoError(t, err)
	stripped := StripFrontmatter(doc)
	assert.Equal(t, "# Content\n", stripped)
}

func TestInjectFrontmatter(t *testing.T) {
	doc := &Document{
		Frontmatter: map[string]any{
			"title":     "Test Page",
			"ownership": "managed",
		},
		Body: "# Hello\n\nContent here",
	}

	result, err := InjectFrontmatter(doc)
	require.NoError(t, err)
	assert.Contains(t, result, "title: Test Page")
	assert.Contains(t, result, "ownership: managed")
	assert.Contains(t, result, "# Hello")
}

func TestNormalizeHeadings(t *testing.T) {
	doc := &Document{Body: "# H1\n## H2\n### H3"}

	// Shift by +1 (H1 becomes H2)
	result := NormalizeHeadings(doc, 1)
	assert.Contains(t, result, "## H1")
	assert.Contains(t, result, "### H2")
	assert.Contains(t, result, "#### H3")
}

func TestExtractWikiLinks(t *testing.T) {
	body := "Check [[Module A]] and [[Module B]] for details. Also see [[Module A]] again."
	links := ExtractWikiLinks(body)
	assert.ElementsMatch(t, []string{"Module A", "Module B"}, links)
}

func TestValidateFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		doc     *Document
		wantErr bool
	}{
		{
			name: "valid",
			doc: &Document{
				Frontmatter: map[string]any{
					"title":     "Test",
					"ownership": "managed",
				},
			},
			wantErr: false,
		},
		{
			name: "missing title",
			doc: &Document{
				Frontmatter: map[string]any{
					"ownership": "managed",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid ownership",
			doc: &Document{
				Frontmatter: map[string]any{
					"title":     "Test",
					"ownership": "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrontmatter(tt.doc)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHasFrontmatter(t *testing.T) {
	with := &Document{Raw: "---\ntitle: Test\n---\ncontent"}
	without := &Document{Raw: "just content"}

	assert.True(t, HasFrontmatter(with))
	assert.False(t, HasFrontmatter(without))
}