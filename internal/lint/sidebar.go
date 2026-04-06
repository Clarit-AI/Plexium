package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/markdown"
)

// SidebarValidator verifies all links in _Sidebar.md resolve.
type SidebarValidator struct {
	wikiPath string
	crawler  *LinkCrawler
}

// SidebarValidation contains sidebar validation results.
type SidebarValidation struct {
	Valid        bool
	BrokenLinks  []BrokenSidebarLink
}

// BrokenSidebarLink represents a link in sidebar that doesn't resolve.
type BrokenSidebarLink struct {
	LinkText string
	Target   string
	LineNum  int
}

// NewSidebarValidator creates a new SidebarValidator.
func NewSidebarValidator(wikiPath string) *SidebarValidator {
	return &SidebarValidator{
		wikiPath: wikiPath,
		crawler:  NewLinkCrawler(wikiPath),
	}
}

// Validate checks that all sidebar links resolve.
func (v *SidebarValidator) Validate() (*SidebarValidation, error) {
	sidebarPath := filepath.Join(v.wikiPath, "_Sidebar.md")

	// Sidebar is optional
	if _, err := os.Stat(sidebarPath); os.IsNotExist(err) {
		return &SidebarValidation{Valid: true}, nil
	}

	content, err := os.ReadFile(sidebarPath)
	if err != nil {
		return nil, fmt.Errorf("reading sidebar: %w", err)
	}

	result := &SidebarValidation{Valid: true}

	// Extract links from sidebar
	links := markdown.ExtractWikiLinks(string(content))
	if len(links) == 0 {
		return result, nil
	}

	// Check each link
	lines := strings.Split(string(content), "\n")
	for lineNum, line := range lines {
		lineLinks := markdown.ExtractWikiLinks(line)
		for _, linkText := range lineLinks {
			target, exists := v.crawler.ResolveLink(linkText, "")

			if !exists {
				result.Valid = false
				result.BrokenLinks = append(result.BrokenLinks, BrokenSidebarLink{
					LinkText: linkText,
					Target:   target,
					LineNum:  lineNum + 1,
				})
			}
		}
	}

	return result, nil
}
