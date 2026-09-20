// Package baseline implements the deterministic abstaining baseline the
// protocol requires for each task. The baseline emits a fixed label and
// refuses to invent confidence / latency / cost values; downstream scorers
// must therefore distinguish baseline predictions from model-driven ones.
package baseline

import (
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// Shortlist is the minimal interface the baseline inspects; it accepts
// either the candidate.Shortlist type or a stub used in tests.
type Shortlist interface {
	Candidates() []protocol.Candidate
}

// Predict returns the deterministic abstaining baseline label for a fixture.
// The baseline never invents confidence.
//
//   - entity-type:   "document" (the default fallback vocabulary entry)
//   - relationship:  "insufficient-evidence" — the protocol requires
//     "absent evidence is insufficient" rather than guessing a predicate.
//     The label is reported as an abstention.
//   - claim-support: "insufficient-evidence"
//
// The relationship case special-cases an empty shortlist: when no candidates
// were produced the baseline abstains explicitly; otherwise it abstains via
// the same label because deterministic code cannot infer a predicate.
func Predict(task protocol.Task, list Shortlist) (label string, abstained bool) {
	switch task {
	case protocol.TaskEntityType:
		return "document", false
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
