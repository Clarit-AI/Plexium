package lint

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
)

// LintReport is the complete lint report structure.
type LintReport struct {
	Type          string                 `json:"type"`
	Timestamp     string                 `json:"timestamp"`
	Deterministic DeterministicReport    `json:"deterministic"`
	Summary       LintSummary            `json:"summary"`
}

// DeterministicReport contains all deterministic lint results.
type DeterministicReport struct {
	BrokenLinks       []BrokenLinkReport      `json:"brokenLinks"`
	OrphanPages       []OrphanReport          `json:"orphanPages"`
	StaleCandidates   []StaleReport           `json:"staleCandidates"`
	MissingSources    []string                `json:"missingSourceFiles"`
	ManifestDrift     []ManifestErrorReport   `json:"manifestDrift"`
	SidebarIssues     []SidebarIssueReport   `json:"sidebarIssues"`
	FrontmatterIssues []FrontmatterIssueReport `json:"frontmatterIssues"`
}

// BrokenLinkReport represents a broken wiki link.
type BrokenLinkReport struct {
	PagePath string `json:"pagePath"`
	LineNum  int    `json:"lineNum"`
	Target   string `json:"target"`
	RawLink  string `json:"rawLink"`
}

// OrphanReport represents an orphan page.
type OrphanReport struct {
	WikiPath string `json:"wikiPath"`
	Reason   string `json:"reason"`
	Severity string `json:"severity"`
}

// StaleReport represents a stale page.
type StaleReport struct {
	WikiPath        string   `json:"wikiPath"`
	SourceFiles     []string `json:"sourceFiles"`
	DaysSinceUpdate int      `json:"daysSinceUpdate"`
	Severity        string   `json:"severity"`
}

