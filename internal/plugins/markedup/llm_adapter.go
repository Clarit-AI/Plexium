package markedup

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/Clarit-AI/markedup/enrich"

	"github.com/Clarit-AI/Plexium/internal/agent"
)

// CascadeLLMProvider adapts a Plexium *agent.ProviderCascade to the
// markedup LLMProvider interface. It instructs the underlying chat
// provider to return a JSON object matching enrich.ModelResult plus an
// optional natural language summary, then unmarshals the response.
//
// This adapter lives next to the EnricherPlugin so the bootstrap layer
// can wire it without leaking markedup-internal types into internal/agent.
//
// On unparseable model output the adapter returns (nil, "", err); the
// enricher treats that as a non-fatal Tier 2 failure (logs + falls back
// to Tier 1) so a flaky model doesn't abort the entire convert pass.
type CascadeLLMProvider struct {
	Cascade     *agent.ProviderCascade
	RateTracker *agent.RateLimitTracker // optional; records token/cost per call when non-nil
}

// NewCascadeLLMProvider constructs a CascadeLLMProvider. Returns nil if
// cascade is nil or has no usable providers — callers should treat a nil
// return as "Tier 2 unavailable" and skip wiring.
//
// rateTracker is optional: when non-nil, every successful ExtractGraph
// call records the underlying provider's TokensUsed/CostUSD against the
// daily-spend ledger so convert-time enrichment is budgeted alongside
// the daemon's lint/debt jobs and `agent spend` reports. Pass nil to
// disable budget recording (calls still succeed).
func NewCascadeLLMProvider(cascade *agent.ProviderCascade, rateTracker *agent.RateLimitTracker) *CascadeLLMProvider {
	if cascade == nil || !cascade.HasProviders() {
		return nil
	}
	return &CascadeLLMProvider{Cascade: cascade, RateTracker: rateTracker}
}

// extractResponse is the JSON shape we ask the model to produce. It
// embeds enrich.ModelResult plus a top-level summary so callers can
// pass both directly into enrich.EnrichPageWithModel.
type extractResponse struct {
	enrich.ModelResult
	Summary string `json:"summary"`
}

// ExtractGraph satisfies the LLMProvider interface.
func (a *CascadeLLMProvider) ExtractGraph(ctx context.Context, body string) (*enrich.ModelResult, string, error) {
	if a == nil || a.Cascade == nil {
		return nil, "", fmt.Errorf("markedup: nil cascade")
	}

	prompt := buildExtractionPrompt(body)
	completion, err := a.Cascade.Complete(ctx, prompt)
	if err != nil {
		return nil, "", fmt.Errorf("markedup: cascade.Complete: %w", err)
	}
	// Defensive: some Provider implementations can return (nil, nil) on
	// degenerate paths (e.g. a stub provider whose IsAvailable flickers).
	// Without this guard the downstream `completion.Response` deref would
	// panic; returning a normal error lets the enricher's non-fatal Tier 2
	// path log + degrade to Tier 1 instead.
	if completion == nil {
		return nil, "", fmt.Errorf("markedup: provider returned nil completion")
	}

	// Best-effort: record this call against the daily-spend ledger so
	// the convert path's Tier 2 usage shows up in `agent spend`. A
	// failure here must not abort enrichment — log and continue.
	if a.RateTracker != nil && completion.Provider != "" {
		if err := a.RateTracker.Record(completion.Provider, completion.TokensUsed, completion.CostUSD); err != nil {
			log.Printf("markedup: rate tracker record (%s): %v", completion.Provider, err)
		}
	}

	raw := strings.TrimSpace(completion.Response)
	if raw == "" {
		return nil, "", fmt.Errorf("markedup: provider returned empty response")
	}

	// Strip ```json ... ``` fences some providers wrap responses in.
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i >= 0 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}

	var resp extractResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, "", fmt.Errorf("markedup: parse extraction response: %w", err)
	}

	model := resp.ModelResult
	return &model, resp.Summary, nil
}

func buildExtractionPrompt(body string) string {
	const maxBody = 8000
	trimmed := body
	if len(trimmed) > maxBody {
		// Back up to the nearest UTF-8 rune boundary so we don't
		// slice mid-sequence — multibyte runes (smart quotes, emoji,
		// non-Latin prose) would otherwise leave an invalid tail
		// that some tokenizers reject or render as replacement chars.
		cut := maxBody
		for cut > 0 && !utf8.RuneStart(trimmed[cut]) {
			cut--
		}
		trimmed = trimmed[:cut]
	}

	var b strings.Builder
	b.WriteString("Extract a knowledge graph from the following markdown document.\n")
	b.WriteString("Return ONLY a single JSON object matching this schema:\n")
	b.WriteString(`{
  "entities": [{"name": "...", "aliases": ["..."], "role": "..."}],
  "relationships": [{"target": "...", "type": "...", "strength": 0.0}],
  "entity_type": "...",
  "summary": "one or two sentence natural-language summary",
  "semantic_hints": ["..."],
  "possible_questions": ["..."]
}` + "\n\n")
	b.WriteString("All fields are optional; omit any you cannot infer. Do not include prose outside the JSON object.\n\n")
	b.WriteString("Document:\n```md\n")
	b.WriteString(trimmed)
	b.WriteString("\n```\n")
	return b.String()
}
