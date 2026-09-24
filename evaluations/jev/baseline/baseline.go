// Package baseline implements the deterministic abstaining baseline the
// protocol requires for each task. The baseline emits a fixed label and
// refuses to invent confidence / latency / cost values; downstream scorers
// must therefore distinguish baseline predictions from model-driven ones.
//
// Two baselines are kept side-by-side:
//
//   - Predict() (legacy v0.3): returns default-fallback labels.
//     entity-type → "document"; candidate-type → "CONCEPT".
//   - PredictV2() (v0.4 abstaining): explicitly abstains on typing
//     tasks per user-selected convention. entity-type →
//     "insufficient-evidence"; candidate-type →
//     "insufficient-evidence" (or "insufficient-evidence" if the
//     shortlist is empty).
//
// PredictV2 is the baseline recorded as the v0.4 protocol baseline.
// Predict is kept as a named constant for any consumer that still
// needs to compare against the legacy v0.3 default-fallback behaviour.
// A measured-accuracy claim against v0.4 vocabulary MUST use
// PredictV2; using Predict on a v0.4 fixture set inflates the
// score because Predict returns the first vocabulary label rather
// than the explicit abstention.
package baseline

import (
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// Shortlist is the minimal interface the baseline inspects; it accepts
// either the candidate.Shortlist type or a stub used in tests.
type Shortlist interface {
	Candidates() []protocol.Candidate
}

// BaselineVersion tags which baseline a measurement used.
type BaselineVersion string

const (
	// BaselineLegacy is the v0.3 default-fallback baseline.
	BaselineLegacy BaselineVersion = "v0.3-legacy"
	// BaselineV2 is the v0.4 abstaining baseline.
	BaselineV2 BaselineVersion = "v0.4-abstaining"
)

// Predict returns the LEGACY v0.3 default-fallback baseline label. The
// baseline never invents confidence.
//
//   - entity-type:    "document" (the default fallback vocabulary entry)
//   - candidate-type: "CONCEPT" (deterministic abstaining fallback; the
//     most generic candidate role tag, deliberately chosen so the
//     baseline is provably wrong on PERSON/ORGANIZATION/TOOL/EVENT/
//     LOCATION/DOCUMENT cases — those are the labels any real model
//     should outperform the baseline on).
//   - relationship:   "insufficient-evidence" — the protocol requires
//     "absent evidence is insufficient" rather than guessing a predicate.
//   - claim-support:  "insufficient-evidence"
func Predict(task protocol.Task, list Shortlist) (label string, abstained bool) {
	switch task {
	case protocol.TaskEntityType:
		return "document", false
	case protocol.TaskCandidateType:
		return "CONCEPT", false
	case protocol.TaskRelationship:
		return "insufficient-evidence", true
	case protocol.TaskClaimSupport:
		return "insufficient-evidence", true
	}
	return "insufficient-evidence", true
}

// PredictV2 returns the v0.4 explicit-abstaining baseline label per
// the user-selected convention (document typing by primary subject,
// abstain on missing evidence or mixed subjects lacking a clear
// primary). The baseline never invents confidence.
//
//   - entity-type:    "insufficient-evidence" — the deterministic
//     abstention; no semantic analysis of the body, so the baseline
//     cannot determine a primary subject. (The legacy baseline's
//     default "document" fallback is no longer admissible as
//     evidence-grounded gold under v0.4.)
//   - candidate-type: "insufficient-evidence" — same rationale; the
//     baseline cannot determine role from a placeholder candidate.
//   - relationship:   "insufficient-evidence" — unchanged.
//   - claim-support:  "insufficient-evidence" — unchanged.
func PredictV2(task protocol.Task, list Shortlist) (label string, abstained bool) {
	switch task {
	case protocol.TaskEntityType:
		return "insufficient-evidence", true
	case protocol.TaskCandidateType:
		_ = list // baseline cannot semantically type a candidate; abstain
		return "insufficient-evidence", true
	case protocol.TaskRelationship:
		return "insufficient-evidence", true
	case protocol.TaskClaimSupport:
		return "insufficient-evidence", true
	}
	return "insufficient-evidence", true
}

// Wrap converts a slice of candidates into the Shortlist interface. The
// candidate package owns its own Shortlist type; this helper keeps baseline
// independent of that struct's other fields.
func Wrap(cands []protocol.Candidate) Shortlist {
	return wrapped{cands: cands}
}

type wrapped struct {
	cands []protocol.Candidate
}

func (w wrapped) Candidates() []protocol.Candidate { return w.cands }
