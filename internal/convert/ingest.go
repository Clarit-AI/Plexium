package convert

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/generate"
	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// Ingestor translates scour findings into wiki page data.
type Ingestor struct {
	taxonomy *generate.Taxonomy
}

// IngestResult holds the pages produced by ingestion.
type IngestResult struct {
	Pages []PageData
}

// PageData represents a single wiki page to be created.
type PageData struct {
	WikiPath    string
	Title       string
	Section     string
	Content     string
	SourceFiles []string
	Confidence  string // "high", "medium", "low"
	IsStub      bool
}

// NewIngestor creates a new Ingestor.
func NewIngestor() *Ingestor {
	return &Ingestor{
		taxonomy: generate.NewTaxonomy(),
	}
}

// Ingest processes findings and filter results into wiki pages.
func (ing *Ingestor) Ingest(findings *ScourFindings, filter *FilterResult) (*IngestResult, error) {
	result := &IngestResult{}
	seen := make(map[string]bool) // track wikiPaths to avoid duplicates

	// 1. READMEs → Home + architecture pages
	for _, readme := range findings.Readmes {
		page := ing.ingestReadme(readme)
		if !seen[page.WikiPath] {
			seen[page.WikiPath] = true
			result.Pages = append(result.Pages, page)
		}
	}

	// 2. ADRs → Decision pages
	for _, adr := range findings.ADRs {
		page := ing.ingestADR(adr)
		if !seen[page.WikiPath] {
			seen[page.WikiPath] = true
			result.Pages = append(result.Pages, page)
		}
	}

	// 3. Source directories → Module pages
	modules := ing.extractModules(filter.Eligible)
	for _, page := range modules {
		if !seen[page.WikiPath] {
			seen[page.WikiPath] = true
			result.Pages = append(result.Pages, page)
		}
	}

	// 4. Existing docs → appropriate section pages
	for _, doc := range findings.ExistingDocs {
		if doc.Type == "claude" || doc.Type == "agents" || doc.Type == "cursorrules" {
			continue // skip agent instruction files
		}
		page := ing.ingestDoc(doc)
		if !seen[page.WikiPath] {
			seen[page.WikiPath] = true
			result.Pages = append(result.Pages, page)
		}
	}

	// 5. Config files → project overview enrichment (add to Home if exists)
	// Config data enriches existing pages rather than creating new ones

	return result, nil
}

func (ing *Ingestor) ingestReadme(readme ReadmeDoc) PageData {
	if readme.Hierarchy == 0 {
		return PageData{
			WikiPath:    "Home.md",
			Title:       readme.Title,
			Section:     "Root",
			Content:     ing.buildHomePage(readme),
			SourceFiles: []string{readme.Path},
			Confidence:  "high",
		}
	}

	// Nested READMEs become architecture or guide pages
	dir := filepath.Dir(readme.Path)
	slug := generate.ToSlug(filepath.Base(dir))
	section := "Guides"

	// If under src/, it's a module
	if strings.HasPrefix(readme.Path, "src/") || strings.HasPrefix(readme.Path, "internal/") ||
		strings.HasPrefix(readme.Path, "pkg/") || strings.HasPrefix(readme.Path, "lib/") {
		section = "Modules"
		return PageData{
			WikiPath:    fmt.Sprintf("modules/%s.md", slug),
			Title:       readme.Title,
			Section:     section,
			Content:     ing.buildModulePage(readme),
			SourceFiles: []string{readme.Path},
			Confidence:  "high",
		}
	}

	if strings.HasPrefix(readme.Path, "docs/") {
		dir := filepath.Dir(readme.Path)
		parts := strings.Split(filepath.ToSlash(dir), "/")
		if len(parts) >= 2 {
			sectionName := parts[1]
			switch strings.ToLower(sectionName) {
			case "architecture":
				section = "Architecture"
			case "patterns":
				section = "Patterns"
			case "concepts":
				section = "Concepts"
			}
		}
	}

	return PageData{
		WikiPath:    fmt.Sprintf("%s/%s.md", strings.ToLower(section), slug),
		Title:       readme.Title,
		Section:     section,
		Content:     ing.buildGuidePage(readme.Title, readme.Content),
		SourceFiles: []string{readme.Path},
		Confidence:  "medium",
	}
}

func (ing *Ingestor) ingestADR(adr ADRDoc) PageData {
	slug := strings.TrimSuffix(filepath.Base(adr.Path), ".md")

	return PageData{
		WikiPath:    fmt.Sprintf("decisions/%s.md", slug),
		Title:       adr.Title,
		Section:     "Decisions",
		Content:     ing.buildDecisionPage(adr),
		SourceFiles: []string{adr.Path},
		Confidence:  "high",
	}
}

