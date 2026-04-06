package pageindex

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Retriever implements the retrieval chain for plexium retrieve.
type Retriever struct {
	WikiRoot    string
	PageIdx     *PageIndex
	UseFallback bool // true if PageIndex not available
}

// RetrieveResult is the output of a retrieve operation.
type RetrieveResult struct {
	Query  string    `json:"query"`
	Pages  []PageHit `json:"pages"`
	Method string    `json:"method"` // "pageindex" or "fallback"
}

// PageHit represents a single retrieval hit.
type PageHit struct {
	Path      string  `json:"path"`
	Title     string  `json:"title"`
	Summary   string  `json:"summary"`
	Relevance float64 `json:"relevance"`
}

// NewRetriever creates a new Retriever for the given wiki root.
// It attempts to load the PageIndex; falls back to index scanning if unavailable.
func NewRetriever(wikiRoot string) *Retriever {
	idx := New(wikiRoot)
	useFallback := false

	if err := idx.Load(); err != nil {
		useFallback = true
	}

	return &Retriever{
		WikiRoot:    wikiRoot,
		PageIdx:     idx,
		UseFallback: useFallback,
	}
}

// Retrieve performs a query using PageIndex, falling back to index scan.
func (r *Retriever) Retrieve(query string) (*RetrieveResult, error) {
	if r.UseFallback {
		return r.fallbackRetrieve(query)
	}

	results := r.PageIdx.Search(query)
	if len(results) == 0 {
		// Try fallback when PageIndex yields nothing
		return r.fallbackRetrieve(query)
	}

	hits := make([]PageHit, len(results))
	for i, sr := range results {
		hits[i] = PageHit{
			Path:      sr.Page.Path,
			Title:     sr.Page.Title,
			Summary:   sr.Page.Summary,
			Relevance: sr.Score,
		}
	}

	return &RetrieveResult{
		Query:  query,
		Pages:  hits,
		Method: "pageindex",
	}, nil
}

// fallbackRetrieve uses _index.md and content grep when PageIndex unavailable.
func (r *Retriever) fallbackRetrieve(query string) (*RetrieveResult, error) {
	queryLower := strings.ToLower(query)
	terms := strings.Fields(queryLower)
	if len(terms) == 0 {
		return &RetrieveResult{
			Query:  query,
			Pages:  []PageHit{},
			Method: "fallback",
		}, nil
	}

	var hits []PageHit

	// Strategy 1: Parse _index.md for page entries
	indexPath := filepath.Join(r.WikiRoot, "_index.md")
	if indexContent, err := os.ReadFile(indexPath); err == nil {
		hits = append(hits, r.searchIndex(string(indexContent), terms)...)
	}

	// Strategy 2: Scan markdown files directly for content matches
	contentHits, err := r.scanContent(terms)
	if err == nil {
		// Merge, avoiding duplicates
		seen := make(map[string]bool)
		for _, h := range hits {
			seen[h.Path] = true
		}
		for _, h := range contentHits {
			if !seen[h.Path] {
				hits = append(hits, h)
			}
		}
	}

	// Sort by relevance descending
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Relevance > hits[j].Relevance
	})

	// Limit to top 20
	if len(hits) > 20 {
		hits = hits[:20]
	}

	return &RetrieveResult{
		Query:  query,
		Pages:  hits,
		Method: "fallback",
	}, nil
}

// indexEntryRegex matches lines like: - [[path|Title]]: Summary
var indexEntryRegex = regexp.MustCompile(`-\s+\[\[([^\]|]+)(?:\|([^\]]+))?\]\](?::\s*(.*))?`)

// searchIndex parses _index.md content and matches entries against query terms.
func (r *Retriever) searchIndex(content string, terms []string) []PageHit {
	var hits []PageHit
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		matches := indexEntryRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		path := matches[1]
		title := matches[2]
		if title == "" {
			title = path
		}
		summary := ""
		if len(matches) > 3 {
			summary = matches[3]
		}

		lineLower := strings.ToLower(line)
		var score float64
		for _, term := range terms {
			if strings.Contains(strings.ToLower(title), term) {
				score += 1.0
			}
			if strings.Contains(strings.ToLower(summary), term) {
				score += 0.6
			}
			if strings.Contains(lineLower, term) && score == 0 {
				score += 0.3
			}
		}

		if score > 0 {
			// Normalize to 0-1 range (max possible score = len(terms) * 1.6)
			maxPossible := float64(len(terms)) * 1.6
			normalized := score / maxPossible
			if normalized > 1.0 {
				normalized = 1.0
			}

			hits = append(hits, PageHit{
				Path:      path,
				Title:     title,
				Summary:   summary,
				Relevance: normalized,
			})
		}
	}

	return hits
}

// scanContent walks wiki markdown files looking for term matches.
func (r *Retriever) scanContent(terms []string) ([]PageHit, error) {
	var hits []PageHit

	err := filepath.Walk(r.WikiRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors in walk
		}

		baseName := filepath.Base(path)

		// Skip hidden files/dirs
		if strings.HasPrefix(baseName, ".") && path != r.WikiRoot {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip raw/ directory
		if info.IsDir() && baseName == "raw" {
			return filepath.SkipDir
		}

		if info.IsDir() || !strings.HasSuffix(baseName, ".md") {
			return nil
		}

		// Skip _index.md (already handled)
		if baseName == "_index.md" {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		contentLower := strings.ToLower(string(content))
		relPath, _ := filepath.Rel(r.WikiRoot, path)

		var score float64
		for _, term := range terms {
			if strings.Contains(contentLower, term) {
				score += 0.4
			}
			// Boost for filename match
			if strings.Contains(strings.ToLower(relPath), term) {
				score += 0.6
			}
		}

		if score > 0 {
			// Parse for title
			doc, parseErr := parseQuickTitle(string(content))
			title := strings.TrimSuffix(baseName, ".md")
			if parseErr == nil && doc != "" {
				title = doc
			}

			maxPossible := float64(len(terms)) * 1.0
			normalized := score / maxPossible
			if normalized > 1.0 {
				normalized = 1.0
			}

			hits = append(hits, PageHit{
				Path:      relPath,
				Title:     title,
				Relevance: normalized,
			})
		}

		return nil
	})

	return hits, err
}

// parseQuickTitle extracts the title from frontmatter or first heading.
func parseQuickTitle(content string) (string, error) {
	// Check frontmatter
	if strings.HasPrefix(content, "---\n") {
		endIdx := strings.Index(content[4:], "\n---\n")
		if endIdx >= 0 {
			fm := content[4 : 4+endIdx]
			for _, line := range strings.Split(fm, "\n") {
				if strings.HasPrefix(line, "title:") {
					title := strings.TrimPrefix(line, "title:")
					title = strings.TrimSpace(title)
					title = strings.Trim(title, "\"'")
					if title != "" {
						return title, nil
					}
				}
			}
		}
	}

	// Fall back to first heading
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "#")), nil
		}
	}

	return "", fmt.Errorf("no title found")
}
