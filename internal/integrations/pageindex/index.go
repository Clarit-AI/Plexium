package pageindex

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/markdown"
)

// PageInfo represents metadata about a single wiki page.
type PageInfo struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title"`
	Section     string                 `json:"section"` // e.g., "modules", "decisions", "concepts"
	Summary     string                 `json:"summary"` // first paragraph after title
	Links       []string               `json:"links"`   // outbound [[wiki-links]]
	Frontmatter map[string]interface{} `json:"frontmatter"`
}

// PageContent holds the full content of a page.
type PageContent struct {
	Info    PageInfo `json:"info"`
	Content string   `json:"content"`
}

// SearchResult represents a single search hit.
type SearchResult struct {
	Page      PageInfo `json:"page"`
	Score     float64  `json:"score"`     // relevance 0.0-1.0
	MatchType string   `json:"matchType"` // "title", "content", "link", "section"
	Snippet   string   `json:"snippet"`   // context around match
}

// PageIndex maintains an in-memory index of wiki pages for fast retrieval.
type PageIndex struct {
	WikiRoot string
	pages    []PageInfo
	loaded   bool
}

// wikiLinkRegex matches [[wiki-links]] with optional [[target|display]] syntax.
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// skipDirs are directories to skip during indexing.
var skipDirs = map[string]bool{
	"raw": true,
}

// New creates a new PageIndex rooted at the given wiki directory.
func New(wikiRoot string) *PageIndex {
	return &PageIndex{
		WikiRoot: wikiRoot,
	}
}

