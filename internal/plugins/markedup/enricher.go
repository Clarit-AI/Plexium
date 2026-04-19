package markedup

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Clarit-AI/markedup/enrich"
	"github.com/Clarit-AI/markedup/markdown"
	"github.com/Clarit-AI/markedup/schema"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// EnricherVersion is recorded in manifest.PageEntry.EnrichedBy.
const EnricherVersion = "markedup-v1"

// LLMProvider is the minimal contract the EnricherPlugin needs from an
// assistive LLM provider in order to run Tier 2 (model-based) enrichment.
//
// ExtractGraph receives the raw page body (markdown without frontmatter)
// and returns a structured *enrich.ModelResult plus an optional natural
// language summary. The returned ModelResult is fed into
// enrich.EnrichPageWithModel, which merges it on top of the Tier 1
// frontmatter using the same MergeOptions semantics.
//
// Implementations are expected to be safe for concurrent use and to
// honor ctx cancellation. Returning (nil, "", nil) is treated as
// "model declined to extract anything" — the enricher will fall back to
// Tier 1 only without surfacing an error.
//
// The interface is intentionally narrow so that any assistive provider
// (Plexium's ProviderCascade today, a future direct ModelExtractor, or a
// test fake) can satisfy it via a thin adapter.
type LLMProvider interface {
	ExtractGraph(ctx context.Context, body string) (*enrich.ModelResult, string, error)
}

// EnricherPlugin runs MarkedUp enrichment after the convert pipeline has
// written wiki pages. Tier 1 (deterministic) extraction produces entities,
// relationships, semantic hints, etc. from file structure; the resulting
// graph metadata is written to the Plexium manifest. When the
// WriteEnrichedFrontmatter config flag is true, the enriched frontmatter
// is also written back into the .wiki/ files.
//
// Tier 2 (LLM-based) extraction runs when cfg.ModelEnrich is true AND an
// LLMProvider has been attached via NewEnricherWithLLM. The bootstrap layer
// owns this wiring: it builds a CascadeLLMProvider over the assistive
// agent's ProviderCascade and passes it in here. When ModelEnrich is on
// but the provider is nil (no usable assistive cascade configured), the
// enricher logs a warning at Process time and continues with Tier 1 only
// — keeping convert/daemon usable instead of failing the whole pipeline.
type EnricherPlugin struct {
	cfg Config
	// llm is optional. Required when cfg.ModelEnrich is true; a nil
	// llm with ModelEnrich=true triggers the warn-and-fallback path
	// described above.
	llm LLMProvider

	// missingLLMWarnOnce guards the "ModelEnrich=true but no LLM
	// provider attached" warning so it logs at most once per process
	// lifetime. The daemon calls Process every refresh interval; on
	// large repos with modelEnrich:true and no provider, a per-page
	// warning would flood logs. Once-per-EnricherPlugin keeps a single
	// signal visible without spam.
	missingLLMWarnOnce sync.Once

	// warnLogger is the destination for the missing-LLM warning. Tests
	// inject a buffered logger to assert the warning fires exactly once
	// across multiple Process calls; production leaves it nil and falls
	// back to the package-level log.Printf.
	warnLogger *log.Logger

	// touchLastEnrichedOnNoOp, when true, forces a manifest write that
	// bumps LastEnriched even when the semantic graph fields are
	// unchanged. This breaks an infinite-requeue loop in the daemon:
	// without it, a stale page whose enrichment produces identical
	// graph content would skip the manifest save, leave LastEnriched
	// unchanged, and be re-detected as stale on every subsequent tick.
	//
	// Convert-time callers leave this false to preserve the Phase C
	// idempotency optimization (no disk write when nothing changed);
	// only the daemon flips it on, since only the daemon owns the
	// "treat unchanged-but-old as freshly checked" semantics.
	touchLastEnrichedOnNoOp bool
}

