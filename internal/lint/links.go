package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LinkCrawler finds and resolves all [[wiki-links]] in wiki pages.
type LinkCrawler struct {
	wikiPath string
}

// WikiLink represents a found wiki link
type WikiLink struct {
	PagePath  string // Path to the page containing the link
	RawLink   string // The raw [[link]] text
	Target    string // Resolved target (e.g., "modules/auth.md")
	Resolved  bool   // Whether target exists
	LineNum   int    // Line number where link appears
}

// wikiLinkRegex matches [[wiki-links]] with optional [[target|display]] syntax
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// NewLinkCrawler creates a new LinkCrawler for the given wiki root path.
func NewLinkCrawler(wikiPath string) *LinkCrawler {
	return &LinkCrawler{wikiPath: wikiPath}
}

// Crawl finds all [[wiki-links]] in .wiki/ markdown files.
func (c *LinkCrawler) Crawl() ([]WikiLink, error) {
	var links []WikiLink

	err := filepath.Walk(c.wikiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Skip _schema.md — it contains [[wiki-links]] as documentation
		// examples of syntax, not actual cross-references.
		if info.Name() == "_schema.md" {
			return nil
		}

		// Read the file
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		// Find all wiki links with line numbers
		relPath, err := filepath.Rel(c.wikiPath, path)
		if err != nil {
			relPath = path
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			matches := wikiLinkRegex.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				rawLink := m[0]
				target := m[1]

				// Resolve the link
				resolved, exists := c.ResolveLink(target, filepath.Dir(relPath))

				links = append(links, WikiLink{
					PagePath: relPath,
					RawLink:  rawLink,
					Target:   resolved,
					Resolved: exists,
					LineNum:  lineNum + 1,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("crawling wiki: %w", err)
	}

	return links, nil
}

// ResolveLink resolves a wiki link target to an absolute wiki path.
// Handles: [[modules/auth]], [[auth]], [[modules/auth#heading]], [[../decisions/001]]
func (c *LinkCrawler) ResolveLink(linkText string, sourceDir string) (targetPath string, exists bool) {
	// Remove anchor if present
	anchor := ""
	if idx := strings.Index(linkText, "#"); idx >= 0 {
		anchor = linkText[idx:]
		linkText = linkText[:idx]
	}

	// Normalize: add .md if no extension
	target := linkText
	if !strings.HasSuffix(target, ".md") {
		target += ".md"
	}

	var resolved string

	if strings.HasPrefix(target, "../") {
		// Relative path from wiki root - resolve directly
		resolved = filepath.Join(c.wikiPath, target)
	} else if strings.Contains(target, "/") {
		// Path contains directory separator - resolve from wiki root
		// e.g., [[modules/auth]] → wikiPath/modules/auth.md
		resolved = filepath.Join(c.wikiPath, target)
	} else {
		// Simple filename - resolve relative to source page's directory
		// e.g., [[auth]] in modules/ → modules/auth.md
		resolved = filepath.Join(c.wikiPath, sourceDir, target)
	}

	// Normalize path separators
	resolved = filepath.Clean(resolved)

	// Check if the file exists
	if _, err := os.Stat(resolved); err == nil {
		// Return path relative to wiki root
		relPath, _ := filepath.Rel(c.wikiPath, resolved)
		return relPath + anchor, true
	}

	return target + anchor, false
}

// GetBrokenLinks returns only the links that failed to resolve.
func (c *LinkCrawler) GetBrokenLinks() ([]WikiLink, error) {
	links, err := c.Crawl()
	if err != nil {
		return nil, err
	}

	var broken []WikiLink
	for _, link := range links {
		if !link.Resolved {
			broken = append(broken, link)
		}
	}
	return broken, nil
}