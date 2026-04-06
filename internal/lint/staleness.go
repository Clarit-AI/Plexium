package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// StalenessDetector compares source file hashes vs manifest to detect drift.
type StalenessDetector struct {
	manifestMgr *manifest.Manager
	wikiPath    string
}

// StalenessResult contains pages with stale content.
type StalenessResult struct {
	StalePages []StalePage
}

// StalePage represents a page where source content has changed.
type StalePage struct {
	WikiPath        string
	SourceFiles     []string // Which sources changed
	LastUpdated     string
	DaysSinceUpdate int
	Severity        string // "error" or "warning"
}

// StalenessThresholdDays is the number of days after which a page is considered stale
const StalenessThresholdDays = 30

// NewStalenessDetector creates a new StalenessDetector.
func NewStalenessDetector(wikiPath string, manifestMgr *manifest.Manager) *StalenessDetector {
	return &StalenessDetector{
		manifestMgr: manifestMgr,
		wikiPath:    wikiPath,
	}
}

// Detect finds pages where source hashes differ from manifest.
func (d *StalenessDetector) Detect() (*StalenessResult, error) {
	m, err := d.manifestMgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	var stale []StalePage

	for _, entry := range m.Pages {
		var changedSources []string

		// Check each source file's hash
		for _, sf := range entry.SourceFiles {
			// Skip if path is not absolute
			if !filepath.IsAbs(sf.Path) {
				sf.Path = filepath.Join(d.wikiPath, "..", sf.Path)
			}

			currentHash, err := manifest.ComputeHash(sf.Path)
			if err != nil {
				// Source file missing → will be handled by manifest validator
				continue
			}

			if currentHash != sf.Hash {
				changedSources = append(changedSources, sf.Path)
			}
		}

		// Calculate days since last update
		daysSinceUpdate := 0
		if entry.LastUpdated != "" {
			if t, err := time.Parse("2006-01-02", entry.LastUpdated); err == nil {
				daysSinceUpdate = int(time.Since(t).Hours() / 24)
			}
		}

		// Determine severity
		severity := "warning"
		if len(changedSources) > 0 {
			severity = "error" // Source changed → wiki may be outdated
		} else if daysSinceUpdate >= StalenessThresholdDays {
			severity = "warning" // Just stale by time
		} else {
			continue // Not stale
		}

		// Make paths relative for display
		var relSources []string
		for _, s := range changedSources {
			if rel, err := filepath.Rel(d.wikiPath, s); err == nil {
				relSources = append(relSources, rel)
			} else {
				relSources = append(relSources, s)
			}
		}

		stale = append(stale, StalePage{
			WikiPath:        entry.WikiPath,
			SourceFiles:     relSources,
			LastUpdated:     entry.LastUpdated,
			DaysSinceUpdate: daysSinceUpdate,
			Severity:        severity,
		})
	}

	return &StalenessResult{StalePages: stale}, nil
}

// GetWikiFiles returns all wiki-managed file paths from the manifest.
func GetWikiFiles(m *manifest.Manifest) ([]string, error) {
	wikiFiles := make([]string, 0, len(m.Pages))
	for _, p := range m.Pages {
		wikiFiles = append(wikiFiles, p.WikiPath)
	}
	return wikiFiles, nil
}

// IsSourceFileMissing checks if a source file path exists on disk.
func IsSourceFileMissing(sourcePath string) bool {
	// Try as absolute path
	if _, err := os.Stat(sourcePath); err == nil {
		return false
	}
	// Try as relative to wiki root parent
	relPath := filepath.Join("..", sourcePath)
	if _, err := os.Stat(relPath); err == nil {
		return false
	}
	return true
}

// NormalizeSourcePath normalizes a source path to absolute.
func NormalizeSourcePath(sourcePath, wikiPath string) string {
	if filepath.IsAbs(sourcePath) {
		return sourcePath
	}
	return filepath.Join(wikiPath, "..", sourcePath)
}
