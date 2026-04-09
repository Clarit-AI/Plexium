package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/markdown"
	"github.com/Clarit-AI/Plexium/internal/prompts"
)

// LLMClient is the interface for making LLM API calls.
// Implementations can use any provider (OpenAI, Anthropic, Ollama, etc).
type LLMClient interface {
	// Complete sends a prompt and returns the completion text.
	Complete(prompt string) (string, error)
}

// LLMAnalyzer performs semantic analysis of wiki pages using an LLM.
type LLMAnalyzer struct {
	Client    LLMClient
	WikiRoot  string
	RateLimit int // max pages per run (0 = unlimited)
	Profile   string
}

// LLMAnalysisResult contains all semantic analysis findings.
type LLMAnalysisResult struct {
	Contradictions    []ContradictionReport   `json:"contradictions"`
	SuggestedPages    []string                `json:"suggestedPages"`
	MissingCrossRefs  []MissingCrossRefReport `json:"missingCrossRefs"`
	SemanticStaleness []SemanticStalePage     `json:"semanticStaleness"`
	PagesAnalyzed     int                     `json:"pagesAnalyzed"`
	TokensUsed        int                     `json:"tokensUsed"`
}

// SemanticStalePage represents a page that is semantically outdated.
type SemanticStalePage struct {
	WikiPath    string `json:"wikiPath"`
	Description string `json:"description"`
	Confidence  string `json:"confidence"` // "high", "medium", "low"
}

// pageContent holds loaded page data for analysis.
type pageContent struct {
	path    string
	title   string
	content string
	links   []string
}

// DefaultRateLimit is the default maximum pages per LLM analysis run.
const DefaultRateLimit = 50

// NewLLMAnalyzer creates a new LLMAnalyzer.
func NewLLMAnalyzer(client LLMClient, wikiRoot string) *LLMAnalyzer {
	return &LLMAnalyzer{
		Client:    client,
		WikiRoot:  wikiRoot,
		RateLimit: DefaultRateLimit,
		Profile:   prompts.DefaultProfile,
	}
}

// Analyze runs all semantic checks on the given pages.
// If pages is nil, analyzes all pages in the wiki.
func (a *LLMAnalyzer) Analyze(pages []string) (*LLMAnalysisResult, error) {
	loaded, err := a.loadPages(pages)
	if err != nil {
		return nil, fmt.Errorf("loading pages: %w", err)
	}

	// Apply rate limit
	if a.RateLimit > 0 && len(loaded) > a.RateLimit {
		// Sort by modification time (most recent first) then truncate
		loaded = a.sortByRecency(loaded)
		loaded = loaded[:a.RateLimit]
	}

	result := &LLMAnalysisResult{
		PagesAnalyzed: len(loaded),
	}

	// Run all analysis passes
	contradictions, err := a.detectContradictions(loaded)
	if err != nil {
		return nil, fmt.Errorf("detecting contradictions: %w", err)
	}
	result.Contradictions = contradictions

	suggested, err := a.suggestMissingPages(loaded)
	if err != nil {
		return nil, fmt.Errorf("suggesting missing pages: %w", err)
	}
	result.SuggestedPages = suggested

	crossRefs, err := a.suggestCrossRefs(loaded)
	if err != nil {
		return nil, fmt.Errorf("suggesting cross-refs: %w", err)
	}
	result.MissingCrossRefs = crossRefs

	staleness, err := a.detectSemanticStaleness(loaded)
	if err != nil {
		return nil, fmt.Errorf("detecting staleness: %w", err)
	}
	result.SemanticStaleness = staleness

	return result, nil
}

// loadPages loads page content from the wiki. If paths is nil, loads all pages.
func (a *LLMAnalyzer) loadPages(paths []string) ([]pageContent, error) {
	if paths != nil {
		return a.loadSpecificPages(paths)
	}
	return a.loadAllPages()
}

