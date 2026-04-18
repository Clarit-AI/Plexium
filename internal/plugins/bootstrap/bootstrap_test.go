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
	"github.com/Clarit-AI/Plexium/internal/retry"
)

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
	if reg == nil || reg.PluginCount() != 0 {
		t.Fatalf("BuildRegistry(empty cfg): want empty registry, got count=%d", reg.PluginCount())
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