func (ing *Ingestor) ingestDoc(doc ExistingDoc) PageData {
	// Classify using taxonomy
	f := scanner.File{Path: doc.Path, Content: doc.Content}
	classification, err := ing.taxonomy.Classify(f)
	if err != nil {
		// Fallback: use path-based classification
		slug := generate.PathToSlug(doc.Path)
		return PageData{
			WikiPath:    fmt.Sprintf("guides/%s.md", slug),
			Title:       generate.ToSlug(slug),
			Section:     "Guides",
			Content:     ing.buildGuidePage(slug, doc.Content),
			SourceFiles: []string{doc.Path},
			Confidence:  "low",
		}
	}

	wikiPath := generate.SectionSlug(classification.Section, classification.Slug)
	return PageData{
		WikiPath:    wikiPath,
		Title:       classification.Title,
		Section:     classification.Section,
		Content:     ing.buildGuidePage(classification.Title, doc.Content),
		SourceFiles: []string{doc.Path},
		Confidence:  "medium",
	}
}

// extractModules finds unique source directories and creates module pages.
func (ing *Ingestor) extractModules(eligible []scanner.File) []PageData {
	// Group files by top-level source directory
	moduleDirs := make(map[string][]string) // dir → list of file paths
	srcPrefixes := []string{"src/", "internal/", "pkg/", "lib/", "cmd/"}

	for _, f := range eligible {
		for _, prefix := range srcPrefixes {
			if strings.HasPrefix(f.Path, prefix) {
				parts := strings.SplitN(f.Path, "/", 3)
				if len(parts) >= 2 {
					moduleDir := parts[0] + "/" + parts[1]
					moduleDirs[moduleDir] = append(moduleDirs[moduleDir], f.Path)
				}
				break
			}
		}
	}

	var pages []PageData
	for dir, files := range moduleDirs {
		parts := strings.SplitN(dir, "/", 2)
		moduleName := parts[1]
		slug := generate.ToSlug(moduleName)

		pages = append(pages, PageData{
			WikiPath:    fmt.Sprintf("modules/%s.md", slug),
			Title:       formatModuleTitle(moduleName),
			Section:     "Modules",
			Content:     ing.buildModuleStubPage(moduleName, files),
			SourceFiles: files,
			Confidence:  "medium",
			IsStub:      true,
		})
	}

	return pages
}

// --- Page content builders ---

func (ing *Ingestor) buildHomePage(readme ReadmeDoc) string {
	now := time.Now().Format("2006-01-02")
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", readme.Title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", now))
	b.WriteString("updated-by: plexium-convert\n")
	b.WriteString("---\n\n")
	b.WriteString(readme.Content)

	return b.String()
}

func (ing *Ingestor) buildModulePage(readme ReadmeDoc) string {
	now := time.Now().Format("2006-01-02")
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", readme.Title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", now))
	b.WriteString("updated-by: plexium-convert\n")
	b.WriteString(fmt.Sprintf("source-files: [\"%s\"]\n", readme.Path))
	b.WriteString("confidence: high\n")
	b.WriteString("review-status: unreviewed\n")
	b.WriteString("---\n\n")
	b.WriteString(readme.Content)

	return b.String()
}

func (ing *Ingestor) buildModuleStubPage(name string, files []string) string {
	now := time.Now().Format("2006-01-02")
	title := formatModuleTitle(name)
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", now))
	b.WriteString("updated-by: plexium-convert\n")
	b.WriteString("confidence: medium\n")
	b.WriteString("review-status: unreviewed\n")
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# %s\n\n", title))
	b.WriteString("<!-- STATUS: stub -->\n\n")
	b.WriteString(fmt.Sprintf("Module containing %d source files.\n\n", len(files)))
	b.WriteString("## Source Files\n\n")

	for _, f := range files {
		b.WriteString(fmt.Sprintf("- `%s`\n", f))
	}

	return b.String()
}

func (ing *Ingestor) buildDecisionPage(adr ADRDoc) string {
	now := time.Now().Format("2006-01-02")
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", adr.Title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("date: %s\n", now))
	b.WriteString(fmt.Sprintf("status: %s\n", adr.Status))
	b.WriteString("review-status: unreviewed\n")
	b.WriteString("---\n\n")
	b.WriteString(adr.Content)

	return b.String()
}

func (ing *Ingestor) buildGuidePage(title, content string) string {
	now := time.Now().Format("2006-01-02")
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", now))
	b.WriteString("updated-by: plexium-convert\n")
	b.WriteString("confidence: medium\n")
	b.WriteString("review-status: unreviewed\n")
	b.WriteString("---\n\n")
	b.WriteString(content)

	return b.String()
}

func formatModuleTitle(name string) string {
	s := strings.ReplaceAll(name, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
