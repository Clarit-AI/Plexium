package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// ManifestValidator validates manifest structure and references.
type ManifestValidator struct {
	manifestMgr *manifest.Manager
	wikiPath    string
}

// ManifestValidation contains validation results.
type ManifestValidation struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// ValidationError represents a fatal validation error.
type ValidationError struct {
	Path    string // manifest.json path or wiki path
	Field   string
	Message string
}

// ValidationWarning represents a non-fatal issue.
type ValidationWarning struct {
	Path    string
	Field   string
	Message string
}

// gitSHARegex matches valid git SHAs (40 hex characters)
var gitSHARegex = regexp.MustCompile(`^[a-f0-9]{40}$`)

// NewManifestValidator creates a new ManifestValidator.
func NewManifestValidator(wikiPath string, manifestMgr *manifest.Manager) *ManifestValidator {
	return &ManifestValidator{
		manifestMgr: manifestMgr,
		wikiPath:    wikiPath,
	}
}

// Validate checks manifest consistency.
func (v *ManifestValidator) Validate() (*ManifestValidation, error) {
	m, err := v.manifestMgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	result := &ManifestValidation{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Check version
	if m.Version == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "manifest.json",
			Field:   "version",
			Message: "version field is required",
		})
	}

	// Build set of all wiki pages for link validation
	wikiPages := make(map[string]bool)
	err = filepath.Walk(v.wikiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(v.wikiPath, path)
			wikiPages[rel] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking wiki: %w", err)
	}

	// Validate each page entry
	for _, entry := range m.Pages {
		// Check wikiPath exists
		if entry.WikiPath != "" {
			wikiPath := filepath.Join(v.wikiPath, entry.WikiPath)
			if _, err := os.Stat(wikiPath); os.IsNotExist(err) {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Path:    "manifest.json",
					Field:   "wikiPath",
					Message: fmt.Sprintf("wiki path does not exist: %s", entry.WikiPath),
				})
			}
		}

		// Check ownership value
		validOwnership := map[string]bool{
			"managed": true, "human-authored": true, "co-maintained": true,
		}
		if entry.Ownership != "" && !validOwnership[entry.Ownership] {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Path:    entry.WikiPath,
				Field:   "ownership",
				Message: fmt.Sprintf("invalid ownership value: %s (must be managed, human-authored, or co-maintained)", entry.Ownership),
			})
		}

		// Check source files exist (if paths are absolute)
		for _, sf := range entry.SourceFiles {
			if filepath.IsAbs(sf.Path) {
				if _, err := os.Stat(sf.Path); os.IsNotExist(err) {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Path:    "manifest.json",
						Field:   "sourceFiles",
						Message: fmt.Sprintf("source file does not exist: %s", sf.Path),
					})
				}
			}
		}

		// Validate inbound links reference existing wiki pages
		for _, link := range entry.InboundLinks {
			normalized := link
			if !strings.HasSuffix(normalized, ".md") {
				normalized += ".md"
			}
			if !wikiPages[normalized] && !wikiPages[link] {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Path:    entry.WikiPath,
					Field:   "inboundLinks",
					Message: fmt.Sprintf("inbound link references non-existent page: %s", link),
				})
			}
		}

		// Validate outbound links reference existing wiki pages
		for _, link := range entry.OutboundLinks {
			normalized := link
			if !strings.HasSuffix(normalized, ".md") {
				normalized += ".md"
			}
			if !wikiPages[normalized] && !wikiPages[link] {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Path:    entry.WikiPath,
					Field:   "outboundLinks",
					Message: fmt.Sprintf("outbound link references non-existent page: %s", link),
				})
			}
		}

		// Check lastProcessedCommit format (if present)
		if entry.LastUpdated != "" {
			// Check if it's a valid date format
			if len(entry.LastUpdated) == 10 && strings.Count(entry.LastUpdated, "-") == 2 {
				// Likely a date, validate format more thoroughly if needed
			}
		}
	}

	// Validate lastProcessedCommit is a valid git SHA (if present)
	if m.LastProcessedCommit != "" && !gitSHARegex.MatchString(m.LastProcessedCommit) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Path:    "manifest.json",
			Field:   "lastProcessedCommit",
			Message: "lastProcessedCommit should be a valid 40-character git SHA",
		})
	}

	return result, nil
}