// SetDaemonMode toggles the "touch LastEnriched on a no-op enrichment"
// behavior. The daemon calls this after construction so that pages whose
// graph content is unchanged still advance their LastEnriched timestamp,
// preventing the daemon's stale-page detector from queuing the same page
// on every tick forever.
//
// Convert and other one-shot callers must NOT set this — they should
// keep the Phase C idempotency optimization (no manifest write when the
// graph content matches what's already stored).
func (p *EnricherPlugin) SetDaemonMode(on bool) {
	if p == nil {
		return
	}
	p.touchLastEnrichedOnNoOp = on
}

// NewEnricher constructs an EnricherPlugin from a parsed Config.
//
// The returned plugin runs Tier 1 only. To enable Tier 2 (model-based)
// enrichment, set cfg.ModelEnrich = true AND attach an LLMProvider via
// NewEnricherWithLLM. NewEnricher with cfg.ModelEnrich=true is a valid
// construction — the enricher will log a warning at Process time and
// continue with Tier 1 only — so the bootstrap layer can wire an
// LLM lazily without coupling Config parsing to provider availability.
func NewEnricher(cfg Config) *EnricherPlugin {
	return &EnricherPlugin{cfg: cfg}
}

// NewEnricherWithLLM constructs an EnricherPlugin with an attached LLM
// provider for Tier 2 enrichment. Pass a nil llm to opt out of Tier 2
// (equivalent to NewEnricher).
func NewEnricherWithLLM(cfg Config, llm LLMProvider) *EnricherPlugin {
	return &EnricherPlugin{cfg: cfg, llm: llm}
}

// warnMissingLLMOnce emits the "ModelEnrich=true but no LLM provider
// attached" warning at most once per EnricherPlugin instance. It uses
// p.warnLogger when set (tests) and falls back to the package log
// otherwise. The page-path is intentionally omitted from the once-only
// message because the warning concerns plugin configuration, not any
// individual page — operators tail logs for "missing provider", not
// for one specific WikiPath.
func (p *EnricherPlugin) warnMissingLLMOnce() {
	p.missingLLMWarnOnce.Do(func() {
		const msg = "markedup: ModelEnrich=true but no LLM provider attached; falling back to Tier 1 (this warning is logged once per process)"
		if p.warnLogger != nil {
			p.warnLogger.Print(msg)
			return
		}
		log.Print(msg)
	})
}

// HasLLMProvider reports whether the enricher has a non-nil Tier 2 LLM
// attached. Exposed for bootstrap-layer tests that need to assert the
// caller's cascade reached the plugin without poking at unexported fields.
func (p *EnricherPlugin) HasLLMProvider() bool {
	return p != nil && p.llm != nil
}

// Plugin interface

func (p *EnricherPlugin) Name() string                 { return "markedup-enricher" }
func (p *EnricherPlugin) Version() string              { return EnricherVersion }
func (p *EnricherPlugin) Description() string          { return "MarkedUp knowledge-graph enrichment" }
func (p *EnricherPlugin) Type() plugins.PluginType     { return plugins.PluginTypePipeline }
func (p *EnricherPlugin) Stage() plugins.PipelineStage { return plugins.StageAfterWrite }

