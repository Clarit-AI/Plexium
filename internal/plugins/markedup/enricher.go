package markedup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KHAEntertainment/markedup/enrich"
	"github.com/KHAEntertainment/markedup/markdown"
	"github.com/KHAEntertainment/markedup/schema"

	"github.com/Clarit-AI/Plexium/internal/manifest"
	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// EnricherVersion is recorded in manifest.PageEntry.EnrichedBy.
const EnricherVersion = "markedup-v1"

// EnricherPlugin runs MarkedUp enrichment after the convert pipeline has
// written wiki pages. Tier 1 (deterministic) extraction produces entities,
// relationships, semantic hints, etc. from file structure; the resulting
// graph metadata is written to the Plexium manifest. When the
// WriteEnrichedFrontmatter config flag is true, the enriched frontmatter
// is also written back into the .wiki/ files.
//
// Tier 2 (LLM-based) extraction is planned but not yet implemented in this
// plugin; callers can enable it via the ModelEnrich config flag once the
// assistive-agent wiring lands.
type EnricherPlugin struct {
	cfg Config
}

// NewEnricher constructs an EnricherPlugin from a parsed Config.
func NewEnricher(cfg Config) *EnricherPlugin {
	return &EnricherPlugin{cfg: cfg}
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

		// Tier 2 (ModelEnrich) requires assistive-agent wiring that has
		// not landed yet. Until it does, a run with AutoEnrich=false and
		// ModelEnrich=true has nothing real to write — skip silently
		// rather than stamping LastEnriched with identical frontmatter
		// every pipeline pass. A follow-up PR will add the actual Tier 2
		// pathway and flip this to write when the model produces new
		// entities/summary/hints.
		if !hasEnrichment {
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
			// don't re-stamp LastEnriched or trigger a manifest save.
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

	if applied == 0 {
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
