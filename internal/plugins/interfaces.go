package plugins

import (
	"context"
	"encoding/json"
)

// PluginType enumerates the plugin categories.
type PluginType string

const (
	// PluginTypeAdapter generates agent instruction files (Claude, Codex, etc).
	PluginTypeAdapter PluginType = "adapter"
	// PluginTypePipeline hooks into the convert pipeline stages.
	PluginTypePipeline PluginType = "pipeline"
	// PluginTypeRetrieval provides search/retrieval capabilities.
	PluginTypeRetrieval PluginType = "retrieval"
	// PluginTypeWatch adds daemon watch behaviors.
	PluginTypeWatch PluginType = "watch"
)

// Plugin is the base interface all plugins implement.
type Plugin interface {
	// Name returns the unique identifier for the plugin.
	Name() string
	// Version returns the plugin version string.
	Version() string
	// Description returns a human-readable description.
	Description() string
	// Type returns the plugin category.
	Type() PluginType
}

// Configurable is implemented by plugins that accept configuration
// from the plugins section of .plexium/config.yml.
type Configurable interface {
	Configure(cfg map[string]any) error
}

// PipelineStage identifies where in the convert pipeline a plugin runs.
type PipelineStage string

const (
	StageAfterIngest PipelineStage = "after-ingest"
	StageAfterLink   PipelineStage = "after-link"
	StageAfterLint   PipelineStage = "after-lint"
	StageAfterWrite  PipelineStage = "after-write"
)

// PipelineData carries state between pipeline stages and plugins.
type PipelineData struct {
	RepoRoot string
	WikiRoot string
	Pages    []PipelinePage
}

// PipelinePage represents a wiki page flowing through the pipeline.
type PipelinePage struct {
	WikiPath    string
	Title       string
	Section     string
	Content     string
	SourceFiles []string
}

// PipelinePlugin hooks into the convert pipeline.
type PipelinePlugin interface {
	Plugin
	// Stage returns which pipeline stage this plugin runs after.
	Stage() PipelineStage
	// Process runs the plugin's logic on pipeline data.
	Process(ctx context.Context, data *PipelineData) error
}

// SearchOpts configures a retrieval search.
type SearchOpts struct {
	Limit    int
	MinScore float64
	Semantic bool // request semantic/embedding search if available
}

// PluginSearchResult represents a search hit from a retrieval plugin.
type PluginSearchResult struct {
	PagePath  string  `json:"pagePath"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
	MatchType string  `json:"matchType"`
}

// MCPToolDef describes an MCP tool that a retrieval plugin exposes.
type MCPToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema any `json:"inputSchema"`
}

// RetrievalPlugin provides search capabilities beyond the built-in PageIndex.
type RetrievalPlugin interface {
	Plugin
	// Initialize loads or builds the plugin's index from the wiki root.
	Initialize(ctx context.Context, wikiRoot string) error
	// Search returns results for a query.
	Search(ctx context.Context, query string, opts SearchOpts) ([]PluginSearchResult, error)
	// MCPTools returns additional MCP tool definitions this plugin exposes.
	MCPTools() []MCPToolDef
	// HandleMCPCall handles an MCP tool invocation by name.
	HandleMCPCall(ctx context.Context, tool string, args json.RawMessage) (any, error)
}

// WatchPlugin adds custom daemon watch behaviors.
type WatchPlugin interface {
	Plugin
	// ShouldRun returns true if the watch has work to do.
	ShouldRun(ctx context.Context, repoRoot string) (bool, error)
	// Run executes the watch logic.
	Run(ctx context.Context, repoRoot string) error
}
