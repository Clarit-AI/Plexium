package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

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
	RegenerationErrors  []string `json:"regenerationErrors,omitempty"`
	// NoProviderReason is set when --regenerate was not requested (or no
	// provider was configured) so the caller can explain why stale pages
	// remained stale. Empty when no explanation is needed.
	NoProviderReason string `json:"noProviderReason,omitempty"`
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
	MarkReviewed bool                   // advance ValidatedHash without an LLM call (debt mark)
	Cascade      *agent.ProviderCascade // required when Regenerate=true
}

// Run performs an incremental sync: detects stale pages, advances the
// observed source hash, and only advances ValidatedHash when wiki content
// has actually been refreshed (successful regen or explicit --mark-reviewed).
//
// The freshness model:
//
//   - A page is "stale" when the current file hash differs from the wiki
//     page's ValidatedHash (or when ValidatedHash is empty — v1 legacy).
//   - The observed source hash (SourceFile.Hash) is updated on every sync
//     so operators can see what the file looks like now.
//   - ValidatedHash is only updated when wiki content has actually been
//     refreshed: a successful regeneration, or an explicit --mark-reviewed.
//     Failed, skipped, or unavailable regeneration NEVER advances
//     ValidatedHash. This is what stops a plain `plexium sync` from
//     erasing the stale evidence that an LLM has not yet rewritten the
//     page.
//
// The default `plexium sync` (no --regenerate, no --mark-reviewed) is
// therefore fast, deterministic, and explicit: it records what was seen
// and reports what remains stale. It never silently falls through to LLM
// calls.
func Run(opts Options) (*SyncResult, error) {
	if opts.DryRun && opts.Regenerate {
		return nil, fmt.Errorf("cannot regenerate in dry-run mode")
	}
	if opts.Regenerate && opts.MarkReviewed {
		return nil, fmt.Errorf("cannot combine --regenerate with --mark-reviewed")
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

	// Detect stale pages by comparing stored *validated* hashes to current file hashes.
	stalePages, err := mgr.DetectStalePages(func(path string) (string, error) {
		return manifest.ComputeHash(filepath.Join(opts.RepoRoot, path))
	})
	if err != nil {
		return nil, fmt.Errorf("detecting stale pages: %w", err)
	}
	initialStale := len(stalePages)
	result.StalePages = initialStale

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
			}
		}
		stalePage.SourceFiles = newSources

		if validateThisPage {
			validatedUpdated += len(newSources)
			if opts.MarkReviewed {
				result.PagesMarkedReviewed++
			}
		}

		if err := mgr.UpsertPage(stalePage); err != nil {
			return nil, fmt.Errorf("updating page %s: %w", stalePage.WikiPath, err)
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
		return nil, fmt.Errorf("re-detecting stale pages after upsert: %w", err)
	}
	result.StalePages = len(finalStale)
	if result.StalePages > 0 {
		switch {
		case opts.MarkReviewed:
			// --mark-reviewed is exhaustive — if we reach here it means
			// a page wasn't in the manifest or another error occurred.
			result.NoProviderReason = ""
		case opts.Regenerate:
			result.NoProviderReason = fmt.Sprintf(
				"%d page(s) had no successful regeneration; run with a working provider or use --mark-reviewed to advance validated freshness",
				result.StalePages,
			)
		default:
			stillFresh := initialStale - result.StalePages
			if stillFresh > 0 {
				result.NoProviderReason = fmt.Sprintf(
					"%d page(s) remain stale; run 'plexium sync --regenerate' to refresh via LLM provider, or 'plexium sync --mark-reviewed' to advance validated freshness as explicit debt",
					result.StalePages,
				)
			} else {
				result.NoProviderReason = fmt.Sprintf(
					"%d page(s) remain stale; plain plexium sync does not advance validated freshness. Run 'plexium sync --regenerate' to refresh via LLM provider, or 'plexium sync --mark-reviewed' to advance validated freshness as explicit debt",
					result.StalePages,
				)
			}
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

// anyStale is no longer used; staleness is recomputed inline after the
// upsert loop. Kept as a stable signature for external callers until we
// confirm none rely on it.
var _ = (*manifest.Manifest)(nil)

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
