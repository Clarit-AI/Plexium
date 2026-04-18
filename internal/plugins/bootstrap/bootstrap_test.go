package bootstrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/config"
	"github.com/Clarit-AI/Plexium/internal/plugins"
	"github.com/Clarit-AI/Plexium/internal/plugins/bootstrap"
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
