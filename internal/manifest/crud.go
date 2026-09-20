package manifest

import (
	"fmt"
	"time"

	"github.com/bmatcuk/doublestar/v2"
)

// PagesFromSource returns all wiki pages that were generated from the given source path.
// Supports glob-style patterns: "src/auth/**" matches any source file under src/auth/.
func (m *Manager) PagesFromSource(sourcePath string) ([]PageEntry, error) {
	manifest, err := m.Load()
	if err != nil {
		return nil, err
	}

	var results []PageEntry
	for _, page := range manifest.Pages {
		for _, sf := range page.SourceFiles {
			if sf.Path == sourcePath || matchGlob(sourcePath, sf.Path) {
				results = append(results, page)
				break
			}
		}
	}
	return results, nil
}

// SourcesFromPage returns all source files that feed into the given wiki page.
func (m *Manager) SourcesFromPage(wikiPath string) ([]SourceFile, error) {
	manifest, err := m.Load()
	if err != nil {
		return nil, err
	}

	for _, page := range manifest.Pages {
		if page.WikiPath == wikiPath {
			return page.SourceFiles, nil
		}
	}
	return nil, nil
}

// IsManaged returns true if the wiki page is managed by Plexium.
func (m *Manager) IsManaged(wikiPath string) (bool, error) {
	manifest, err := m.Load()
	if err != nil {
		return false, err
	}

	for _, page := range manifest.Pages {
		if page.WikiPath == wikiPath {
			return true, nil
		}
	}
	return false, nil
}

// GetPage returns a page entry by wiki path, or nil if not found.
func (m *Manager) GetPage(wikiPath string) (*PageEntry, error) {
	manifest, err := m.Load()
	if err != nil {
		return nil, err
	}

	for i := range manifest.Pages {
		if manifest.Pages[i].WikiPath == wikiPath {
			return &manifest.Pages[i], nil
		}
	}
	return nil, nil
}

// UpsertPage adds or updates a page entry. If a page with the same WikiPath exists,
// it is replaced. Otherwise the page is appended.
func (m *Manager) UpsertPage(entry PageEntry) error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	found := false
	for i, page := range manifest.Pages {
		if page.WikiPath == entry.WikiPath {
			// Never overwrite human-authored pages
			if page.Ownership == "human-authored" && entry.Ownership == "managed" {
				return fmt.Errorf("cannot overwrite human-authored page: %s", page.WikiPath)
			}
			manifest.Pages[i] = entry
			found = true
			break
		}
	}

	if !found {
		manifest.Pages = append(manifest.Pages, entry)
	}

	return m.Save(manifest)
}

// RemovePage removes a page entry by wiki path.
func (m *Manager) RemovePage(wikiPath string) error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	filtered := make([]PageEntry, 0, len(manifest.Pages))
	for _, page := range manifest.Pages {
		if page.WikiPath != wikiPath {
			filtered = append(filtered, page)
		}
	}
	manifest.Pages = filtered

	return m.Save(manifest)
}

// AddUnmanaged records an unmanaged wiki page.
func (m *Manager) AddUnmanaged(entry UnmanagedEntry) error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	// Check if already tracked
	for _, u := range manifest.UnmanagedPages {
		if u.WikiPath == entry.WikiPath {
			return nil // Already tracked
		}
	}

	manifest.UnmanagedPages = append(manifest.UnmanagedPages, entry)
	return m.Save(manifest)
}

// RemoveUnmanaged removes an unmanaged page entry.
func (m *Manager) RemoveUnmanaged(wikiPath string) error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	filtered := make([]UnmanagedEntry, 0, len(manifest.UnmanagedPages))
	for _, u := range manifest.UnmanagedPages {
		if u.WikiPath != wikiPath {
			filtered = append(filtered, u)
		}
	}
	manifest.UnmanagedPages = filtered

	return m.Save(manifest)
}

// DetectStalePages returns all pages where source files have changed since
// the wiki page text was last validated against them.
//
// For each source file, we compare the current file hash to ValidatedHash.
// If ValidatedHash is empty (legacy v1 manifest), the page is treated as
// never-validated and is flagged stale so the operator regenerates or marks
// the page reviewed. The legacy Hash field is intentionally ignored here:
// keeping a separate observed-vs-validated distinction is what stops a plain
// plexium sync from advancing freshness without proof of wiki refresh.
//
// A page is stale if any of its source files is stale.
func (m *Manager) DetectStalePages(hashFn func(path string) (string, error)) ([]PageEntry, error) {
	manifest, err := m.Load()
	if err != nil {
		return nil, err
	}

	var stale []PageEntry
	for _, page := range manifest.Pages {
		if page.Ownership == "human-authored" {
			continue // Never flag human-authored pages as stale
		}

		for _, sf := range page.SourceFiles {
			currentHash, err := hashFn(sf.Path)
			if err != nil {
				// File might have been deleted — that's stale
				stale = append(stale, page)
				break
			}
			baseline := sf.ValidatedHash
			if baseline == "" {
				// Empty ValidatedHash means "never validated". Treat as
				// stale so the user must regenerate or mark-reviewed the
				// page to advance freshness. This is the v1→v2 migration:
				// pages written by older plexium versions will need one
				// explicit validation step.
				stale = append(stale, page)
				break
			}
			if currentHash != baseline {
				stale = append(stale, page)
				break
			}
		}
	}
	return stale, nil
}

// MarkPageValidated records that the given wiki page's content has been
// validated against the current source-file hashes. Each SourceFile in the
// page is updated with ValidatedHash set to its observed Hash and
// LastValidatedAt set to the supplied timestamp. Caller is responsible for
// persisting the manifest afterwards (or for invoking Save themselves when
// running outside this method).
//
// Returns true if the page was found and updated, false if no page with the
// given wiki path exists in the manifest. An error is returned only when
// loading the manifest fails.
func (m *Manager) MarkPageValidated(wikiPath, validatedAt string) (bool, error) {
	manifest, err := m.Load()
	if err != nil {
		return false, err
	}

	for i, page := range manifest.Pages {
		if page.WikiPath != wikiPath {
			continue
		}
		// Take a copy so we don't mutate the in-memory page while iterating.
		updated := page
		for j, sf := range updated.SourceFiles {
			// Promote the most recently observed hash (sf.Hash) to the
			// validated slot. If sf.Hash is empty (legacy data), leave
			// the validated slot empty so the next detection still flags
			// the page as stale.
			if sf.Hash != "" {
				updated.SourceFiles[j].ValidatedHash = sf.Hash
				updated.SourceFiles[j].LastValidatedAt = validatedAt
			}
		}
		manifest.Pages[i] = updated
		if err := m.Save(manifest); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// UpdatePublishTimestamp updates the lastPublishTimestamp to now.
func (m *Manager) UpdatePublishTimestamp() error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	manifest.LastPublishTimestamp = time.Now().UTC().Format(time.RFC3339)
	return m.Save(manifest)
}

// UpdateProcessedCommit updates the lastProcessedCommit.
func (m *Manager) UpdateProcessedCommit(commit string) error {
	manifest, err := m.Load()
	if err != nil {
		return err
	}

	manifest.LastProcessedCommit = commit
	return m.Save(manifest)
}

// matchGlob delegates to doublestar.Match for proper ** glob support.
func matchGlob(pattern, name string) bool {
	// doublestar.Match handles ** recursively, unlike filepath.Match.
	matched, err := doublestar.Match(pattern, name)
	if err != nil {
		return false
	}
	return matched
}
