package convert

import (
	"encoding/json"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReport_Generate(t *testing.T) {
	gen := NewReportGenerator()

	pages := []PageData{
		{WikiPath: "Home.md", Title: "Home", Section: "Root"},
		{WikiPath: "modules/auth.md", Title: "Auth", Section: "Modules", IsStub: true},
	}

	filter := &FilterResult{
		Eligible: []scanner.File{
			{Path: "README.md"},
			{Path: "src/auth/handler.go"},
		},
		Skipped: []scanner.File{
			{Path: "image.png"},
			{Path: "vendor/lib.go"},
		},
		SkipReasons: map[string]string{
			"image.png":      "binary file",
			"vendor/lib.go":  "excluded by pattern",
		},
	}

	lint := &LintResult{
		UndocumentedModules: []string{"api"},
		Orphans:             []string{"modules/auth.md"},
		MissingCrossRefs: []CrossRefSuggestion{
			{FromPage: "Home.md", ToPage: "modules/auth.md", Reason: "orphan"},
		},
		GapScore: 0.5,
	}

	inbound := map[string][]string{
		"Home.md": {"modules/auth.md"},
	}

	report := gen.Generate(pages, filter, lint, inbound)

	assert.Equal(t, "conversion", report.Type)
	assert.Equal(t, 4, report.Sources.Scanned)
	assert.Equal(t, 2, report.Sources.Included)
	assert.Equal(t, 2, report.Sources.Skipped)
	assert.Equal(t, 2, report.Pages.Generated)
	assert.Equal(t, 1, report.Pages.Stubs)
	assert.Equal(t, 0.5, report.GapScore)

	// Check gaps
	assert.GreaterOrEqual(t, len(report.Gaps), 2, "should have undocumented + orphan gaps")
}

func TestReport_ToJSON(t *testing.T) {
	gen := NewReportGenerator()
	report := gen.Generate(
		[]PageData{{WikiPath: "Home.md", Title: "Home"}},
		&FilterResult{SkipReasons: map[string]string{}},
		&LintResult{GapScore: 1.0},
		map[string][]string{},
	)

	data, err := report.ToJSON()
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "conversion", parsed["type"])
}

func TestReport_ToMarkdown(t *testing.T) {
	gen := NewReportGenerator()
	report := gen.Generate(
		[]PageData{
			{WikiPath: "Home.md", Title: "Home"},
			{WikiPath: "modules/auth.md", Title: "Auth", IsStub: true},
		},
		&FilterResult{
			Eligible:    []scanner.File{{Path: "README.md"}},
			Skipped:     []scanner.File{{Path: "img.png"}},
			SkipReasons: map[string]string{"img.png": "binary file"},
		},
		&LintResult{
			UndocumentedModules: []string{"api"},
			GapScore:            0.5,
		},
		map[string][]string{},
	)

	md := report.ToMarkdown()
	assert.Contains(t, md, "# Conversion Report")
	assert.Contains(t, md, "Scanned")
	assert.Contains(t, md, "binary file")
	assert.Contains(t, md, "Gap score: 50%")
}

func TestReport_SkipReasonCounts(t *testing.T) {
	gen := NewReportGenerator()

	filter := &FilterResult{
		Skipped: []scanner.File{
			{Path: "a.png"},
			{Path: "b.png"},
			{Path: "vendor/c.go"},
		},
		SkipReasons: map[string]string{
			"a.png":      "binary file",
			"b.png":      "binary file",
			"vendor/c.go": "excluded by pattern",
		},
	}

	report := gen.Generate(nil, filter, &LintResult{GapScore: 1.0}, nil)

	assert.Equal(t, 2, report.Sources.SkipReasons["binary file"])
	assert.Equal(t, 1, report.Sources.SkipReasons["excluded by pattern"])
}
