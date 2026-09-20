// Package validate contains the shared semantic validation used by both
// the live adapters and the JSONL replay path. The rule set is the
// protocol's contract for what counts as a usable observation: required
// fields present, labels in the closed vocabulary, probabilities in
// [0,1] with finite values and sum≈1, missing-field semantics for
// optional fields, and absence (not zero) when something is optional and
// not provided.
//
// Live adapters call these validators after parsing; replay calls them
// before scoring. The shared function set ensures that a downstream
// scorer cannot tell whether an observation came from a network call or a
// replay file beyond what the source label records.
package validate

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// ProbabilityTolerance mirrors adapter.ProbabilityTolerance; duplicated
// here to keep the validator package free of adapter imports.
const ProbabilityTolerance = 1e-3

// DecisionObservation is the minimum the replay/validator layer needs from
// a Decision. Fields mirror adapter.Decision.
type DecisionObservation struct {
	Choice        string
	Confidence    *float64
	Probabilities map[string]float64
	ResolvedModel string
	Attempts      int
}

// Decision verifies the shape of one observation against the protocol.
// Required: Choice non-empty, Attempts >= 1.
// Optional: ResolvedModel (empty for non-model sources like the
// deterministic baseline; live adapters must populate it). Confidence (nil
// when absent); Probabilities (nil when absent, but when present must be
// finite, in [0,1], and sum to ~1).
//
// The vocabulary argument restricts the Choice label space. Pass nil to
// skip vocabulary checking (live adapter enforces the protocol's choice
// IDs at request time; the validator uses it for replay admission).
func Decision(obs DecisionObservation, vocabulary []string) error {
	if strings.TrimSpace(obs.Choice) == "" {
		return errors.New("validate: choice is empty")
	}
	if obs.Attempts < 1 {
		return fmt.Errorf("validate: attempts must be >= 1, got %d", obs.Attempts)
	}
	if vocabulary != nil {
		found := false
		for _, v := range vocabulary {
			if v == obs.Choice {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("validate: choice %q not in vocabulary", obs.Choice)
		}
	}
	if obs.Confidence != nil {
		if !isFinite(*obs.Confidence) || *obs.Confidence < 0 || *obs.Confidence > 1 {
			return fmt.Errorf("validate: confidence %v is not in [0,1]", *obs.Confidence)
		}
	}
	if len(obs.Probabilities) > 0 {
		if err := Probabilities(obs.Probabilities); err != nil {
			return err
		}
	}
	return nil
}

// Probabilities enforces finiteness, [0,1] range, and sum≈1.
func Probabilities(p map[string]float64) error {
	if len(p) == 0 {
		return errors.New("validate: probabilities map is empty")
	}
	sum := 0.0
	for k, v := range p {
		if !isFinite(v) {
			return fmt.Errorf("validate: probability %q is not finite", k)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("validate: probability %q = %v outside [0,1]", k, v)
		}
		sum += v
	}
	if math.Abs(sum-1.0) > ProbabilityTolerance {
		return fmt.Errorf("validate: probabilities sum to %v, tolerance %v", sum, ProbabilityTolerance)
	}
	return nil
}

// ChatObservation is the minimum the replay/validator layer needs from a
// ChatObservation.
type ChatObservation struct {
	Content       map[string]any
	ResolvedModel string
	Attempts      int
}

// Chat verifies the shape of one chat observation. Content must be a
// non-empty JSON object (which map[string]any guarantees post-decode).
// ResolvedModel is optional (empty for non-model sources).
func Chat(obs ChatObservation) error {
	if obs.Attempts < 1 {
		return fmt.Errorf("validate: chat attempts must be >= 1, got %d", obs.Attempts)
	}
	if len(obs.Content) == 0 {
		return errors.New("validate: chat content is empty")
	}
	for k, v := range obs.Content {
		if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
			return fmt.Errorf("validate: chat content field %q has non-finite numeric value", k)
		}
	}
	return nil
}

// FixtureID validates the shape of a fixture ID for replay. IDs must be
// non-empty and free of stray whitespace; the harness reserves prefix
// matching for opaque ID checks upstream.
func FixtureID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("validate: fixture ID is empty")
	}
	if strings.ContainsAny(id, "\n\t\r ") {
		return fmt.Errorf("validate: fixture ID %q contains whitespace", id)
	}
	return nil
}

// Label checks that label is non-empty and (when vocabulary is non-nil) a
// member of vocabulary.
func Label(label string, vocabulary []string) error {
	if strings.TrimSpace(label) == "" {
		return errors.New("validate: label is empty")
	}
	if vocabulary == nil {
		return nil
	}
	for _, v := range vocabulary {
		if v == label {
			return nil
		}
	}
	return fmt.Errorf("validate: label %q not in vocabulary", label)
}

// VocabularyFor returns the protocol vocabulary for a task. It is a thin
// wrapper so callers do not need to import the protocol package. Pass
// nil to fetch the closed vocabulary for any supported task.
func VocabularyFor(t protocol.Task) []string {
	return protocol.AllowedLabelsFor(t)
}

// DecisionObservationFor returns the validation rules that apply to a
// decision-style observation per task. Submission adapters and replay
// runners share these rules.
func DecisionObservationFor(t protocol.Task) (vocab []string, abstentionLabel string, abstainIsValid bool) {
	switch t {
	case protocol.TaskRelationship:
		return protocol.AllowedLabelsFor(t), "insufficient-evidence", true
	case protocol.TaskClaimSupport:
		return protocol.AllowedLabelsFor(t), "insufficient-evidence", true
	case protocol.TaskEntityType:
		return protocol.AllowedLabelsFor(t), "document", true
	case protocol.TaskCandidateType:
		return protocol.AllowedLabelsFor(t), "", false
	}
	return nil, "", false
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
