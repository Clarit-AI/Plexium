package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrDeterministicTask is returned when a deterministic task is routed to the
// LLM cascade. Deterministic tasks should be handled without LLM calls.
var ErrDeterministicTask = errors.New("agent: deterministic tasks don't need LLM")

// TaskComplexity classifies how much LLM capability a wiki task requires.
type TaskComplexity string

const (
	ComplexityLow           TaskComplexity = "low"
	ComplexityMedium        TaskComplexity = "medium"
	ComplexityHigh          TaskComplexity = "high"
	ComplexityDeterministic TaskComplexity = "deterministic"
)

// WikiTask describes a unit of wiki maintenance work to be routed.
type WikiTask struct {
	Type       string         `json:"type"`
	Complexity TaskComplexity `json:"complexity"`
	Prompt     string         `json:"prompt"`
	Context    []string       `json:"context"` // wiki page paths
}

// TaskRouter selects the appropriate provider cascade path based on task
// complexity. Deterministic tasks are rejected — they should never hit an LLM.
// The router has two cascade paths:
//   - assistive cascade: low/medium complexity tasks (cost-optimised providers)
//   - primary cascade: high complexity tasks (the coding agent's own LLM)
type TaskRouter struct {
	assistiveCascade *ProviderCascade
	primaryCascade   *ProviderCascade
}

// NewRouter creates a TaskRouter backed by the given assistive cascade.
func NewRouter(cascade *ProviderCascade) *TaskRouter {
	return &TaskRouter{assistiveCascade: cascade}
}

// SetPrimaryCascade sets the cascade used for high-complexity tasks.
// Typically this is the InheritProvider pointing to the coding agent's own LLM.
func (r *TaskRouter) SetPrimaryCascade(cascade *ProviderCascade) {
	r.primaryCascade = cascade
}

// ClassifyTask returns the complexity level for a known task type.
// Unknown types default to Medium.
func ClassifyTask(taskType string) TaskComplexity {
	switch taskType {
	// Low complexity — simple, templated operations.
	case "frontmatter-update",
		"log-entry",
		"index-regeneration",
		"sidebar-regeneration",
		"link-validation",
		"manifest-update",
		"page-state-transition":
		return ComplexityLow

	// Medium complexity — requires some reasoning.
	case "cross-reference-suggestion",
		"module-summary",
		"staleness-check":
		return ComplexityMedium

	// High complexity — deep synthesis or analysis.
	case "architecture-synthesis",
		"contradiction-detection",
		"adr-creation",
		"complex-ingest",
		"deep-code-analysis":
		return ComplexityHigh

	// Deterministic — pure computation, no LLM needed.
	case "hash-computation",
		"path-validation",
		"orphan-detection":
		return ComplexityDeterministic

	default:
		return ComplexityMedium
	}
}

// Route sends a WikiTask through the appropriate provider cascade based on its
// complexity. High-complexity tasks skip to the primary cascade (the coding
// agent's own LLM); low/medium tasks use the assistive cascade.
// Deterministic tasks return ErrDeterministicTask — they must be handled by
// deterministic code paths (e.g. the compile engine), not an LLM.
func (r *TaskRouter) Route(ctx context.Context, task WikiTask) (*CompletionResult, error) {
	complexity := task.Complexity
	if complexity == "" {
		complexity = ClassifyTask(task.Type)
	}

	if complexity == ComplexityDeterministic {
		return nil, fmt.Errorf("%w: task type %q", ErrDeterministicTask, task.Type)
	}

	prompt := task.Prompt
	if len(task.Context) > 0 {
		prompt = buildPromptWithContext(task.Prompt, task.Context)
	}

	// High-complexity tasks route to the primary cascade (coding agent's own LLM).
	// Without a primary cascade, fall back to the assistive cascade.
	if complexity == ComplexityHigh {
		if r.primaryCascade != nil {
			return r.primaryCascade.Complete(ctx, prompt)
		}
		// Fall through to assistive cascade if no primary configured.
	}

	return r.assistiveCascade.Complete(ctx, prompt)
}

// buildPromptWithContext prepends context page references to the prompt.
func buildPromptWithContext(prompt string, context []string) string {
	if len(context) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("Context pages:\n")
	for _, p := range context {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(prompt)
	return b.String()
}
