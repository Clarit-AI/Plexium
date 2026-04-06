package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/markdown"
)

// FrontmatterValidator verifies all wiki pages have valid frontmatter.
type FrontmatterValidator struct {
	wikiPath string
}

// FrontmatterValidation contains frontmatter validation results.
type FrontmatterValidation struct {
	Valid  bool
	Errors []FrontmatterError
}

// FrontmatterError represents a frontmatter validation error.
type FrontmatterError struct {
	WikiPath string
	Field    string
	Expected string
	Actual   string
	Message  string
}

// RequiredFrontmatterFields that must be present.
var RequiredFrontmatterFields = []string{"title", "ownership"}

// ValidOwnershipValues for validation.
var ValidOwnershipValues = []string{"managed", "human-authored", "co-maintained"}

// ValidReviewStatuses for validation.
var ValidReviewStatuses = []string{"unreviewed", "human-verified", "stale"}

// ValidConfidenceLevels for validation.
var ValidConfidenceLevels = []string{"high", "medium", "low"}

// NewFrontmatterValidator creates a new FrontmatterValidator.
func NewFrontmatterValidator(wikiPath string) *FrontmatterValidator {
	return &FrontmatterValidator{wikiPath: wikiPath}
}

// Validate checks all wiki pages for valid frontmatter.
func (v *FrontmatterValidator) Validate() (*FrontmatterValidation, error) {
	result := &FrontmatterValidation{Valid: true}

	err := filepath.Walk(v.wikiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		relPath, _ := filepath.Rel(v.wikiPath, path)

		// Skip navigation hub files - they may have different frontmatter
		baseName := filepath.Base(path)
		if NavigationHubPages[baseName] {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		doc, err := markdown.Parse(string(content))
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, FrontmatterError{
				WikiPath: relPath,
				Field:    "format",
				Message:  fmt.Sprintf("failed to parse frontmatter: %v", err),
			})
			return nil
		}

		// Check required fields
		for _, field := range RequiredFrontmatterFields {
			if _, ok := doc.Frontmatter[field]; !ok {
				result.Valid = false
				result.Errors = append(result.Errors, FrontmatterError{
					WikiPath: relPath,
					Field:    field,
					Expected: "present",
					Actual:   "missing",
					Message:  fmt.Sprintf("missing required frontmatter field: %s", field),
				})
			}
		}

		// Validate title is non-empty string
		if title, ok := doc.Frontmatter["title"]; ok {
			if titleStr, ok := title.(string); !ok || strings.TrimSpace(titleStr) == "" {
				result.Valid = false
				result.Errors = append(result.Errors, FrontmatterError{
					WikiPath: relPath,
					Field:    "title",
					Expected: "non-empty string",
					Actual:   fmt.Sprintf("%v", title),
					Message:  "title must be a non-empty string",
				})
			}
		}

		// Validate ownership value
		if ownership, ok := doc.Frontmatter["ownership"].(string); ok {
			valid := false
			for _, v := range ValidOwnershipValues {
				if ownership == v {
					valid = true
					break
				}
			}
			if !valid {
				result.Valid = false
				result.Errors = append(result.Errors, FrontmatterError{
					WikiPath: relPath,
					Field:    "ownership",
					Expected: strings.Join(ValidOwnershipValues, ", "),
					Actual:   ownership,
					Message:  fmt.Sprintf("invalid ownership value: %s", ownership),
				})
			}
		}

		// Validate review-status if present
		if reviewStatus, ok := doc.Frontmatter["review-status"].(string); ok {
			valid := false
			for _, v := range ValidReviewStatuses {
				if reviewStatus == v {
					valid = true
					break
				}
			}
			if !valid {
				result.Valid = false
				result.Errors = append(result.Errors, FrontmatterError{
					WikiPath: relPath,
					Field:    "review-status",
					Expected: strings.Join(ValidReviewStatuses, ", "),
					Actual:   reviewStatus,
					Message:  fmt.Sprintf("invalid review-status value: %s", reviewStatus),
				})
			}
		}

		// Validate confidence if present
		if confidence, ok := doc.Frontmatter["confidence"].(string); ok {
			valid := false
			for _, v := range ValidConfidenceLevels {
				if confidence == v {
					valid = true
					break
				}
			}
			if !valid {
				result.Valid = false
				result.Errors = append(result.Errors, FrontmatterError{
					WikiPath: relPath,
					Field:    "confidence",
					Expected: strings.Join(ValidConfidenceLevels, ", "),
					Actual:   confidence,
					Message:  fmt.Sprintf("invalid confidence value: %s", confidence),
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking wiki: %w", err)
	}

	return result, nil
}