// Load scans the wiki directory and builds the index.
func (idx *PageIndex) Load() error {
	idx.pages = nil

	err := filepath.Walk(idx.WikiRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files/dirs
		baseName := filepath.Base(path)
		if strings.HasPrefix(baseName, ".") && path != idx.WikiRoot {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip excluded directories
		if info.IsDir() {
			if skipDirs[baseName] {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process markdown files
		if !strings.HasSuffix(baseName, ".md") {
			return nil
		}

		relPath, err := filepath.Rel(idx.WikiRoot, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		pageInfo := idx.parsePage(relPath, string(content))
		idx.pages = append(idx.pages, pageInfo)

		return nil
	})

	if err != nil {
		return fmt.Errorf("loading wiki index: %w", err)
	}

	idx.loaded = true
	return nil
}

// parsePage extracts PageInfo from a markdown file's content.
func (idx *PageIndex) parsePage(relPath, content string) PageInfo {
	doc, err := markdown.Parse(content)

	info := PageInfo{
		Path:        relPath,
		Frontmatter: make(map[string]interface{}),
		Links:       []string{},
	}

	// Determine section from first path component
	parts := strings.SplitN(relPath, string(filepath.Separator), 2)
	if len(parts) > 1 {
		info.Section = parts[0]
	}

	if err != nil {
		// If parsing fails, still index the file with limited info
		info.Title = strings.TrimSuffix(filepath.Base(relPath), ".md")
		return info
	}

	info.Frontmatter = doc.Frontmatter

	// Extract title from frontmatter or first heading
	if title, ok := doc.Frontmatter["title"].(string); ok && title != "" {
		info.Title = title
	} else {
		info.Title = extractFirstHeading(doc.Body)
		if info.Title == "" {
			info.Title = strings.TrimSuffix(filepath.Base(relPath), ".md")
		}
	}

	// Extract summary: first non-empty paragraph after title heading
	info.Summary = extractSummary(doc.Body)

	// Extract outbound wiki links
	info.Links = extractWikiLinks(doc.Body)

	return info
}

// extractFirstHeading returns the text of the first markdown heading.
func extractFirstHeading(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
	}
	return ""
}

// extractSummary returns the first non-empty paragraph after any leading heading.
func extractSummary(body string) string {
	lines := strings.Split(body, "\n")
	pastHeading := false
	var para []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip leading blank lines
		if !pastHeading && trimmed == "" {
			continue
		}

		// Skip the first heading line
		if !pastHeading && strings.HasPrefix(trimmed, "#") {
			pastHeading = true
			continue
		}

		pastHeading = true

		// Skip blank lines between heading and first paragraph
		if len(para) == 0 && trimmed == "" {
			continue
		}

		// End of paragraph
		if trimmed == "" {
			break
		}

		// Skip subsequent headings
		if strings.HasPrefix(trimmed, "#") {
			break
		}

		para = append(para, trimmed)
	}

	summary := strings.Join(para, " ")
	// Truncate long summaries
	if len(summary) > 200 {
		summary = summary[:197] + "..."
	}
	return summary
}

// extractWikiLinks returns all [[wiki-link]] targets from the body.
func extractWikiLinks(body string) []string {
	matches := wikiLinkRegex.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var links []string
	for _, m := range matches {
		target := m[1]
		if !seen[target] {
			seen[target] = true
			links = append(links, target)
		}
	}
	if links == nil {
		links = []string{}
	}
	return links
}

// Search performs a query against the index.
// Scoring: title match = 1.0, section match = 0.8, summary match = 0.6,
// content match = 0.4, link match = 0.2.
// Results are normalized to 0.0-1.0 and sorted descending.
func (idx *PageIndex) Search(query string) []SearchResult {
	if !idx.loaded || len(idx.pages) == 0 {
		return nil
	}

	queryLower := strings.ToLower(query)
	terms := strings.Fields(queryLower)
	if len(terms) == 0 {
		return nil
	}

	type scored struct {
		page      PageInfo
		rawScore  float64
		matchType string
		snippet   string
	}

	var hits []scored
	var maxScore float64

	for _, page := range idx.pages {
		var score float64
		bestMatchType := ""
		bestSnippet := ""

		titleLower := strings.ToLower(page.Title)
		sectionLower := strings.ToLower(page.Section)
		summaryLower := strings.ToLower(page.Summary)

		for _, term := range terms {
			if strings.Contains(titleLower, term) {
				score += 1.0
				if bestMatchType == "" || bestMatchType != "title" {
					bestMatchType = "title"
					bestSnippet = page.Title
				}
			}

			if strings.Contains(sectionLower, term) {
				score += 0.8
				if bestMatchType == "" {
					bestMatchType = "section"
					bestSnippet = page.Section
				}
			}

			if strings.Contains(summaryLower, term) {
				score += 0.6
				if bestMatchType == "" {
					bestMatchType = "content"
					bestSnippet = page.Summary
				}
			}

			// Check outbound links
			for _, link := range page.Links {
				if strings.Contains(strings.ToLower(link), term) {
					score += 0.2
					if bestMatchType == "" {
						bestMatchType = "link"
						bestSnippet = link
					}
					break // count once per term for links
				}
			}
		}

		if score > 0 {
			hits = append(hits, scored{
				page:      page,
				rawScore:  score,
				matchType: bestMatchType,
				snippet:   bestSnippet,
			})
			if score > maxScore {
				maxScore = score
			}
		}
	}

	// Sort by raw score descending
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].rawScore > hits[j].rawScore
	})

	// Limit to top 20
	if len(hits) > 20 {
		hits = hits[:20]
	}

	// Normalize scores to 0.0-1.0
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		normalizedScore := 0.0
		if maxScore > 0 {
			normalizedScore = h.rawScore / maxScore
		}
		// Round to 2 decimal places
		normalizedScore = math.Round(normalizedScore*100) / 100

		results[i] = SearchResult{
			Page:      h.page,
			Score:     normalizedScore,
			MatchType: h.matchType,
			Snippet:   h.snippet,
		}
	}

	return results
}

// GetPage returns full content of a specific page.
func (idx *PageIndex) GetPage(path string) (*PageContent, error) {
	if !idx.loaded {
		if err := idx.Load(); err != nil {
			return nil, err
		}
	}

	fullPath := filepath.Join(idx.WikiRoot, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading page %s: %w", path, err)
	}

	// Find page info
	var info PageInfo
	found := false
	for _, p := range idx.pages {
		if p.Path == path {
			info = p
			found = true
			break
		}
	}

	if !found {
		// Parse it on the fly
		info = idx.parsePage(path, string(content))
	}

	return &PageContent{
		Info:    info,
		Content: string(content),
	}, nil
}

// ListPages returns all indexed pages.
func (idx *PageIndex) ListPages() []PageInfo {
	if !idx.loaded {
		return nil
	}
	result := make([]PageInfo, len(idx.pages))
	copy(result, idx.pages)
	return result
}

// ListSections returns available sections (directories under .wiki/).
func (idx *PageIndex) ListSections() []string {
	if !idx.loaded {
		return nil
	}

	seen := make(map[string]bool)
	var sections []string

	for _, page := range idx.pages {
		if page.Section != "" && !seen[page.Section] {
			seen[page.Section] = true
			sections = append(sections, page.Section)
		}
	}

	sort.Strings(sections)
	return sections
}
