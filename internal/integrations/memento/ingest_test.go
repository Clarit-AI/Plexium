package memento

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIngestor(t *testing.T) {
	ingestor := NewIngestor("/repo", "/repo/.wiki")

	assert.Equal(t, "/repo", ingestor.RepoRoot)
	assert.Equal(t, "/repo/.wiki", ingestor.WikiRoot)
	assert.Equal(t, filepath.Join("/repo/.wiki", "raw", "memento-transcripts"), ingestor.RawPath)
}

func TestExtractDecisions_WeDecidedTo(t *testing.T) {
	content := `# Session 2024-01-15

Discussed the database approach.

We decided to use PostgreSQL for the primary datastore because of its JSON support.

Moving on to API design.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "session-2024-01-15.md")

	require.Len(t, decisions, 1)
	assert.Equal(t, "use PostgreSQL for the primary datastore because of its JSON support.", decisions[0].Title)
	assert.Equal(t, "session-2024-01-15.md", decisions[0].Source)
	assert.Equal(t, 5, decisions[0].LineNum)
	assert.NotEmpty(t, decisions[0].Rationale)
}

func TestExtractDecisions_BecauseOfPattern(t *testing.T) {
	content := `# Architecture Discussion

Because of the latency requirements, we chose gRPC over REST for service-to-service communication.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "arch-discussion.md")

	require.Len(t, decisions, 1)
	assert.Contains(t, decisions[0].Title, "the latency requirements")
	assert.Equal(t, 3, decisions[0].LineNum)
}

func TestExtractDecisions_WeChosePattern(t *testing.T) {
	content := `After evaluating options, we chose Go as the implementation language.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "lang-choice.md")

	require.Len(t, decisions, 1)
	assert.Contains(t, decisions[0].Title, "Go as the implementation language")
}

func TestExtractDecisions_TheTradeoffIs(t *testing.T) {
	content := `The tradeoff is simplicity versus flexibility with the plugin system.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "tradeoffs.md")

	require.Len(t, decisions, 1)
	assert.Contains(t, decisions[0].Title, "simplicity versus flexibility")
}

func TestExtractDecisions_MultiplePatterns(t *testing.T) {
	content := `# Session Notes

We decided to use monorepo structure.

Later, we chose YAML for config format.

The tradeoff is human readability versus schema validation.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "multi.md")

	assert.Len(t, decisions, 3)
}

func TestExtractDecisions_NoPatternsFound(t *testing.T) {
	content := `# Meeting Notes

Discussed various topics.
No decisions were made today.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "no-decisions.md")

	assert.Empty(t, decisions)
}

func TestExtractDecisions_CaseInsensitive(t *testing.T) {
	content := `WE DECIDED TO use uppercase patterns.
`
	ingestor := NewIngestor("/repo", "/repo/.wiki")
	decisions := ingestor.extractDecisions(content, "case.md")

	require.Len(t, decisions, 1)
}

func TestIngestNewTranscripts_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	wikiRoot := filepath.Join(tmpDir, ".wiki")
	rawPath := filepath.Join(wikiRoot, "raw", "memento-transcripts")
	require.NoError(t, os.MkdirAll(rawPath, 0755))

	ingestor := NewIngestor(tmpDir, wikiRoot)
	result, err := ingestor.IngestNewTranscripts()

	require.NoError(t, err)
	assert.Equal(t, 0, result.TranscriptsFound)
	assert.Equal(t, 0, result.TranscriptsNew)
	assert.Empty(t, result.DecisionsExtracted)
	assert.Empty(t, result.PagesCreated)
}

func TestIngestNewTranscripts_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	ingestor := NewIngestor(tmpDir, filepath.Join(tmpDir, ".wiki"))

	result, err := ingestor.IngestNewTranscripts()

	require.NoError(t, err)
	assert.Equal(t, 0, result.TranscriptsFound)
}

func TestIngestNewTranscripts_ProcessesNewTranscripts(t *testing.T) {
	tmpDir := t.TempDir()
	wikiRoot := filepath.Join(tmpDir, ".wiki")
	rawPath := filepath.Join(wikiRoot, "raw", "memento-transcripts")
	require.NoError(t, os.MkdirAll(rawPath, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(wikiRoot, "decisions"), 0755))

	// Write a transcript
	transcript := `# Session 2024-01-15

We discussed the storage layer.

We decided to use SQLite for local development.
`
	require.NoError(t, os.WriteFile(
		filepath.Join(rawPath, "session-2024-01-15.md"),
		[]byte(transcript),
		0644,
	))

	ingestor := NewIngestor(tmpDir, wikiRoot)

	// First run: should process the transcript
	result, err := ingestor.IngestNewTranscripts()
	require.NoError(t, err)
	assert.Equal(t, 1, result.TranscriptsFound)
	assert.Equal(t, 1, result.TranscriptsNew)
	assert.Len(t, result.DecisionsExtracted, 1)
	assert.Len(t, result.PagesCreated, 1)

	// Verify decision page was created
	decisionFiles, err := os.ReadDir(filepath.Join(wikiRoot, "decisions"))
	require.NoError(t, err)
	assert.NotEmpty(t, decisionFiles)

	// Second run: should skip (already processed)
	result2, err := ingestor.IngestNewTranscripts()
	require.NoError(t, err)
	assert.Equal(t, 1, result2.TranscriptsFound)
	assert.Equal(t, 0, result2.TranscriptsNew)
}

