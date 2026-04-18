// Package bootstrap constructs the runtime plugin Registry from a loaded
// *config.Config. It lives in its own package (not directly under
// internal/plugins) because each built-in plugin sub-package — markedup,
// future adapters, etc. — already imports internal/plugins for the
// Plugin/Registry types. Putting BuildRegistry inside internal/plugins
// would create an import cycle the moment we register the first such
// built-in here.
//
// The convert command (and any other surface that needs the registry)
// calls bootstrap.BuildRegistry once at startup.
//
// The bootstrap takes optional collaborators (LLM cascade, embedder
// factory) via the Options struct so callers that don't need a given
// capability can omit it without changing the call site.
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	mucache "github.com/Clarit-AI/markedup/cache"
	muembed "github.com/Clarit-AI/markedup/embed"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
)

// Options carries the optional collaborators BuildRegistry can inject
// into built-in plugins. All fields are optional; nil values mean the
// caller doesn't need that capability and any plugin that requires it
// will gracefully degrade (typically by skipping its advanced tier and
// logging a warning at first use).
//
// Splitting these out keeps BuildRegistry's signature stable as new
// plugin classes land — the convert command and tests can opt in to
// each capability independently.
type Options struct {
	// Cascade is the assistive-agent LLM provider cascade. When set
	// AND the markedup plugin has modelEnrich: true, the enricher
	// receives an LLMProvider that delegates to this cascade.
	Cascade *agent.ProviderCascade

	// EmbeddingProvider, when non-nil, lets a caller supply a fully
	// constructed markedup embedder. Most callers should leave this
	// nil and let BuildRegistry build one from cfg.AssistiveAgent
	// (the "inherit" path); this hook exists for tests and for
	// future explicit-provider configurations.
	EmbeddingProvider muembed.Embedder
}

// BuildRegistry constructs a plugin Registry populated with every built-in
// plugin enabled in cfg. It always returns a non-nil registry; an empty
// configuration simply yields an empty registry.
//
// Today this only handles the markedup plugin pair (enricher + retrieval).
// As more built-in plugins land, register them here so a single call sets
// up the full surface for convert / pageindex / daemon.
func BuildRegistry(ctx context.Context, repoRoot string, cfg *config.Config, opts ...Options) (*plugins.Registry, error) {
	reg := plugins.NewRegistry()
	if cfg == nil {
		return reg, nil
	}

	var resolved Options
	if len(opts) > 0 {
		resolved = opts[0]
	}

	// markedup
	rawMarkedup := cfg.PluginSettings("markedup")
	if len(rawMarkedup) > 0 {
		mcfg, err := markedup.ParseConfig(rawMarkedup)
		if err != nil {
			return nil, fmt.Errorf("plugins.markedup: %w", err)
		}
		if mcfg.Enabled {
			enricher := buildMarkedupEnricher(mcfg, resolved.Cascade)
			if err := reg.Register(enricher); err != nil {
				return nil, fmt.Errorf("register markedup-enricher: %w", err)
			}
			retrieval := buildMarkedupRetrieval(mcfg, repoRoot, cfg, resolved)
			if err := reg.Register(retrieval); err != nil {
				return nil, fmt.Errorf("register markedup-retrieval: %w", err)
			}
		}
	}

	return reg, nil
}

// buildMarkedupEnricher constructs the markedup EnricherPlugin and, when
// the user has opted into Tier 2 (modelEnrich: true), wires the
// assistive cascade through the markedup LLM adapter. When modelEnrich
// is requested but the cascade is nil or has no usable providers, we
// log a warning and return a Tier-1-only enricher so the convert
// pipeline can still run.
func buildMarkedupEnricher(mcfg markedup.Config, cascade *agent.ProviderCascade) *markedup.EnricherPlugin {
	if !mcfg.ModelEnrich {
		return markedup.NewEnricher(mcfg)
	}
	llm := markedup.NewCascadeLLMProvider(cascade)
	if llm == nil {
		log.Printf("plugins.markedup: modelEnrich is enabled but no assistive provider is configured; Tier 2 disabled")
		return markedup.NewEnricher(mcfg)
	}
	return markedup.NewEnricherWithLLM(mcfg, llm)
}

