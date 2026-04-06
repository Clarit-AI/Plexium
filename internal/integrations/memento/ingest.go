package memento

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MementoIngestor scans .wiki/raw/memento-transcripts/ for new session transcripts
// and extracts decisions/rationale into wiki pages.
type MementoIngestor struct {
	RepoRoot string
	WikiRoot string
	RawPath  string // .wiki/raw/memento-transcripts/
}

// IngestResult tracks what was ingested during a run.
type IngestResult struct {
	TranscriptsFound   int
	TranscriptsNew     int
	DecisionsExtracted []ExtractedDecision
	PagesCreated       []string
	PagesUpdated       []string
	Contradictions     []Contradiction
}

// ExtractedDecision represents a single decision extracted from a transcript.
type ExtractedDecision struct {
	Title     string
	Rationale string
	Source    string // transcript filename
	LineNum   int
}

// Contradiction represents a conflict between newly extracted content and an existing wiki page.
type Contradiction struct {
	NewContent   string
	ExistingPage string
	Description  string
}

// NewIngestor creates a MementoIngestor with default paths derived from repoRoot and wikiRoot.
func NewIngestor(repoRoot, wikiRoot string) *MementoIngestor {
	return &MementoIngestor{
		RepoRoot: repoRoot,
		WikiRoot: wikiRoot,
		RawPath:  filepath.Join(wikiRoot, "raw", "memento-transcripts"),
	}
}

