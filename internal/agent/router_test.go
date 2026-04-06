package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ClassifyTask
// ---------------------------------------------------------------------------

func TestClassifyTask_Low(t *testing.T) {
	lowTypes := []string{
		"frontmatter-update",
		"log-entry",
		"index-regeneration",
		"sidebar-regeneration",
		"link-validation",
		"manifest-update",
		"page-state-transition",
	}
	for _, tt := range lowTypes {
		assert.Equal(t, ComplexityLow, ClassifyTask(tt), tt)
	}
}

func TestClassifyTask_Medium(t *testing.T) {
	medTypes := []string{
		"cross-reference-suggestion",
		"module-summary",
		"staleness-check",
	}
	for _, tt := range medTypes {
		assert.Equal(t, ComplexityMedium, ClassifyTask(tt), tt)
	}
}

func TestClassifyTask_High(t *testing.T) {
	highTypes := []string{
		"architecture-synthesis",
		"contradiction-detection",
		"adr-creation",
		"complex-ingest",
		"deep-code-analysis",
	}
	for _, tt := range highTypes {
		assert.Equal(t, ComplexityHigh, ClassifyTask(tt), tt)
	}
}

func TestClassifyTask_Deterministic(t *testing.T) {
	detTypes := []string{
		"hash-computation",
		"path-validation",
		"orphan-detection",
	}
	for _, tt := range detTypes {
		assert.Equal(t, ComplexityDeterministic, ClassifyTask(tt), tt)
	}
}

func TestClassifyTask_UnknownDefaultsMedium(t *testing.T) {
	assert.Equal(t, ComplexityMedium, ClassifyTask("unknown-task"))
	assert.Equal(t, ComplexityMedium, ClassifyTask(""))
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func newTestRouter(provider *mockProvider) *TaskRouter {
	cascade := NewCascade([]Provider{provider}, noRetryPolicy())
	return NewRouter(cascade)
}

func TestRoute_LowComplexity(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.001, response: "ok", tokens: 10}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "frontmatter-update",
		Prompt: "update frontmatter",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Response)
	assert.Equal(t, 1, p.callCount)
}

func TestRoute_MediumComplexity(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.001, response: "summary", tokens: 50}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "module-summary",
		Prompt: "summarize module",
	})
	require.NoError(t, err)
	assert.Equal(t, "summary", result.Response)
}

func TestRoute_HighComplexity_FallsBackToAssistiveWhenNoPrimary(t *testing.T) {
	// When no primary cascade is set, high complexity falls back to the assistive cascade.
	p := &mockProvider{name: "test", available: true, cost: 0.01, response: "synthesis", tokens: 200}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "architecture-synthesis",
		Prompt: "synthesize architecture",
	})
	require.NoError(t, err)
	assert.Equal(t, "synthesis", result.Response)
}

func TestRoute_HighComplexity_UsesPrimaryCascade(t *testing.T) {
	// High complexity should route to primary cascade when one is configured.
	assistive := &mockProvider{name: "assistive", available: true, cost: 0.001, response: "assistive", tokens: 10}
	primary := &mockProvider{name: "primary", available: true, cost: 0.1, response: "primary-response", tokens: 100}

	assistiveCascade := NewCascade([]Provider{assistive}, noRetryPolicy())
	primaryCascade := NewCascade([]Provider{primary}, noRetryPolicy())

	r := NewRouter(assistiveCascade)
	r.SetPrimaryCascade(primaryCascade)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "architecture-synthesis",
		Prompt: "synthesize architecture",
	})
	require.NoError(t, err)
	assert.Equal(t, "primary-response", result.Response)
	assert.Equal(t, 0, assistive.callCount, "assistive provider should not be called")
	assert.Equal(t, 1, primary.callCount, "primary provider should be called")
}

func TestRoute_Context_PrependedToPrompt(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.001, response: "ok", tokens: 10}
	r := newTestRouter(p)

	_, err := r.Route(context.Background(), WikiTask{
		Type:    "frontmatter-update",
		Prompt:  "update frontmatter",
		Context: []string{"modules/auth.md", "modules/token.md"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, p.callCount)
	// Provider receives the prompt with context prepended.
	assert.Contains(t, p.lastPrompt, "Context pages:")
	assert.Contains(t, p.lastPrompt, "modules/auth.md")
	assert.Contains(t, p.lastPrompt, "modules/token.md")
	assert.Contains(t, p.lastPrompt, "update frontmatter")
}

func TestRoute_ExplicitComplexityOverridesClassification(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.001, response: "forced", tokens: 5}
	r := newTestRouter(p)

	// Even though "hash-computation" classifies as deterministic,
	// an explicit Complexity field overrides that.
	result, err := r.Route(context.Background(), WikiTask{
		Type:       "hash-computation",
		Complexity: ComplexityLow,
		Prompt:     "force low",
	})
	require.NoError(t, err)
	assert.Equal(t, "forced", result.Response)
}

func TestRoute_CascadeErrorPropagates(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.001, err: errors.New("boom")}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "module-summary",
		Prompt: "fail",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoProvider))
}
