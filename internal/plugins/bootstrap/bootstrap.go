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

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
)

// BuildRegistry constructs a plugin Registry populated with every built-in
// plugin enabled in cfg. It always returns a non-nil registry; an empty
// configuration simply yields an empty registry.
//
// Today this only handles the markedup plugin pair (enricher + retrieval).
// As more built-in plugins land, register them here so a single call sets
// up the full surface for convert / pageindex / daemon.
//
// TODO(D2): accept an assistive LLM provider arg and pass it to
// markedup.NewEnricher once Tier 2 enrichment is wired.
// TODO(D3): accept an embedder factory arg and pass it to
// markedup.NewRetrieval once semantic search is wired.
func BuildRegistry(ctx context.Context, repoRoot string, cfg *config.Config) (*plugins.Registry, error) {
	reg := plugins.NewRegistry()
	if cfg == nil {
		return reg, nil
	}

	// markedup
	rawMarkedup := cfg.PluginSettings("markedup")
	if len(rawMarkedup) > 0 {
		mcfg, err := markedup.ParseConfig(rawMarkedup)
		if err != nil {
			return nil, fmt.Errorf("plugins.markedup: %w", err)
		}
		if mcfg.Enabled {
			if err := reg.Register(markedup.NewEnricher(mcfg)); err != nil {
				return nil, fmt.Errorf("register markedup-enricher: %w", err)
			}
			if err := reg.Register(markedup.NewRetrieval(mcfg)); err != nil {
				return nil, fmt.Errorf("register markedup-retrieval: %w", err)
			}
		}
	}

	return reg, nil
}
