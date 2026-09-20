package markedup_test

// Contract-test helpers that wrap MarkedUp public APIs we depend on but
// don't want to import in every test file. Kept here so the contract tests
// can be read in isolation without searching for utility functions.

import (
	"github.com/Clarit-AI/markedup/markdown"
	"github.com/Clarit-AI/markedup/schema"
)

// markedupFrontmatter is a re-export of schema.GraphFrontmatter for the
// contract tests. Using a local alias keeps the test files readable as
// "frontmatter" rather than "GraphFrontmatter".
type markedupFrontmatter = schema.GraphFrontmatter

// markedupParseFrontmatter parses raw markdown bytes (with optional YAML
// frontmatter) into the schema frontmatter. Equivalent to
// `markdown.ParseBytesPermissive` but returning the frontmatter directly
// for tests that only care about the parsed YAML.
//
// Panics on parse error: contract tests MUST use well-formed frontmatter,
// and a panic surfaces a typo immediately rather than a t.Fatal deep in
// test logic.
func markedupParseFrontmatter(raw []byte) markedupFrontmatter {
	page, err := markdown.ParseBytesPermissive(raw)
	if err != nil {
		panic("markedupParseFrontmatter: " + err.Error())
	}
	return page.Frontmatter
}
