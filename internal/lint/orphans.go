package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// OrphanDetector finds pages with no inbound links.
type OrphanDetector struct {
	crawler    *LinkCrawler
	manifestMgr *manifest.Manager
	wikiPath   string
}

// OrphanResult contains pages with no inbound links.
type OrphanResult struct {
	Orphans []OrphanPage
}

// OrphanPage represents a page with no inbound links.
type OrphanPage struct {
	WikiPath string
	Reason   string // "no inbound links", "not in index", etc.
	Severity string // "error" or "warning"
}

// NavigationHubPages is a set of pages that are allowed to have no inbound links
// because they are navigation infrastructure.
var NavigationHubPages = map[string]bool{
	"_index.md":    true,
	"_Sidebar.md":  true,
	"_Footer.md":  true,
	"Home.md":      true,
	"_schema.md":   true,
}

// NewOrphanDetector creates a new OrphanDetector.
func NewOrphanDetector(wikiPath string, manifestMgr *manifest.Manager) *OrphanDetector {
	return &OrphanDetector{
		crawler:    NewLinkCrawler(wikiPath),
		manifestMgr: manifestMgr,
		wikiPath:   wikiPath,
	}
}

// Detect finds all orphan pages.
func (d *OrphanDetector) Detect() (*OrphanResult, error) {
	// Crawl all links
	links, err := d.crawler.Crawl()
	if err != nil {
		return nil, fmt.Errorf("crawling links: %w", err)
	}

	// Build inbound link graph
	inbound := make(map[string][]string) // target wikiPath → source wikiPaths
	for _, link := range links {
		if link.Resolved {
			inbound[link.Target] = append(inbound[link.Target], link.PagePath)
		}
	}

	// Also check sidebar links as "reachable" sources
	sidebarLinks := d.getSidebarLinks()

	// Find all wiki pages
	pages, err := d.getAllWikiPages()
	if err != nil {
		return nil, fmt.Errorf("finding wiki pages: %w", err)
	}

	var orphans []OrphanPage
	for _, page := range pages {
		baseName := filepath.Base(page)

		// Navigation hubs are never orphans
		if NavigationHubPages[baseName] {
			continue
		}

		// Check if page is reachable from sidebar (considered "linked" even without inbound)
		relPath, _ := filepath.Rel(d.wikiPath, page)
		if sidebarLinks[relPath] {
			// Has sidebar link but no inbound links → warning
			if len(inbound[relPath]) == 0 {
				orphans = append(orphans, OrphanPage{
					WikiPath: relPath,
					Reason:   "no inbound links but reachable from sidebar",
					Severity: "warning",
				})
			}
			continue
		}

		// No inbound links at all → error
		if len(inbound[relPath]) == 0 {
			orphans = append(orphans, OrphanPage{
				WikiPath: relPath,
				Reason:   "no inbound links",
				Severity: "error",
			})
		}
	}

	return &OrphanResult{Orphans: orphans}, nil
}

// getSidebarLinks returns a map of all links in the sidebar (they make pages "reachable")
func (d *OrphanDetector) getSidebarLinks() map[string]bool {
	links := make(map[string]bool)

	sidebarPath := filepath.Join(d.wikiPath, "_Sidebar.md")
	content, err := os.ReadFile(sidebarPath)
	if err != nil {
		return links
	}

	// Extract wiki links from sidebar
	matches := wikiLinkRegex.FindAllStringSubmatch(string(content), -1)
	for _, m := range matches {
		target := m[1]
		if !strings.HasSuffix(target, ".md") {
			target += ".md"
		}
		links[target] = true
	}

	return links
}

// getAllWikiPages returns all markdown files in the wiki
func (d *OrphanDetector) getAllWikiPages() ([]string, error) {
	var pages []string

	err := filepath.Walk(d.wikiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			pages = append(pages, path)
		}
		return nil
	})

	return pages, err
}
