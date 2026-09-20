package sync

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/compile"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/regen"
	"github.com/Clarit-AI/Plexium/internal/scanner"
)

// SyncResult captures the outcome of a sync operation.
type SyncResult struct {
	SourceFilesChecked  int      `json:"sourceFilesChecked"`
	StalePages          int      `json:"stalePages"`
	HashesUpdated       int      `json:"hashesUpdated"`
	ValidatedUpdated    int      `json:"validatedUpdated"`
	NavRecompiled       bool     `json:"navRecompiled"`
	DryRun              bool     `json:"dryRun"`
	PagesAffected       []string `json:"pagesAffected"`
	PagesRegenerated    int      `json:"pagesRegenerated"`
	PagesMarkedReviewed int      `json:"pagesMarkedReviewed"`
	NoProviderReason    string   `json:"noProviderReason,omitempty"`
	RegenerationErrors  []string `json:"regenerationErrors,omitempty"`
}

// ExitCode returns 1 if stale pages were found, 0 otherwise.
// Used by --ci mode to signal CI pipeline failures.
func (r *SyncResult) ExitCode() int {
	if r.StalePages > 0 {
		return 1
	}
	return 0
}

// Options configures a sync run.
type Options struct {
	RepoRoot     string
	Config       *config.Config
	DryRun       bool
	Regenerate   bool
	MarkReviewed bool
	Cascade      *agent.ProviderCascade // required when Regenerate=true
}

