package generate

import (
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/template"
)

func TestHomeGenerator_Generate(t *testing.T) {
	engine, _ := template.DefaultEngine()
	gen := NewHomeGenerator(engine)

	sections := []SectionInfo{
		{Name: "Modules", Slug: "modules", Summary: "Source modules"},
		{Name: "Decisions", Slug: "decisions", Summary: "Architecture decisions"},
	}

	doc, err := gen.Generate("My Project", "A test project", sections, "# Welcome")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if doc.Frontmatter["title"] != "My Project" {
		t.Errorf("title = %v, want My Project", doc.Frontmatter["title"])
	}
	if doc.Frontmatter["ownership"] != "managed" {
		t.Errorf("ownership = %v, want managed", doc.Frontmatter["ownership"])
	}
}

func TestSidebarGenerator_Generate(t *testing.T) {
	engine, _ := template.DefaultEngine()
	gen := NewSidebarGenerator(engine)

	pages := []PageInfo{
		{Path: "modules/auth.md", Title: "Auth", Section: "Modules", NavOrder: 1},
		{Path: "modules/api.md", Title: "API", Section: "Modules", NavOrder: 2},
		{Path: "decisions/001-foo.md", Title: "Foo Decision", Section: "Decisions", NavOrder: 1},
	}

	doc, err := gen.Generate(pages)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check that body contains expected sections
	if !strings.Contains(doc.Body, "## Modules") {
		t.Error("Missing Modules section")
	}
	if !strings.Contains(doc.Body, "## Decisions") {
		t.Error("Missing Decisions section")
	}
}

func TestIndexGenerator_Generate(t *testing.T) {
	gen := NewIndexGenerator()

	entries := []IndexEntry{
		{Path: "modules/auth.md", Title: "Auth", Section: "Modules", Summary: "Auth module", Ownership: "managed"},
		{Path: "decisions/001-foo.md", Title: "Foo", Section: "Decisions", Summary: "", Ownership: "managed"},
	}

	doc, err := gen.Generate(entries)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(doc.Body, "modules/auth.md") {
		t.Error("Missing modules/auth.md entry")
	}
	if !strings.Contains(doc.Body, "decisions/001-foo.md") {
		t.Error("Missing decisions/001-foo.md entry")
	}
}

func TestIndexGenerator_GenerateJSON(t *testing.T) {
	gen := NewIndexGenerator()

	entries := []IndexEntry{
		{Path: "modules/auth.md", Title: "Auth", Section: "Modules", Ownership: "managed"},
	}

	json, err := gen.GenerateJSON(entries)
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if !strings.Contains(json, `"path": "modules/auth.md"`) {
		t.Error("Missing path in JSON")
	}
	if !strings.Contains(json, `"title": "Auth"`) {
		t.Error("Missing title in JSON")
	}
}

func TestFooterGenerator_Generate(t *testing.T) {
	gen := NewFooterGenerator()

	doc, err := gen.Generate("0.2.0")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(doc.Body, "Plexium") {
		t.Error("Missing Plexium reference in footer")
	}
	if !strings.Contains(doc.Body, "0.2.0") {
		t.Error("Missing version in footer")
	}
}
