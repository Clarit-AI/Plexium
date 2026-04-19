package bootstrap_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/agent"
	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/bootstrap"
	"github.com/Clarit-AI/Plexium/internal/plugins/markedup"
	"github.com/Clarit-AI/Plexium/internal/retry"
)

// stubEmbedder is the minimum embed.Embedder implementation needed to
// prove the wiring path: we never invoke Embed in these tests, only
// observe whether the resulting retrieval plugin reports a live
// embedder via HasEmbedder.
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}
func (stubEmbedder) Dimensions() int { return 4 }
func (stubEmbedder) Model() string   { return "stub" }

// findRetrieval returns the markedup-retrieval plugin from a registry,
// or fails the test. Centralizing the type assertion keeps the
// individual test bodies focused on the assertion that matters.
func findRetrieval(t *testing.T, reg *plugins.Registry) *markedup.RetrievalPlugin {
	t.Helper()
	for _, p := range reg.RetrievalPlugins() {
		if rp, ok := p.(*markedup.RetrievalPlugin); ok {
			return rp
		}
	}
	t.Fatal("no markedup-retrieval plugin registered")
	return nil
}

func TestBuildRegistry_NilConfig(t *testing.T) {
	t.Parallel()
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", nil)
	if err != nil {
		t.Fatalf("BuildRegistry(nil cfg): unexpected error: %v", err)
	}
	if reg == nil {
		t.Fatal("BuildRegistry(nil cfg): registry must never be nil")
	}
	if got := reg.PluginCount(); got != 0 {
		t.Fatalf("BuildRegistry(nil cfg): expected 0 plugins, got %d", got)
	}
}

func TestBuildRegistry_EmptyConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg)
	if err != nil {
		t.Fatalf("BuildRegistry(empty cfg): unexpected error: %v", err)
	}
	if reg == nil {
		t.Fatal("BuildRegistry(empty cfg): registry must never be nil")
	}
	if got := reg.PluginCount(); got != 0 {
		t.Fatalf("BuildRegistry(empty cfg): want empty registry, got count=%d", got)
	}
}

func TestBuildRegistry_MarkedupDisabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": false},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg)
	if err != nil {
		t.Fatalf("BuildRegistry(disabled): unexpected error: %v", err)
	}
	if got := reg.PluginCount(); got != 0 {
		t.Fatalf("BuildRegistry(disabled): expected 0 plugins, got %d", got)
	}
}

func TestBuildRegistry_MarkedupEnabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": true},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg)
	if err != nil {
		t.Fatalf("BuildRegistry(enabled): unexpected error: %v", err)
	}
	if got := reg.PluginCount(); got != 2 {
		t.Fatalf("BuildRegistry(enabled): expected 2 plugins (enricher+retrieval), got %d", got)
	}
	pipelinePlugins := reg.PipelinePlugins(plugins.StageAfterWrite)
	if len(pipelinePlugins) != 1 {
		t.Fatalf("expected 1 after-write pipeline plugin, got %d", len(pipelinePlugins))
	}
	if name := pipelinePlugins[0].Name(); name != "markedup-enricher" {
		t.Fatalf("expected markedup-enricher, got %q", name)
	}
	retrievalPlugins := reg.RetrievalPlugins()
	if len(retrievalPlugins) != 1 {
		t.Fatalf("expected 1 retrieval plugin, got %d", len(retrievalPlugins))
	}
	if name := retrievalPlugins[0].Name(); name != "markedup-retrieval" {
		t.Fatalf("expected markedup-retrieval, got %q", name)
	}
}

// TestBuildRegistry_MarkedupModelEnrichWithoutCascade verifies that
// requesting Tier 2 enrichment without supplying a cascade does NOT
// fail BuildRegistry — the enricher silently degrades to Tier 1 and
// logs a warning. This keeps the convert command robust against
// misconfigured assistive agents.
func TestBuildRegistry_MarkedupModelEnrichWithoutCascade(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": true, "modelEnrich": true},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg)
	if err != nil {
		t.Fatalf("expected no error when modelEnrich requested without cascade, got: %v", err)
	}
	if got := reg.PluginCount(); got != 2 {
		t.Fatalf("expected 2 plugins (enricher+retrieval), got %d", got)
	}
	if !strings.Contains(buf.String(), "modelEnrich is enabled but no assistive provider") {
		t.Errorf("expected degradation warning in log; got: %s", buf.String())
	}
}

