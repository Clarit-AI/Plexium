package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrontmatterValidator_Validate(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create modules directory
	os.MkdirAll(filepath.Join(wikiDir, "modules"), 0755)

	// Create a page with valid frontmatter
	validPage := `---
title: Auth Module
ownership: managed
review-status: unreviewed
---
# Auth
Content here.
`
	os.WriteFile(filepath.Join(wikiDir, "modules/auth.md"), []byte(validPage), 0644)

	// Create a page with invalid frontmatter (missing title)
	invalidPage := `---
ownership: managed
---
# Auth
Content here.
`
	os.WriteFile(filepath.Join(wikiDir, "modules/invalid.md"), []byte(invalidPage), 0644)

	// Create a page with invalid ownership
	badOwnership := `---
title: Bad Page
ownership: invalid-value
---
# Bad
Content.
`
	os.WriteFile(filepath.Join(wikiDir, "bad.md"), []byte(badOwnership), 0644)

	// Create a page with empty title
	emptyTitle := `---
title: ""
ownership: managed
---
# Bad
`
	os.WriteFile(filepath.Join(wikiDir, "empty.md"), []byte(emptyTitle), 0644)

	// Navigation hubs should be skipped
	os.WriteFile(filepath.Join(wikiDir, "_Sidebar.md"), []byte("# Sidebar\n"), 0644)
	os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte("# Home\n"), 0644)

	validator := NewFrontmatterValidator(wikiDir)
	result, err := validator.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if result.Valid {
		t.Fatal("Expected validation to fail, but it passed")
	}

	// Should have 3 errors: invalid.md (missing title), bad.md (bad ownership), empty.md (empty title)
	if len(result.Errors) != 3 {
		t.Errorf("Expected 3 errors, got %d: %+v", len(result.Errors), result.Errors)
	}
}

func TestFrontmatterValidator_ValidPages(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".wiki")
	os.MkdirAll(wikiDir, 0755)

	// Create a valid page
	validPage := `---
title: Valid Module
ownership: managed
review-status: human-verified
confidence: high
tags:
  - auth
  - security
source-files:
  - src/auth.go
---
# Valid
Content.
`
	os.WriteFile(filepath.Join(wikiDir, "valid.md"), []byte(validPage), 0644)

	validator := NewFrontmatterValidator(wikiDir)
	result, err := validator.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected validation to pass, got errors: %+v", result.Errors)
	}
}

func TestFrontmatterValidator_ValidOwnershipValues(t *testing.T) {
	testCases := []struct {
		ownership string
		wantErr   bool
	}{
		{"managed", false},
		{"human-authored", false},
		{"co-maintained", false},
		{"invalid", true},
		{"", false}, // empty is caught by required field check
	}

	for _, tc := range testCases {
		t.Run(tc.ownership, func(t *testing.T) {
			if tc.ownership == "" {
				return // Skip empty - tested elsewhere
			}

			valid := false
			for _, v := range ValidOwnershipValues {
				if tc.ownership == v {
					valid = true
					break
				}
			}

			if valid == tc.wantErr {
				t.Errorf("ownership %q: valid=%v, wantErr=%v", tc.ownership, valid, tc.wantErr)
			}
		})
	}
}
