package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/bootstrap"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
)

// markedupEnrichJobPayload separates "which pages are stale" from the
// human-readable Target/Reason fields so the executor doesn't have to
// re-scan the manifest. It is JSON-encoded into upkeepJob.Payload.
//
// The encoding is intentionally newline-separated wikiPaths rather than
// JSON to match the existing payload conventions in upkeep.go (e.g. the
// drift job's PagesAffected join). Keeping the shape identical means the
// snapshot persistence layer doesn't need a special case.
type markedupEnrichJobPayload struct {
	StalePages []string
}

func (p markedupEnrichJobPayload) Encode() string {
	return strings.Join(p.StalePages, "\n")
}

func decodeMarkedupEnrichPayload(s string) markedupEnrichJobPayload {
	if s == "" {
		return markedupEnrichJobPayload{}
	}
	pages := strings.Split(s, "\n")
	cleaned := pages[:0]
	for _, p := range pages {
		if strings.TrimSpace(p) != "" {
			cleaned = append(cleaned, p)
		}
	}
	return markedupEnrichJobPayload{StalePages: cleaned}
}

// markedupConfig parses the plugins.markedup block off the daemon's
// loaded config. Returns (cfg, true) when a usable parsed config exists,
// or (zero, false) when the block is missing, malformed, or disabled.
//
// We deliberately swallow parse errors here: the daemon must remain
// resilient when one plugin's config is busted. The convert command
// surfaces parse errors at startup, which is the right place to catch
// them — the daemon should never block its other watches because of a
// markedup typo.
func (d *Daemon) markedupConfig() (markedup.Config, bool) {
	if d.config.Config == nil {
		return markedup.Config{}, false
	}
	raw := d.config.Config.PluginSettings("markedup")
	if len(raw) == 0 {
		return markedup.Config{}, false
	}
	cfg, err := markedup.ParseConfig(raw)
	if err != nil {
		return markedup.Config{}, false
	}
	if !cfg.Enabled {
		return markedup.Config{}, false
	}
	return cfg, true
}

// detectMarkedupEnrichJob queues a markedup re-enrichment job when one or
// more manifest entries have a missing or stale LastEnriched timestamp.
//
// The watch is a strict superset of "plugin disabled" → no work: when the
// markedup plugin is disabled (or not configured), this returns no job
// regardless of manifest state. The daemon must never enrich pages that
// the user has not opted in to.
//
// "Stale" is defined as: LastEnriched is empty OR LastEnriched parses
// successfully and is older than now - cfg.Daemon.RefreshIntervalDuration().
// An unparseable LastEnriched is treated as missing — the next run will
// rewrite it with a valid RFC3339 timestamp.
func (d *Daemon) detectMarkedupEnrichJob() (*upkeepJob, TickAction) {
	cfg, ok := d.markedupConfig()
	if !ok {
		return nil, TickAction{}
	}

	mgr, err := manifest.NewManager(manifest.DefaultPath(d.config.RepoRoot))
	if err != nil {
		return nil, TickAction{Watch: "markedup", Action: "scan", Target: d.wikiRootTarget(), Success: false, Error: err.Error()}
	}
	m, err := mgr.Load()
	if err != nil {
		// A missing manifest is not an error here — it just means there
		// is nothing to enrich yet. Surfacing it as a tick failure would
		// noise up the daemon log on every fresh repo.
		if os.IsNotExist(err) {
			return nil, TickAction{}
		}
		return nil, TickAction{Watch: "markedup", Action: "scan", Target: d.wikiRootTarget(), Success: false, Error: err.Error()}
	}

	ttl := cfg.Daemon.RefreshIntervalDuration()
	cutoff := time.Now().Add(-ttl)

	stale := make([]string, 0, len(m.Pages))
	for _, page := range m.Pages {
		if page.WikiPath == "" {
			continue
		}
		if page.LastEnriched == "" {
			stale = append(stale, page.WikiPath)
			continue
		}
		t, perr := time.Parse(time.RFC3339, page.LastEnriched)
		if perr != nil || t.Before(cutoff) {
			stale = append(stale, page.WikiPath)
		}
	}

	if len(stale) == 0 {
		return nil, TickAction{}
	}

	target := strings.Join(limitStrings(stale, 3), ", ")
	if len(stale) > 3 {
		target = fmt.Sprintf("%s (+%d more)", target, len(stale)-3)
	}
	payload := markedupEnrichJobPayload{StalePages: stale}.Encode()

	return &upkeepJob{
			ID:      fmt.Sprintf("markedup-enrich-%d", time.Now().UnixMilli()),
			Type:    jobTypeMarkedupEnrich,
			Target:  target,
			Reason:  fmt.Sprintf("%d page(s) past %s refresh interval", len(stale), ttl),
			Payload: payload,
		},
		TickAction{Watch: "markedup", Action: "queue", Target: target, Success: true}
}

