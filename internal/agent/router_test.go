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

func TestRoute_HighComplexity(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.01, response: "synthesis", tokens: 200}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "architecture-synthesis",
		Prompt: "synthesize architecture",
	})
	require.NoError(t, err)
	assert.Equal(t, "synthesis", result.Response)
}

func TestRoute_DeterministicRejected(t *testing.T) {
	p := &mockProvider{name: "test", available: true, cost: 0.0, response: "should not reach"}
	r := newTestRouter(p)

	result, err := r.Route(context.Background(), WikiTask{
		Type:   "hash-computation",
		Prompt: "compute hash",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDeterministicTask))
	assert.Equal(t, 0, p.callCount, "provider should not be called for deterministic tasks")
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
