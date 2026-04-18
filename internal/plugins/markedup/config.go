// Package markedup wires the MarkedUp knowledge-graph library
// (github.com/Clarit-AI/markedup) into Plexium's plugin system as
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
	"time"
)

// DefaultRefreshInterval is the fallback TTL for the daemon's
// re-enrichment watch when plugins.markedup.daemon.refreshInterval
// is unset or unparseable.
const DefaultRefreshInterval = 24 * time.Hour

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

	// Daemon controls the daemon's periodic re-enrichment behavior.
	Daemon DaemonConfig `yaml:"daemon"`
}

// DaemonConfig controls how the Plexium daemon periodically refreshes
// markedup enrichment for tracked manifest entries.
type DaemonConfig struct {
	// RefreshInterval is the TTL after which a manifest entry's
	// LastEnriched timestamp is considered stale and a re-enrichment
	// job is queued. Parsed via time.ParseDuration. Defaults to
	// DefaultRefreshInterval (24h) when unset or unparseable.
	RefreshInterval string `yaml:"refreshInterval"`
}

// RefreshIntervalDuration returns the parsed RefreshInterval, falling
// back to DefaultRefreshInterval when the value is empty or invalid.
// Centralizing the parse here keeps the daemon caller free of any
// markedup-internal knowledge of the YAML field's string shape.
func (d DaemonConfig) RefreshIntervalDuration() time.Duration {
	if d.RefreshInterval == "" {
		return DefaultRefreshInterval
	}
	parsed, err := time.ParseDuration(d.RefreshInterval)
	if err != nil || parsed <= 0 {
		return DefaultRefreshInterval
	}
	return parsed
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
// ParseConfig starts from these defaults when plugins.markedup contains
// at least one recognized key AND explicitly sets enabled: true; an
// omitted, empty, or enabled-unset block leaves the plugin disabled.
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
		Daemon: DaemonConfig{
			RefreshInterval: "",
		},
	}
}

// allowedTopLevelKeys names every key ParseConfig will accept at the
// root of the plugins.markedup block. Anything else is a config error.
var allowedTopLevelKeys = map[string]struct{}{
	"enabled":                  {},
	"autoEnrich":               {},
	"modelEnrich":              {},
	"writeEnrichedFrontmatter": {},
	"embeddings":               {},
	"reranking":                {},
	"daemon":                   {},
}

// allowedDaemonKeys constrains the nested daemon block (see allowed*Keys
// for the rationale).
var allowedDaemonKeys = map[string]struct{}{
	"refreshInterval": {},
}

// allowedEmbeddingsKeys and allowedRerankingKeys constrain the nested
// blocks. Typos like `enabeld: true` should fail parsing, not be silently
// accepted and fall back to defaults.
var allowedEmbeddingsKeys = map[string]struct{}{
	"enabled":   {},
	"provider":  {},
	"model":     {},
	"endpoint":  {},
	"apiKeyEnv": {},
	"dims":      {},
}

var allowedRerankingKeys = map[string]struct{}{
	"enabled":   {},
	"provider":  {},
	"model":     {},
	"endpoint":  {},
	"apiKeyEnv": {},
}

