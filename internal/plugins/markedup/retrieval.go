package markedup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/KHAEntertainment/markedup/index"

	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// RetrievalPlugin exposes MarkedUp search, graph traversal, and graph
// summary as MCP tools on the PageIndex server.
//
// The underlying *index.KnowledgeIndex is rebuilt by Initialize (and can
// be refreshed with Reload) and stored behind an atomic.Pointer so that
// concurrent MCP calls never observe a half-rebuilt index.
type RetrievalPlugin struct {
	cfg Config
	idx atomic.Pointer[index.KnowledgeIndex]
}

// NewRetrieval constructs a RetrievalPlugin from a parsed Config.
func NewRetrieval(cfg Config) *RetrievalPlugin {
	return &RetrievalPlugin{cfg: cfg}
}

// Plugin interface

func (p *RetrievalPlugin) Name() string             { return "markedup-retrieval" }
func (p *RetrievalPlugin) Version() string          { return EnricherVersion }
func (p *RetrievalPlugin) Description() string      { return "MarkedUp semantic search and graph traversal" }
func (p *RetrievalPlugin) Type() plugins.PluginType { return plugins.PluginTypeRetrieval }

// Initialize loads the knowledge graph at wikiRoot. Safe to call multiple
// times; each call swaps in a new *KnowledgeIndex atomically.
//
// Semantic-search bindings (embedder / reranker / vector cache) are a
// follow-up; for now Initialize builds the index without them and
// Search degrades to keyword-only scoring.
func (p *RetrievalPlugin) Initialize(ctx context.Context, wikiRoot string) error {
	if !p.cfg.Enabled {
		return nil
	}
	res, err := index.Load(ctx, wikiRoot,
		index.WithIgnoreErrors(true),
		index.WithAutoEnrich(false),
	)
	if err != nil {
		return fmt.Errorf("markedup-retrieval: load index: %w", err)
	}
	p.idx.Store(res.Index)
	return nil
}

// Search runs keyword search against the loaded index. Returns
// plugins.PluginSearchResult slices for rendering in the caller's format.
func (p *RetrievalPlugin) Search(ctx context.Context, query string, opts plugins.SearchOpts) ([]plugins.PluginSearchResult, error) {
	idx := p.idx.Load()
	if idx == nil {
		return nil, fmt.Errorf("markedup-retrieval: not initialized")
	}

	searchOpts := []index.SearchOption{index.WithContext(ctx)}
	if opts.Limit > 0 {
		searchOpts = append(searchOpts, index.WithLimit(opts.Limit))
	}
	if opts.MinScore > 0 {
		searchOpts = append(searchOpts, index.WithMinScore(opts.MinScore))
	}
	// NOTE: semantic/rerank configuration is a follow-up; opts.Semantic is
	// honored as a signal but has no effect until the embedder is wired.

	results := index.Search(idx, query, searchOpts...)
	out := make([]plugins.PluginSearchResult, 0, len(results))
	for _, r := range results {
		matchType := ""
		if len(r.Matches) > 0 {
			matchType = r.Matches[0].Field
		}
		out = append(out, plugins.PluginSearchResult{
			PagePath:  r.Page.SourcePath,
			Title:     r.Page.Frontmatter.Title,
			Score:     r.Score,
			Snippet:   firstNonEmptyLine(r.Page.Body),
			MatchType: matchType,
		})
	}
	return out, nil
}

// MCPTools declares the MarkedUp-specific MCP tool surface on top of the
// standard pageindex_* tools.
func (p *RetrievalPlugin) MCPTools() []plugins.MCPToolDef {
	return []plugins.MCPToolDef{
		{
			Name:        "markedup_search",
			Description: "Keyword + (future) semantic search over the MarkedUp knowledge graph. Returns ranked pages with relevance scores.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query string"},
					"limit": map[string]any{"type": "integer", "description": "Max results (default 10)"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "markedup_traverse",
			Description: "Walk the knowledge graph starting from a page ID. Returns reachable nodes and the edges that connect them.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string", "description": "Starting page ID"},
					"depth": map[string]any{"type": "integer", "description": "Max traversal depth (default 2)"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "markedup_graph",
			Description: "Compact, token-efficient summary of the knowledge graph suitable for LLM context injection.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entityType": map[string]any{"type": "string", "description": "Optional: filter to a single entity type"},
					"tag":        map[string]any{"type": "string", "description": "Optional: filter to pages with this tag"},
					"maxPages":   map[string]any{"type": "integer", "description": "Optional: cap the returned page count"},
				},
			},
		},
	}
}

// HandleMCPCall dispatches an MCP tool invocation to the right MarkedUp API.
func (p *RetrievalPlugin) HandleMCPCall(ctx context.Context, tool string, args json.RawMessage) (any, error) {
	idx := p.idx.Load()
	if idx == nil {
		return nil, fmt.Errorf("markedup-retrieval: not initialized")
	}

	switch tool {
	case "markedup_search":
		var input struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("markedup_search: invalid args: %w", err)
		}
		if strings.TrimSpace(input.Query) == "" {
			return nil, fmt.Errorf("markedup_search: query is required")
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 10
		}
		return p.Search(ctx, input.Query, plugins.SearchOpts{Limit: limit})

	case "markedup_traverse":
		var input struct {
			ID    string `json:"id"`
			Depth int    `json:"depth"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("markedup_traverse: invalid args: %w", err)
		}
		if strings.TrimSpace(input.ID) == "" {
			return nil, fmt.Errorf("markedup_traverse: id is required")
		}
		opts := []index.TraverseOption{}
		if input.Depth > 0 {
			opts = append(opts, index.WithDepth(input.Depth))
		}
		return index.Traverse(idx, input.ID, opts...)

	case "markedup_graph":
		var input struct {
			EntityType string `json:"entityType"`
			Tag        string `json:"tag"`
			MaxPages   int    `json:"maxPages"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("markedup_graph: invalid args: %w", err)
		}
		opts := []index.SummaryOption{}
		if input.EntityType != "" {
			opts = append(opts, index.WithEntityTypeFilter(input.EntityType))
		}
		if input.Tag != "" {
			opts = append(opts, index.WithTagFilter(input.Tag))
		}
		if input.MaxPages > 0 {
			opts = append(opts, index.WithMaxPages(input.MaxPages))
		}
		return idx.CompactGraphSummary(opts...), nil

	default:
		return nil, fmt.Errorf("markedup-retrieval: unknown tool %q", tool)
	}
}

// firstNonEmptyLine returns the first non-blank line of body for snippet
// display. Empty bodies return empty string.
func firstNonEmptyLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if trim != "" {
			return trim
		}
	}
	return ""
}