// loadAllPages walks the wiki root and loads all eligible markdown files.
func (a *LLMAnalyzer) loadAllPages() ([]pageContent, error) {
	var pages []pageContent

	err := filepath.Walk(a.WikiRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			baseName := filepath.Base(path)
			// Skip raw/ directory
			if baseName == "raw" && path != a.WikiRoot {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-markdown files
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Skip _-prefixed files (nav files, schema, etc.)
		baseName := filepath.Base(path)
		if strings.HasPrefix(baseName, "_") {
			return nil
		}

		page, err := a.loadPage(path)
		if err != nil {
			// Skip pages that fail to load rather than aborting
			return nil
		}

		pages = append(pages, page)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking wiki root: %w", err)
	}

	return pages, nil
}

// loadSpecificPages loads specific pages by path.
func (a *LLMAnalyzer) loadSpecificPages(paths []string) ([]pageContent, error) {
	var pages []pageContent
	for _, p := range paths {
		fullPath := p
		if !filepath.IsAbs(p) {
			fullPath = filepath.Join(a.WikiRoot, p)
		}

		page, err := a.loadPage(fullPath)
		if err != nil {
			continue // Skip pages that fail to load
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// loadPage reads and parses a single wiki page.
func (a *LLMAnalyzer) loadPage(path string) (pageContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pageContent{}, fmt.Errorf("reading %s: %w", path, err)
	}

	content := string(data)
	doc, err := markdown.Parse(content)
	if err != nil {
		return pageContent{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	relPath, err := filepath.Rel(a.WikiRoot, path)
	if err != nil {
		relPath = path
	}

	title := relPath
	if t, ok := doc.Frontmatter["title"].(string); ok && t != "" {
		title = t
	}

	links := markdown.ExtractWikiLinks(doc.Body)

	return pageContent{
		path:    relPath,
		title:   title,
		content: doc.Body,
		links:   links,
	}, nil
}

// sortByRecency sorts pages by file modification time (most recent first).
func (a *LLMAnalyzer) sortByRecency(pages []pageContent) []pageContent {
	type pageWithTime struct {
		page    pageContent
		modTime int64
	}

	withTimes := make([]pageWithTime, len(pages))
	for i, p := range pages {
		fullPath := filepath.Join(a.WikiRoot, p.path)
		info, err := os.Stat(fullPath)
		if err != nil {
			withTimes[i] = pageWithTime{page: p, modTime: 0}
		} else {
			withTimes[i] = pageWithTime{page: p, modTime: info.ModTime().UnixNano()}
		}
	}

	sort.Slice(withTimes, func(i, j int) bool {
		return withTimes[i].modTime > withTimes[j].modTime
	})

	result := make([]pageContent, len(withTimes))
	for i, pt := range withTimes {
		result[i] = pt.page
	}
	return result
}

// estimateTokens provides a rough token count estimate (1 token ~= 4 chars).
func estimateTokens(text string) int {
	return len(text) / 4
}

// detectContradictions compares pairs of related pages for conflicting statements.
func (a *LLMAnalyzer) detectContradictions(pages []pageContent) ([]ContradictionReport, error) {
	var results []ContradictionReport

	// Build a set of page paths for quick lookup
	pageByPath := make(map[string]pageContent)
	for _, p := range pages {
		pageByPath[p.path] = p
	}

	// Compare pages that are linked to each other (related pages)
	seen := make(map[string]bool)
	for _, page := range pages {
		for _, link := range page.links {
			// Normalize link to path
			target := link
			if !strings.HasSuffix(target, ".md") {
				target += ".md"
			}

			other, ok := pageByPath[target]
			if !ok {
				continue
			}

			// Avoid duplicate pairs
			pairKey := page.path + "|" + other.path
			reversePairKey := other.path + "|" + page.path
			if seen[pairKey] || seen[reversePairKey] {
				continue
			}
			seen[pairKey] = true

			prompt, err := prompts.Render(filepath.Dir(a.WikiRoot), prompts.PromptContradiction, a.Profile, map[string]string{
				"Page1Title":   page.title,
				"Page1Content": page.content,
				"Page2Title":   other.title,
				"Page2Content": other.content,
			})
			if err != nil {
				return nil, fmt.Errorf("render contradiction prompt: %w", err)
			}
			response, err := a.Client.Complete(prompt)
			if err != nil {
				return nil, fmt.Errorf("LLM call for contradiction check: %w", err)
			}

			parsed := parseContradictions(response, page.path, other.path)
			results = append(results, parsed...)
		}
	}

	return results, nil
}

// parseContradictions parses LLM response for contradiction detection.
func parseContradictions(response string, page1, page2 string) []ContradictionReport {
	if strings.TrimSpace(response) == "NONE" {
		return nil
	}

	var results []ContradictionReport
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CONTRADICTION: ") {
			desc := strings.TrimPrefix(line, "CONTRADICTION: ")
			results = append(results, ContradictionReport{
				Pages:       []string{page1, page2},
				Description: desc,
			})
		}
	}
	return results
}

// suggestMissingPages finds concepts mentioned in 3+ pages without their own page.
func (a *LLMAnalyzer) suggestMissingPages(pages []pageContent) ([]string, error) {
	// Build a summary of all pages for the prompt
	var sb strings.Builder
	for _, p := range pages {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n%s\n\n", p.title, p.path, p.content))
	}

	prompt, err := prompts.Render(filepath.Dir(a.WikiRoot), prompts.PromptMissingConcepts, a.Profile, map[string]string{
		"Pages": sb.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("render missing concept prompt: %w", err)
	}
	response, err := a.Client.Complete(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call for concept extraction: %w", err)
	}

	return parseConcepts(response), nil
}

// parseConcepts parses LLM response for concept extraction.
func parseConcepts(response string) []string {
	if strings.TrimSpace(response) == "NONE" {
		return nil
	}

	var concepts []string
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CONCEPT: ") {
			concept := strings.TrimPrefix(line, "CONCEPT: ")
			if concept != "" {
				concepts = append(concepts, concept)
			}
		}
	}
	return concepts
}

// suggestCrossRefs finds related pages that should link but don't.
func (a *LLMAnalyzer) suggestCrossRefs(pages []pageContent) ([]MissingCrossRefReport, error) {
	// Build page summaries with their existing links
	var sb strings.Builder
	for _, p := range pages {
		sb.WriteString(fmt.Sprintf("## %s (%s)\nLinks: %s\n%s\n\n",
			p.title, p.path, strings.Join(p.links, ", "), p.content))
	}

	prompt, err := prompts.Render(filepath.Dir(a.WikiRoot), prompts.PromptCrossReference, a.Profile, map[string]string{
		"Pages": sb.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("render cross-reference prompt: %w", err)
	}
	response, err := a.Client.Complete(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call for cross-ref suggestion: %w", err)
	}

	return parseCrossRefs(response), nil
}

// parseCrossRefs parses LLM response for cross-reference suggestions.
func parseCrossRefs(response string) []MissingCrossRefReport {
	if strings.TrimSpace(response) == "NONE" {
		return nil
	}

	var results []MissingCrossRefReport
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "CROSSREF: ") {
			continue
		}

		rest := strings.TrimPrefix(line, "CROSSREF: ")

		// Parse "[from] -> [to]: [reason]"
		// First split on " -> "
		arrowParts := strings.SplitN(rest, " -> ", 2)
		if len(arrowParts) != 2 {
			continue
		}

		from := strings.Trim(arrowParts[0], "[] ")

		// Split the second part on ": " for target and reason
		colonParts := strings.SplitN(arrowParts[1], ": ", 2)
		if len(colonParts) < 1 {
			continue
		}

		to := strings.Trim(colonParts[0], "[] ")
		reason := ""
		if len(colonParts) == 2 {
			reason = colonParts[1]
		}

		results = append(results, MissingCrossRefReport{
			From:         from,
			ShouldLinkTo: to,
			Reason:       reason,
		})
	}
	return results
}

// detectSemanticStaleness finds pages whose content appears outdated in meaning.
func (a *LLMAnalyzer) detectSemanticStaleness(pages []pageContent) ([]SemanticStalePage, error) {
	var results []SemanticStalePage

	for _, page := range pages {
		prompt, err := prompts.Render(filepath.Dir(a.WikiRoot), prompts.PromptStaleness, a.Profile, map[string]string{
			"PageTitle":   page.title,
			"PageContent": page.content,
		})
		if err != nil {
			return nil, fmt.Errorf("render staleness prompt: %w", err)
		}
		response, err := a.Client.Complete(prompt)
		if err != nil {
			return nil, fmt.Errorf("LLM call for staleness check on %s: %w", page.path, err)
		}

		stale := parseStaleness(response, page.path)
		if stale != nil {
			results = append(results, *stale)
		}
	}

	return results, nil
}

// parseStaleness parses LLM response for semantic staleness detection.
func parseStaleness(response string, pagePath string) *SemanticStalePage {
	trimmed := strings.TrimSpace(response)
	if trimmed == "CURRENT" {
		return nil
	}

	// Parse "STALE: [description] | CONFIDENCE: [level]"
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "STALE: ") {
			continue
		}

		rest := strings.TrimPrefix(line, "STALE: ")

		// Split on " | CONFIDENCE: "
		parts := strings.SplitN(rest, " | CONFIDENCE: ", 2)
		if len(parts) != 2 {
			// Try without CONFIDENCE if format is slightly off
			return &SemanticStalePage{
				WikiPath:    pagePath,
				Description: rest,
				Confidence:  "medium", // default confidence
			}
		}

		confidence := strings.ToLower(strings.TrimSpace(parts[1]))
		// Validate confidence level
		switch confidence {
		case "high", "medium", "low":
			// valid
		default:
			confidence = "medium"
		}

		return &SemanticStalePage{
			WikiPath:    pagePath,
			Description: parts[0],
			Confidence:  confidence,
		}
	}

	return nil
}
