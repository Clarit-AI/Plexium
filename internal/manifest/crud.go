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

// DetectStalePages returns all pages where source files have changed since last processed.
// Compares stored hashes against current file content hashes.
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
			if currentHash != sf.Hash {
				stale = append(stale, page)
				break
			}
		}
	}
	return stale, nil
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