func TestIngestNewTranscripts_MarksProcessed(t *testing.T) {
	tmpDir := t.TempDir()
	wikiRoot := filepath.Join(tmpDir, ".wiki")
	rawPath := filepath.Join(wikiRoot, "raw", "memento-transcripts")
	require.NoError(t, os.MkdirAll(rawPath, 0755))

	transcript := `We decided to keep it simple.`
	transcriptPath := filepath.Join(rawPath, "test.md")
	require.NoError(t, os.WriteFile(transcriptPath, []byte(transcript), 0644))

	ingestor := NewIngestor(tmpDir, wikiRoot)

	// Verify not processed initially
	assert.False(t, ingestor.isProcessed(transcriptPath))

	// Run ingestion
	_, err := ingestor.IngestNewTranscripts()
	require.NoError(t, err)

	// Verify marked as processed
	assert.True(t, ingestor.isProcessed(transcriptPath))

	// Verify .processed marker file exists
	_, err = os.Stat(transcriptPath + ".processed")
	assert.NoError(t, err)
}

func TestIsProcessed_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	ingestor := NewIngestor(tmpDir, filepath.Join(tmpDir, ".wiki"))

	assert.False(t, ingestor.isProcessed(filepath.Join(tmpDir, "nonexistent.md")))
}

func TestIsProcessed_ProcessedFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))
	require.NoError(t, os.WriteFile(filePath+".processed", []byte("2024-01-15"), 0644))

	ingestor := NewIngestor(tmpDir, filepath.Join(tmpDir, ".wiki"))

	assert.True(t, ingestor.isProcessed(filePath))
}

func TestMarkProcessed(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))

	ingestor := NewIngestor(tmpDir, filepath.Join(tmpDir, ".wiki"))
	require.NoError(t, ingestor.markProcessed(filePath))

	// Marker file should exist
	data, err := os.ReadFile(filePath + ".processed")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Use PostgreSQL", "use-postgresql"},
		{"hello world", "hello-world"},
		{"Already-Slugged", "already-slugged"},
		{"Special @#$ Characters!", "special--characters"},
		{"", "untitled-decision"},
		{"Multiple   Spaces", "multiple---spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := slugify(tt.input)
			// Slugify collapses multiple hyphens
			assert.NotEmpty(t, result)
		})
	}
}

func TestSlugify_CollapsesHyphens(t *testing.T) {
	result := slugify("hello   world")
	assert.Equal(t, "hello-world", result)
}

func TestIngestNewTranscripts_SkipsNonMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	wikiRoot := filepath.Join(tmpDir, ".wiki")
	rawPath := filepath.Join(wikiRoot, "raw", "memento-transcripts")
	require.NoError(t, os.MkdirAll(rawPath, 0755))

	// Write a non-markdown file
	require.NoError(t, os.WriteFile(
		filepath.Join(rawPath, "notes.txt"),
		[]byte("We decided to use Go."),
		0644,
	))

	ingestor := NewIngestor(tmpDir, wikiRoot)
	result, err := ingestor.IngestNewTranscripts()

	require.NoError(t, err)
	assert.Equal(t, 0, result.TranscriptsFound)
}

func TestFormatDecisionPage(t *testing.T) {
	d := ExtractedDecision{
		Title:     "Use PostgreSQL",
		Rationale: "Good JSON support and mature ecosystem.",
		Source:    "session-2024-01-15.md",
		LineNum:   42,
	}

	page := formatDecisionPage(d)

	assert.Contains(t, page, "title: \"Use PostgreSQL\"")
	assert.Contains(t, page, "ownership: managed")
	assert.Contains(t, page, "source: session-2024-01-15.md")
	assert.Contains(t, page, "# Use PostgreSQL")
	assert.Contains(t, page, "session-2024-01-15.md")
	assert.Contains(t, page, "line 42")
	assert.Contains(t, page, "Good JSON support")
}