// TestBuildRegistry_MarkedupModelEnrichWithCascade verifies that when a
// cascade with at least one usable provider is supplied AND modelEnrich
// is true, BuildRegistry wires it through (no warning logged).
func TestBuildRegistry_MarkedupModelEnrichWithCascade(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": true, "modelEnrich": true},
		},
	}
	// Build a cascade with a real (mock-callback) ollama provider so
	// HasProviders() returns true.
	provider := agent.NewOllamaProvider("http://localhost:11434", "test",
		func(ctx context.Context, url, body string) (string, int, error) {
			return "{}", 0, nil
		},
	)
	cascade := agent.NewCascade([]agent.Provider{provider}, retry.DefaultPolicy())

	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg, bootstrap.Options{Cascade: cascade})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := reg.PluginCount(); got != 2 {
		t.Fatalf("expected 2 plugins, got %d", got)
	}
	if strings.Contains(buf.String(), "no assistive provider") {
		t.Errorf("did not expect degradation warning when cascade is supplied; got: %s", buf.String())
	}
	// Ensure pipeline registration still resolves the enricher.
	pipelinePlugins := reg.PipelinePlugins(plugins.StageAfterWrite)
	if len(pipelinePlugins) != 1 || pipelinePlugins[0].Name() != "markedup-enricher" {
		t.Fatalf("expected markedup-enricher registered, got %+v", pipelinePlugins)
	}
	// Pin the cascade-reached-enricher invariant: the daemon path used
	// to call BuildRegistry without bootstrap.Options, silently nil-ing
	// the cascade and degrading to Tier 1 on every periodic refresh.
	// HasLLMProvider() proves the cascade actually produced an
	// LLMProvider that was wired into the EnricherPlugin.
	enricher, ok := pipelinePlugins[0].(*markedup.EnricherPlugin)
	if !ok {
		t.Fatalf("expected *markedup.EnricherPlugin, got %T", pipelinePlugins[0])
	}
	if !enricher.HasLLMProvider() {
		t.Fatal("expected EnricherPlugin to have a Tier 2 LLM provider when cascade is supplied via bootstrap.Options")
	}
}

// TestBuildRegistry_MarkedupModelEnrich_CascadeOmittedDegradesToTier1
// is the regression test for the daemon-path bug where
// runMarkedupEnrichJob called BuildRegistry without bootstrap.Options.
// When no Options is passed (cascade defaults to nil) AND modelEnrich
// is true, the enricher must end up with no LLMProvider attached — the
// expected degraded-but-correct behavior. Pairing this with the
// MarkedupModelEnrichWithCascade case above guards against regressions
// in either direction (always-attached or never-attached).
func TestBuildRegistry_MarkedupModelEnrich_CascadeOmittedDegradesToTier1(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": true, "modelEnrich": true},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg) // no Options
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	pipelinePlugins := reg.PipelinePlugins(plugins.StageAfterWrite)
	if len(pipelinePlugins) != 1 {
		t.Fatalf("expected 1 pipeline plugin, got %d", len(pipelinePlugins))
	}
	enricher := pipelinePlugins[0].(*markedup.EnricherPlugin)
	if enricher.HasLLMProvider() {
		t.Fatal("expected nil LLMProvider when no cascade Options provided (degraded Tier-1 path)")
	}
}

func TestBuildRegistry_MarkedupInvalidConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			// Unknown nested key — ParseConfig must reject it.
			"markedup": {"enabled": true, "bogusKey": 123},
		},
	}
	_, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg)
	if err == nil {
		t.Fatal("BuildRegistry(invalid): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "markedup") {
		t.Fatalf("BuildRegistry(invalid): error should mention markedup, got: %v", err)
	}
}

