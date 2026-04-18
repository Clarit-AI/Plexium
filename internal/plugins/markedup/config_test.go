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
