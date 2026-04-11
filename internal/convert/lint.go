package convert

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// ConvertLinter performs gap analysis on conversion results.
type ConvertLinter struct {
	linker *Linker
}

// LintResult holds the outcome of lint analysis.
type LintResult struct {
	UndocumentedModules []string
	MissingCrossRefs    []CrossRefSuggestion
	Orphans             []string
	GapScore            float64 // 0.0–1.0
	StubPages           []PageData
}

// CrossRefSuggestion suggests a cross-reference between two pages.
type CrossRefSuggestion struct {
	FromPage string
	ToPage   string
	Reason   string
}

// NewConvertLinter creates a new ConvertLinter.
func NewConvertLinter(linker *Linker) *ConvertLinter {
	return &ConvertLinter{linker: linker}
}

// Analyze runs gap analysis on the conversion results.
func (cl *ConvertLinter) Analyze(pages []PageData, eligible []scanner.File) *LintResult {
	result := &LintResult{}

	// 1. Find undocumented modules (source dirs without wiki pages)
	result.UndocumentedModules = cl.findUndocumentedModules(pages, eligible)

	// 2. Create stubs for undocumented modules
	for _, mod := range result.UndocumentedModules {
		result.StubPages = append(result.StubPages, cl.createStub(mod))
	}

	// 3. Find orphan pages (no inbound links)
	inbound, _ := cl.linker.ComputeLinks(pages)
	for _, p := range pages {
		if p.WikiPath == "Home.md" {
			continue // Home is always linked
		}
		if _, hasInbound := inbound[p.WikiPath]; !hasInbound {
			result.Orphans = append(result.Orphans, p.WikiPath)
		}
	}

	// 4. Suggest missing cross-references for orphans
	for _, orphan := range result.Orphans {
		// Suggest linking from Home to this orphan
		result.MissingCrossRefs = append(result.MissingCrossRefs, CrossRefSuggestion{
			FromPage: "Home.md",
			ToPage:   orphan,
			Reason:   "orphan page with no inbound links",
		})
	}

	// 5. Compute gap score
	totalEligibleDirs := countSourceDirs(eligible)
	documentedDirs := countDocumentedDirs(pages)
	if totalEligibleDirs > 0 {
		result.GapScore = float64(documentedDirs) / float64(totalEligibleDirs)
	} else {
		if len(pages) > 0 {
			result.GapScore = 1.0
		} else {
			result.GapScore = 0.0
		}
	}

	return result
}

// findUndocumentedModules finds source directories that don't have corresponding wiki pages.
func (cl *ConvertLinter) findUndocumentedModules(pages []PageData, eligible []scanner.File) []string {
	// Collect all module slugs from pages
	documented := make(map[string]bool)
	for _, p := range pages {
		if p.Section == "Modules" {
			slug := strings.TrimSuffix(filepath.Base(p.WikiPath), ".md")
			documented[strings.ToLower(slug)] = true
		}
	}

	// Collect all source directories from eligible files
	srcDirs := make(map[string]bool)
	srcPrefixes := []string{"src/", "internal/", "pkg/", "lib/", "cmd/"}
	for _, f := range eligible {
		for _, prefix := range srcPrefixes {
			if strings.HasPrefix(f.Path, prefix) {
				parts := strings.SplitN(f.Path, "/", 3)
				if len(parts) >= 2 {
					srcDirs[parts[1]] = true
				}
				break
			}
		}
	}

	// Find undocumented
	var undocumented []string
	for dir := range srcDirs {
		slug := strings.ToLower(dir)
		if !documented[slug] {
			undocumented = append(undocumented, dir)
		}
	}

	return undocumented
}

func (cl *ConvertLinter) createStub(moduleName string) PageData {
	title := formatModuleTitle(moduleName)
	slug := strings.ToLower(moduleName)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", title))
	b.WriteString("ownership: managed\n")
	b.WriteString("confidence: low\n")
	b.WriteString("review-status: unreviewed\n")
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# %s\n\n", title))
	b.WriteString("<!-- STATUS: stub -->\n\n")
	b.WriteString("This module was detected during conversion but has no documentation.\n")
	b.WriteString("Please add content describing this module's purpose and usage.\n")

	return PageData{
		WikiPath:   fmt.Sprintf("modules/%s.md", slug),
		Title:      title,
		Section:    "Modules",
		Content:    b.String(),
		Confidence: "low",
		IsStub:     true,
	}
}

func countSourceDirs(eligible []scanner.File) int {
	dirs := make(map[string]bool)
	srcPrefixes := []string{"src/", "internal/", "pkg/", "lib/", "cmd/"}
	for _, f := range eligible {
		for _, prefix := range srcPrefixes {
			if strings.HasPrefix(f.Path, prefix) {
				parts := strings.SplitN(f.Path, "/", 3)
				if len(parts) >= 2 {
					dirs[parts[0]+"/"+parts[1]] = true
				}
				break
			}
		}
	}
	return len(dirs)
}

func countDocumentedDirs(pages []PageData) int {
	count := 0
	for _, p := range pages {
		if p.Section == "Modules" && !p.IsStub {
			count++
		}
	}
	return count
}