// TestBuildRegistry_EmbeddingsDisabled_NoEmbedder verifies that with
// embeddings.enabled left at the default (false), the retrieval plugin
// is built in keyword-only mode regardless of whether a cascade and an
// explicit embedder were supplied. This is the backward-compat guard:
// existing configs do not silently flip to semantic mode.
func TestBuildRegistry_EmbeddingsDisabled_NoEmbedder(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {"enabled": true},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), "/tmp/repo", cfg,
		bootstrap.Options{EmbeddingProvider: stubEmbedder{}},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval should not have embedder when embeddings.enabled=false")
	}
}

// TestBuildRegistry_EmbeddingsEnabled_ExplicitEmbedder verifies the
// happy path: embeddings.enabled is true, a caller supplies a fully
// constructed embedder via Options, and the resulting retrieval
// plugin reports a live embedder. This is the wiring contract D4
// will rely on when listing semantic-search MCP tools.
func TestBuildRegistry_EmbeddingsEnabled_ExplicitEmbedder(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled":    true,
				"embeddings": map[string]any{"enabled": true},
			},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg,
		bootstrap.Options{EmbeddingProvider: stubEmbedder{}},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval should have embedder when embeddings.enabled=true and EmbeddingProvider supplied")
	}
}

// TestBuildRegistry_EmbeddingsEnabled_InheritFromCascade verifies the
// "inherit" path that real users will hit: embeddings.enabled is true,
// no explicit embedder is supplied, but the assistive cascade has at
// least one openai-compatible provider that the bootstrap can pull
// endpoint/model out of. We don't actually fire the embedder, only
// confirm that one was constructed and wired.
func TestBuildRegistry_EmbeddingsEnabled_InheritFromCascade(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		AssistiveAgent: config.AssistiveAgent{
			Providers: []config.ProviderConfig{
				{
					Name:     "test",
					Enabled:  true,
					Type:     "openai-compatible",
					Endpoint: "http://localhost:8080",
					Model:    "test-model",
				},
			},
		},
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled": true,
				"embeddings": map[string]any{
					"enabled": true,
					// inherit mode requires an explicit embedding model;
					// pc.Model is the chat model and is not a valid
					// fallback (see F2 fix in bootstrap.go).
					"model": "text-embedding-3-small",
				},
			},
		},
	}
	provider := agent.NewOllamaProvider("http://localhost:11434", "test",
		func(ctx context.Context, url, body string) (string, int, error) {
			return "{}", 0, nil
		},
	)
	cascade := agent.NewCascade([]agent.Provider{provider}, retry.DefaultPolicy())

	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg,
		bootstrap.Options{Cascade: cascade},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval should have embedder when embeddings.enabled=true and cascade is supplied")
	}
}

// TestBuildRegistry_EmbeddingsInherit_MissingModelDisables is the
// regression test for CodeRabbit pass 5 F2: inherit mode used to fall
// back to the chat/completion model (pc.Model) when
// plugins.markedup.embeddings.model was empty, silently producing a
// broken embedder. The fix refuses to wire and logs a directed warning
// so the operator knows exactly which key to set.
func TestBuildRegistry_EmbeddingsInherit_MissingModelDisables(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg := &config.Config{
		AssistiveAgent: config.AssistiveAgent{
			Providers: []config.ProviderConfig{
				{
					Name:     "test",
					Enabled:  true,
					Type:     "openai-compatible",
					Endpoint: "http://localhost:8080",
					Model:    "gpt-4o-mini", // chat model — must NOT be reused
				},
			},
		},
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled":    true,
				"embeddings": map[string]any{"enabled": true},
			},
		},
	}
	provider := agent.NewOllamaProvider("http://localhost:11434", "test",
		func(ctx context.Context, url, body string) (string, int, error) {
			return "{}", 0, nil
		},
	)
	cascade := agent.NewCascade([]agent.Provider{provider}, retry.DefaultPolicy())

	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg,
		bootstrap.Options{Cascade: cascade},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval must not wire an embedder in inherit mode when embeddings.model is empty")
	}
	if !strings.Contains(buf.String(), "embeddings.model is empty") {
		t.Errorf("expected directed warning about missing embeddings.model; got: %s", buf.String())
	}
}

