// Package markedup wires the MarkedUp knowledge-graph library
// (github.com/KHAEntertainment/markedup) into Plexium's plugin system as
// two plugins:
//
//   - EnricherPlugin (PipelinePlugin, StageAfterWrite): loads the wiki as
//     a knowledge graph, runs Tier 1 (deterministic) enrichment on each
//     page, optionally runs Tier 2 (model-based) enrichment, and writes
//     the resulting graph metadata into Plexium's manifest. By default
//     the .wiki/ files are left untouched (manifest-only mode); an opt-in
//     flag permits writing enriched frontmatter back to disk.
//
//   - RetrievalPlugin: exposes semantic search, graph traversal, and a
//     compact graph summary as MCP tools on the PageIndex server.
//
// Configuration flows through the plugins.markedup block in
// .plexium/config.yml. See Config for the full surface.
package markedup

import (
	"fmt"
)

// Config is the parsed form of the plugins.markedup section in
// .plexium/config.yml. All fields are optional; zero values give safe
// defaults (Tier 1 enrichment on, everything else off).
type Config struct {
	Enabled bool `yaml:"enabled"`

	// AutoEnrich runs Tier 1 deterministic extraction during the convert
	// pipeline hook. Defaults to true when the plugin is enabled.
	AutoEnrich bool `yaml:"autoEnrich"`

	// ModelEnrich runs Tier 2 LLM-based extraction using the provider
	// configured on the assistive agent (or an explicit override). When
	// false, only Tier 1 runs.
	ModelEnrich bool `yaml:"modelEnrich"`

	// WriteEnrichedFrontmatter controls whether the enricher writes the
	// merged YAML frontmatter back into the .wiki/ files. When false
	// (default), enrichment data is stored only in .plexium/manifest.json.
	// When true, .wiki/ pages are mutated with the enriched frontmatter
	// in addition to the manifest write.
	WriteEnrichedFrontmatter bool `yaml:"writeEnrichedFrontmatter"`

	// Embeddings configures the vector cache. Disabled by default.
	Embeddings EmbeddingsConfig `yaml:"embeddings"`

	// Reranking configures an optional cross-encoder reranker. Disabled
	// by default.
	Reranking RerankingConfig `yaml:"reranking"`
}

// EmbeddingsConfig controls semantic-search indexing.
type EmbeddingsConfig struct {
	Enabled bool `yaml:"enabled"`

	// Provider selects where the embedder config comes from.
	//   "inherit" - reuse Plexium's assistiveAgent provider (default)
	//   any other string - explicit provider name; requires Model/Endpoint
	Provider string `yaml:"provider"`

	// Model overrides the embedding model. Empty uses the provider's
	// default or the inherited assistive agent's embedding model.
	Model string `yaml:"model"`

	// Endpoint and APIKeyEnv are consulted when Provider != "inherit".
	Endpoint  string `yaml:"endpoint"`
	APIKeyEnv string `yaml:"apiKeyEnv"`

	// Dims is the embedding dimensionality. Defaults to 768 when unset.
	Dims int `yaml:"dims"`
}

// RerankingConfig controls an optional cross-encoder reranker on top of
// semantic search.
type RerankingConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"` // "jina" or "openai-compatible"
	Model     string `yaml:"model"`
	Endpoint  string `yaml:"endpoint"`
	APIKeyEnv string `yaml:"apiKeyEnv"`
}

// DefaultConfig returns a Config with the recommended defaults applied.
// This is the Config that results from an empty plugins.markedup block.
func DefaultConfig() Config {
	return Config{
		Enabled:                  true,
		AutoEnrich:               true,
		ModelEnrich:              false,
		WriteEnrichedFrontmatter: false,
		Embeddings: EmbeddingsConfig{
			Enabled:  false,
			Provider: "inherit",
			Dims:     768,
		},
		Reranking: RerankingConfig{
			Enabled:  false,
			Provider: "jina",
		},
	}
}

// ParseConfig reads the plugins.markedup block from the raw map form used
// by config.Config.PluginSettings. It applies defaults for missing fields
// and validates internal consistency.
//
// A nil/empty map yields DefaultConfig() with Enabled=false (the plugin
// does nothing unless explicitly enabled in the user's config).
func ParseConfig(raw map[string]any) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		cfg.Enabled = false
		return cfg, nil
	}

	if v, ok := raw["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := raw["autoEnrich"].(bool); ok {
		cfg.AutoEnrich = v
	}
	if v, ok := raw["modelEnrich"].(bool); ok {
		cfg.ModelEnrich = v
	}
	if v, ok := raw["writeEnrichedFrontmatter"].(bool); ok {
		cfg.WriteEnrichedFrontmatter = v
	}

	if emb, ok := raw["embeddings"].(map[string]any); ok {
		if v, ok := emb["enabled"].(bool); ok {
			cfg.Embeddings.Enabled = v
		}
		if v, ok := emb["provider"].(string); ok && v != "" {
			cfg.Embeddings.Provider = v
		}
		if v, ok := emb["model"].(string); ok {
			cfg.Embeddings.Model = v
		}
		if v, ok := emb["endpoint"].(string); ok {
			cfg.Embeddings.Endpoint = v
		}
		if v, ok := emb["apiKeyEnv"].(string); ok {
			cfg.Embeddings.APIKeyEnv = v
		}
		if v, ok := emb["dims"].(int); ok && v > 0 {
			cfg.Embeddings.Dims = v
		}
	}

	if rr, ok := raw["reranking"].(map[string]any); ok {
		if v, ok := rr["enabled"].(bool); ok {
			cfg.Reranking.Enabled = v
		}
		if v, ok := rr["provider"].(string); ok && v != "" {
			cfg.Reranking.Provider = v
		}
		if v, ok := rr["model"].(string); ok {
			cfg.Reranking.Model = v
		}
		if v, ok := rr["endpoint"].(string); ok {
			cfg.Reranking.Endpoint = v
		}
		if v, ok := rr["apiKeyEnv"].(string); ok {
			cfg.Reranking.APIKeyEnv = v
		}
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks for internally-inconsistent settings.
func (c Config) Validate() error {
	if c.Embeddings.Enabled && c.Embeddings.Provider != "inherit" {
		if c.Embeddings.Endpoint == "" {
			return fmt.Errorf("markedup.embeddings: non-inherit provider requires endpoint")
		}
		if c.Embeddings.Model == "" {
			return fmt.Errorf("markedup.embeddings: non-inherit provider requires model")
		}
	}
	if c.Reranking.Enabled && c.Reranking.Model == "" {
		return fmt.Errorf("markedup.reranking: model is required when reranking is enabled")
	}
	return nil
}
