package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// Compiler regenerates shared navigation files (_index.md, _Sidebar.md)
// from the current manifest state. All operations are deterministic — no LLM calls.
// Compile is safe for concurrent use: a sync.Mutex serialises write operations.
type Compiler struct {
	repoRoot     string
	wikiPath     string // absolute path to .wiki/
	manifestPath string // absolute path to .plexium/manifest.json
	dryRun       bool
}

// compileMu serialises writes to .wiki/ to prevent concurrent compile races.
var compileMu sync.Mutex

// CompileResult captures what the compile pass produced.
type CompileResult struct {
	FilesGenerated []string `json:"filesGenerated"`
	FilesSkipped   []string `json:"filesSkipped"`
	DryRun         bool     `json:"dryRun"`
}

// NewCompiler creates a Compiler rooted at repoRoot.
func NewCompiler(repoRoot string, dryRun bool) *Compiler {
	return &Compiler{
		repoRoot:     repoRoot,
		wikiPath:     filepath.Join(repoRoot, ".wiki"),
		manifestPath: manifest.DefaultPath(repoRoot),
		dryRun:       dryRun,
	}
}

// Compile reads the manifest and regenerates _index.md and _Sidebar.md.
func (c *Compiler) Compile() (*CompileResult, error) {
	mgr, err := manifest.NewManager(c.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("creating manifest manager: %w", err)
	}

	m, err := mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	groups := groupBySection(m.Pages)
	sections := sortedKeys(groups)

	indexContent := generateIndex(sections, groups)
	sidebarContent := generateSidebar(sections, groups)

	result := &CompileResult{DryRun: c.dryRun}

	indexPath := filepath.Join(c.wikiPath, "_index.md")
	sidebarPath := filepath.Join(c.wikiPath, "_Sidebar.md")

	if c.dryRun {
		result.FilesSkipped = []string{indexPath, sidebarPath}
		return result, nil
	}

	if err := os.MkdirAll(c.wikiPath, 0755); err != nil {
		return nil, fmt.Errorf("creating wiki directory: %w", err)
	}

	compileMu.Lock()
	defer compileMu.Unlock()

	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return nil, fmt.Errorf("writing _index.md: %w", err)
	}

	if err := os.WriteFile(sidebarPath, []byte(sidebarContent), 0644); err != nil {
		return nil, fmt.Errorf("writing _Sidebar.md: %w", err)
	}

	result.FilesGenerated = []string{indexPath, sidebarPath}
	return result, nil
}

// groupBySection buckets pages by their Section field.
func groupBySection(pages []manifest.PageEntry) map[string][]*manifest.PageEntry {
	groups := make(map[string][]*manifest.PageEntry)
	for i := range pages {
		sec := pages[i].Section
		if sec == "" {
			sec = "Uncategorized"
		}
		groups[sec] = append(groups[sec], &pages[i])
	}
	// Sort pages within each section by Title.
	for _, entries := range groups {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Title < entries[j].Title
		})
	}
	return groups
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys(m map[string][]*manifest.PageEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// generateIndex builds the _index.md content.
func generateIndex(sections []string, groups map[string][]*manifest.PageEntry) string {
	var b strings.Builder
	b.WriteString("# Wiki Index\n")

	for _, sec := range sections {
		b.WriteString("\n## ")
		b.WriteString(sec)
		b.WriteString("\n")
		for _, p := range groups[sec] {
			slug := slugFromPath(p.WikiPath)
			fmt.Fprintf(&b, "- [[%s]] — %s\n", slug, p.Title)
		}
	}
	return b.String()
}

// generateSidebar builds the _Sidebar.md content.
func generateSidebar(sections []string, groups map[string][]*manifest.PageEntry) string {
	var b strings.Builder
	b.WriteString("**[[Home]]**\n")

	for _, sec := range sections {
		b.WriteString("\n**")
		b.WriteString(sec)
		b.WriteString("**\n")
		for _, p := range groups[sec] {
			slug := slugFromPath(p.WikiPath)
			fmt.Fprintf(&b, "- [[%s]]\n", slug)
		}
	}
	return b.String()
}

// slugFromPath extracts the wiki-link slug from a WikiPath.
// "modules/auth-module.md" → "auth-module"
func slugFromPath(wikiPath string) string {
	base := filepath.Base(wikiPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
