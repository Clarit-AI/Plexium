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
// The bootstrap deliberately does NOT take an LLM provider or embedder
// argument yet; tickets D2 (Tier 2 enrichment) and D3 (semantic search
// embedder) will extend the signature once those surfaces are designed.
// The TODO markers below name the owning ticket so the next worker can
// find them quickly.
package bootstrap

import (
	"context"
	"fmt"
	"log"

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

	// TODO(D3): add Embedder field for semantic search wiring.
}

// BuildRegistry constructs a plugin Registry populated with every built-in
// plugin enabled in cfg. It always returns a non-nil registry; an empty
// configuration simply yields an empty registry.
//
// Today this only handles the markedup plugin pair (enricher + retrieval).
// As more built-in plugins land, register them here so a single call sets
// up the full surface for convert / pageindex / daemon.
//
// TODO(D3): accept an embedder factory arg and pass it to
// markedup.NewRetrieval once semantic search is wired.
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
			if err := reg.Register(markedup.NewRetrieval(mcfg)); err != nil {
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