// Process runs enrichment over every page in the pipeline data and
// applies the resulting graph metadata to .plexium/manifest.json.
//
// When p.cfg.Enabled is false the call is a no-op. When no enrichment
// tier is enabled (AutoEnrich=false and ModelEnrich=false) the call is
// also a no-op — the plugin honors the user's disabled setting even if
// the plugin itself is registered.
func (p *EnricherPlugin) Process(ctx context.Context, data *plugins.PipelineData) error {
	if !p.cfg.Enabled {
		return nil
	}
	if !p.cfg.AutoEnrich && !p.cfg.ModelEnrich {
		return nil
	}

	mgr, err := manifest.NewManager(manifest.DefaultPath(data.RepoRoot))
	if err != nil {
		return fmt.Errorf("markedup: manifest manager: %w", err)
	}
	m, err := mgr.Load()
	if err != nil {
		return fmt.Errorf("markedup: load manifest: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	applied := 0
	// touched counts pages whose only change is a daemon-mode
	// LastEnriched bump (no semantic graph change). It's tracked
	// separately from applied so we can decide to save the manifest
	// even when zero pages had real enrichment changes.
	touched := 0

	// Buffer frontmatter-write tasks rather than flushing per-page. If we
	// wrote files during the loop and then hit an error on a later page,
	// the manifest save would be skipped and the .wiki/ files would carry
	// enriched frontmatter with no corresponding manifest record — the
	// two sources of truth would drift. Instead we commit the manifest
	// first; only if that succeeds do we flush the buffered writes.
	// pendingWrite holds the pre-rendered file bytes for a deferred
	// frontmatter write. We render before the manifest save so any
	// template/YAML errors surface before mutating any state, and flush
	// only after the manifest save confirms the enrichment committed.
	type pendingWrite struct {
		filePath string
		wikiPath string
		content  []byte // rendered file content ready for os.WriteFile
	}
	var pending []pendingWrite

	for _, page := range data.Pages {
		if err := ctx.Err(); err != nil {
			return err
		}

		filePath := filepath.Join(data.WikiRoot, page.WikiPath)
		raw, rerr := os.ReadFile(filePath)
		if rerr != nil {
			// Page may not exist on disk (dry-run, write failure upstream).
			// Skip silently — there's nothing to enrich.
			continue
		}

		parsed, perr := markdown.ParseBytesPermissive(raw)
		if perr != nil {
			return fmt.Errorf("markedup: parse %s: %w", page.WikiPath, perr)
		}
		parsed.SourcePath = filePath

		var enriched *schema.Page
		var delta enrich.EnrichmentDelta
		hasEnrichment := false

		if p.cfg.AutoEnrich {
			enriched, delta = enrich.EnrichPage(parsed, filePath, data.WikiRoot, enrich.MergeOptions{})
			hasEnrichment = delta.Changed
		} else {
			enriched = parsed
		}

		// Tier 2 (ModelEnrich): when enabled AND a provider is wired,
		// call the LLM to extract a richer ModelResult and merge it on
		// top of whatever Tier 1 produced. When the flag is enabled but
		// no provider is attached, log a single warning and continue
		// with Tier 1 only — this lets users toggle modelEnrich in
		// config.yml without a hard failure when their assistive agent
		// is misconfigured or temporarily unavailable.
		if p.cfg.ModelEnrich {
			if p.llm == nil {
				p.warnMissingLLMOnce()
			} else {
				model, summary, mErr := p.llm.ExtractGraph(ctx, enriched.Body)
				if mErr != nil {
					// Tier 2 failure is non-fatal: a transient model
					// outage should not abort the entire convert pass.
					// Log it and keep whatever Tier 1 already produced.
					log.Printf("markedup: Tier 2 extraction failed for %s: %v", page.WikiPath, mErr)
				} else if model != nil {
					modelEnriched, modelDelta := enrich.EnrichPageWithModel(enriched, model, summary, enrich.MergeOptions{})
					enriched = modelEnriched
					if modelDelta.Changed {
						hasEnrichment = true
					}
				}
			}
		}

		// Nothing changed in either tier — skip the apply so we don't
		// re-stamp LastEnriched with identical frontmatter every pass.
		// In daemon mode we still need to bump LastEnriched so the
		// daemon's stale-page detector doesn't requeue this page on
		// every tick; do that via the same touch path used below.
		if !hasEnrichment {
			if p.touchLastEnrichedOnNoOp {
				if touchPageLastEnriched(m, page.WikiPath, now) {
					touched++
				}
			}
			continue
		}

		newMeta := toGraphMetadata(enriched.Frontmatter, now)

		existingMeta, tracked := m.GraphMetadataForPage(page.WikiPath)
		if !tracked {
			// Page flowed through the pipeline but isn't tracked in the
			// manifest (stale PipelineData entry, path-normalization
			// mismatch, or an unmanaged page). Skip — we must not
			// invent a manifest entry here.
			continue
		}
		if manifest.GraphMetadataSemanticEqual(existingMeta, newMeta) {
			// Idempotent run: the enrichment produced the same graph
			// content the manifest already has. Skip the apply so we
			// don't re-stamp LastEnriched or trigger a manifest save —
			// unless we're in daemon mode, in which case we MUST bump
			// LastEnriched to break the daemon's infinite-requeue loop
			// (semantic-equal but past TTL → enricher runs → no save →
			// still past TTL on the next tick).
			if p.touchLastEnrichedOnNoOp {
				if touchPageLastEnriched(m, page.WikiPath, now) {
					touched++
				}
			}
			continue
		}
		// Safe to assume ApplyGraphMetadata returns true here — we
		// already confirmed the page is tracked via GraphMetadataForPage.
		m.ApplyGraphMetadata(page.WikiPath, newMeta)
		applied++

		if p.cfg.WriteEnrichedFrontmatter {
			// Pre-build the content now while we still have `raw` and
			// the merged frontmatter in hand, but defer the actual
			// disk write until after the manifest commit succeeds.
			newContent, werr := markdown.ReplaceFrontmatter(&enriched.Frontmatter, raw)
			if werr != nil {
				return fmt.Errorf("markedup: build frontmatter for %s: %w", page.WikiPath, werr)
			}
			pending = append(pending, pendingWrite{
				filePath: filePath,
				wikiPath: page.WikiPath,
				content:  newContent,
			})
		}
	}

	if applied == 0 && touched == 0 {
		return nil
	}
	if err := mgr.Save(m); err != nil {
		return fmt.Errorf("markedup: save manifest: %w", err)
	}
	for _, w := range pending {
		if err := markdown.WriteFrontmatterFile(w.filePath, w.content); err != nil {
			// The manifest is already saved; a single frontmatter write
			// failure here creates a manifest-has-data / disk-missing
			// asymmetry for this one file. Surface the error so the
			// operator can retry the pipeline pass (EnrichPage is
			// idempotent — a second run will re-attempt the write).
			return fmt.Errorf("markedup: write frontmatter for %s: %w", w.wikiPath, err)
		}
	}
	return nil
}

// touchPageLastEnriched bumps the LastEnriched (and EnrichedBy) field on
// the manifest entry for wikiPath without touching any of the semantic
// graph fields. Used by the daemon-mode no-op path to advance the stale
// detector's TTL clock so the same page isn't requeued every tick.
//
// Returns true when the page was found and the timestamp was actually
// updated (an existing identical timestamp is a no-op and returns
// false to avoid an unnecessary manifest save). When the page isn't
// tracked in the manifest, returns false — same contract as
// ApplyGraphMetadata.
func touchPageLastEnriched(m *manifest.Manifest, wikiPath, now string) bool {
	for i := range m.Pages {
		if m.Pages[i].WikiPath != wikiPath {
			continue
		}
		if m.Pages[i].LastEnriched == now && m.Pages[i].EnrichedBy == EnricherVersion {
			return false
		}
		m.Pages[i].LastEnriched = now
		m.Pages[i].EnrichedBy = EnricherVersion
		return true
	}
	return false
}

// toGraphMetadata converts markedup's GraphFrontmatter into the narrower
// manifest.GraphMetadata shape. The manifest intentionally stores a subset
// — it records "what did enrichment produce" for retrieval and display,
// not the entire knowledge-graph schema.
func toGraphMetadata(fm schema.GraphFrontmatter, timestamp string) manifest.GraphMetadata {
	entities := make([]manifest.EntityRef, 0, len(fm.Entities))
	for _, e := range fm.Entities {
		entities = append(entities, manifest.EntityRef{Name: e.Name, Role: e.Role})
	}

	rels := make([]manifest.RelationshipRef, 0, len(fm.Relationships))
	for _, r := range fm.Relationships {
		rels = append(rels, manifest.RelationshipRef{
			Target:   r.Target,
			Type:     r.Type,
			Strength: r.Strength,
		})
	}

	hints := make([]string, len(fm.SemanticHints))
	copy(hints, fm.SemanticHints)

	return manifest.GraphMetadata{
		EntityType:    fm.EntityType,
		Entities:      entities,
		Relationships: rels,
		Confidence:    fm.Confidence,
		SemanticHints: hints,
		LastEnriched:  timestamp,
		EnrichedBy:    EnricherVersion,
	}
}