// runMarkedupEnrichJob executes a markedup re-enrichment job in-process.
//
// Unlike the wiki-upkeep jobs handled by executeJob, this path never
// touches a worktree or invokes a coding-agent runner: enrichment is a
// pure-Go operation against the existing wiki + manifest, so we run it
// directly against the live repo. This also means it does not contend
// with the runner's max-concurrent worktree budget.
//
// The function builds a fresh registry from the loaded config (D1's
// bootstrap.BuildRegistry), pulls the EnricherPlugin out, constructs a
// PipelineData containing only the stale subset, and calls Process. The
// EnricherPlugin itself is responsible for updating LastEnriched in the
// manifest.
func (d *Daemon) runMarkedupEnrichJob(ctx context.Context, job *upkeepJob) TickAction {
	action := TickAction{Watch: "markedup", Action: "execute", Target: job.Target}
	if d.config.Config == nil {
		action.Error = "markedup-enrich job dispatched without a loaded config"
		return action
	}

	// Pass the assistive cascade so markedup's Tier 2 enrichment
	// (modelEnrich: true) can construct an LLM-backed extractor for
	// periodic refreshes. Without this, BuildRegistry receives a nil
	// cascade and silently degrades to Tier 1, contradicting the user's
	// modelEnrich opt-in on every 24h cycle. The rate tracker keeps
	// daemon-driven enrichment on the same daily-spend ledger as
	// convert-time enrichment.
	reg, err := bootstrap.BuildRegistry(ctx, d.config.RepoRoot, d.config.Config, bootstrap.Options{
		Cascade:     d.cascade,
		RateTracker: d.rateTracker,
	})
	if err != nil {
		action.Error = fmt.Sprintf("build registry: %v", err)
		return action
	}

	// The enricher is a PipelinePlugin at the StageAfterWrite stage —
	// pull it out by stage rather than by name so a future rename stays
	// internal to the markedup package.
	pps := reg.PipelinePlugins(plugins.StageAfterWrite)
	var enricher plugins.PipelinePlugin
	for _, p := range pps {
		if p.Name() == "markedup-enricher" {
			enricher = p
			break
		}
	}
	if enricher == nil {
		// Plugin disabled between detection and execution; treat as a
		// successful no-op rather than a failure so the daemon doesn't
		// repeatedly log a transient state.
		action.Success = true
		action.Action = "skipped"
		action.Target = "markedup plugin no longer enabled"
		return action
	}

	// Reconstruct the WikiRoot the same way the convert pipeline does
	// so the enricher reads the right files. configuredWikiRootAbs
	// already encapsulates the cfg.Wiki.Root override.
	wikiRoot := configuredWikiRootAbs(d.config.RepoRoot, d.config.Config)

	payload := decodeMarkedupEnrichPayload(job.Payload)
	pages := make([]plugins.PipelinePage, 0, len(payload.StalePages))

	// We need each page's title/section/source-files for the enricher to
	// behave the same as it does mid-convert. Read those off the manifest
	// rather than re-deriving them — the manifest is authoritative for
	// "what plexium thinks this page is" and avoids re-parsing the file
	// just to populate metadata the enricher then reparses anyway.
	mgr, err := manifest.NewManager(manifest.DefaultPath(d.config.RepoRoot))
	if err != nil {
		action.Error = fmt.Sprintf("manifest manager: %v", err)
		return action
	}
	m, err := mgr.Load()
	if err != nil {
		action.Error = fmt.Sprintf("load manifest: %v", err)
		return action
	}
	byPath := make(map[string]manifest.PageEntry, len(m.Pages))
	for _, pg := range m.Pages {
		byPath[pg.WikiPath] = pg
	}
	for _, wp := range payload.StalePages {
		entry, ok := byPath[wp]
		if !ok {
			// Page disappeared from the manifest between queue and run;
			// skip it silently. The next tick will discover any new
			// staleness.
			continue
		}
		sources := make([]string, 0, len(entry.SourceFiles))
		for _, sf := range entry.SourceFiles {
			sources = append(sources, sf.Path)
		}
		pages = append(pages, plugins.PipelinePage{
			WikiPath:    entry.WikiPath,
			Title:       entry.Title,
			Section:     entry.Section,
			SourceFiles: sources,
		})
	}

	if len(pages) == 0 {
		action.Success = true
		action.Action = "skipped"
		action.Target = "no stale pages to enrich"
		return action
	}

	data := &plugins.PipelineData{
		RepoRoot: d.config.RepoRoot,
		WikiRoot: wikiRoot,
		Pages:    pages,
	}

	if err := enricher.Process(ctx, data); err != nil {
		action.Error = fmt.Sprintf("enricher: %v", err)
		return action
	}

	action.Success = true
	action.Target = fmt.Sprintf("%d page(s) enriched", len(pages))
	return action
}

// markedupEnrichWikiRoot is a thin wrapper that exists so the test file
// can stub out the wiki root resolution if it ever needs to. Today it's
// a one-liner — kept here purely to keep the seam available.
//
//nolint:unused // reserved for future test injection
func markedupEnrichWikiRoot(repoRoot string, daemon *Daemon) string {
	return filepath.Join(repoRoot, daemon.wikiRootRel())
}