// buildMarkedupRetrieval constructs the markedup RetrievalPlugin and,
// when embeddings are enabled in config, wires an embedder + a vector
// cache rooted at .plexium/knowledge/. The cascade-derived "inherit"
// path reuses the first usable assistive provider's endpoint/model/key
// so the operator doesn't have to maintain a second provider block
// just for embeddings.
//
// Any failure to construct the embedder (no cascade, no usable
// provider, missing endpoint) logs a warning and falls through to a
// keyword-only RetrievalPlugin. Embeddings are an enhancement, never
// a hard dependency for retrieval to work.
func buildMarkedupRetrieval(mcfg markedup.Config, repoRoot string, cfg *config.Config, opts Options) *markedup.RetrievalPlugin {
	if !mcfg.Embeddings.Enabled {
		return markedup.NewRetrieval(mcfg)
	}

	embedder := opts.EmbeddingProvider
	if embedder == nil {
		embedder = buildInheritedEmbedder(mcfg, cfg, opts.Cascade)
	}
	if embedder == nil {
		// Reason already logged by buildInheritedEmbedder when applicable.
		return markedup.NewRetrieval(mcfg)
	}

	// Vector cache lives under .plexium/knowledge/ per Phase C decision.
	cacheDir := filepath.Join(repoRoot, ".plexium", "knowledge")
	vc := mucache.NewVectorCache(cacheDir)

	return markedup.NewRetrievalWithEmbedder(mcfg, embedder, vc)
}

// buildInheritedEmbedder picks the first usable provider out of the
// assistive cascade's config and wraps it via embed.NewFromProvider.
// We read endpoint/model/apiKey from cfg.AssistiveAgent.Providers
// rather than the cascade itself because Provider is intentionally
// opaque about its credentials at the interface level — config is
// the source of truth.
//
// Provider override modes (mcfg.Embeddings.Provider != "inherit")
// are not yet implemented here; ParseConfig.Validate already requires
// explicit endpoint+model in that mode, so a follow-up can read them
// directly off mcfg.Embeddings without touching the cascade.
func buildInheritedEmbedder(mcfg markedup.Config, cfg *config.Config, cascade *agent.ProviderCascade) muembed.Embedder {
	if mcfg.Embeddings.Provider != "" && mcfg.Embeddings.Provider != "inherit" {
		// Explicit provider: build directly from mcfg.Embeddings.
		// APIKeyEnv resolution is deliberately done by the caller for
		// the inherit path (via cfg.AssistiveAgent loadAPIKey at the
		// CLI layer); for an explicit-provider config the env-var
		// lookup is local to this function.
		apiKey := ""
		if mcfg.Embeddings.APIKeyEnv != "" {
			apiKey = os.Getenv(mcfg.Embeddings.APIKeyEnv)
		}
		return muembed.NewFromProvider(
			mcfg.Embeddings.Endpoint,
			mcfg.Embeddings.Model,
			apiKey,
			mcfg.Embeddings.Dims,
		)
	}

	if cascade == nil || !cascade.HasProviders() {
		log.Printf("plugins.markedup.embeddings: enabled but no assistive cascade available; semantic search disabled")
		return nil
	}
	if cfg == nil {
		return nil
	}

	// Pick the first enabled, non-inherit provider with an endpoint.
	for _, pc := range cfg.AssistiveAgent.Providers {
		if !pc.Enabled || pc.Type == "inherit" || pc.Endpoint == "" {
			continue
		}
		model := mcfg.Embeddings.Model
		if model == "" {
			model = pc.Model
		}
		apiKey := ""
		if pc.APIKeyEnv != "" {
			apiKey = os.Getenv(pc.APIKeyEnv)
		}
		return muembed.NewFromProvider(pc.Endpoint, model, apiKey, mcfg.Embeddings.Dims)
	}
	log.Printf("plugins.markedup.embeddings: no usable assistive provider found; semantic search disabled")
	return nil
}