// ManifestErrorReport represents a manifest validation error.
type ManifestErrorReport struct {
	Path    string `json:"path"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// SidebarIssueReport represents a sidebar validation issue.
type SidebarIssueReport struct {
	LineNum  int    `json:"lineNum"`
	Target   string `json:"target"`
	LinkText string `json:"linkText"`
}

// FrontmatterIssueReport represents a frontmatter validation issue.
type FrontmatterIssueReport struct {
	WikiPath string `json:"wikiPath"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

// LintSummary contains counts of issues found.
type LintSummary struct {
	Errors   int  `json:"errors"`
	Warnings int  `json:"warnings"`
	Info     int  `json:"info"`
	PassesCI bool `json:"passesCI"`
}

// Linter runs deterministic lint checks.
type Linter struct {
	repoRoot string
	cfg      *config.Config
}

// NewLinter creates a new Linter.
func NewLinter(repoRoot string, cfg *config.Config) *Linter {
	return &Linter{
		repoRoot: repoRoot,
		cfg:      cfg,
	}
}

// RunDeterministic runs all deterministic lint checks.
func (l *Linter) RunDeterministic() (*LintReport, error) {
	report := &LintReport{
		Type:      "lint",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	wikiRoot := l.wikiRoot()

	// Get manifest manager
	manifestPath := filepath.Join(l.repoRoot, ".plexium", "manifest.json")
	manifestMgr, err := manifest.NewManager(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("creating manifest manager: %w", err)
	}

	det := &DeterministicReport{}

	// 1. Link crawler
	brokenLinks, err := l.checkLinks(wikiRoot)
	if err != nil {
		return nil, fmt.Errorf("checking links: %w", err)
	}
	det.BrokenLinks = brokenLinks

	// 2. Orphan detector
	orphans, err := l.checkOrphans(wikiRoot, manifestMgr)
	if err != nil {
		return nil, fmt.Errorf("checking orphans: %w", err)
	}
	det.OrphanPages = orphans

	// 3. Staleness detector
	stale, err := l.checkStaleness(wikiRoot, manifestMgr)
	if err != nil {
		return nil, fmt.Errorf("checking staleness: %w", err)
	}
	det.StaleCandidates = stale

	// 4. Manifest validator
	manifestIssues, err := l.checkManifest(wikiRoot, manifestMgr)
	if err != nil {
		return nil, fmt.Errorf("checking manifest: %w", err)
	}
	det.ManifestDrift = manifestIssues

	// 5. Sidebar validator
	sidebarIssues, err := l.checkSidebar(wikiRoot)
	if err != nil {
		return nil, fmt.Errorf("checking sidebar: %w", err)
	}
	det.SidebarIssues = sidebarIssues

	// 6. Frontmatter validator
	frontmatterIssues, err := l.checkFrontmatter(wikiRoot)
	if err != nil {
		return nil, fmt.Errorf("checking frontmatter: %w", err)
	}
	det.FrontmatterIssues = frontmatterIssues

	report.Deterministic = *det
	report.Summary = l.summarize(det)

	return report, nil
}

func (l *Linter) wikiRoot() string {
	if l.cfg != nil && l.cfg.Wiki.Root != "" {
		return filepath.Join(l.repoRoot, l.cfg.Wiki.Root)
	}
	return filepath.Join(l.repoRoot, ".wiki")
}

func (l *Linter) checkLinks(wikiRoot string) ([]BrokenLinkReport, error) {
	crawler := NewLinkCrawler(wikiRoot)
	broken, err := crawler.GetBrokenLinks()
	if err != nil {
		return nil, err
	}

	reports := make([]BrokenLinkReport, 0, len(broken))
	for _, b := range broken {
		reports = append(reports, BrokenLinkReport{
			PagePath: b.PagePath,
			LineNum:  b.LineNum,
			Target:   b.Target,
			RawLink:  b.RawLink,
		})
	}
	return reports, nil
}

func (l *Linter) checkOrphans(wikiRoot string, mgr *manifest.Manager) ([]OrphanReport, error) {
	detector := NewOrphanDetector(wikiRoot, mgr)
	result, err := detector.Detect()
	if err != nil {
		return nil, err
	}

	reports := make([]OrphanReport, 0, len(result.Orphans))
	for _, o := range result.Orphans {
		reports = append(reports, OrphanReport{
			WikiPath: o.WikiPath,
			Reason:   o.Reason,
			Severity: o.Severity,
		})
	}
	return reports, nil
}

func (l *Linter) checkStaleness(wikiRoot string, mgr *manifest.Manager) ([]StaleReport, error) {
	detector := NewStalenessDetector(wikiRoot, mgr)
	result, err := detector.Detect()
	if err != nil {
		return nil, err
	}

	reports := make([]StaleReport, 0, len(result.StalePages))
	for _, s := range result.StalePages {
		reports = append(reports, StaleReport{
			WikiPath:        s.WikiPath,
			SourceFiles:     s.SourceFiles,
			DaysSinceUpdate: s.DaysSinceUpdate,
			Severity:        s.Severity,
		})
	}
	return reports, nil
}

func (l *Linter) checkManifest(wikiRoot string, mgr *manifest.Manager) ([]ManifestErrorReport, error) {
	validator := NewManifestValidator(wikiRoot, mgr)
	result, err := validator.Validate()
	if err != nil {
		return nil, err
	}

	var reports []ManifestErrorReport
	for _, e := range result.Errors {
		reports = append(reports, ManifestErrorReport{
			Path:    e.Path,
			Field:   e.Field,
			Message: e.Message,
		})
	}
	return reports, nil
}

func (l *Linter) checkSidebar(wikiRoot string) ([]SidebarIssueReport, error) {
	validator := NewSidebarValidator(wikiRoot)
	result, err := validator.Validate()
	if err != nil {
		return nil, err
	}

	reports := make([]SidebarIssueReport, 0, len(result.BrokenLinks))
	for _, b := range result.BrokenLinks {
		reports = append(reports, SidebarIssueReport{
			LineNum:  b.LineNum,
			Target:   b.Target,
			LinkText: b.LinkText,
		})
	}
	return reports, nil
}

func (l *Linter) checkFrontmatter(wikiRoot string) ([]FrontmatterIssueReport, error) {
	validator := NewFrontmatterValidator(wikiRoot)
	result, err := validator.Validate()
	if err != nil {
		return nil, err
	}

	reports := make([]FrontmatterIssueReport, 0, len(result.Errors))
	for _, e := range result.Errors {
		reports = append(reports, FrontmatterIssueReport{
			WikiPath: e.WikiPath,
			Field:    e.Field,
			Message:  e.Message,
		})
	}
	return reports, nil
}

func (l *Linter) summarize(d *DeterministicReport) LintSummary {
	var s LintSummary

	// Errors: broken links, manifest drift (errors only)
	s.Errors = len(d.BrokenLinks) + len(d.ManifestDrift)

	// Add frontmatter errors as errors
	for _, f := range d.FrontmatterIssues {
		_ = f // All frontmatter errors are errors
		s.Errors++
	}

	// Warnings: orphans (errors), stale candidates, sidebar issues
	s.Warnings = len(d.OrphanPages) + len(d.StaleCandidates) + len(d.SidebarIssues)

	// Info: missing sources
	s.Info = len(d.MissingSources)

	// PassesCI if no errors
	s.PassesCI = s.Errors == 0

	return s
}

// ToJSON formats the lint report as JSON.
func (r *LintReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ExitCode returns the appropriate exit code for the lint results.
func (r *LintReport) ExitCode() int {
	if r.Summary.Errors > 0 {
		return 1
	}
	if r.Summary.Warnings > 0 {
		return 2
	}
	return 0
}