// IngestNewTranscripts scans for .md files in raw/memento-transcripts/,
// checks which ones haven't been processed (by checking a .processed marker),
// extracts decisions using pattern matching, creates/updates decision pages
// in .wiki/decisions/, and returns the result.
func (i *MementoIngestor) IngestNewTranscripts() (*IngestResult, error) {
	result := &IngestResult{}

	entries, err := os.ReadDir(i.RawPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("reading transcript directory %s: %w", i.RawPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// Skip .processed marker files
		if strings.HasSuffix(entry.Name(), ".processed") {
			continue
		}

		result.TranscriptsFound++

		transcriptPath := filepath.Join(i.RawPath, entry.Name())
		if i.isProcessed(transcriptPath) {
			continue
		}

		result.TranscriptsNew++

		content, err := os.ReadFile(transcriptPath)
		if err != nil {
			return nil, fmt.Errorf("reading transcript %s: %w", transcriptPath, err)
		}

		decisions := i.extractDecisions(string(content), entry.Name())
		result.DecisionsExtracted = append(result.DecisionsExtracted, decisions...)

		// Create decision pages for each extracted decision
		for _, d := range decisions {
			pagePath, created, err := i.writeDecisionPage(d)
			if err != nil {
				return nil, fmt.Errorf("writing decision page for %q: %w", d.Title, err)
			}
			if created {
				result.PagesCreated = append(result.PagesCreated, pagePath)
			} else {
				result.PagesUpdated = append(result.PagesUpdated, pagePath)
			}
		}

		if err := i.markProcessed(transcriptPath); err != nil {
			return nil, fmt.Errorf("marking transcript processed %s: %w", transcriptPath, err)
		}
	}

	return result, nil
}

// decisionPatterns lists the phrases used to detect decisions in transcripts.
// Each pattern is checked via case-insensitive contains matching.
var decisionPatterns = []string{
	"we decided to",
	"the tradeoff is",
	"because of",
	"we chose",
	"decision:",
	"the decision was",
	"agreed to",
	"settled on",
}

// extractDecisions parses transcript content for decision patterns.
func (i *MementoIngestor) extractDecisions(content, filename string) []ExtractedDecision {
	var decisions []ExtractedDecision
	lines := strings.Split(content, "\n")

	for lineIdx, line := range lines {
		lower := strings.ToLower(line)
		for _, pattern := range decisionPatterns {
			if strings.Contains(lower, pattern) {
				decision := ExtractedDecision{
					Title:     extractDecisionTitle(line, pattern),
					Rationale: extractRationale(lines, lineIdx),
					Source:    filename,
					LineNum:   lineIdx + 1, // 1-based
				}
				// Skip duplicates within same transcript (same title)
				if !hasDuplicateTitle(decisions, decision.Title) {
					decisions = append(decisions, decision)
				}
				break // only match one pattern per line
			}
		}
	}

	return decisions
}

// isProcessed checks if a transcript has already been ingested
// by looking for a .processed marker file alongside the transcript.
func (i *MementoIngestor) isProcessed(transcriptPath string) bool {
	markerPath := transcriptPath + ".processed"
	_, err := os.Stat(markerPath)
	return err == nil
}

// markProcessed marks a transcript as ingested by creating a .processed marker file.
func (i *MementoIngestor) markProcessed(transcriptPath string) error {
	markerPath := transcriptPath + ".processed"
	return os.WriteFile(markerPath, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// writeDecisionPage creates or updates a decision page in .wiki/decisions/.
// Returns the page path and whether it was created (true) or updated (false).
func (i *MementoIngestor) writeDecisionPage(d ExtractedDecision) (string, bool, error) {
	decisionsDir := filepath.Join(i.WikiRoot, "decisions")
	if err := os.MkdirAll(decisionsDir, 0755); err != nil {
		return "", false, fmt.Errorf("creating decisions directory: %w", err)
	}

	slug := slugify(d.Title)
	pagePath := filepath.Join(decisionsDir, slug+".md")

	_, err := os.Stat(pagePath)
	created := os.IsNotExist(err)

	pageContent := formatDecisionPage(d)
	if err := os.WriteFile(pagePath, []byte(pageContent), 0644); err != nil {
		return "", false, fmt.Errorf("writing decision page: %w", err)
	}

	return pagePath, created, nil
}

// extractDecisionTitle derives a short title from a line that matched a decision pattern.
func extractDecisionTitle(line, pattern string) string {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, pattern)
	if idx < 0 {
		return strings.TrimSpace(line)
	}

	// Take the text after the pattern as the title basis
	after := strings.TrimSpace(line[idx+len(pattern):])
	// Clean up leading punctuation/whitespace
	after = strings.TrimLeft(after, " ,:;-")
	after = strings.TrimSpace(after)

	if after == "" {
		return strings.TrimSpace(line)
	}

	// Cap at a reasonable length for a title
	if len(after) > 80 {
		after = after[:80]
		// Try to break at a word boundary
		if lastSpace := strings.LastIndex(after, " "); lastSpace > 40 {
			after = after[:lastSpace]
		}
	}

	return after
}

// extractRationale gathers surrounding context lines as rationale.
func extractRationale(lines []string, lineIdx int) string {
	start := lineIdx - 1
	if start < 0 {
		start = 0
	}
	end := lineIdx + 3
	if end > len(lines) {
		end = len(lines)
	}

	var contextLines []string
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			contextLines = append(contextLines, trimmed)
		}
	}
	return strings.Join(contextLines, "\n")
}

// hasDuplicateTitle checks if a title already exists in the decisions slice.
func hasDuplicateTitle(decisions []ExtractedDecision, title string) bool {
	for _, d := range decisions {
		if d.Title == title {
			return true
		}
	}
	return false
}

// slugify converts a title to a filename-safe slug.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '_' || r == '-' {
			return '-'
		}
		return -1
	}, s)

	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")

	if s == "" {
		s = "untitled-decision"
	}
	return s
}

// formatDecisionPage renders a decision as a wiki-compatible markdown page.
func formatDecisionPage(d ExtractedDecision) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", d.Title))
	b.WriteString("ownership: managed\n")
	b.WriteString(fmt.Sprintf("last-updated: %s\n", time.Now().Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("source: %s\n", d.Source))
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# %s\n\n", d.Title))
	b.WriteString("## Context\n\n")
	b.WriteString(fmt.Sprintf("Extracted from session transcript `%s` (line %d).\n\n", d.Source, d.LineNum))
	if d.Rationale != "" {
		b.WriteString("## Rationale\n\n")
		b.WriteString(d.Rationale)
		b.WriteString("\n")
	}
	return b.String()
}