// Run performs an incremental sync: detects changed source files, updates
// manifest hashes for stale pages, and recompiles navigation files.
func Run(opts Options) (*SyncResult, error) {
	if opts.DryRun && opts.Regenerate {
		return nil, fmt.Errorf("cannot regenerate in dry-run mode")
	}
	if opts.Regenerate && opts.MarkReviewed {
		return nil, fmt.Errorf("--regenerate with --mark-reviewed are mutually exclusive")
	}

	result := &SyncResult{DryRun: opts.DryRun}

	mgr, err := manifest.NewManager(manifest.DefaultPath(opts.RepoRoot))
	if err != nil {
		return nil, fmt.Errorf("opening manifest: %w", err)
	}

	m, err := mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	// Count total source files tracked in manifest
	sourceSet := make(map[string]bool)
	for _, page := range m.Pages {
		for _, sf := range page.SourceFiles {
			sourceSet[sf.Path] = true
		}
	}
	result.SourceFilesChecked = len(sourceSet)

	// Detect stale pages by comparing current source hashes against each
	// page's ValidatedHash (not the observed Hash field). This is the
	// KHA-287 fix: a plain sync must not advance validated freshness.
	stalePages, err := mgr.DetectStalePages(func(path string) (string, error) {
		return manifest.ComputeHash(filepath.Join(opts.RepoRoot, path))
	})
	if err != nil {
		return nil, fmt.Errorf("detecting stale pages: %w", err)
	}
	result.StalePages = len(stalePages)

	// Collect affected page paths
	for _, p := range stalePages {
		result.PagesAffected = append(result.PagesAffected, p.WikiPath)
	}

	// Always scan for new source files, even when no pages are stale
	if opts.Config != nil {
		newFiles, err := detectNewSources(opts.RepoRoot, opts.Config, m)
		if err != nil {
			return nil, fmt.Errorf("detecting new sources: %w", err)
		}
		for _, f := range newFiles {
			result.PagesAffected = append(result.PagesAffected, fmt.Sprintf("(new source) %s", f))
		}
	}

	// Regenerate stale pages via LLM provider cascade when requested.
	// Track which pages succeeded so we only update ValidatedHash for those.
	regenerated := make(map[string]bool)
	if opts.Regenerate && len(stalePages) > 0 {
		if opts.Cascade == nil {
			return nil, fmt.Errorf("no LLM providers configured; --regenerate requires at least one provider in the cascade")
		}

		wikiRoot := ".wiki"
		if opts.Config != nil && opts.Config.Wiki.Root != "" {
			wikiRoot = opts.Config.Wiki.Root
		}

		for _, page := range stalePages {
			regenResult, regenErr := regen.RegeneratePage(context.Background(), regen.PageRegenOptions{
				RepoRoot: opts.RepoRoot,
				WikiRoot: wikiRoot,
				Page:     page,
				Config:   opts.Config,
				Cascade:  opts.Cascade,
			})
			if regenErr != nil {
				errMsg := fmt.Sprintf("%s: %v", page.WikiPath, regenErr)
				result.RegenerationErrors = append(result.RegenerationErrors, errMsg)
				continue
			}
			if regenResult.Written {
				result.PagesRegenerated++
				regenerated[page.WikiPath] = true
			}
		}
	}

	// If there's nothing stale to act on, we're done.
	if len(stalePages) == 0 {
		return result, nil
	}

	if opts.DryRun {
		return result, nil
	}

	// Refresh the observed source hashes regardless of validation path so
	// operators can see what the file looks like right now. The Hash field
	// is intentionally separate from ValidatedHash; only ValidatedHash is
	// gated on proof of wiki refresh.
	now := time.Now().UTC().Format(time.RFC3339)
	updated := 0
	validatedUpdated := 0
	for _, stalePage := range stalePages {
		newSources := make([]manifest.SourceFile, len(stalePage.SourceFiles))
		copy(newSources, stalePage.SourceFiles)

		// True only when we have evidence that wiki content has actually
		// been refreshed against the current source state. ValidatedHash
		// must not advance otherwise — this is the KHA-287 fix.
		validateThisPage := opts.MarkReviewed ||
			(opts.Regenerate && regenerated[stalePage.WikiPath])

		validated := 0
		for i, sf := range newSources {
			newHash, err := manifest.ComputeHash(filepath.Join(opts.RepoRoot, sf.Path))
			if err != nil {
				// Source file may have been deleted — keep old entry
				continue
			}
			newSources[i].Hash = newHash
			updated++
			if validateThisPage {
				newSources[i].ValidatedHash = newHash
				newSources[i].LastValidatedAt = now
				validated++
			}
		}
		stalePage.SourceFiles = newSources

		if validateThisPage {
			validatedUpdated += validated
			if opts.MarkReviewed {
				result.PagesMarkedReviewed++
			}
		}

		if err := mgr.UpsertPage(stalePage); err != nil {
			return nil, fmt.Errorf("updating page %s: %w", stalePage.WikiPath)
		}
	}
	result.HashesUpdated = updated
	result.ValidatedUpdated = validatedUpdated

	// Annotate why stale pages may remain. Recompute against the
	// just-upserted manifest so we only emit when pages are actually still
	// stale.
	finalStale, err := mgr.DetectStalePages(func(path string) (string, error) {
		return manifest.ComputeHash(filepath.Join(opts.RepoRoot, path))
	})
	if err != nil {
		return result, nil // best effort
	}
	if len(finalStale) > 0 {
		switch {
		case !opts.Regenerate && !opts.MarkReviewed:
			result.NoProviderReason = fmt.Sprintf(
				"%d page(s) remain stale: plexium sync did not invoke an LLM provider. "+
					"Run `plexium sync --regenerate` (with a configured cascade) or `plexium sync --mark-reviewed` "+
					"to clear validated freshness for these pages.", len(finalStale))
		case opts.Regenerate && len(result.RegenerationErrors) > 0:
			result.NoProviderReason = fmt.Sprintf(
				"%d page(s) remain stale after regeneration: providers failed. "+
					"See regenerationErrors for details.", len(finalStale))
		}
	}

	// Recompile navigation files
	compiler := compile.NewCompiler(opts.RepoRoot, false)
	if _, err := compiler.Compile(); err != nil {
		return nil, fmt.Errorf("recompiling navigation: %w", err)
	}
	result.NavRecompiled = true

	return result, nil
}

// detectNewSources finds source files matching config globs that aren't tracked in the manifest.
func detectNewSources(repoRoot string, cfg *config.Config, m *manifest.Manifest) ([]string, error) {
	s, err := scanner.New(cfg.Sources.Include, cfg.Sources.Exclude)
	if err != nil {
		return nil, err
	}

	files, err := s.Scan(repoRoot)
	if err != nil {
		return nil, err
	}

	// Build set of all tracked source paths
	tracked := make(map[string]bool)
	for _, page := range m.Pages {
		for _, sf := range page.SourceFiles {
			tracked[sf.Path] = true
		}
	}

	var newFiles []string
	for _, f := range files {
		if !tracked[f.Path] {
			newFiles = append(newFiles, f.Path)
		}
	}
	return newFiles, nil
}