// ParseConfig reads the plugins.markedup block from the raw map form used
// by config.Config.PluginSettings. It applies defaults on top of the
// provided values, rejects unknown keys and malformed types, and runs
// Validate() on the result.
//
// Enablement rule:
//   - The plugin is OFF unless raw["enabled"] is explicitly true. A nil,
//     empty, or enabled-unset block all produce a disabled plugin. This
//     prevents typos like `enabeld: true` from silently producing an
//     enabled plugin with default settings.
//   - An unknown key, a malformed nested block, or an invalid value
//     (e.g. non-positive embeddings.dims) returns an error.
func ParseConfig(raw map[string]any) (Config, error) {
	cfg := DefaultConfig()
	cfg.Enabled = false // strict: only an explicit `enabled: true` turns it on
	if len(raw) == 0 {
		return cfg, nil
	}

	if err := rejectUnknownKeys("markedup", raw, allowedTopLevelKeys); err != nil {
		return Config{}, err
	}

	if v, ok := raw["enabled"].(bool); ok {
		cfg.Enabled = v
	} else if _, present := raw["enabled"]; present {
		return Config{}, fmt.Errorf("markedup.enabled must be a boolean")
	}
	if v, ok := raw["autoEnrich"].(bool); ok {
		cfg.AutoEnrich = v
	} else if _, present := raw["autoEnrich"]; present {
		return Config{}, fmt.Errorf("markedup.autoEnrich must be a boolean")
	}
	if v, ok := raw["modelEnrich"].(bool); ok {
		cfg.ModelEnrich = v
	} else if _, present := raw["modelEnrich"]; present {
		return Config{}, fmt.Errorf("markedup.modelEnrich must be a boolean")
	}
	if v, ok := raw["writeEnrichedFrontmatter"].(bool); ok {
		cfg.WriteEnrichedFrontmatter = v
	} else if _, present := raw["writeEnrichedFrontmatter"]; present {
		return Config{}, fmt.Errorf("markedup.writeEnrichedFrontmatter must be a boolean")
	}

	if embRaw, present := raw["embeddings"]; present {
		emb, ok := embRaw.(map[string]any)
		if !ok {
			return Config{}, fmt.Errorf("markedup.embeddings must be a map")
		}
		if err := rejectUnknownKeys("markedup.embeddings", emb, allowedEmbeddingsKeys); err != nil {
			return Config{}, err
		}
		if v, ok := emb["enabled"].(bool); ok {
			cfg.Embeddings.Enabled = v
		} else if _, p := emb["enabled"]; p {
			return Config{}, fmt.Errorf("markedup.embeddings.enabled must be a boolean")
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
		if dimsRaw, p := emb["dims"]; p {
			n, ok := toInt(dimsRaw)
			if !ok || n <= 0 {
				return Config{}, fmt.Errorf("markedup.embeddings.dims must be a positive integer")
			}
			cfg.Embeddings.Dims = n
		}
	}

	if rrRaw, present := raw["reranking"]; present {
		rr, ok := rrRaw.(map[string]any)
		if !ok {
			return Config{}, fmt.Errorf("markedup.reranking must be a map")
		}
		if err := rejectUnknownKeys("markedup.reranking", rr, allowedRerankingKeys); err != nil {
			return Config{}, err
		}
		if v, ok := rr["enabled"].(bool); ok {
			cfg.Reranking.Enabled = v
		} else if _, p := rr["enabled"]; p {
			return Config{}, fmt.Errorf("markedup.reranking.enabled must be a boolean")
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

	if dRaw, present := raw["daemon"]; present {
		dm, ok := dRaw.(map[string]any)
		if !ok {
			return Config{}, fmt.Errorf("markedup.daemon must be a map")
		}
		if err := rejectUnknownKeys("markedup.daemon", dm, allowedDaemonKeys); err != nil {
			return Config{}, err
		}
		if v, ok := dm["refreshInterval"].(string); ok {
			cfg.Daemon.RefreshInterval = v
		} else if _, p := dm["refreshInterval"]; p {
			return Config{}, fmt.Errorf("markedup.daemon.refreshInterval must be a string (e.g. \"24h\")")
		}
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// rejectUnknownKeys returns a config error if got contains any key that
// isn't in allowed. scope is prepended to the error message for context.
func rejectUnknownKeys(scope string, got map[string]any, allowed map[string]struct{}) error {
	for k := range got {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("%s: unknown key %q", scope, k)
		}
	}
	return nil
}

// toInt normalizes the numeric shapes that Viper / YAML emit for
// integer-valued config keys. It accepts int, int32, int64, and
// whole-number float64; it rejects fractional floats and every other
// type by returning (0, false). json.Number and numeric strings are
// deliberately NOT accepted — they don't appear in the current
// Viper-backed config pipeline, and letting them through would paper
// over a misconfigured decoder at the config boundary.
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		// Only treat whole-number floats as integers to avoid silently
		// truncating a user-provided fractional dim count.
		if x == float64(int(x)) {
			return int(x), true
		}
	}
	return 0, false
}

// Validate checks for internally-inconsistent settings. When the plugin
// is disabled at the top level, nested embeddings/reranking settings are
// not enforced — a user may scaffold them in config.yml while the plugin
// is turned off, and that shouldn't produce a validation error.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
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
	if c.Daemon.RefreshInterval != "" {
		// Surface a typo'd duration at config-load time rather than
		// silently falling through to the 24h default at daemon-tick
		// time, where the user has no easy way to see the fallback.
		if _, err := time.ParseDuration(c.Daemon.RefreshInterval); err != nil {
			return fmt.Errorf("markedup.daemon.refreshInterval: invalid duration %q: %w", c.Daemon.RefreshInterval, err)
		}
	}
	return nil
}