// TestBuildRegistry_EmbeddingsEnabled_NoCascadeWarns confirms graceful
// degradation: embeddings requested but neither an explicit embedder
// nor a cascade provided. The plugin must still build (keyword-only)
// and a single warning must surface so the operator can debug.
// TestBuildRegistry_EmbeddingsEnabled_ExplicitProviderRejectsNonOpenAI
// is the regression test for the pass-2 finding: the explicit-provider
// override branch (embeddings.provider != "" && != "inherit") must
// apply the same OpenAI-compatible guard the inherit loop uses. A
// config with provider: "ollama" passes ParseConfig.Validate but
// muembed.NewFromProvider only speaks /v1/embeddings, so it would
// silently produce broken embeddings at query time. BuildRegistry
// must drop to keyword-only and log a warning instead.
func TestBuildRegistry_EmbeddingsEnabled_ExplicitProviderRejectsNonOpenAI(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled": true,
				"embeddings": map[string]any{
					"enabled":  true,
					"provider": "ollama",
					"endpoint": "http://localhost:11434",
					"model":    "nomic-embed-text",
				},
			},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval must not wire a non-OpenAI-compatible explicit provider")
	}
	if !strings.Contains(buf.String(), "not OpenAI-compatible") {
		t.Errorf("expected explicit-provider rejection warning; got: %s", buf.String())
	}
}

// TestBuildRegistry_EmbeddingsEnabled_APIKeyResolverShared is the
// regression test for the pass-2 finding: the inherited embedder
// path used to call os.Getenv directly, bypassing the credential
// resolver buildCascadeFromConfig uses. A user who set their key
// via `plexium agent setup` (which writes .plexium/credentials.json)
// would get a working Tier 2 LLM but an empty-key embedder. We
// install a fake resolver and assert it's consulted for the
// expected env name; the embedder is constructed (HasEmbedder true)
// regardless of the key value, so resolver invocation is the
// observable signal here.
func TestBuildRegistry_EmbeddingsEnabled_APIKeyResolverShared(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		AssistiveAgent: config.AssistiveAgent{
			Providers: []config.ProviderConfig{
				{
					Name:      "test",
					Enabled:   true,
					Type:      "openai-compatible",
					Endpoint:  "http://localhost:8080",
					Model:     "test-model",
					APIKeyEnv: "PLEXIUM_TEST_API_KEY",
				},
			},
		},
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled": true,
				"embeddings": map[string]any{
					"enabled": true,
					// inherit mode requires an explicit embedding model
					// (see F2 fix); pc.Model is the chat model.
					"model": "text-embedding-3-small",
				},
			},
		},
	}
	provider := agent.NewOllamaProvider("http://localhost:11434", "test",
		func(ctx context.Context, url, body string) (string, int, error) {
			return "{}", 0, nil
		},
	)
	cascade := agent.NewCascade([]agent.Provider{provider}, retry.DefaultPolicy())

	var seenEnv string
	resolver := func(envName string) string {
		seenEnv = envName
		return "resolved-key"
	}

	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg,
		bootstrap.Options{Cascade: cascade, APIKeyResolver: resolver},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("expected embedder to be wired when cascade + resolver are supplied")
	}
	if seenEnv != "PLEXIUM_TEST_API_KEY" {
		t.Errorf("expected APIKeyResolver to be called with %q, got %q", "PLEXIUM_TEST_API_KEY", seenEnv)
	}
}

func TestBuildRegistry_EmbeddingsEnabled_NoCascadeWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg := &config.Config{
		Plugins: map[string]map[string]any{
			"markedup": {
				"enabled":    true,
				"embeddings": map[string]any{"enabled": true},
			},
		},
	}
	reg, err := bootstrap.BuildRegistry(context.Background(), t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findRetrieval(t, reg).HasEmbedder() {
		t.Errorf("retrieval must fall back to keyword-only when no embedder source is available")
	}
	if !strings.Contains(buf.String(), "semantic search disabled") {
		t.Errorf("expected degradation warning; got: %s", buf.String())
	}
}
