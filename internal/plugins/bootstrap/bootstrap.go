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

	// RateTracker, when non-nil, is used by the markedup LLM adapter
	// to record per-provider token usage and cost after every Tier 2
	// extraction call so convert-time enrichment is budgeted against
	// the same daily-spend ledger the daemon and `agent spend` use.
	// A nil tracker disables budget recording (calls still succeed)
	// — useful for tests and short-lived one-off runs.
	RateTracker *agent.RateLimitTracker

	// EmbeddingProvider, when non-nil, lets a caller supply a fully
	// constructed markedup embedder. Most callers should leave this
	// nil and let BuildRegistry build one from cfg.AssistiveAgent
	// (the "inherit" path); this hook exists for tests and for
	// future explicit-provider configurations.
	EmbeddingProvider muembed.Embedder

	// DaemonMode signals that this registry is being built for the
	// daemon's periodic re-enrichment path rather than a one-shot
	// convert. When true, the markedup enricher bumps LastEnriched on
	// pages whose graph content is unchanged, preventing the daemon's
	// stale-page detector from queuing the same page on every tick
	// (semantic-equal but past TTL → enricher runs → no manifest save
	// → still past TTL on the next tick — an infinite requeue loop).
	//
	// Convert callers leave this false to preserve the Phase C
	// idempotency optimization (no manifest write when the graph
	// content matches what's already stored).
	DaemonMode bool

	// APIKeyResolver resolves provider API keys by env-var name.
	// Callers should pass the same resolver used to build the
	// assistive cascade (e.g. cmd/plexium's loadAPIKey, which checks
	// .plexium/credentials.json before falling back to env) so that
	// inherited embeddings share credentials with the LLM cascade.
	// When nil, BuildRegistry falls back to os.Getenv — preserving
	// the legacy behavior for callers (e.g. tests) that don't care
	// about the credentials store.
	APIKeyResolver func(envName string) string
}

// resolveAPIKey returns the API key for envName, using the caller's
// APIKeyResolver when supplied and falling back to os.Getenv otherwise.
//
// The resolver is consulted FIRST, even when envName is empty. This is
// deliberate: callers like cmd/plexium's loadAPIKey can resolve a key
// from .plexium/credentials.json without an env-var name (the credentials
// store is keyed by provider, not by env). Short-circuiting on empty
// envName before invoking the resolver would skip that path entirely
// and silently break embeddings/Tier 2 for credentials-store users.
//
// When no resolver is supplied, an empty envName returns "" (os.Getenv("")
// would return "" too, but this avoids the syscall).
func (o Options) resolveAPIKey(envName string) string {
	if o.APIKeyResolver != nil {
		return o.APIKeyResolver(envName)
	}
	if envName == "" {
		return ""
	}
	return os.Getenv(envName)
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
			enricher := buildMarkedupEnricher(mcfg, resolved.Cascade, resolved.RateTracker)
			enricher.SetDaemonMode(resolved.DaemonMode)
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
func buildMarkedupEnricher(mcfg markedup.Config, cascade *agent.ProviderCascade, rateTracker *agent.RateLimitTracker) *markedup.EnricherPlugin {
	if !mcfg.ModelEnrich {
		return markedup.NewEnricher(mcfg)
	}
	llm := markedup.NewCascadeLLMProvider(cascade, rateTracker)
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
		embedder = buildInheritedEmbedder(mcfg, cfg, opts)
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
func buildInheritedEmbedder(mcfg markedup.Config, cfg *config.Config, opts Options) muembed.Embedder {
	if mcfg.Embeddings.Provider != "" && mcfg.Embeddings.Provider != "inherit" {
		// Explicit provider: build directly from mcfg.Embeddings.
		// muembed.NewFromProvider only speaks the OpenAI-compatible
		// /v1/embeddings protocol — guard the same way the inherit
		// branch does so that a user-supplied embeddings.provider of
		// e.g. "ollama" doesn't pass Validate() and then silently
		// produce broken embeddings at query time.
		if !isOpenAICompatibleEmbeddingProvider(mcfg.Embeddings.Provider) {
			log.Printf("plugins.markedup.embeddings: provider %q is not OpenAI-compatible; semantic search disabled", mcfg.Embeddings.Provider)
			return nil
		}
		// API key resolution uses the caller-supplied resolver so the
		// explicit-provider path also picks up keys written by
		// `plexium agent setup` into .plexium/credentials.json.
		apiKey := opts.resolveAPIKey(mcfg.Embeddings.APIKeyEnv)
		return muembed.NewFromProvider(
			mcfg.Embeddings.Endpoint,
			mcfg.Embeddings.Model,
			apiKey,
			mcfg.Embeddings.Dims,
		)
	}

	if opts.Cascade == nil || !opts.Cascade.HasProviders() {
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
		// muembed.NewFromProvider speaks the OpenAI-compatible
		// /v1/embeddings protocol. Skip provider types whose APIs
		// don't expose that surface (e.g. ollama's /api/generate,
		// anthropic's /v1/messages) — wiring them would silently
		// produce broken embeddings discovered only at query time.
		if !isOpenAICompatibleEmbeddingProvider(pc.Type) {
			continue
		}
		model := mcfg.Embeddings.Model
		if model == "" {
			// pc.Model is the chat/completion model; using it as an
			// embedding model would either be rejected by the provider
			// or — worse — silently return wrong-shape vectors at query
			// time. Refuse to wire and tell the operator exactly what
			// to set.
			log.Printf("plugins.markedup.embeddings: enabled in inherit mode but embeddings.model is empty; semantic search disabled (set plugins.markedup.embeddings.model to an embedding model such as text-embedding-3-small)")
			return nil
		}
		// Use the shared resolver so inherited embeddings see the
		// same key the cascade does (env OR
		// .plexium/credentials.json) — without it, a user who set
		// their key via `plexium agent setup` would have a working
		// Tier 2 LLM but a broken embedder.
		apiKey := opts.resolveAPIKey(pc.APIKeyEnv)
		return muembed.NewFromProvider(pc.Endpoint, model, apiKey, mcfg.Embeddings.Dims)
	}
	log.Printf("plugins.markedup.embeddings: no usable assistive provider found; semantic search disabled")
	return nil
}

// isOpenAICompatibleEmbeddingProvider reports whether a provider type
// string can be wired through muembed.NewFromProvider, which only
// speaks the OpenAI /v1/embeddings protocol. Centralizing the check
// keeps the explicit-provider branch and the cascade-inherit loop in
// agreement; adding a new compatible type only needs editing this one
// helper.
func isOpenAICompatibleEmbeddingProvider(provider string) bool {
	switch provider {
	case "openai", "openai-compatible":
		return true
	default:
		return false
	}
}
