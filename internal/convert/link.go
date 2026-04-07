package convert

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Linker generates cross-references between wiki pages.
type Linker struct {
	pages    map[string]*PageData // wikiPath → page
	slugMap  map[string]string    // lowercase slug → wikiPath (for fuzzy matching)
	titleMap map[string]string    // lowercase title → wikiPath
}

// NewLinker creates a new Linker.
func NewLinker() *Linker {
	return &Linker{
		pages:    make(map[string]*PageData),
		slugMap:  make(map[string]string),
		titleMap: make(map[string]string),
	}
}

// AddPages indexes all pages for cross-referencing.
func (l *Linker) AddPages(pages []PageData) {
	for i := range pages {
		p := &pages[i]
		l.pages[p.WikiPath] = p

		// Build lookup maps
		slug := strings.TrimSuffix(strings.ToLower(p.WikiPath), ".md")
		// Use the basename as well
		parts := strings.Split(slug, "/")
		baseName := parts[len(parts)-1]

		l.slugMap[slug] = p.WikiPath
		l.slugMap[baseName] = p.WikiPath
		l.titleMap[strings.ToLower(p.Title)] = p.WikiPath
	}
}

// wikiLinkRegex matches existing [[wiki-links]] and optionally [[path|display]] form.
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// GenerateCrossReferences scans all page content and injects [[wiki-links]]
// where page titles or slugs are mentioned in other pages' content.
func (l *Linker) GenerateCrossReferences(pages []PageData) []PageData {
	result := make([]PageData, len(pages))
	copy(result, pages)

	for i := range result {
		result[i].Content = l.injectLinks(result[i].WikiPath, result[i].Content)
	}

	return result
}

// injectLinks finds mentions of other page titles in content and wraps them in [[wiki-links]].
func (l *Linker) injectLinks(selfPath, content string) string {
	// Collect all titles/slugs we might link to, sorted longest first
	// to avoid partial matches
	type candidate struct {
		term     string
		wikiPath string
	}
	var candidates []candidate

	for _, p := range l.pages {
		if p.WikiPath == selfPath {
			continue
		}
		// Add title as a candidate
		if p.Title != "" && len(p.Title) >= 3 {
			candidates = append(candidates, candidate{term: p.Title, wikiPath: p.WikiPath})
		}
	}

	// Sort longest first to prevent partial matches
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].term) > len(candidates[j].term)
	})

	// Track which terms we've already linked (only link first occurrence)
	linked := make(map[string]bool)

	lines := strings.Split(content, "\n")
	for lineIdx, line := range lines {
		// Skip frontmatter, headings, and existing wiki-links
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Skip lines inside frontmatter
		if lineIdx < frontmatterEnd(content) {
			continue
		}
		// Skip code blocks
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "    ") {
			continue
		}

		for _, c := range candidates {
			if linked[c.wikiPath] {
				continue
			}

			// Case-insensitive search for the term
			idx := strings.Index(strings.ToLower(line), strings.ToLower(c.term))
			if idx < 0 {
				continue
			}

			// Don't link if already inside a [[wiki-link]] or [markdown-link]
			before := line[:idx]
			if strings.Count(before, "[[") > strings.Count(before, "]]") {
				continue
			}
			if strings.Count(before, "[") > strings.Count(before, "]") {
				continue
			}

			// Replace the match with a wiki-link
			original := line[idx : idx+len(c.term)]
			wikiLink := fmt.Sprintf("[[%s|%s]]", c.wikiPath, original)
			lines[lineIdx] = line[:idx] + wikiLink + line[idx+len(c.term):]
			line = lines[lineIdx]
			linked[c.wikiPath] = true
		}
	}

	return strings.Join(lines, "\n")
}

// ComputeLinks computes inbound and outbound links for all pages.
func (l *Linker) ComputeLinks(pages []PageData) (inbound, outbound map[string][]string) {
	inbound = make(map[string][]string)
	outbound = make(map[string][]string)

	for _, p := range pages {
		// Find all [[wiki-links]] in this page's content
		matches := wikiLinkRegex.FindAllStringSubmatch(p.Content, -1)
		for _, m := range matches {
			target := m[1]
			// Normalize: ensure .md suffix
			if !strings.HasSuffix(target, ".md") {
				target += ".md"
			}
			outbound[p.WikiPath] = append(outbound[p.WikiPath], target)
			inbound[target] = append(inbound[target], p.WikiPath)
		}
	}

	// Deduplicate
	for k, v := range inbound {
		inbound[k] = dedup(v)
	}
	for k, v := range outbound {
		outbound[k] = dedup(v)
	}

	return inbound, outbound
}

func dedup(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func frontmatterEnd(content string) int {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i + 1
		}
	}
	return 0
}
