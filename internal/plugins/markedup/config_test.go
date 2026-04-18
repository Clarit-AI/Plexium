package markedup

import "testing"

func TestParseConfig_EmptyIsDisabled(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg.Enabled {
		t.Error("empty config should yield Enabled=false")
	}
}

func TestParseConfig_AppliesDefaultsWhenEnabled(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if !cfg.AutoEnrich {
		t.Error("expected AutoEnrich default=true")
	}
	if cfg.ModelEnrich {
		t.Error("expected ModelEnrich default=false")
	}
	if cfg.WriteEnrichedFrontmatter {
		t.Error("expected WriteEnrichedFrontmatter default=false (manifest-only mode)")
	}
	if cfg.Embeddings.Provider != "inherit" {
		t.Errorf("expected embeddings.provider=inherit, got %q", cfg.Embeddings.Provider)
	}
	if cfg.Embeddings.Dims != 768 {
		t.Errorf("expected embeddings.dims=768, got %d", cfg.Embeddings.Dims)
	}
}

func TestParseConfig_OverridesAreHonored(t *testing.T) {
	raw := map[string]any{
		"enabled":                  true,
		"autoEnrich":               false,
		"modelEnrich":              true,
		"writeEnrichedFrontmatter": true,
		"embeddings": map[string]any{
			"enabled":   true,
			"provider":  "openai-compatible",
			"model":     "text-embedding-3-small",
			"endpoint":  "https://api.openai.com",
			"apiKeyEnv": "OPENAI_API_KEY",
			"dims":      1536,
		},
		"reranking": map[string]any{
			"enabled":  true,
			"provider": "jina",
			"model":    "jina-reranker-v2-base-multilingual",
		},
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg.AutoEnrich {
		t.Error("AutoEnrich override not applied")
	}
	if !cfg.ModelEnrich {
		t.Error("ModelEnrich override not applied")
	}
	if !cfg.WriteEnrichedFrontmatter {
		t.Error("WriteEnrichedFrontmatter override not applied")
	}
	if cfg.Embeddings.Dims != 1536 {
		t.Errorf("embeddings.dims=%d", cfg.Embeddings.Dims)
	}
	if cfg.Reranking.Model != "jina-reranker-v2-base-multilingual" {
		t.Errorf("reranking.model=%q", cfg.Reranking.Model)
	}
}

func TestParseConfig_ValidationCatchesIncompleteProvider(t *testing.T) {
	// Non-inherit embeddings provider without endpoint/model should fail.
	raw := map[string]any{
		"enabled": true,
		"embeddings": map[string]any{
			"enabled":  true,
			"provider": "ollama",
			// model and endpoint missing
		},
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected validation error for missing endpoint/model")
	}
}

func TestParseConfig_ValidationCatchesRerankingWithoutModel(t *testing.T) {
	raw := map[string]any{
		"enabled": true,
		"reranking": map[string]any{
			"enabled": true,
			// model missing
		},
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected validation error for reranking without model")
	}
}

// Regression: YAML decoders can emit numeric values as int, int64, or
// float64 depending on the library in the pipeline. toInt should accept
// all whole-number forms without silently dropping the override.
// Addresses CodeRabbit review finding on config.go:148.
func TestParseConfig_AcceptsNumericDimsAcrossTypes(t *testing.T) {
	cases := map[string]any{
		"int":            int(1536),
		"int64":          int64(1536),
		"float64(whole)": float64(1536),
	}
	for name, dims := range cases {
		t.Run(name, func(t *testing.T) {
			raw := map[string]any{
				"enabled": true,
				"embeddings": map[string]any{
					"enabled":  true,
					"provider": "inherit",
					"dims":     dims,
				},
			}
			cfg, err := ParseConfig(raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if cfg.Embeddings.Dims != 1536 {
				t.Errorf("expected Dims=1536 for %s, got %d", name, cfg.Embeddings.Dims)
			}
		})
	}
}

// Regression for CodeRabbit pass 3: Validate must short-circuit when
// the plugin is globally disabled. A disabled plugin may still have
// scaffolded embeddings/reranking blocks; those shouldn't cause config
// load to fail.
func TestParseConfig_DisabledSkipsNestedValidation(t *testing.T) {
	raw := map[string]any{
		"enabled": false,
		"embeddings": map[string]any{
			"enabled":  true,
			"provider": "openai-compatible",
			// endpoint and model deliberately missing — would normally
			// fail Validate, but the plugin is disabled so we accept.
		},
		"reranking": map[string]any{
			"enabled": true,
			// model deliberately missing.
		},
	}
	_, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("disabled plugin should skip nested validation; got %v", err)
	}
}

// Strict parsing: a fractional float isn't a legitimate dim count. Under
// the pass-4 tightening this is now an error, not a silent fallback.
func TestParseConfig_RejectsFractionalDims(t *testing.T) {
	raw := map[string]any{
		"enabled": true,
		"embeddings": map[string]any{
			"enabled":  true,
			"provider": "inherit",
			"dims":     3.14,
		},
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for fractional dims")
	}
}

// Strict parsing regressions covering CodeRabbit pass-4 feedback:
// typos, unknown keys, wrong types, non-positive dims must all surface
// as errors rather than silently producing a half-configured plugin.

func TestParseConfig_RejectsUnknownTopLevelKey(t *testing.T) {
	// "enabeld" is a typo for "enabled". Under silent-acceptance this
	// would leave Enabled=false (from the default) and the user would
	// never know their config had no effect. Under strict parsing it
	// must be rejected.
	raw := map[string]any{
		"enabeld": true,
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for unknown top-level key")
	}
}

func TestParseConfig_RejectsUnknownEmbeddingsKey(t *testing.T) {
	raw := map[string]any{
		"enabled": true,
		"embeddings": map[string]any{
			"enabled":    true,
			"provider":   "inherit",
			"dimensions": 768, // typo — correct key is "dims"
		},
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for unknown embeddings key")
	}
}

func TestParseConfig_RejectsNonBooleanEnabled(t *testing.T) {
	raw := map[string]any{
		"enabled": "yes", // string where bool expected
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for non-boolean enabled")
	}
}

func TestParseConfig_RejectsNonPositiveDims(t *testing.T) {
	raw := map[string]any{
		"enabled": true,
		"embeddings": map[string]any{
			"enabled":  true,
			"provider": "inherit",
			"dims":     0,
		},
	}
	_, err := ParseConfig(raw)
	if err == nil {
		t.Fatal("expected error for dims=0")
	}
}

// Without explicit enabled: true, a block with other keys must leave the
// plugin disabled (strict enablement). This is the critical behavior
// change from pass 3 — previously a non-empty block implied Enabled=true.
func TestParseConfig_NonEmptyBlockWithoutEnabledStaysDisabled(t *testing.T) {
	raw := map[string]any{
		"autoEnrich":  false,
		"modelEnrich": true,
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Enabled {
		t.Error("non-empty block without enabled:true must leave plugin disabled")
	}
}

func TestParseConfig_InheritEmbeddingsAllowedWithoutEndpoint(t *testing.T) {
	// provider="inherit" means reuse assistiveAgent config, so endpoint
	// and model may legitimately be empty in the plugin block.
	raw := map[string]any{
		"enabled": true,
		"embeddings": map[string]any{
			"enabled":  true,
			"provider": "inherit",
		},
	}
	_, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
