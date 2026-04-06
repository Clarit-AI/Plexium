package generate

import (
	"testing"

	"github.com/Clarit-AI/Plexium/internal/scanner"
)

func TestTaxonomy_Classify(t *testing.T) {
	tax := NewTaxonomy()

	tests := []struct {
		name     string
		file     scanner.File
		wantType string
		wantSlug string
	}{
		{
			name: "README becomes Home",
			file: scanner.File{Path: "README.md"},
			wantType: "home",
			wantSlug: "Home",
		},
		{
			name: "src/auth/ becomes modules/auth",
			file: scanner.File{Path: "src/auth/middleware.go"},
			wantType: "module",
			wantSlug: "auth",
		},
		{
			name: "ADR in adr/ directory",
			file: scanner.File{Path: "adr/001-chose-postgres.md"},
			wantType: "decision",
			wantSlug: "001-chose-postgres",
		},
		{
			name: "ADR in docs/decisions/",
			file: scanner.File{Path: "docs/decisions/002-event-sourcing.md"},
			wantType: "decision",
			wantSlug: "002-event-sourcing",
		},
		{
			name: "docs/concepts/ file",
			file: scanner.File{Path: "docs/concepts/authentication.md"},
			wantType: "concept",
			wantSlug: "authentication",
		},
		{
			name: "docs/patterns/ file",
			file: scanner.File{Path: "docs/patterns/error-handling.md"},
			wantType: "pattern",
			wantSlug: "error-handling",
		},
		{
			name: "docs/architecture/ file",
			file: scanner.File{Path: "docs/architecture/overview.md"},
			wantType: "architecture",
			wantSlug: "overview",
		},
		{
			name: "docs root file",
			file: scanner.File{Path: "docs/guide.md"},
			wantType: "guide",
			wantSlug: "guide",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tax.Classify(tt.file)
			if err != nil {
				t.Fatalf("Classify() error = %v", err)
			}
			if got.PageType != tt.wantType {
				t.Errorf("PageType = %v, want %v", got.PageType, tt.wantType)
			}
			if got.Slug != tt.wantSlug {
				t.Errorf("Slug = %v, want %v", got.Slug, tt.wantSlug)
			}
		})
	}
}

func TestTaxonomy_ADRPatterns(t *testing.T) {
	tax := NewTaxonomy()

	tests := []struct {
		filename string
		wantNum  string
		wantSlug string
	}{
		{"001-chose-postgres.md", "001", "001-chose-postgres"},
		{"2024-01-15-use-kafka.md", "", "2024-01-15-use-kafka"},
		{"42-my-decision.md", "42", "42-my-decision"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			file := scanner.File{Path: "adr/" + tt.filename}
			got, err := tax.Classify(file)
			if err != nil {
				t.Fatalf("Classify() error = %v", err)
			}
			if got.PageType != "decision" {
				t.Errorf("PageType = %v, want decision", got.PageType)
			}
			if got.Slug != tt.wantSlug {
				t.Errorf("Slug = %v, want %v", got.Slug, tt.wantSlug)
			}
		})
	}
}

func TestFormatTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"auth", "Auth"},
		{"auth-middleware", "Auth Middleware"},
		{"my_api_client", "My Api Client"},
		{"APIv2", "APIv2"},
		{"auth middleware", "Auth Middleware"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatTitle(tt.input)
			if got != tt.want {
				t.Errorf("formatTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
