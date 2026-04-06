package generate

import (
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/template"
)

func TestDecisionGenerator_ParseADR(t *testing.T) {
	engine, _ := template.DefaultEngine()
	gen := NewDecisionGenerator(engine)

	// Register a simple template
	gen.engine.Register("decision.md", `---
title: {{.Title}}
status: {{.Status}}
date: {{.Date}}
---

# {{.Title}}

## Context
{{.Context}}

## Decision
{{.Decision}}

## Consequences
{{.Consequences}}
`)

	tests := []struct {
		name        string
		path        string
		content     string
		wantTitle   string
		wantStatus  string
		wantContext bool
	}{
		{
			name: "standard ADR format",
			path: "adr/001-chose-postgres.md",
			content: `# ADR 001: Chose PostgreSQL for Primary Database

**Status:** Accepted
**Date:** 2024-01-15

## Context

We needed a primary database that supports ACID compliance and rich indexing.

## Decision

We chose PostgreSQL because of its robustness and feature set.

## Consequences

- ACID compliance
- Rich indexing support
`,
			wantTitle:   "Chose PostgreSQL for Primary Database",
			wantStatus:  "Accepted",
			wantContext: true,
		},
		{
			name: "minimal ADR",
			path: "adr/002-foo.md",
			content: `# ADR 002: Foo

**Status:** Proposed

## Context

Context text.

## Decision

Decision text.
`,
			wantTitle:   "Foo",
			wantStatus:  "Proposed",
			wantContext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := gen.parseADR(tt.path, tt.content)
			if err != nil {
				t.Fatalf("parseADR() error = %v", err)
			}

			if data.Title != tt.wantTitle {
				t.Errorf("Title = %v, want %v", data.Title, tt.wantTitle)
			}
			if data.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", data.Status, tt.wantStatus)
			}
			if tt.wantContext && data.Context == "" {
				t.Error("Context is empty, want non-empty")
			}
		})
	}
}

func TestDecisionGenerator_Generate(t *testing.T) {
	engine, _ := template.DefaultEngine()
	gen := NewDecisionGenerator(engine)

	gen.engine.Register("decision.md", `---
title: {{.Title}}
ownership: managed
date: {{.Date}}
status: {{.Status}}
---

# {{.Title}}

## Context
{{.Context}}

## Decision
{{.Decision}}

## Consequences
{{.Consequences}}
`)

	adrContent := `# ADR 001: Test Decision

**Status:** Accepted
**Date:** 2024-01-15

## Context

The context of the decision.

## Decision

The decision made.

## Consequences

- Positive: It works
- Negative: None
`

	doc, err := gen.Generate("adr/001-test.md", adrContent)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if doc.Frontmatter["title"] != "Test Decision" {
		t.Errorf("title frontmatter = %v, want Test Decision", doc.Frontmatter["title"])
	}
	if doc.Frontmatter["status"] != "Accepted" {
		t.Errorf("status frontmatter = %v, want Accepted", doc.Frontmatter["status"])
	}
	if !strings.Contains(doc.Body, "The context of the decision") {
		t.Errorf("Body missing context")
	}
}
